package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executionregistry"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

type alphaAdmissionExecutor struct {
	*executor.BPSExecutor
	provider string
	calls    int
}

func (e *alphaAdmissionExecutor) Identifier() string { return e.provider }
func (e *alphaAdmissionExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	return coreexecutor.Response{}, errors.New("BPS inference must not execute for Alpha Search")
}

type alphaBackendExecutor struct {
	*executor.BPSExecutor
	t            *testing.T
	calls        int
	bodies       [][]byte
	body         string
	status       int
	cancel       context.CancelFunc
	apiKeys      []string
	publishUsage bool
}

func (e *alphaBackendExecutor) Identifier() string { return "claude" }
func (e *alphaBackendExecutor) Execute(ctx context.Context, selected *coreauth.Auth, request coreexecutor.Request, options coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	e.bodies = append(e.bodies, append([]byte(nil), request.Payload...))
	e.apiKeys = append(e.apiKeys, usage.APIKeyFromContext(ctx))
	if selected.ID != "alpha-search-backend" || selected.Provider != "claude" || ctx.Value("gin") != nil || options.Headers.Get("Authorization") != "" || options.Headers.Get("Session_id") != "" {
		e.t.Error("Alpha Search backend inherited the outer credential or session")
	}
	for _, key := range []string{coreexecutor.PinnedAuthMetadataKey, coreexecutor.ExecutionSessionMetadataKey} {
		if options.Metadata[key] != nil {
			e.t.Errorf("Alpha Search backend inherited %s", key)
		}
	}
	if e.cancel != nil {
		e.cancel()
		return coreexecutor.Response{}, ctx.Err()
	}
	if e.status == 403 {
		return coreexecutor.Response{}, bpsSearchBackendDenied{}
	}
	if e.publishUsage {
		reporter := helps.NewUsageReporter(ctx, "claude", request.Model, selected)
		reporter.Publish(ctx, usage.Detail{InputTokens: 7, OutputTokens: 11, TotalTokens: 18})
	}
	return coreexecutor.Response{Payload: []byte(e.body)}, nil
}

func alphaPluginConfig(t *testing.T, value string) config.PluginInstanceConfig {
	t.Helper()
	var plugin config.PluginInstanceConfig
	if err := yaml.Unmarshal([]byte(value), &plugin); err != nil {
		t.Fatal(err)
	}
	return plugin
}

func newAlphaFixture(t *testing.T, provider string) (*Server, *config.Config, *alphaAdmissionExecutor, *alphaBackendExecutor) {
	t.Helper()
	cfg := &config.Config{
		SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"search-key"}},
		BPS:       config.BPSConfig{Models: []string{"alpha-bps-model"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()},
		Plugins: config.PluginsConfig{Enabled: true, Configs: map[string]config.PluginInstanceConfig{
			"cpa-bps":                  alphaPluginConfig(t, "enabled: true\nweb_search: true"),
			"claude-web-search-router": alphaPluginConfig(t, "enabled: true\ncodex_model: alpha-codex-search"),
		}},
	}
	server := newTestServerWithConfig(t, cfg)
	manager := server.handlers.AuthManager
	manager.SetConfig(cfg)
	bps := &alphaAdmissionExecutor{BPSExecutor: executor.NewBPSExecutor(cfg), provider: provider}
	backend := &alphaBackendExecutor{BPSExecutor: executor.NewBPSExecutor(cfg), t: t, body: `{"content":[{"type":"web_search_tool_result","content":[{"type":"web_search_result","url":"https://docs.example.com/source","title":"Source"}]},{"type":"text","text":"Actual backend findings."}]}`}
	manager.RegisterExecutor(bps)
	manager.RegisterExecutor(backend)
	server.handlers.SetModelRouterHost(&bpsWebSearchRouter{t: t})
	for _, item := range []struct{ id, provider, model, token string }{{"alpha-bps-auth", provider, "alpha-bps-model", "BPS_ONLY"}, {"alpha-search-backend", "claude", "search-fixture", "SEARCH_ONLY"}} {
		a := &coreauth.Auth{ID: item.id, Provider: item.provider, Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": item.token, "account_id": item.id, "sub": "synthetic"}}
		if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(a.ID, a.Provider, []*registry.ModelInfo{{ID: item.model, Type: a.Provider, UserDefined: true}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
	}
	manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network transport")
		return nil, errors.New("offline Alpha fixture")
	}))
	return server, server.getConfig(), bps, backend
}

func alphaBody() *ojson.Doc {
	return ojson.NewDocString(`{"id":"synthetic-search-session","model":"alpha-bps-model","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Recent user context"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Recent assistant context"}]}],"commands":{"search_query":[{"q":"synthetic query","domains":["docs.example.com"]}],"response_length":"short"},"settings":{"allowed_callers":["direct"],"external_web_access":true,"filters":{"allowed_domains":["example.com"]}},"max_output_tokens":2000}`)
}

func alphaRequest(server *Server, ctx context.Context, path string, body []byte, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Session_id", "OUTER_SESSION")
	w := httptest.NewRecorder()
	server.engine.ServeHTTP(w, r)
	return w
}

func TestBPSAlphaSearchPublicLive(t *testing.T) {
	for _, provider := range []string{"bps", "cpa-bps"} {
		for _, path := range []string{"/v1/alpha/search", "/backend-api/codex/alpha/search"} {
			t.Run(provider+path, func(t *testing.T) {
				server, _, bps, backend := newAlphaFixture(t, provider)
				ctx := handlers.WithPinnedAuthID(context.Background(), "alpha-bps-auth")
				w := alphaRequest(server, ctx, path, alphaBody().Bytes(), "search-key")
				result := ojson.ParseBytes(w.Body.Bytes())
				if w.Code != 200 || result.Get("output").Type != ojson.String || !strings.Contains(result.Get("output").String(), "https://docs.example.com/source") || result.Get("encrypted_output").Exists() || bps.calls != 0 || backend.calls != 1 {
					t.Fatalf("status=%d body=%s bps=%d backend=%d", w.Code, w.Body.String(), bps.calls, backend.calls)
				}
				if backend.apiKeys[0] != "search-key" || ojson.GetBytes(backend.bodies[0], "max_tokens").Int() != 1024 || ojson.GetBytes(backend.bodies[0], "tools.0.allowed_domains.0").String() != "docs.example.com" || !strings.Contains(string(backend.bodies[0]), "Recent assistant context") {
					t.Fatalf("billing or constraints lost: key=%v request=%s", backend.apiKeys, backend.bodies[0])
				}
				t.Logf("ALPHA_RESULT provider=%s path=%s status=%d response=%s", provider, path, w.Code, w.Body.String())
				alphaClientFixture(t, provider+"_live", "live", w)
			})
		}
	}
}

func TestBPSAlphaSearchLiveFailureBoundaries(t *testing.T) {
	for _, scenario := range []string{"unsupported_command", "recency", "blocked_domains", "disjoint_domains", "wrong_api_key", "wrong_pin", "unknown_model", "protocol_disabled", "native_search_disabled", "plugin_search_disabled", "backend_missing", "empty_result", "missing_result", "bad_domain", "backend403", "canceled", "multiple_queries", "text_input", "suspended"} {
		t.Run(scenario, func(t *testing.T) {
			provider := "bps"
			if scenario == "plugin_search_disabled" {
				provider = "cpa-bps"
			}
			server, cfg, bps, backend := newAlphaFixture(t, provider)
			doc := alphaBody()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			key, expected, calls := "search-key", 422, 0
			falseValue := false
			switch scenario {
			case "unsupported_command":
				_ = doc.Set("commands.open", []any{map[string]any{"ref_id": "https://example.com"}})
			case "recency":
				_ = doc.Set("commands.search_query.0.recency", 7)
			case "blocked_domains":
				_ = doc.Set("settings.filters.blocked_domains", []string{"other.example"})
			case "disjoint_domains":
				_ = doc.Set("commands.search_query.0.domains", []string{"other.invalid"})
			case "wrong_api_key":
				key, expected = "wrong", 401
			case "wrong_pin":
				ctx = handlers.WithPinnedAuthID(ctx, "alpha-search-backend")
				expected = 503
			case "unknown_model":
				_ = doc.Set("model", "unregistered-model")
				expected = 503
			case "protocol_disabled":
				cfg.BPSProtocol.Enabled = &falseValue
				expected = 503
			case "native_search_disabled":
				cfg.BPS.WebSearch = &falseValue
				expected = 503
			case "plugin_search_disabled":
				cfg.Plugins.Configs["cpa-bps"] = alphaPluginConfig(t, "enabled: true\nweb_search: false")
				expected = 503
			case "backend_missing":
				server.handlers.SetModelRouterHost(nil)
				expected = 503
			case "empty_result":
				backend.body = `{"content":[{"type":"web_search_tool_result","content":[]}]}`
				expected, calls = 200, 1
			case "missing_result":
				backend.body = `{"content":[{"type":"text","text":"No tool result"}]}`
				expected, calls = 502, 1
			case "bad_domain":
				backend.body = strings.ReplaceAll(backend.body, "docs.example.com", "evil.invalid")
				expected, calls = 502, 1
			case "backend403":
				backend.status = 403
				expected, calls = 403, 1
			case "canceled":
				backend.cancel = cancel
				expected, calls = 499, 1
			case "multiple_queries":
				_ = doc.Set("commands.search_query.1", map[string]any{"q": "second query", "domains": []string{"example.com"}})
				expected, calls = 200, 2
			case "text_input":
				_ = doc.Delete("commands")
				_ = doc.Set("input", "explicit query")
				expected, calls = 200, 1
			case "suspended":
				server.handlers.AuthManager.MarkResult(ctx, coreauth.Result{AuthID: "alpha-bps-auth", Provider: "bps", Model: "alpha-bps-model", Error: &coreauth.Error{HTTPStatus: 403, Message: "synthetic denied"}, PermissionDenial: &coreauth.PermissionDenial{Source: "http", UpstreamHTTPStatus: 403, SemanticStatus: 403, CredentialScope: true}})
				expected = 503
			}
			w := alphaRequest(server, ctx, "/v1/alpha/search", doc.Bytes(), key)
			if w.Code != expected || backend.calls != calls || bps.calls != 0 {
				t.Fatalf("status=%d want=%d body=%s bps=%d backend=%d want=%d", w.Code, expected, w.Body.String(), bps.calls, backend.calls, calls)
			}
			if scenario == "backend403" {
				a, _ := server.handlers.AuthManager.GetByID("alpha-bps-auth")
				if len(a.PermissionSuspensions) != 0 || a.LastError != nil {
					t.Fatalf("search denial affected BPS auth: %+v", a)
				}
			}
			if scenario == "empty_result" || scenario == "backend_missing" {
				alphaClientFixture(t, scenario, "live", w)
			}
		})
	}
}

func registerAlphaCodex(t *testing.T, server *Server, model string) *codexSearchCaptureExecutor {
	t.Helper()
	codex := &codexSearchCaptureExecutor{responseBody: io.NopCloser(strings.NewReader(`{"output":"Native Alpha source https://example.com","encrypted_output":null}`))}
	server.handlers.AuthManager.RegisterExecutor(codex)
	a := &coreauth.Auth{ID: "alpha-codex-backend", Provider: "codex", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "CODEX_SEARCH_ONLY", "account_id": "codex-search-account"}}
	if _, err := server.handlers.AuthManager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient(a.ID, "codex", []*registry.ModelInfo{{ID: model, Type: "codex", UserDefined: true}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
	return codex
}

func TestBPSAlphaSearchCachedIndexed(t *testing.T) {
	for _, provider := range []string{"bps", "cpa-bps"} {
		for _, mode := range []any{false, "cached", "indexed"} {
			for _, path := range []string{"/v1/alpha/search", "/backend-api/codex/alpha/search"} {
				t.Run(provider+path+stringMode(mode), func(t *testing.T) {
					server, _, bps, backend := newAlphaFixture(t, provider)
					codex := registerAlphaCodex(t, server, "alpha-codex-search")
					doc := alphaBody()
					_ = doc.Set("settings.external_web_access", mode)
					_ = doc.Set("commands.search_query.0.recency", 7)
					ctx := handlers.WithPinnedAuthID(context.Background(), "alpha-bps-auth")
					w := alphaRequest(server, ctx, path, doc.Bytes(), "search-key")
					if w.Code != 200 || ojson.GetBytes(w.Body.Bytes(), "output").Type != ojson.String || codex.httpCalls != 1 || backend.calls != 0 || bps.calls != 0 {
						t.Fatalf("status=%d body=%s codex=%d live=%d bps=%d", w.Code, w.Body.String(), codex.httpCalls, backend.calls, bps.calls)
					}
					_ = doc.Set("model", "alpha-codex-search")
					if !reflect.DeepEqual(ojson.ParseBytes(codex.body).Value(), ojson.ParseBytes(doc.Bytes()).Value()) || codex.request.Header.Get("Authorization") != "Bearer CODEX_SEARCH_ONLY" || codex.request.Header.Get("Session_id") != "" || codex.request.Header.Get("Chatgpt-Account-Id") != "codex-search-account" {
						t.Fatalf("dedicated Alpha contract mismatch: body=%s expected=%s headers=%v", codex.body, doc.String(), codex.request.Header)
					}
					t.Logf("ALPHA_RESULT provider=%s mode=%v path=%s status=%d response=%s", provider, mode, path, w.Code, w.Body.String())
					modeName := "cached"
					if mode == "indexed" {
						modeName = "indexed"
					}
					alphaClientFixture(t, provider+"_"+modeName, modeName, w)
				})
			}
		}
	}
}

func alphaClientFixture(t *testing.T, name, mode string, response *httptest.ResponseRecorder) {
	t.Helper()
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("case", name)
	_ = doc.Set("mode", mode)
	_ = doc.Set("status", response.Code)
	_ = doc.Set("response", ojson.ParseBytes(response.Body.Bytes()).Value())
	t.Log("ALPHA_CLIENT_FIXTURE=" + doc.String())
}

type alphaHomeDispatcher struct {
	provider string
	calls    []string
}

func (*alphaHomeDispatcher) HeartbeatOK() bool       { return true }
func (*alphaHomeDispatcher) AbortAmbiguousDispatch() {}
func (d *alphaHomeDispatcher) RPopAuth(_ context.Context, model, _ string, _ http.Header, _ int) ([]byte, error) {
	d.calls = append(d.calls, model)
	provider, id, token := d.provider, "alpha-bps-auth", "BPS_ONLY"
	if model == "search-fixture" {
		provider, id, token = "claude", "alpha-search-backend", "SEARCH_ONLY"
	} else if model == "alpha-codex-search" {
		provider, id, token = "codex", "alpha-codex-backend", "CODEX_SEARCH_ONLY"
	}
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("model", model)
	_ = doc.Set("auth_index", id)
	_ = doc.Set("auth", map[string]any{"id": id, "provider": provider, "status": "active", "metadata": map[string]any{"access_token": token, "account_id": "codex-search-account"}})
	_ = doc.Set("concurrency", map[string]any{"accounted": true, "credential_id": id, "model": model})
	return doc.Bytes(), nil
}

func (d *alphaHomeDispatcher) RPopAuthWithPolicy(ctx context.Context, model, session string, headers http.Header, count int, _ string) ([]byte, error) {
	return d.RPopAuth(ctx, model, session, headers, count)
}

func TestBPSAlphaSearchHomeLifecycle(t *testing.T) {
	for _, provider := range []string{"bps", "cpa-bps"} {
		for _, scenario := range []string{"live", "cached", "indexed", "cancel_live", "cancel_cached", "http_failure", "invalid_response"} {
			t.Run(provider+"/"+scenario, func(t *testing.T) {
				server, cfg, bps, live := newAlphaFixture(t, provider)
				codex := registerAlphaCodex(t, server, "alpha-codex-search")
				homeConfig := *cfg
				homeConfig.Home.Enabled = true
				manager := server.handlers.AuthManager
				manager.SetConfig(&homeConfig)
				dispatcher := &alphaHomeDispatcher{provider: provider}
				registry := executionregistry.New()
				manager.PublishHomeDispatch(dispatcher, registry, 1)
				doc := alphaBody()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				expected := 200
				switch scenario {
				case "cached":
					_ = doc.Set("settings.external_web_access", false)
				case "indexed":
					_ = doc.Set("settings.external_web_access", "indexed")
				case "cancel_live":
					live.cancel = cancel
					expected = 499
				case "cancel_cached":
					_ = doc.Set("settings.external_web_access", false)
					codex.beforeReturn = cancel
					expected = 499
				case "http_failure":
					_ = doc.Set("settings.external_web_access", false)
					codex.httpErr = errors.New("synthetic alpha transport failed")
					expected = 502
				case "invalid_response":
					_ = doc.Set("settings.external_web_access", false)
					codex.responseBody = io.NopCloser(strings.NewReader(`{"results":[]}`))
					expected = 502
				}
				w := alphaRequest(server, ctx, "/v1/alpha/search", doc.Bytes(), "search-key")
				if w.Code != expected || bps.calls != 0 || len(dispatcher.calls) != 2 {
					t.Fatalf("status=%d want=%d body=%s bps=%d dispatch=%v", w.Code, expected, w.Body.String(), bps.calls, dispatcher.calls)
				}
				pendingCtx, pendingCancel := context.WithTimeout(context.Background(), time.Second)
				defer pendingCancel()
				if err := registry.WaitPending(pendingCtx); err != nil {
					t.Fatal(err)
				}
				if remaining := registry.FreezeInFlight(time.Now()).Executions; len(remaining) != 0 {
					t.Fatalf("Home selection leaked after response: %+v", remaining)
				}
				if err := registry.Drain(pendingCtx); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

type alphaUsageCapture struct {
	mu      sync.Mutex
	records []usage.Record
	signal  chan struct{}
}

func (c *alphaUsageCapture) HandleUsage(_ context.Context, record usage.Record) {
	if record.AuthID != "alpha-search-backend" && record.AuthID != "alpha-bps-auth" && record.AuthID != "alpha-codex-backend" {
		return
	}
	c.mu.Lock()
	c.records = append(c.records, record)
	c.mu.Unlock()
	select {
	case c.signal <- struct{}{}:
	default:
	}
}

func TestBPSAlphaSearchUsageIdentity(t *testing.T) {
	server, _, bps, backend := newAlphaFixture(t, "bps")
	backend.publishUsage = true
	capture := &alphaUsageCapture{signal: make(chan struct{}, 8)}
	usage.RegisterNamedPlugin(t.Name(), capture)
	t.Cleanup(func() { usage.RegisterNamedPlugin(t.Name(), noopHomeUnauthorizedUsagePlugin{}) })
	doc := alphaBody()
	_ = doc.Set("commands.search_query.1", map[string]any{"q": "second query"})
	w := alphaRequest(server, context.Background(), "/v1/alpha/search", doc.Bytes(), "search-key")
	if w.Code != 200 || backend.calls != 2 || bps.calls != 0 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for i := 0; i < 2; i++ {
		select {
		case <-capture.signal:
		case <-time.After(time.Second):
			t.Fatal("missing search usage record")
		}
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.records) != 2 {
		t.Fatalf("usage count=%d", len(capture.records))
	}
	for _, record := range capture.records {
		if record.Provider != "claude" || record.AuthID != "alpha-search-backend" || record.APIKey != "search-key" || record.Detail.InputTokens != 7 || record.Detail.OutputTokens != 11 {
			t.Fatalf("incorrect search usage attribution: %+v", record)
		}
	}
}

func stringMode(value any) string {
	if value == false {
		return "false"
	}
	return value.(string)
}

func TestBPSAlphaSearchDedicatedBackendBoundaries(t *testing.T) {
	for _, scenario := range []string{"missing_config", "disabled_router", "missing_model", "missing_auth", "ordinary_api_key", "backend403", "invalid_response", "payload_barrier", "mode_override"} {
		t.Run(scenario, func(t *testing.T) {
			server, cfg, bps, live := newAlphaFixture(t, "bps")
			codex := registerAlphaCodex(t, server, "alpha-codex-search")
			doc := alphaBody()
			_ = doc.Set("settings.external_web_access", "indexed")
			expected, calls := 503, 0
			switch scenario {
			case "missing_config":
				cfg.Plugins.Configs["claude-web-search-router"] = alphaPluginConfig(t, "enabled: true")
			case "disabled_router":
				cfg.Plugins.Configs["claude-web-search-router"] = alphaPluginConfig(t, "enabled: false\ncodex_model: alpha-codex-search")
			case "missing_model":
				registry.GetGlobalRegistry().UnregisterClient("alpha-codex-backend")
			case "missing_auth", "ordinary_api_key":
				a, _ := server.handlers.AuthManager.GetByID("alpha-codex-backend")
				if scenario == "missing_auth" {
					a.Disabled = true
				} else {
					a.Metadata = nil
					a.Attributes = map[string]string{coreauth.AttributeAPIKey: "SYNTHETIC_KEY"}
				}
				if _, err := server.handlers.AuthManager.Update(coreauth.WithSkipPersist(context.Background()), a); err != nil {
					t.Fatal(err)
				}
			case "backend403":
				codex.statuses = []int{403}
				expected, calls = 403, 1
			case "invalid_response":
				codex.responseBody = io.NopCloser(strings.NewReader(`{"results":[]}`))
				expected, calls = 502, 1
			case "payload_barrier":
				cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "alpha-codex-search"}}, Params: map[string]any{"max_output_tokens": 77}}}
				expected, calls = 200, 1
			case "mode_override":
				cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "alpha-codex-search"}}, Params: map[string]any{"settings.external_web_access": true}}}
				expected = 502
			}
			w := alphaRequest(server, context.Background(), "/v1/alpha/search", doc.Bytes(), "search-key")
			if w.Code != expected || codex.httpCalls != calls || live.calls != 0 || bps.calls != 0 {
				t.Fatalf("status=%d want=%d body=%s alpha=%d want=%d live=%d bps=%d", w.Code, expected, w.Body.String(), codex.httpCalls, calls, live.calls, bps.calls)
			}
			if scenario == "payload_barrier" && ojson.GetBytes(codex.body, "max_output_tokens").Int() != 77 {
				t.Fatalf("payload rule lost: %s", codex.body)
			}
			a, _ := server.handlers.AuthManager.GetByID("alpha-bps-auth")
			if a.LastError != nil || len(a.PermissionSuspensions) != 0 {
				t.Fatal("dedicated Alpha backend failure changed BPS account")
			}
		})
	}
}

func TestBPSAlphaSearchCodexPriority(t *testing.T) {
	for _, scenario := range []string{"same_model", "codex_error", "explicit_codex", "no_model"} {
		t.Run(scenario, func(t *testing.T) {
			server, _, bps, live := newAlphaFixture(t, "bps")
			codex := registerAlphaCodex(t, server, "alpha-bps-model")
			doc := alphaBody()
			expected := 200
			if scenario == "codex_error" {
				codex.statuses = []int{503}
				expected = 503
			} else if scenario == "explicit_codex" {
				server.handlers.SetModelRouterHost(&codexSearchModelRouter{handled: true, response: pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetProvider, Target: "codex", TargetModel: "alpha-bps-model"}})
			} else if scenario == "no_model" {
				_ = doc.Delete("model")
			}
			w := alphaRequest(server, context.Background(), "/v1/alpha/search", doc.Bytes(), "search-key")
			if w.Code != expected || codex.httpCalls != 1 || live.calls != 0 || bps.calls != 0 {
				t.Fatalf("status=%d body=%s codex=%d live=%d bps=%d", w.Code, w.Body.String(), codex.httpCalls, live.calls, bps.calls)
			}
		})
	}
}
