package oauthlogin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type SetupResolver interface {
	ResolveSetup(context.Context) (store.Setup, bool, error)
}
type Input struct {
	TaskID     string `json:"task_id,omitempty"`
	Profile    string `json:"profile"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
}
type Task struct {
	ID               string    `json:"id"`
	Profile          string    `json:"profile"`
	Status           string    `json:"status"`
	ErrorCode        string    `json:"error_code,omitempty"`
	Stage            string    `json:"stage,omitempty"`
	Reason           string    `json:"reason,omitempty"`
	AuthID           string    `json:"auth_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	workerID         string
	cancel           context.CancelFunc
	setup            store.Setup
	done             chan struct{}
	cleanupConfirmed bool
	batchID          string
	authIndex        string
}
type credentials struct {
	LoginMethod  string `json:"login_method"`
	Profile      string `json:"profile"`
	ClientID     string `json:"client_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token,omitempty"`
	AccountID    string `json:"account_id"`
	Email        string `json:"email"`
	Expired      string `json:"expired"`
}
type workerTask struct {
	ID        string       `json:"id"`
	Status    string       `json:"status"`
	ErrorCode string       `json:"error_code"`
	Stage     string       `json:"stage"`
	Reason    string       `json:"reason"`
	Result    *credentials `json:"result"`
}
type upstreamError struct {
	status int
	code   string
}

func (e *upstreamError) Error() string { return "上游拒绝登录请求" }
func callbackDefinitelyRejected(err error) bool {
	return CallbackRejectionCode(err) != ""
}

// CallbackRejectionCode 仅返回 CPA 明确拒绝回调的白名单诊断码。
func CallbackRejectionCode(err error) string {
	var upstream *upstreamError
	if !errors.As(err, &upstream) || upstream.status != http.StatusBadRequest {
		return ""
	}
	switch upstream.code {
	case "invalid_callback_url", "invalid_callback_query", "conflicting_callback_parameters", "callback_state_mismatch", "callback_code_required", "oauth_provider_mismatch", "invalid_body", "invalid_state", "unsupported_provider":
		return upstream.code
	}
	return ""
}

type Service struct {
	mu                sync.Mutex
	tasks             map[string]*Task
	sessions          map[string]*oauthSession
	resolver          SetupResolver
	mutations         *cpaauthfiles.MutationCoordinator
	workerURL, secret string
	client            *http.Client
	timeout, poll     time.Duration
	workerResetMu     sync.Mutex
	workerReady       bool
	batches           map[string]*batchState
	activeBatch       string
	batchTTL          time.Duration
	batchRetention    time.Duration
}

type oauthSession struct {
	provider   string
	setup      store.Setup
	created    time.Time
	release    func()
	committing bool
}

var ErrTargetChanged = errors.New("CPA连接已变更，请重新发起授权")
var ErrBusy = errors.New("已有登录任务正在进行")
var ErrUnavailable = errors.New("登录服务未配置")
var ErrNotFound = errors.New("登录任务不存在或已过期")
var ErrImporting = errors.New("账号导入已开始，请等待真实结果")
var ErrInput = errors.New("登录输入无效")

func New(resolver SetupResolver, mutations *cpaauthfiles.MutationCoordinator, workerURL, secret string) *Service {
	if mutations == nil {
		mutations = cpaauthfiles.NewMutationCoordinator()
	}
	return &Service{batches: map[string]*batchState{}, batchTTL: 30 * time.Minute, batchRetention: time.Hour, tasks: map[string]*Task{}, sessions: map[string]*oauthSession{}, resolver: resolver, mutations: mutations,
		workerURL: strings.TrimRight(workerURL, "/"), secret: strings.TrimSpace(secret),
		client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, timeout: 25 * time.Minute, poll: time.Second}
}

// PasswordLoginConfigured 只报告配置，不连接登录器或启动登录。
func (s *Service) PasswordLoginConfigured() bool {
	return strings.TrimSpace(s.workerURL) != "" && s.secret != ""
}

func terminal(status string) bool {
	return status != "created" && status != "running" && status != "importing" && status != "cancelling"
}
func snapshot(t *Task) Task { out := *t; out.cancel = nil; out.workerID = ""; return out }
func (s *Service) Create(input Input) (Task, error) {
	return s.createBound(input, nil, "")
}
func (s *Service) createBound(input Input, expected *store.Setup, batchID string) (Task, error) {
	if s.workerURL == "" || s.secret == "" {
		return Task{}, ErrUnavailable
	}
	// 首次登录前清除上一次 Manager 进程遗留的内部任务；不从磁盘恢复凭据。
	if s.resetWorker() != nil {
		return Task{}, ErrUnavailable
	}
	if (input.Profile != "codex" && input.Profile != "bps") || strings.TrimSpace(input.Email) == "" || input.Password == "" || input.TOTPSecret == "" || len(input.Password) > 4096 || len(input.TOTPSecret) > 1024 {
		return Task{}, ErrInput
	}
	setup, ok, err := s.resolver.ResolveSetup(context.Background())
	if err != nil || !ok || setup.CPAUpstreamURL == "" || setup.ManagementKey == "" {
		return Task{}, ErrUnavailable
	}
	if expected != nil && !sameSetup(setup, *expected) {
		return Task{}, ErrTargetChanged
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBatch != "" && s.activeBatch != batchID {
		return Task{}, ErrBusy
	}
	if batchID != "" {
		b := s.batches[batchID]
		if b == nil || b.Status == "cancelling" || b.Status == "interrupted" {
			return Task{}, ErrBusy
		}
	}
	if input.TaskID != "" {
		if !validTaskID(input.TaskID) {
			return Task{}, ErrInput
		}
		if existing := s.tasks[input.TaskID]; existing != nil {
			if existing.Profile != "" && existing.Profile != input.Profile {
				return Task{}, ErrInput
			}
			return snapshot(existing), nil
		}
	}
	for id, t := range s.tasks {
		if terminal(t.Status) && time.Since(t.CreatedAt) > 30*time.Minute {
			delete(s.tasks, id)
			continue
		}
		if !terminal(t.Status) || !taskDone(t) {
			return Task{}, ErrBusy
		}
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return Task{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	key[6] = (key[6] & 0x0f) | 0x40
	key[8] = (key[8] & 0x3f) | 0x80
	rawID := hex.EncodeToString(key)
	taskID := rawID[:8] + "-" + rawID[8:12] + "-" + rawID[12:16] + "-" + rawID[16:20] + "-" + rawID[20:]
	if input.TaskID != "" {
		taskID = input.TaskID
	}
	t := &Task{ID: taskID, Profile: input.Profile, Status: "created", CreatedAt: time.Now().UTC(), cancel: cancel, setup: setup, done: make(chan struct{}), batchID: batchID}
	s.tasks[t.ID] = t
	go s.run(ctx, t.ID, input)
	return snapshot(t), nil
}
func (s *Service) Get(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[id]
	if t == nil || time.Since(t.CreatedAt) > 30*time.Minute {
		return Task{}, ErrNotFound
	}
	return snapshot(t), nil
}
func (s *Service) Cancel(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[id]
	if t == nil {
		if !validTaskID(id) {
			return Task{}, ErrNotFound
		}
		t = &Task{ID: id, Status: "cancelled", CreatedAt: time.Now().UTC()}
		s.tasks[id] = t
		return snapshot(t), nil
	}
	if t.Status == "importing" {
		return snapshot(t), ErrImporting
	}
	if !terminal(t.Status) {
		t.Status = "cancelling"
		t.cancel()
	}
	return snapshot(t), nil
}
func (s *Service) finish(id, status, code, authID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tasks[id]; t != nil {
		t.Status = status
		t.ErrorCode = code
		t.AuthID = authID
		t.workerID = ""
		t.cancel()
	}
}
func (s *Service) run(ctx context.Context, id string, input Input) {
	input.TaskID = id
	defer func() {
		confirmed := s.cleanupWorker(id)
		s.mu.Lock()
		if t := s.tasks[id]; t != nil {
			t.cleanupConfirmed = confirmed
			close(t.done)
		}
		s.mu.Unlock()
	}()
	var created workerTask
	err := s.worker(ctx, http.MethodPost, "/tasks", input, &created)
	input = Input{}
	if err != nil || created.ID != id {
		if ctx.Err() != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			stopErr := s.worker(stopCtx, http.MethodDelete, "/tasks/"+url.PathEscape(id), nil, nil)
			cancel()
			if stopErr != nil {
				s.finish(id, "failed", "worker_stop_unconfirmed", "")
				return
			}
			status := "cancelled"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status = "expired"
			}
			s.finish(id, status, "", "")
			return
		}
		s.finish(id, "failed", "worker_unavailable", "")
		return
	}
	s.mu.Lock()
	t := s.tasks[id]
	t.workerID = created.ID
	if t.Status != "cancelling" {
		t.Status = "running"
	}
	s.mu.Unlock()

	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			var stopped workerTask
			stopErr := s.worker(stopCtx, http.MethodDelete, "/tasks/"+url.PathEscape(created.ID), nil, &stopped)
			cancel()
			if stopErr != nil {
				s.finish(id, "failed", "worker_stop_unconfirmed", "")
				return
			}
			status := "cancelled"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status = "expired"
			}
			s.finish(id, status, "", "")
			return
		}
		var current workerTask
		if err = s.worker(ctx, http.MethodGet, "/tasks/"+url.PathEscape(created.ID), nil, &current); err != nil {
			if ctx.Err() != nil {
				continue
			}
			s.finish(id, "failed", "worker_unavailable", "")
			return
		}
		s.mu.Lock()
		s.tasks[id].Stage = safeStage(current.Stage)
		s.tasks[id].Reason = safeReason(current.Reason)
		s.mu.Unlock()
		switch current.Status {
		case "succeeded":
			s.mu.Lock()
			if ctx.Err() != nil || s.tasks[id].Status == "cancelling" {
				s.mu.Unlock()
				continue
			}
			s.tasks[id].Status = "importing"
			s.tasks[id].Stage = "cpa_import"
			s.tasks[id].Reason = ""
			s.mu.Unlock()
			s.importCredentials(id, current.Result)
			current.Result = nil
			return
		case "needs_user_action":
			s.finish(id, "needs_user_action", "needs_user_action", "")
			return
		case "failed", "cancelled", "expired":
			s.finish(id, current.Status, safeCode(current.ErrorCode), "")
			return
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
func safeCode(code string) string {
	switch code {
	case "invalid_credentials", "password_rejected", "totp_rejected", "needs_user_action", "timeout", "cancelled", "protocol_error", "callback_invalid", "token_exchange_failed", "credentials_incomplete", "identity_mismatch", "invalid_profile", "transport_error", "process_failed", "output_invalid":
		return code
	}
	return "login_failed"
}
func (s *Service) importCredentials(id string, result *credentials) {
	s.mu.Lock()
	provider := s.tasks[id].Profile
	expected := s.tasks[id].setup
	isBatch := s.tasks[id].batchID != ""
	s.mu.Unlock()
	profile, client := "codex", "app_EMoamEEZ73f0CkXaXp7hrann"
	if provider == "bps" {
		profile = "excel"
		client = "app_fnr0pYvVwwFDocDumLG3H2Bp"
	}
	if result == nil || result.Profile != profile || result.ClientID != client || result.AccessToken == "" || result.RefreshToken == "" || result.AccountID == "" {
		s.finish(id, "failed", "credential_contract", "")
		return
	}
	if _, err := time.Parse(time.RFC3339, result.Expired); err != nil {
		s.finish(id, "failed", "credential_contract", "")
		return
	}
	result.LoginMethod = "2fa"
	// 导入一旦发起即独立收敛，取消请求不得把已经落盘的账号报告成撤回。
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	release, err := s.mutations.AcquireAll(ctx)
	if err != nil {
		s.finish(id, "failed", "import_busy", "")
		return
	}
	defer release()
	var response struct {
		Status     string `json:"status"`
		Provider   string `json:"provider"`
		AuthID     string `json:"auth_id"`
		Saved      bool   `json:"saved"`
		Registered bool   `json:"registered"`
	}
	if err = s.cpaBound(ctx, expected, http.MethodPost, "import", url.Values{"provider": {provider}}, result, &response); err != nil {
		if errors.Is(err, ErrTargetChanged) {
			s.finish(id, "failed", "cpa_target_changed", "")
			return
		}
		s.finish(id, "failed", "import_unconfirmed", "")
		return
	}
	if response.Status != "ok" || response.Provider != provider || !response.Saved || !response.Registered || response.AuthID == "" {
		s.finish(id, "failed", "import_unconfirmed", "")
		return
	}
	if isBatch {
		index, err := s.importedCredentialIndex(ctx, expected, response.AuthID, provider)
		if err != nil {
			s.finish(id, "failed", "credential_mapping_unconfirmed", response.AuthID)
			return
		}
		s.mu.Lock()
		s.tasks[id].authIndex = index
		s.mu.Unlock()
	}
	s.finish(id, "succeeded", "", response.AuthID)
}
func (s *Service) worker(ctx context.Context, method, path string, input, output any) error {
	return s.request(ctx, method, s.workerURL+path, s.secret, input, output)
}
func sameSetup(a, b store.Setup) bool {
	return strings.TrimRight(a.CPAUpstreamURL, "/") == strings.TrimRight(b.CPAUpstreamURL, "/") && a.ManagementKey == b.ManagementKey
}
func (s *Service) cpaBound(ctx context.Context, expected store.Setup, method, action string, query url.Values, input, output any) error {
	setup, ok, err := s.resolver.ResolveSetup(ctx)
	if err != nil || !ok || setup.CPAUpstreamURL == "" || setup.ManagementKey == "" {
		return ErrUnavailable
	}
	if !sameSetup(setup, expected) {
		return ErrTargetChanged
	}
	target := strings.TrimRight(expected.CPAUpstreamURL, "/") + "/v8/management/oauth/" + action
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return s.request(ctx, method, target, expected.ManagementKey, input, output)
}
func (s *Service) request(ctx context.Context, method, target, key string, input, output any) error {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return ErrInput
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if method == http.MethodDelete && res.StatusCode == http.StatusNotFound {
		if cleared, ok := output.(*workerCleanupResponse); ok {
			cleared.ResultCleared = true
		}
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var payload struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 4097))
		decoder := json.NewDecoder(bytes.NewReader(body))
		code := ""
		if readErr == nil && len(body) <= 4096 && decoder.Decode(&payload) == nil && decoder.Decode(&struct{}{}) == io.EOF {
			code = payload.Code
			if code == "" {
				code = payload.Error
			}
		}
		switch code {
		case "invalid_callback_url", "invalid_callback_query", "conflicting_callback_parameters", "callback_state_mismatch", "callback_code_required", "oauth_provider_mismatch", "invalid_body", "invalid_state", "unsupported_provider":
		default:
			code = "upstream_rejected"
		}
		return &upstreamError{status: res.StatusCode, code: code}
	}
	if output == nil {
		return nil
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(output) != nil {
		return ErrUnavailable
	}
	return nil
}

// OAuth 只转发精确方法和路径；浏览器没有凭据导入或读取 token 的入口。
func (s *Service) OAuth(ctx context.Context, method, action string, input map[string]string) (map[string]any, error) {
	allowed := map[string]string{"auth-url": http.MethodPost, "callback": http.MethodPost, "status": http.MethodGet, "session": http.MethodDelete}
	if allowed[action] != method {
		return nil, ErrInput
	}
	state := input["state"]
	provider := input["provider"]
	expected, ok, err := s.resolver.ResolveSetup(ctx)
	if err != nil || !ok {
		return nil, ErrUnavailable
	}
	if action != "auth-url" {
		s.mu.Lock()
		saved, ok := s.sessions[state]
		s.mu.Unlock()
		if !ok || time.Since(saved.created) > 25*time.Minute {
			return nil, ErrNotFound
		}
		expected = saved.setup
		if provider != "" && provider != saved.provider {
			return nil, ErrInput
		}
		provider = saved.provider
	}
	if provider != "codex" && provider != "bps" {
		return nil, ErrInput
	}
	var payload any
	query := url.Values{}
	switch action {
	case "auth-url":
		payload = map[string]any{"provider": provider, "is_webui": true}
	case "callback":
		redirect, err := url.Parse(input["redirect_url"])
		if err != nil || redirect.Query().Get("state") != state || redirect.Query().Get("code") == "" {
			return nil, ErrInput
		}
		payload = map[string]string{"provider": provider, "state": state, "redirect_url": input["redirect_url"]}
	default:
		query.Set("state", state)
	}
	var raw map[string]any
	if action == "callback" {
		s.mu.Lock()
		session := s.sessions[state]
		if session == nil || session.committing {
			s.mu.Unlock()
			return nil, ErrBusy
		}
		session.committing = true
		s.mu.Unlock()
		release, err := s.mutations.AcquireAll(ctx)
		if err != nil {
			s.mu.Lock()
			session.committing = false
			s.mu.Unlock()
			return nil, ErrBusy
		}
		s.mu.Lock()
		if s.sessions[state] != session {
			s.mu.Unlock()
			release()
			return nil, ErrNotFound
		}
		session.release = release
		s.mu.Unlock()
	}
	if err := s.cpaBound(ctx, expected, method, action, query, payload, &raw); err != nil {
		if action == "callback" && (errors.Is(err, ErrTargetChanged) || callbackDefinitelyRejected(err)) {
			s.mu.Lock()
			if session := s.sessions[state]; session != nil {
				if session.release != nil {
					session.release()
					session.release = nil
				}
				session.committing = false
			}
			s.mu.Unlock()
		}
		return nil, err
	}
	out := map[string]any{}
	if action == "auth-url" {
		u, _ := raw["url"].(string)
		st, _ := raw["state"].(string)
		if u == "" || st == "" {
			return nil, ErrUnavailable
		}
		s.mu.Lock()
		for key, session := range s.sessions {
			if time.Since(session.created) > 25*time.Minute {
				if session.release != nil {
					session.release()
				}
				delete(s.sessions, key)
			}
		}
		s.sessions[st] = &oauthSession{provider: provider, setup: expected, created: time.Now()}
		s.mu.Unlock()
		time.AfterFunc(25*time.Minute, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if session := s.sessions[st]; session != nil {
				if session.release != nil {
					session.release()
				}
				delete(s.sessions, st)
			}
		})
		out["url"] = u
		out["state"] = st
		return out, nil
	}
	status, _ := raw["status"].(string)
	if status != "ok" && status != "wait" && status != "error" {
		return nil, ErrUnavailable
	}
	out["status"] = status
	if status == "error" {
		out["error"] = "授权失败，请重新发起或处理官方页面验证"
	}
	if action == "session" || (action == "status" && status != "wait") {
		s.mu.Lock()
		if session := s.sessions[state]; session != nil && session.release != nil {
			session.release()
		}
		delete(s.sessions, state)
		s.mu.Unlock()
	}
	return out, nil
}
