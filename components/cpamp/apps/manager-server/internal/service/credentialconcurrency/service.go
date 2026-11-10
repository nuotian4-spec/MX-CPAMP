package credentialconcurrency

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type SetupResolver interface {
	ResolveSetup(context.Context) (store.Setup, bool, error)
}
type Service struct {
	resolver        SetupResolver
	mutations       *cpaauthfiles.MutationCoordinator
	client          *http.Client
	connectionMu    sync.Mutex
	connectionRef   string
	connectionSetup store.Setup
}
type Credential struct {
	AuthID         string `json:"auth_id"`
	AuthIndex      string `json:"auth_index"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Source         string `json:"source"`
	Editable       bool   `json:"editable"`
	ReadOnlyReason string `json:"read_only_reason"`
	Limit          *int   `json:"concurrency_limit"`
	EffectiveLimit int    `json:"effective_limit"`
	LimitSource    string `json:"limit_source"`
	Active         int    `json:"active"`
	PinnedWaiting  int    `json:"pinned_waiting"`
}
type Snapshot struct {
	Mode          string       `json:"mode"`
	ConnectionRef string       `json:"connection_ref,omitempty"`
	DefaultLimit  *int         `json:"default_limit,omitempty"`
	PoolWaiting   *int         `json:"pool_waiting,omitempty"`
	Credentials   []Credential `json:"credentials"`
}
type Input struct {
	AuthID        string          `json:"auth_id"`
	ConnectionRef string          `json:"connection_ref"`
	Limit         json.RawMessage `json:"concurrency_limit"`
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
func New(resolver SetupResolver, mutations *cpaauthfiles.MutationCoordinator) *Service {
	if mutations == nil {
		mutations = cpaauthfiles.NewMutationCoordinator()
	}
	return &Service{resolver: resolver, mutations: mutations, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (s *Service) setup(ctx context.Context) (store.Setup, error) {
	setup, ok, err := s.resolver.ResolveSetup(ctx)
	if err != nil || !ok || setup.CPAUpstreamURL == "" || setup.ManagementKey == "" {
		return store.Setup{}, failure(503, "cpa_unavailable", "CPA连接暂不可用")
	}
	return setup, nil
}
func (s *Service) List(ctx context.Context) (Snapshot, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	result, err := s.listBound(ctx, setup)
	if err != nil || result.Mode == "home" {
		return result, err
	}
	result.ConnectionRef, err = s.bindConnection(ctx, setup)
	return result, err
}
func sameConnection(a, b store.Setup) bool {
	return strings.TrimRight(a.CPAUpstreamURL, "/") == strings.TrimRight(b.CPAUpstreamURL, "/") && a.ManagementKey == b.ManagementKey
}
func (s *Service) bindConnection(ctx context.Context, shown store.Setup) (string, error) {
	current, err := s.setup(ctx)
	if err != nil {
		return "", err
	}
	if !sameConnection(current, shown) {
		return "", failure(409, "cpa_target_changed", "CPA连接已变更，请刷新凭证列表")
	}
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	if s.connectionRef == "" || !sameConnection(s.connectionSetup, shown) {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", failure(503, "connection_reference_unavailable", "连接引用暂不可用，请刷新凭证列表")
		}
		s.connectionRef = hex.EncodeToString(token[:])
		s.connectionSetup = shown
	}
	return s.connectionRef, nil
}
func (s *Service) checkConnectionRef(reference string, current store.Setup) error {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	if reference == "" || reference != s.connectionRef || !sameConnection(s.connectionSetup, current) {
		return failure(409, "cpa_target_changed", "CPA连接已变更或页面已过期，请刷新凭证列表")
	}
	return nil
}
func (s *Service) listBound(ctx context.Context, setup store.Setup) (Snapshot, error) {
	var result Snapshot
	_, err := s.request(ctx, setup, http.MethodGet, "/credentials/concurrency", "", "", nil, &result)
	if err != nil {
		return Snapshot{}, err
	}
	if result.Mode == "home" {
		return Snapshot{Mode: "home", Credentials: []Credential{}}, nil
	}
	if result.Mode != "local" || result.DefaultLimit == nil || result.PoolWaiting == nil {
		return Snapshot{}, failure(502, "snapshot_invalid", "并发状态回执不完整")
	}
	if result.Credentials == nil {
		result.Credentials = []Credential{}
	}
	return result, nil
}
func ParseLimit(raw json.RawMessage) (*int, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	var value int64
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value < 1 || value > 2147483647 {
		return nil, failure(400, "invalid_limit", "并发上限应为1至2147483647的整数，或留空继承默认")
	}
	limit := int(value)
	return &limit, nil
}
func (s *Service) Update(ctx context.Context, input Input) error {
	limit, err := ParseLimit(input.Limit)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.AuthID) == "" {
		return failure(400, "invalid_auth_id", "请选择凭证")
	}
	if len(input.ConnectionRef) != 32 {
		return failure(400, "connection_reference_required", "请刷新凭证列表后再保存并发设置")
	}
	if _, err := hex.DecodeString(input.ConnectionRef); err != nil {
		return failure(400, "connection_reference_invalid", "连接引用无效，请刷新凭证列表")
	}
	release, err := s.mutations.AcquireAll(ctx)
	if err != nil {
		return failure(409, "mutation_busy", "凭证正在更新，请稍后刷新")
	}
	defer release()
	setup, err := s.setup(ctx)
	if err != nil {
		return err
	}
	if err := s.checkConnectionRef(input.ConnectionRef, setup); err != nil {
		return err
	}
	snapshot, err := s.listBound(ctx, setup)
	if err != nil {
		return err
	}
	if snapshot.Mode == "home" {
		return failure(409, "home_managed", "Home模式由上游管理并发")
	}
	var selected *Credential
	for i := range snapshot.Credentials {
		if snapshot.Credentials[i].AuthID == input.AuthID {
			if selected != nil {
				return failure(409, "credential_ambiguous", "凭证映射不唯一，请刷新列表")
			}
			selected = &snapshot.Credentials[i]
		}
	}
	if selected == nil {
		return failure(404, "credential_not_found", "凭证已变更，请刷新列表")
	}
	if !selected.Editable {
		return failure(409, "credential_read_only", "该凭证来源仅支持查看并发")
	}
	if selected.Source == "file" {
		_, err = s.request(ctx, setup, http.MethodPatch, "/credentials/fields", "", input.ConnectionRef, map[string]any{"name": selected.AuthID, "concurrency_limit": limit}, nil)
		return err
	}
	if selected.Source != "config" || selected.AuthIndex == "" {
		return failure(409, "credential_read_only", "该凭证来源仅支持查看并发")
	}
	var config map[string]any
	etag, err := s.request(ctx, setup, http.MethodGet, "/config/api-keys", "", "", nil, &config)
	if err != nil {
		return err
	}
	if etag == "" {
		return failure(409, "config_revision_missing", "配置缺少版本信息，请刷新后再试")
	}
	var matched map[string]any
	for _, rawGroups := range config {
		groups, _ := rawGroups.([]any)
		for _, rawGroup := range groups {
			group, _ := rawGroup.(map[string]any)
			keys, _ := group["keys"].([]any)
			for _, rawKey := range keys {
				key, _ := rawKey.(map[string]any)
				if key["auth_index"] != selected.AuthIndex {
					continue
				}
				if matched != nil {
					return failure(409, "config_mapping_ambiguous", "配置凭证映射不唯一，请刷新列表")
				}
				matched = key
			}
		}
	}
	if matched == nil {
		return failure(409, "config_mapping_missing", "配置凭证已变更，请刷新列表")
	}
	if limit == nil {
		delete(matched, "concurrency-limit")
	} else {
		matched["concurrency-limit"] = *limit
	}
	_, err = s.request(ctx, setup, http.MethodPut, "/config/api-keys", etag, input.ConnectionRef, config, nil)
	return err
}
func (s *Service) request(ctx context.Context, expected store.Setup, method, path, etag, reference string, input, output any) (string, error) {
	if method != http.MethodGet {
		current, err := s.setup(ctx)
		if err != nil {
			return "", err
		}
		if !sameConnection(current, expected) {
			return "", failure(409, "cpa_target_changed", "CPA连接已变更，请重新选择凭证")
		}
		if err := s.checkConnectionRef(reference, current); err != nil {
			return "", err
		}
	}
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return "", failure(400, "invalid_input", "并发设置输入无效")
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(expected.CPAUpstreamURL, "/")+"/v8/management"+path, body)
	if err != nil {
		return "", failure(503, "cpa_unavailable", "CPA连接暂不可用")
	}
	req.Header.Set("Authorization", "Bearer "+expected.ManagementKey)
	req.Header.Set("Content-Type", "application/json")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return "", failure(502, "result_unconfirmed", "请求回执未确认，请刷新并发状态后再操作")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == 412 {
			return "", failure(409, "config_changed", "配置已被其他操作更新，请刷新后重试")
		}
		if res.StatusCode == 409 {
			return "", failure(409, "upstream_conflict", "上游拒绝此并发修改，请刷新状态")
		}
		return "", failure(502, "upstream_rejected", "CPA未接受并发请求")
	}
	if output != nil {
		decoder := json.NewDecoder(io.LimitReader(res.Body, 8*1024*1024))
		decoder.UseNumber()
		if err := decoder.Decode(output); err != nil {
			return "", failure(502, "response_invalid", "CPA并发回执无效")
		}
	}
	return res.Header.Get("ETag"), nil
}
func HTTPError(err error) (int, string, string) {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Status, typed.Code, typed.Message
	}
	return 502, "service_error", "并发服务暂不可用"
}
