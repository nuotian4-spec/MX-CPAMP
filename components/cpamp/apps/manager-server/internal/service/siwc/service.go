package siwc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type ConnectionBinder interface {
	BindConnection(context.Context, store.Setup) (string, error)
	CheckConnection(context.Context, string, store.Setup) error
}

type Service struct {
	resolver    concurrency.SetupResolver
	connections ConnectionBinder
	client      *http.Client
	now         func() time.Time
	mu          sync.Mutex
	sessions    map[string]*session
	active      map[model.SIWCIdentity]*session
}

type session struct {
	mu        sync.Mutex
	identity  model.SIWCIdentity
	setup     store.Setup
	expires   time.Time
	submitted bool
	terminal  *model.SIWCSessionResult
}

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }
func failure(status int, code, message string) error {
	return &Error{Status: status, Code: code, Message: message}
}

func HTTPError(err error) (int, string, string) {
	var own *Error
	if errors.As(err, &own) {
		return own.Status, own.Code, own.Message
	}
	var connection *concurrency.Error
	if errors.As(err, &connection) {
		return connection.Status, connection.Code, connection.Message
	}
	return 502, "siwc_upstream_failed", "SIWC上游操作未确认"
}

func New(resolver concurrency.SetupResolver, connections ConnectionBinder, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Service{resolver: resolver, connections: connections, client: &copyClient, now: time.Now,
		sessions: make(map[string]*session), active: make(map[model.SIWCIdentity]*session)}
}

func (s *Service) setup(ctx context.Context) (store.Setup, error) {
	if s == nil || s.resolver == nil || s.connections == nil {
		return store.Setup{}, failure(503, "cpa_unavailable", "CPA连接暂不可用")
	}
	setup, ok, err := s.resolver.ResolveSetup(ctx)
	if err != nil || !ok || strings.TrimSpace(setup.CPAUpstreamURL) == "" || strings.TrimSpace(setup.ManagementKey) == "" {
		return store.Setup{}, failure(503, "cpa_unavailable", "CPA连接暂不可用")
	}
	return setup, nil
}

func validateIdentity(identity model.SIWCIdentity, requireReference bool) error {
	if strings.TrimSpace(identity.AuthID) == "" || strings.TrimSpace(identity.AuthIndex) == "" {
		return failure(400, "credential_identity_required", "请选择具有完整标识的凭证")
	}
	if requireReference && identity.ConnectionRef == "" {
		return failure(409, "cpa_target_changed", "请刷新凭证详情后再操作")
	}
	return nil
}

func (s *Service) Credential(ctx context.Context, identity model.SIWCIdentity) (model.SIWCCredential, error) {
	if err := validateIdentity(identity, false); err != nil {
		return model.SIWCCredential{}, err
	}
	setup, err := s.setup(ctx)
	if err != nil {
		return model.SIWCCredential{}, err
	}
	result, err := s.credential(ctx, setup, identity)
	if err != nil {
		return model.SIWCCredential{}, err
	}
	result.ConnectionRef, err = s.connections.BindConnection(ctx, setup)
	return result, err
}

func (s *Service) credential(ctx context.Context, setup store.Setup, identity model.SIWCIdentity) (model.SIWCCredential, error) {
	var ownership struct {
		Mode string `json:"mode"`
	}
	if err := s.request(ctx, setup, identity.ConnectionRef, http.MethodGet, "/credentials/concurrency", nil, &ownership); err != nil {
		return model.SIWCCredential{}, err
	}
	if ownership.Mode != "local" && ownership.Mode != "home" {
		return model.SIWCCredential{}, failure(502, "siwc_response_invalid", "凭证控制方回执不完整")
	}
	var payload struct {
		Files []struct {
			ID                string `json:"id"`
			AuthIndex         string `json:"auth_index"`
			Provider          string `json:"provider"`
			Source            string `json:"source"`
			RuntimeOnly       bool   `json:"runtime_only"`
			SIWCBindSupported *bool  `json:"siwc_bind_supported"`
			UseSIWC           *bool  `json:"use_siwc"`
			SIWCStatus        string `json:"siwc_status"`
			SIWCBound         *bool  `json:"siwc_bound"`
			SIWCExpiresAt     string `json:"siwc_expires_at"`
		} `json:"files"`
	}
	if err := s.request(ctx, setup, identity.ConnectionRef, http.MethodGet, "/credentials", nil, &payload); err != nil {
		return model.SIWCCredential{}, err
	}
	var result model.SIWCCredential
	found := false
	for _, entry := range payload.Files {
		if entry.ID != identity.AuthID {
			continue
		}
		if found || entry.AuthIndex != identity.AuthIndex {
			return result, failure(409, "credential_identity_changed", "凭证标识已变更，请刷新详情")
		}
		found = true
		if entry.Provider != "codex" {
			return result, failure(400, "credential_not_codex", "SIWC仅适用于Codex凭证")
		}
		if entry.SIWCBindSupported == nil || entry.UseSIWC == nil || entry.SIWCBound == nil ||
			(entry.SIWCStatus != "off" && entry.SIWCStatus != "valid" && entry.SIWCStatus != "invalid") {
			return result, failure(501, "siwc_unsupported", "当前CPA内核未提供SIWC管理能力")
		}
		result = model.SIWCCredential{SIWCIdentity: identity, Provider: "codex",
			SIWCBindSupported: *entry.SIWCBindSupported && entry.Source == "file" && !entry.RuntimeOnly,
			UseSIWC:           *entry.UseSIWC, SIWCStatus: entry.SIWCStatus, SIWCBound: *entry.SIWCBound}
		if entry.SIWCExpiresAt != "" {
			if parsed, err := time.Parse(time.RFC3339, entry.SIWCExpiresAt); err == nil {
				result.SIWCExpiresAt = parsed.UTC().Format(time.RFC3339)
			}
		}
		if !result.SIWCBindSupported {
			result.ReadOnlyReason = "credential_ineligible"
		}
		if ownership.Mode == "home" {
			result.SIWCBindSupported = false
			result.ReadOnlyReason = "home_managed"
		}
	}
	if !found {
		return result, failure(409, "credential_identity_changed", "凭证已移除，请刷新详情")
	}
	return result, nil
}

func (s *Service) Bind(ctx context.Context, identity model.SIWCIdentity) (model.SIWCBindResult, error) {
	var result model.SIWCBindResult
	if err := validateIdentity(identity, true); err != nil {
		return result, err
	}
	setup, err := s.setup(ctx)
	if err != nil {
		return result, err
	}
	credential, err := s.credential(ctx, setup, identity)
	if err != nil {
		return result, err
	}
	if !credential.SIWCBindSupported {
		return result, failure(409, credential.ReadOnlyReason, "该来源的凭证仅支持查看SIWC状态")
	}
	// 在生成上游会话前预留目标，合并同一凭证的并发点击。
	current := &session{identity: identity, setup: setup, expires: s.now().Add(5 * time.Minute)}
	s.mu.Lock()
	for state, old := range s.sessions {
		if !s.now().Before(old.expires) {
			delete(s.sessions, state)
		}
	}
	for key, old := range s.active {
		if !s.now().Before(old.expires) {
			delete(s.active, key)
		}
	}
	if old := s.active[identity]; old != nil && s.now().Before(old.expires) {
		s.mu.Unlock()
		return result, failure(409, "siwc_session_active", "该凭证已有待完成的SIWC绑定")
	}
	s.active[identity] = current
	s.mu.Unlock()
	succeeded := false
	defer func() {
		if !succeeded {
			s.release(current)
		}
	}()
	var upstream struct {
		Status    string `json:"status"`
		Flow      string `json:"flow"`
		URL       string `json:"url"`
		State     string `json:"state"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := s.request(ctx, setup, identity.ConnectionRef, http.MethodPost, "/oauth/siwc/bind", map[string]string{"name": identity.AuthID}, &upstream); err != nil {
		return result, err
	}
	link, parseErr := url.Parse(upstream.URL)
	if upstream.Status != "ok" || upstream.Flow != "siwc_bind" || strings.TrimSpace(upstream.State) == "" ||
		upstream.ExpiresIn != 300 || parseErr != nil || link.Host == "" || (link.Scheme != "https" && link.Scheme != "http") {
		return result, failure(502, "siwc_response_invalid", "SIWC链接回执不完整")
	}
	s.mu.Lock()
	if _, exists := s.sessions[upstream.State]; exists {
		s.mu.Unlock()
		return result, failure(502, "siwc_response_invalid", "SIWC会话标识重复")
	}
	s.sessions[upstream.State] = current
	s.mu.Unlock()
	succeeded = true
	return model.SIWCBindResult{SIWCIdentity: identity, Status: "wait", Flow: "siwc_bind", URL: upstream.URL,
		State: upstream.State, ExpiresIn: 300}, nil
}

func (s *Service) release(current *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[current.identity] == current {
		delete(s.active, current.identity)
	}
}

func (s *Service) forgetExpired(current *session, state string, err error) {
	var expired *Error
	if !errors.As(err, &expired) || expired.Code != "siwc_session_expired" {
		return
	}
	s.mu.Lock()
	if s.sessions[state] == current {
		delete(s.sessions, state)
	}
	if s.active[current.identity] == current {
		delete(s.active, current.identity)
	}
	s.mu.Unlock()
}

// lockSession返回锁定的会话；调用方在操作和回执处理完成后解锁。
func (s *Service) lockSession(ctx context.Context, input model.SIWCSessionInput) (*session, error) {
	if err := validateIdentity(input.SIWCIdentity, true); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.State) == "" {
		return nil, failure(400, "siwc_state_required", "缺少SIWC会话标识")
	}
	s.mu.Lock()
	current := s.sessions[input.State]
	s.mu.Unlock()
	if current == nil {
		return nil, failure(410, "siwc_session_expired", "SIWC会话不存在或已过期，请重新生成链接")
	}
	current.mu.Lock()
	if current.identity != input.SIWCIdentity {
		current.mu.Unlock()
		return nil, failure(409, "siwc_session_mismatch", "SIWC会话与当前凭证不匹配")
	}
	if err := s.connections.CheckConnection(ctx, input.ConnectionRef, current.setup); err != nil {
		current.mu.Unlock()
		return nil, err
	}
	if !s.now().Before(current.expires) {
		current.mu.Unlock()
		return nil, failure(410, "siwc_session_expired", "SIWC会话已过期，请重新生成链接")
	}
	return current, nil
}

func (s *Service) Callback(ctx context.Context, input model.SIWCCallbackInput) error {
	redirect, err := url.Parse(input.RedirectURL)
	if err != nil || redirect.Scheme != "http" || redirect.Host != "127.0.0.1:1455" || redirect.Path != "/auth/callback" || redirect.User != nil || redirect.Fragment != "" {
		return failure(400, "siwc_callback_invalid", "请提交完整的SIWC回调地址")
	}
	query, err := url.ParseQuery(redirect.RawQuery)
	if err != nil || len(query["state"]) != 1 || query.Get("state") != input.State || input.State == "" {
		return failure(400, "siwc_callback_state_mismatch", "回调地址与SIWC会话不匹配")
	}
	for _, key := range []string{"code", "client_id", "error"} {
		if len(query[key]) > 1 {
			return failure(400, "siwc_callback_invalid", "SIWC回调包含重复参数")
		}
	}
	if strings.TrimSpace(query.Get("error")) == "" && (strings.TrimSpace(query.Get("code")) == "" || strings.TrimSpace(query.Get("client_id")) == "") {
		return failure(400, "siwc_callback_invalid", "SIWC回调缺少授权码或签发客户端标识")
	}
	current, err := s.lockSession(ctx, input.SIWCSessionInput)
	if err != nil {
		return err
	}
	defer current.mu.Unlock()
	if current.terminal != nil || current.submitted {
		return failure(409, "siwc_callback_already_submitted", "SIWC回调已提交或会话已结束")
	}
	// 即使响应中断，也只允许查询同一会话，不重复提交授权码。
	current.submitted = true
	err = s.request(ctx, current.setup, input.ConnectionRef, http.MethodPost, "/oauth/callback",
		map[string]string{"provider": "codex", "redirect_url": input.RedirectURL}, nil)
	var rejected *Error
	if errors.As(err, &rejected) && rejected.Code == "siwc_callback_invalid" {
		current.submitted = false
	}
	s.forgetExpired(current, input.State, err)
	return err
}

func (s *Service) Status(ctx context.Context, input model.SIWCSessionInput) (model.SIWCSessionResult, error) {
	current, err := s.lockSession(ctx, input)
	if err != nil {
		return model.SIWCSessionResult{}, err
	}
	defer current.mu.Unlock()
	return s.status(ctx, current, input.State)
}

func (s *Service) status(ctx context.Context, current *session, state string) (model.SIWCSessionResult, error) {
	if current.terminal != nil {
		return *current.terminal, nil
	}
	var upstream struct {
		Status string `json:"status"`
	}
	if err := s.request(ctx, current.setup, current.identity.ConnectionRef, http.MethodGet,
		"/oauth/status?state="+url.QueryEscape(state), nil, &upstream); err != nil {
		s.forgetExpired(current, state, err)
		return model.SIWCSessionResult{}, err
	}
	result := model.SIWCSessionResult{Status: upstream.Status}
	switch upstream.Status {
	case "wait":
		return result, nil
	case "error":
		result.Code = "siwc_session_failed"
	case "ok":
		credential, err := s.credential(ctx, current.setup, current.identity)
		if err != nil {
			return model.SIWCSessionResult{}, err
		}
		result.Credential = &credential
	default:
		return model.SIWCSessionResult{}, failure(502, "siwc_response_invalid", "SIWC状态回执不完整")
	}
	current.terminal = &result
	s.release(current)
	return result, nil
}

func (s *Service) Cancel(ctx context.Context, input model.SIWCSessionInput) (model.SIWCCancelResult, error) {
	current, err := s.lockSession(ctx, input)
	if err != nil {
		return model.SIWCCancelResult{}, err
	}
	defer current.mu.Unlock()
	if current.terminal != nil {
		return model.SIWCCancelResult{SIWCSessionResult: *current.terminal, Cancelled: current.terminal.Status == "cancelled"}, nil
	}
	var upstream struct {
		Status    string `json:"status"`
		Cancelled *bool  `json:"cancelled"`
	}
	if err := s.request(ctx, current.setup, input.ConnectionRef, http.MethodDelete,
		"/oauth/session?state="+url.QueryEscape(input.State), nil, &upstream); err != nil {
		s.forgetExpired(current, input.State, err)
		return model.SIWCCancelResult{}, err
	}
	if upstream.Status != "ok" || upstream.Cancelled == nil {
		return model.SIWCCancelResult{}, failure(502, "siwc_response_invalid", "SIWC取消回执不完整")
	}
	if *upstream.Cancelled {
		result := model.SIWCSessionResult{Status: "cancelled"}
		current.terminal = &result
		s.release(current)
		return model.SIWCCancelResult{SIWCSessionResult: result, Cancelled: true}, nil
	}
	// false覆盖提交中、失败、过期和完成，只能查询同state后确定结果。
	result, err := s.status(ctx, current, input.State)
	return model.SIWCCancelResult{SIWCSessionResult: result, Cancelled: false}, err
}

func (s *Service) request(ctx context.Context, setup store.Setup, reference, method, path string, input, output any) error {
	if reference != "" {
		if err := s.connections.CheckConnection(ctx, reference, setup); err != nil {
			return err
		}
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return failure(400, "siwc_request_invalid", "SIWC请求无效")
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(setup.CPAUpstreamURL, "/")+"/v8/management"+path, body)
	if err != nil {
		return failure(503, "cpa_unavailable", "CPA连接暂不可用")
	}
	req.Header.Set("Authorization", "Bearer "+setup.ManagementKey)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return failure(502, "siwc_upstream_failed", "SIWC上游操作未确认，请查询当前会话")
	}
	defer resp.Body.Close()
	if reference != "" {
		if err := s.connections.CheckConnection(ctx, reference, setup); err != nil {
			return err
		}
	}
	switch {
	case resp.StatusCode == http.StatusNotFound && (path == "/oauth/callback" || strings.HasPrefix(path, "/oauth/status?") || strings.HasPrefix(path, "/oauth/session?")):
		return failure(410, "siwc_session_expired", "CPA中的SIWC会话不存在或已过期，请重新生成链接")
	case resp.StatusCode == http.StatusNotFound:
		return failure(501, "siwc_unsupported", "当前CPA内核未提供SIWC管理能力")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return failure(502, "siwc_management_auth_failed", "SIWC管理连接认证失败")
	case resp.StatusCode == http.StatusConflict:
		return failure(409, "siwc_conflict", "SIWC会话或凭证状态已变更，请刷新")
	case resp.StatusCode == http.StatusBadRequest && path == "/oauth/callback":
		return failure(400, "siwc_callback_invalid", "CPA未接收该回调，请检查完整回调地址")
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return failure(400, "siwc_request_rejected", "CPA拒绝了当前SIWC操作")
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return failure(502, "siwc_upstream_failed", "SIWC上游操作未确认")
	}
	if output == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 || json.Unmarshal(data, output) != nil {
		return failure(502, "siwc_response_invalid", "SIWC回执格式无效")
	}
	return nil
}
