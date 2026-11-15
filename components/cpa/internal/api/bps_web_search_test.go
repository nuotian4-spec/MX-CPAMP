package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type bpsWebSearchRouter struct {
	t     *testing.T
	calls int
}

func (r *bpsWebSearchRouter) HasModelRouters() bool             { return true }
func (r *bpsWebSearchRouter) HasModelRoutersExcept(string) bool { return true }
func (r *bpsWebSearchRouter) RouteModel(ctx context.Context, req pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, bool) {
	return r.RouteModelExcept(ctx, req, "")
}
func (r *bpsWebSearchRouter) RouteModelExcept(_ context.Context, req pluginapi.ModelRouteRequest, _ string) (pluginapi.ModelRouteResponse, bool) {
	if req.RequestedModel != "cpa-bps-search-router" {
		return pluginapi.ModelRouteResponse{}, false
	}
	r.calls++
	if req.SourceFormat != "claude" || req.Stream || req.Headers.Get("Authorization") != "" || req.Headers.Get("chatgpt-account-id") != "" || req.Headers.Get("Session-Id") != "" {
		r.t.Errorf("nested routing leaked parent: %+v", req)
	}
	for _, key := range []string{coreexecutor.PinnedAuthMetadataKey, coreexecutor.SelectedAuthCallbackMetadataKey, coreexecutor.ExecutionSessionMetadataKey} {
		if req.Metadata[key] != nil {
			r.t.Errorf("nested metadata leaked %s", key)
		}
	}
	return pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetProvider, Target: "claude", TargetModel: "search-fixture"}, true
}

type bpsWebSearchBackend struct {
	*executor.BPSExecutor
	t      *testing.T
	calls  int
	body   string
	fail   bool
	cancel context.CancelFunc
}

type bpsSearchBackendDenied struct{}

func (bpsSearchBackendDenied) Error() string   { return "synthetic search backend denied" }
func (bpsSearchBackendDenied) StatusCode() int { return 403 }

type bpsSearchUsageCapture struct{ records chan usage.Record }

func (c *bpsSearchUsageCapture) HandleUsage(_ context.Context, r usage.Record) {
	if c.records != nil && r.Provider == "bps" {
		c.records <- r
	}
}

func (e *bpsWebSearchBackend) Identifier() string { return "claude" }
func (e *bpsWebSearchBackend) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	if auth.Provider != "claude" || auth.ID != "search-auth" || auth.Metadata["access_token"] != "SEARCH_ONLY" {
		e.t.Errorf("wrong nested auth: %+v", auth)
	}
	if strings.Contains(string(req.Payload), "BPS_ONLY") || strings.Contains(string(opts.OriginalRequest), "PARENT_ONLY") || opts.Headers.Get("Authorization") != "" || opts.Headers.Get("Session-Id") != "" {
		e.t.Error("nested request leaked parent fields")
	}
	if usage.APIKeyFromContext(ctx) != "search-key" {
		e.t.Errorf("billing identity lost: %q", usage.APIKeyFromContext(ctx))
	}
	if ojson.GetBytes(req.Payload, "tools.0.type").String() != "web_search_20250305" || ojson.GetBytes(req.Payload, "max_tokens").Int() != 4096 {
		e.t.Errorf("search payload=%s", req.Payload)
	}
	if e.fail {
		return coreexecutor.Response{}, bpsSearchBackendDenied{}
	}
	if e.cancel != nil {
		e.cancel()
		<-ctx.Done()
		return coreexecutor.Response{}, ctx.Err()
	}
	return coreexecutor.Response{Payload: []byte(e.body)}, nil
}

func bpsSearchNativeCall(id, name string) map[string]any {
	args := ojson.NewDocString(`{"summary":"search","extended_summary":"","destructive":false,"references":[]}`)
	code := ojson.NewDocString(`{}`)
	_ = code.Set("name", name)
	_ = code.Set("arguments", map[string]any{"query": "synthetic query"})
	_ = args.Set("code", code.String())
	return map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": "run_officejs", "arguments": args.String()}
}

func TestBPSWebSearchPublicRouterAndContinuation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		stream        bool
		rounds, batch int
		scenario      string
	}{
		{"plain", false, 1, 1, ""}, {"stream", true, 1, 1, ""}, {"four_rounds", true, 4, 1, ""}, {"four_batch", false, 1, 4, ""},
		{"fifth_batch", false, 1, 5, "limit"}, {"fifth_round", true, 5, 1, "limit"}, {"mixed", false, 1, 1, "mixed"},
		{"duplicate", false, 2, 1, "duplicate"}, {"backend403", true, 1, 1, "backend"}, {"bad_domain", false, 1, 1, "domain"},
		{"plain_backend", false, 1, 1, "plain_backend"}, {"no_citation", true, 1, 1, "no_citation"}, {"prefix_url", false, 1, 1, "prefix_url"},
		{"none", false, 0, 0, "none"}, {"disabled", false, 0, 0, "disabled"}, {"force_missing", false, 0, 0, "force_missing"},
		{"continuation403", true, 1, 1, "continuation403"}, {"sdk_pinned", false, 1, 1, "sdk"},
		{"continuation_transport", false, 1, 1, "transport"}, {"generic_required_client", false, 0, 0, "generic_client"}, {"schema_metadata", false, 0, 0, "schema"},
		{"cancel_nested", true, 1, 1, "cancel"}, {"payload_barrier", false, 1, 1, "payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &bpsSearchUsageCapture{records: make(chan usage.Record, 8)}
			usage.RegisterNamedPlugin(t.Name(), capture)
			t.Cleanup(func() { usage.RegisterNamedPlugin(t.Name(), &bpsSearchUsageCapture{}) })
			cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"search-key"}}, BPS: config.BPSConfig{Models: []string{"gpt-6-luna"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			if tc.scenario == "payload" {
				cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "gpt-6-luna"}}, Params: map[string]any{"metadata.agent_iteration": "77"}}}
				cfg.Payload.Filter = []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "gpt-6-luna"}}, Params: []string{"metadata.turn_id"}}}
			}
			if tc.scenario == "disabled" {
				value := false
				cfg.BPS.WebSearch = &value
			}
			server := newTestServerWithConfig(t, cfg)
			manager := server.handlers.AuthManager
			manager.SetConfig(cfg)
			manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
			backend := &bpsWebSearchBackend{BPSExecutor: executor.NewBPSExecutor(cfg), t: t, body: `{"type":"message","content":[{"type":"web_search_tool_result","tool_use_id":"search1","content":[{"type":"web_search_result","url":"https://example.com/source","title":"Source"}]},{"type":"text","text":"Found source."}]}`}
			if tc.scenario == "backend" {
				backend.fail = true
			}
			if tc.scenario == "domain" {
				backend.body = strings.ReplaceAll(backend.body, "example.com/source", "example.com.evil.invalid/source")
			}
			if tc.scenario == "plain_backend" {
				backend.body = `{"content":[{"type":"text","text":"no result"}]}`
			}
			manager.RegisterExecutor(backend)
			router := &bpsWebSearchRouter{t: t}
			server.handlers.SetModelRouterHost(router)
			for _, a := range []*coreauth.Auth{{ID: "bps-search-auth", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "BPS_ONLY", "account_id": "bps-account", "sub": "synthetic"}}, {ID: "search-auth", Provider: "claude", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "SEARCH_ONLY"}}} {
				if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
					t.Fatal(err)
				}
				model := "gpt-6-luna"
				if a.Provider == "claude" {
					model = "search-fixture"
				}
				registry.GetGlobalRegistry().RegisterClient(a.ID, a.Provider, []*registry.ModelInfo{{ID: model, Type: a.Provider, UserDefined: true}})
				defer registry.GetGlobalRegistry().UnregisterClient(a.ID)
			}
			posts := 0
			defer func() {
				select {
				case record := <-capture.records:
					if record.Detail.Iterations == nil || len(*record.Detail.Iterations) != posts {
						t.Errorf("usage rows=%+v posts=%d", record.Detail.Iterations, posts)
						return
					}
					for i, item := range *record.Detail.Iterations {
						if i > 0 && item.Type != "bps_search_continuation" {
							t.Errorf("wrong iteration type %s", item.Type)
						}
					}
					if tc.scenario == "transport" || tc.scenario == "continuation403" {
						item := (*record.Detail.Iterations)[1]
						if item.InputTokens != nil || item.OutputTokens != nil {
							t.Errorf("unknown usage fabricated: %+v", item)
						}
					}
					select {
					case <-capture.records:
						t.Error("duplicate BPS parent usage")
					default:
					}
				case <-time.After(3 * time.Second):
					t.Error("missing BPS usage record")
				}
			}()
			manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
				posts++
				wire, _ := io.ReadAll(r.Body)
				if tc.scenario == "payload" && (ojson.GetBytes(wire, "metadata.agent_iteration").String() != "77" || ojson.GetBytes(wire, "metadata.turn_id").Exists()) {
					t.Errorf("final payload barrier changed: %s", wire)
				}
				if r.Header.Get("Authorization") != "Bearer BPS_ONLY" || r.Header.Get("chatgpt-account-id") != "bps-account" {
					t.Error("BPS identity changed")
				}
				if posts > 1 && (!strings.Contains(string(wire), `function_call_output`) || !strings.Contains(string(wire), `untrusted data`)) {
					t.Errorf("missing search continuation: %s", wire)
				}
				if tc.scenario == "continuation403" && posts == 2 {
					return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("denied"))}, nil
				}
				if tc.scenario == "transport" && posts == 2 {
					return nil, errors.New("synthetic transport closed")
				}
				output := []any{}
				if posts <= tc.rounds {
					for i := 0; i < tc.batch; i++ {
						id := fmt.Sprintf("s%d_%d", posts, i)
						if tc.scenario == "duplicate" {
							id = "same"
						}
						output = append(output, bpsSearchNativeCall(id, "__cpa_bps_web_search"))
					}
					if tc.scenario == "mixed" {
						output = append(output, bpsSearchNativeCall("client", "echo"))
					}
				} else {
					answer := "中文🙂 https://example.com/source"
					if tc.scenario == "no_citation" {
						answer = "answer without a URL"
					}
					if tc.scenario == "prefix_url" {
						answer += "-not-real"
					}
					output = append(output, map[string]any{"type": "message", "id": "m_final", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": answer, "annotations": []any{}}}})
				}
				if tc.scenario == "generic_client" || tc.scenario == "schema" {
					call := bpsSearchNativeCall("client", "echo")
					if tc.scenario == "schema" {
						args := ojson.NewDocString(`{"summary":"client","code":"{\"name\":\"echo\",\"arguments\":{\"query\":\"x\"}}"}`)
						call["arguments"] = args.String()
					}
					output = []any{call}
				}
				terminal := ojson.NewDocString(`{"type":"response.completed","response":{"object":"response","status":"completed","model":"gpt-6-luna","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`)
				_ = terminal.Set("response.id", fmt.Sprintf("resp_round_%d", posts))
				_ = terminal.Set("response.output", output)
				created := fmt.Sprintf(`{"type":"response.created","response":{"id":"resp_round_%d","status":"in_progress","output":[]}}`, posts)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + created + "\n\ndata: " + terminal.String() + "\n\n"))}, nil
			}))
			doc := ojson.NewDocString(`{"model":"gpt-6-luna","input":"PARENT_ONLY search","tools":[{"type":"web_search","filters":{"allowed_domains":["example.com"]}},{"type":"function","name":"echo","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}}]}`)
			_ = doc.Set("stream", tc.stream)
			if tc.scenario == "generic_client" {
				_ = doc.Set("tool_choice", "required")
			}
			if tc.scenario == "schema" {
				_ = doc.Set("tools.1.parameters.properties.query.minLength", 5)
			}
			if tc.scenario == "none" {
				_ = doc.Set("tool_choice", "none")
			}
			if tc.scenario == "force_missing" {
				_ = doc.Set("tool_choice", map[string]any{"type": "web_search"})
			}
			w := httptest.NewRecorder()
			if tc.scenario == "sdk" {
				ctx := usage.WithAPIKey(handlers.WithPinnedAuthID(context.Background(), "bps-search-auth"), "search-key")
				ctx = handlers.WithOriginalRequest(ctx, doc.Bytes())
				body, _, failure := server.handlers.ExecuteWithAuthManager(ctx, "openai-response", "gpt-6-luna", doc.Bytes(), "")
				if failure != nil {
					t.Fatalf("SDK failure=%+v", failure)
				}
				w.Write(body)
			} else {
				r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(doc.String()))
				if tc.scenario == "cancel" {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					backend.cancel = cancel
					r = r.WithContext(ctx)
				}
				r.Header.Set("Authorization", "Bearer search-key")
				r.Header.Set("Session-Id", "parent-session")
				server.engine.ServeHTTP(w, r)
			}
			if tc.scenario == "cancel" {
				if posts != 1 || backend.calls != 1 || strings.Contains(w.Body.String(), `"type":"response.completed"`) {
					t.Errorf("cancel replayed or completed: posts=%d backend=%d body=%s", posts, backend.calls, w.Body.String())
				}
				return
			}
			wantFailure := tc.scenario == "limit" || tc.scenario == "mixed" || tc.scenario == "duplicate" || tc.scenario == "backend" || tc.scenario == "domain" || tc.scenario == "plain_backend" || tc.scenario == "force_missing" || tc.scenario == "continuation403" || tc.scenario == "transport" || tc.scenario == "schema"
			if wantFailure {
				if tc.scenario == "backend" || tc.scenario == "continuation403" {
					a, _ := manager.GetByID("bps-search-auth")
					suspended := len(coreauth.PermissionSuspensionSnapshot(a)) > 0
					if suspended != (tc.scenario == "continuation403") {
						t.Errorf("BPS suspension=%v scenario=%s", suspended, tc.scenario)
					}
				}
				if tc.scenario == "schema" {
					for _, part := range []string{`"tool":"echo"`, `"path":"$/query"`, `"rule":"minLength"`} {
						if !strings.Contains(w.Body.String(), part) {
							t.Errorf("missing diagnostic %s: %s", part, w.Body.String())
						}
					}
				}
				if !strings.Contains(w.Body.String(), `error`) {
					t.Fatalf("expected failure %d %s", w.Code, w.Body.String())
				}
				if tc.scenario == "mixed" || tc.batch == 5 {
					if backend.calls != 0 || posts != 1 {
						t.Fatalf("invalid batch dispatched: posts=%d searches=%d", posts, backend.calls)
					}
				}
				if tc.scenario == "limit" && tc.rounds == 5 && backend.calls != 4 {
					t.Fatalf("budget searches=%d", backend.calls)
				}
				if tc.scenario == "duplicate" && backend.calls != 1 {
					t.Fatalf("duplicate searches=%d", backend.calls)
				}
				return
			}
			if w.Code != 200 || strings.Contains(w.Body.String(), `"error"`) {
				t.Fatalf("HTTP=%d body=%s", w.Code, w.Body.String())
			}
			if backend.calls != tc.rounds*tc.batch || posts != tc.rounds+1 || router.calls != backend.calls {
				t.Fatalf("posts=%d search=%d route=%d body=%s", posts, backend.calls, router.calls, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "__cpa_bps_web_search") || strings.Contains(w.Body.String(), "run_officejs") {
				t.Fatal("internal search leaked")
			}
			if tc.rounds == 0 {
				return
			}
			if tc.stream {
				if strings.Count(w.Body.String(), `"type":"response.created"`) != 1 || strings.Count(w.Body.String(), `"type":"response.completed"`) != 1 {
					t.Fatalf("lifecycle=%s", w.Body.String())
				}
				if strings.Count(w.Body.String(), `"type":"response.output_text.delta"`) != 1 {
					t.Fatalf("text was not buffered: %s", w.Body.String())
				}
			}
			if tc.scenario == "no_citation" || tc.scenario == "prefix_url" {
				if strings.Contains(w.Body.String(), `url_citation`) {
					t.Fatal("false citation")
				}
			} else if !strings.Contains(w.Body.String(), `url_citation`) {
				t.Fatal("missing citation")
			}
			if !tc.stream {
				if ojson.GetBytes(w.Body.Bytes(), "usage.total_tokens").Int() != int64(posts*5) {
					t.Fatalf("usage=%s", w.Body.String())
				}
			}
		})
	}
}
