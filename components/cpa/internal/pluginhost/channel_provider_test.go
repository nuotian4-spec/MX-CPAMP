package pluginhost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func providerScopeFixture(t *testing.T, identity string) (*Host, *coreauth.Manager, *handlers.BaseAPIHandler, *atomic.Int32, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	host := NewForTest(newTestSymbolLoader())
	manager := coreauth.NewManager(nil, nil, nil)
	calls := &atomic.Int32{}
	model := t.Name() + "-model"
	rt := scopeRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.Method != http.MethodPost {
			t.Errorf("unexpected upstream method=%s", req.Method)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	invoke := func(req pluginapi.ExecutorRequest) {
		request, _ := http.NewRequest(http.MethodPost, "https://synthetic.invalid/responses", nil)
		response, err := rt.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if req.AuthProvider != "cpa-bps" {
			t.Errorf("credential provider changed: %s", req.AuthProvider)
		}
	}
	executor := &fakeExecutor{identifier: identity,
		execute: func(_ context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
			invoke(req)
			return pluginapi.ExecutorResponse{Payload: []byte(`{"id":"test","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)}, nil
		},
		countTokens: func(_ context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
			invoke(req)
			return pluginapi.ExecutorResponse{Payload: []byte(`{"input_tokens":1}`)}, nil
		},
		executeStream: func(_ context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
			invoke(req)
			ch := make(chan pluginapi.ExecutorStreamChunk, 1)
			ch <- pluginapi.ExecutorStreamChunk{Payload: []byte(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)}
			close(ch)
			return pluginapi.ExecutorStreamResponse{Chunks: ch}, nil
		},
	}
	id := "untrusted-plugin"
	if identity == "synthetic-distinct-provider" {
		id = "cpa-bps"
	}
	plugin := validTestPlugin("cpa-bps")
	plugin.Capabilities = pluginapi.Capabilities{Executor: executor, AuthProvider: fakeAuthProvider{identifier: "cpa-bps"}, ExecutorModelScope: pluginapi.ExecutorModelScopeOAuth, ExecutorInputFormats: []string{"openai"}, ExecutorOutputFormats: []string{"openai"}, ModelProvider: modelProviderFunc{
		staticModels: func(context.Context, pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
			return pluginapi.ModelResponse{Provider: "cpa-bps"}, nil
		},
		modelsForAuth: func(context.Context, pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
			return pluginapi.ModelResponse{Provider: "cpa-bps", Models: []pluginapi.ModelInfo{{ID: model}}}, nil
		},
	}}
	host.RegisterPluginForTest(id, plugin)
	host.RegisterModels(context.Background(), registry.GetGlobalRegistry())
	host.RegisterExecutors(manager, registry.GetGlobalRegistry())
	if _, ok := manager.Executor("cpa-bps"); !ok {
		t.Fatal("real Host.RegisterExecutors did not register cpa-bps")
	}
	for _, suffix := range []string{"-a", "-b"} {
		auth := &coreauth.Auth{ID: t.Name() + suffix, Provider: "cpa-bps", Status: coreauth.StatusActive}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatal(err)
		}
		result := host.ModelsForAuth(context.Background(), auth)
		if !result.Handled || len(result.Models) != 1 {
			t.Fatalf("per-auth catalog=%+v", result)
		}
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, result.Models)
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	}
	h := handlers.NewBaseAPIHandlers(&config.SDKConfig{}, manager)
	// No ModelRouter or direct-executor host is installed on the handler.
	return host, manager, h, calls, model
}

func TestScopedProviderRegisteredHostManagerDispatch(t *testing.T) {
	for _, identity := range []string{"synthetic-distinct-provider", "codex", "kilo", "unknown"} {
		for _, scope := range []string{"bps", "codex", "kilo", ""} {
			for _, mode := range []string{"execute", "count", "stream"} {
				t.Run(identity+"/"+scope+"/"+mode, func(t *testing.T) {
					_, manager, h, calls, model := providerScopeFixture(t, identity)
					ctx := channelscope.With(context.Background(), scope)
					wantAllowed := scope == "bps" && identity == "synthetic-distinct-provider" || scope == "" && identity != "kilo"
					providers := manager.FilterChannelProviders(ctx, []string{"cpa-bps"})
					if len(providers) == 1 && providers[0] != "cpa-bps" {
						t.Fatal("provider was renamed")
					}
					if (len(providers) == 1) != wantAllowed {
						t.Fatalf("filter=%v allowed=%v", providers, wantAllowed)
					}
					req := core.Request{Model: model, Payload: []byte(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`)}
					opts := core.Options{SourceFormat: "openai", ResponseFormat: "openai"}
					var err error
					switch mode {
					case "execute":
						_, err = manager.Execute(ctx, []string{"cpa-bps"}, req, opts)
					case "count":
						_, err = manager.ExecuteCount(ctx, []string{"cpa-bps"}, req, opts)
					case "stream":
						var result *core.StreamResult
						result, err = manager.ExecuteStream(ctx, []string{"cpa-bps"}, req, opts)
						if err == nil {
							for chunk := range result.Chunks {
								if chunk.Err != nil {
									err = chunk.Err
								}
							}
						}
					}
					if wantAllowed {
						if err != nil || calls.Load() != 1 {
							t.Fatalf("same-channel err=%v calls=%d", err, calls.Load())
						}
					} else {
						var scoped *coreauth.Error
						if !errors.As(err, &scoped) || scoped.HTTPStatus != http.StatusForbidden || calls.Load() != 0 {
							t.Fatalf("rejection err=%v calls=%d", err, calls.Load())
						}
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("GET", "/v1/models", nil).WithContext(ctx)
					doc := ojson.NewDocString("{}")
					_ = doc.Set("data", []any{map[string]any{"id": model}})
					h.WriteModelListResponse(c, "openai", doc.Bytes())
					if (len(ojson.GetBytes(rec.Body.Bytes(), "data").Array()) == 1) != wantAllowed {
						t.Fatalf("list disagrees: %s", rec.Body.String())
					}
					if got := h.HasAlternativeExecutionTarget(ctx, "openai", model, req.Payload, t.Name()+"-a"); got != wantAllowed {
						t.Fatalf("alternative=%v want=%v", got, wantAllowed)
					}
				})
			}
		}
	}
}

func TestScopedProviderRequiresLiveTrustedExecutor(t *testing.T) {
	host, manager, _, _, _ := providerScopeFixture(t, "synthetic-distinct-provider")
	ctx := channelscope.With(context.Background(), "bps")
	host.mu.Lock()
	host.activePluginVersions["cpa-bps"] = "replaced"
	host.mu.Unlock()
	if manager.ProviderChannelAllowed(ctx, "cpa-bps") {
		t.Fatal("stale host mapping granted provider access")
	}
	manager.UnregisterExecutor("cpa-bps")
	if manager.ProviderChannelAllowed(ctx, "cpa-bps") {
		t.Fatal("provider string alone granted BPS access")
	}
}
