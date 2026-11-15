package cliproxy

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"gopkg.in/yaml.v3"
)

type sdkSearchTransport func(*http.Request) (*http.Response, error)

func (f sdkSearchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func (f sdkSearchTransport) RoundTripperFor(*coreauth.Auth) http.RoundTripper  { return f }

type sdkSearchCodex struct {
	*runtimeexecutor.BPSExecutor
	calls  int
	status int
	body   []byte
}

func (e *sdkSearchCodex) Identifier() string                                 { return "codex" }
func (e *sdkSearchCodex) PrepareRequest(*http.Request, *coreauth.Auth) error { return nil }
func (e *sdkSearchCodex) HttpRequest(_ context.Context, _ *coreauth.Auth, request *http.Request) (*http.Response, error) {
	e.calls++
	e.body, _ = io.ReadAll(request.Body)
	return &http.Response{StatusCode: e.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"output":"SDK Alpha text","encrypted_output":null}`))}, nil
}

type sdkSearchSelector struct {
	t        *testing.T
	calls    int
	onSelect func()
}

func (s *sdkSearchSelector) Pick(ctx context.Context, provider, _ string, opts coreexecutor.Options, candidates []*coreauth.Auth) (*coreauth.Auth, error) {
	if provider == "codex" {
		s.calls++
		if opts.Metadata[coreexecutor.CallerScopeMetadataKey] != "sdk-caller" || opts.Metadata[coreexecutor.PinnedAuthMetadataKey] != nil || opts.Metadata[coreexecutor.ExecutionSessionMetadataKey] != nil || ctx.Value("gin") != nil {
			s.t.Errorf("nested SDK caller isolation failed: %v", opts.Metadata)
		}
		if s.onSelect != nil {
			s.onSelect()
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return candidates[0], nil
}

func TestBuilderModelExecutorBPSCachedSearch(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success_snapshot", true: "backend403"}[failed], func(t *testing.T) {
			var router config.PluginInstanceConfig
			if err := yaml.Unmarshal([]byte("enabled: true\ncodex_model: sdk-codex-search"), &router); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{BPS: config.BPSConfig{Models: []string{"sdk-bps-model"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}, Plugins: config.PluginsConfig{Enabled: true, Configs: map[string]config.PluginInstanceConfig{"claude-web-search-router": router}}}
			cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "sdk-codex-search"}}, Params: map[string]any{"max_output_tokens": 77}}}
			cfg.OAuthModelAlias = map[string][]config.OAuthModelAlias{"codex": {{Name: "sdk-codex-old", Alias: "sdk-codex-search"}}}
			selector := &sdkSearchSelector{t: t}
			manager := coreauth.NewManager(nil, selector, nil)
			service, err := NewBuilder().WithConfig(cfg).WithConfigPath(filepath.Join(t.TempDir(), "config.yaml")).WithCoreAuthManager(manager).Build()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { helps.RetireProxyPoolPublisher(service.proxyPoolPublisher) })
			if service.server != nil || service.ModelExecutor() == nil {
				t.Fatal("SDK execution unexpectedly requires HTTP server")
			}
			bps := runtimeexecutor.NewBPSExecutor(cfg)
			codex := &sdkSearchCodex{BPSExecutor: bps, status: 200}
			if failed {
				codex.status = 403
			}
			manager.RegisterExecutor(bps)
			manager.RegisterExecutor(codex)
			for _, item := range []struct{ id, provider, model string }{{"sdk-bps-auth", "bps", "sdk-bps-model"}, {"sdk-codex-auth", "codex", "sdk-codex-search"}} {
				a := &coreauth.Auth{ID: item.id, Provider: item.provider, Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "SYNTHETIC", "account_id": item.id, "sub": "synthetic"}}
				if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
					t.Fatal(err)
				}
				models := []*registry.ModelInfo{{ID: item.model, Type: a.Provider, UserDefined: true}}
				if item.provider == "codex" {
					models = append(models, &registry.ModelInfo{ID: "sdk-codex-old", Type: "codex", UserDefined: true}, &registry.ModelInfo{ID: "sdk-codex-new", Type: "codex", UserDefined: true})
				}
				registry.GetGlobalRegistry().RegisterClient(a.ID, a.Provider, models)
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
			}
			selector.onSelect = func() {
				next := service.currentConfig().CloneForRuntime()
				next.Payload.Override[0].Params["max_output_tokens"] = 99
				next.OAuthModelAlias["codex"][0].Name = "sdk-codex-new"
				service.commitConfigUpdate(next)
				manager.SetOAuthModelAlias(next.OAuthModelAlias)
			}
			posts := 0
			manager.SetRoundTripperProvider(sdkSearchTransport(func(request *http.Request) (*http.Response, error) {
				posts++
				body, _ := io.ReadAll(request.Body)
				terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"sdk-response","status":"completed","model":"sdk-bps-model"}}`)
				if posts%2 == 1 {
					args := ojson.NewDocString(`{"summary":"search","extended_summary":"","destructive":false,"references":[]}`)
					_ = args.Set("code", `{"name":"__cpa_bps_web_search","arguments":{"query":"SDK query"}}`)
					_ = terminal.Set("response.output", []any{map[string]any{"type": "function_call", "id": "fc_sdk", "call_id": "sdk-search", "name": "run_officejs", "arguments": args.String()}})
				} else {
					if !strings.Contains(string(body), "SDK Alpha text") {
						t.Fatalf("missing search text: %s", body)
					}
					_ = terminal.Set("response.output", []any{map[string]any{"type": "message", "id": "sdk-final", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "SDK_RESULT", "annotations": []any{}}}}})
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
			}))
			request := handlers.ModelExecutionRequest{EntryProtocol: "openai-response", ExitProtocol: "openai-response", Model: "sdk-bps-model", CallerScope: "sdk-caller", Body: []byte(`{"model":"sdk-bps-model","input":"search","tools":[{"type":"web_search","external_web_access":false}]}`)}
			response, failure := service.ModelExecutor().ExecuteModel(context.Background(), request)
			if codex.calls != 1 || selector.calls != 1 || ojson.GetBytes(codex.body, "max_output_tokens").Int() != 77 || ojson.GetBytes(codex.body, "model").String() != "sdk-codex-old" {
				t.Fatalf("SDK dispatch/snapshot failed: alpha=%d selection=%d body=%s failure=%v", codex.calls, selector.calls, codex.body, failure)
			}
			if failed {
				if failure == nil || posts != 1 {
					t.Fatalf("SDK failed search resumed: posts=%d failure=%v", posts, failure)
				}
			} else {
				if failure != nil || posts != 2 || !strings.Contains(string(response.Body), "SDK_RESULT") {
					t.Fatalf("SDK result: posts=%d failure=%v body=%s", posts, failure, response.Body)
				}
				_, failure = service.ModelExecutor().ExecuteModel(context.Background(), request)
				if failure != nil || codex.calls != 2 || ojson.GetBytes(codex.body, "max_output_tokens").Int() != 99 || ojson.GetBytes(codex.body, "model").String() != "sdk-codex-new" {
					t.Fatalf("SDK config reload not observed: calls=%d body=%s failure=%v", codex.calls, codex.body, failure)
				}
			}
			parent, _ := manager.GetByID("sdk-bps-auth")
			if len(parent.PermissionSuspensions) != 0 {
				t.Fatal("nested denial suspended parent BPS")
			}
		})
	}
}
