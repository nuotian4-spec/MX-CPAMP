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

type scopeRoundTripper func(*http.Request) (*http.Response, error)

func (f scopeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func scopedPluginFixture(t *testing.T) (*Host, *handlers.BaseAPIHandler, map[string]*atomic.Int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	host := NewForTest(newTestSymbolLoader())
	counters := map[string]*atomic.Int32{}
	routes := map[string]string{"alias-bps": "cpa-bps", "alias-codex": "codex-executor", "alias-kilo": "kilo-executor", "alias-unknown": "unknown-executor", "alias-cross": "codex-executor"}
	for _, channel := range []string{"bps", "codex", "kilo", "unknown"} {
		pluginID := channel + "-executor"
		if channel == "bps" {
			pluginID = "cpa-bps"
		}
		count := &atomic.Int32{}
		counters[channel] = count
		rt := scopeRoundTripper(func(req *http.Request) (*http.Response, error) {
			count.Add(1)
			if req.Method != "POST" {
				t.Errorf("method=%s", req.Method)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{\"id\":\"synthetic\",\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}"))}, nil
		})
		upstream := func(ctx context.Context) ([]byte, error) {
			req, _ := http.NewRequestWithContext(ctx, "POST", "https://synthetic.invalid/responses", nil)
			resp, err := rt.RoundTrip(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			return io.ReadAll(resp.Body)
		}
		identifier := channel
		if channel == "bps" {
			identifier = "synthetic-provider-distinct-from-bps"
		}
		exec := &fakeExecutor{identifier: identifier,
			execute: func(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
				body, err := upstream(ctx)
				return pluginapi.ExecutorResponse{Payload: body}, err
			},
			countTokens: func(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
				_, err := upstream(ctx)
				return pluginapi.ExecutorResponse{Payload: []byte("{\"input_tokens\":1}")}, err
			},
			executeStream: func(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
				_, err := upstream(ctx)
				if err != nil {
					return pluginapi.ExecutorStreamResponse{}, err
				}
				ch := make(chan pluginapi.ExecutorStreamChunk, 1)
				ch <- pluginapi.ExecutorStreamChunk{Payload: []byte("{\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}")}
				close(ch)
				return pluginapi.ExecutorStreamResponse{Chunks: ch}, nil
			},
		}
		plugin := validTestPlugin("display-name-is-not-identity")
		if channel == "unknown" {
			plugin.Metadata.Name = "cpa-bps"
		}
		plugin.Capabilities = pluginapi.Capabilities{Executor: exec, ExecutorInputFormats: []string{"openai"}, ExecutorOutputFormats: []string{"openai"}, ModelRouter: modelRouterFunc(func(_ context.Context, req pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, error) {
			target, ok := routes[req.RequestedModel]
			if !ok {
				return pluginapi.ModelRouteResponse{}, nil
			}
			return pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetExecutor, Target: target}, nil
		})}
		if !validPlugin(plugin) {
			t.Fatal("synthetic registration metadata invalid")
		}
		host.RegisterPluginForTest(pluginID, plugin)
	}
	h := handlers.NewBaseAPIHandlers(&config.SDKConfig{}, coreauth.NewManager(nil, nil, nil))
	h.SetPluginHost(host)
	h.SetModelRouterHost(host)
	return host, h, counters
}
func invokeScopedHandler(h *handlers.BaseAPIHandler, ctx context.Context, mode, model string) int {
	payload := []byte("{\"model\":\"" + model + "\",\"messages\":[{\"role\":\"user\",\"content\":\"synthetic\"}]}")
	switch mode {
	case "execute":
		_, _, err := h.ExecuteWithAuthManager(ctx, "openai", model, payload, "")
		if err != nil {
			return err.StatusCode
		}
	case "count":
		_, _, err := h.ExecuteCountWithAuthManager(ctx, "openai", model, payload, "")
		if err != nil {
			return err.StatusCode
		}
	case "stream":
		data, _, errs := h.ExecuteStreamWithAuthManager(ctx, "openai", model, payload, "")
		for data != nil || errs != nil {
			select {
			case _, ok := <-data:
				if !ok {
					data = nil
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
				} else if err != nil {
					return err.StatusCode
				}
			}
		}
	}
	return 200
}
func TestScopedPluginActualHostDispatch(t *testing.T) {
	for _, scope := range []string{"bps", "codex", "kilo"} {
		for _, target := range []string{"bps", "codex", "kilo", "unknown"} {
			for _, mode := range []string{"execute", "count", "stream"} {
				t.Run(scope+"/"+target+"/"+mode, func(t *testing.T) {
					_, h, counters := scopedPluginFixture(t)
					ctx := channelscope.With(context.Background(), scope)
					status := invokeScopedHandler(h, ctx, mode, "alias-"+target)
					want := 403
					if scope == target {
						want = 200
					}
					if status != want {
						t.Fatalf("status=%d want=%d", status, want)
					}
					wantCalls := int32(0)
					if want == 200 {
						wantCalls = 1
					}
					if counters[target].Load() != wantCalls {
						t.Fatalf("upstream=%d", counters[target].Load())
					}
				})
			}
		}
	}
}
func TestScopedPluginListMatchesFinalAliasExecutor(t *testing.T) {
	_, h, _ := scopedPluginFixture(t)
	id := t.Name()
	registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: "alias-cross"}, {ID: "alias-unknown"}})
	defer registry.GetGlobalRegistry().UnregisterClient(id)
	for _, scope := range []string{"bps", "codex", "kilo"} {
		ctx := channelscope.With(context.Background(), scope)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/v1/models", nil).WithContext(ctx)
		c.Request.Header.Set("X-Channel", "bps")
		h.WriteModelListResponse(c, "openai", []byte("{\"data\":[{\"id\":\"alias-bps\"},{\"id\":\"alias-codex\"},{\"id\":\"alias-kilo\"},{\"id\":\"alias-unknown\"},{\"id\":\"alias-cross\"}]}"))
		got := map[string]bool{}
		for _, model := range ojson.GetBytes(rec.Body.Bytes(), "data").Array() {
			got[model.Get("id").String()] = true
		}
		if !got["alias-"+scope] || got["alias-unknown"] || got["alias-cross"] != (scope == "codex") {
			t.Fatalf("scope=%s models=%v", scope, got)
		}
		want := 403
		if scope == "codex" {
			want = 200
		}
		if status := invokeScopedHandler(h, ctx, "execute", "alias-cross"); status != want {
			t.Fatalf("final alias status=%d", status)
		}
	}
}
func TestScopedPluginIgnoresCatalogProviderAndDisplayIdentity(t *testing.T) {
	host, _, counters := scopedPluginFixture(t)
	host.mu.Lock()
	host.modelProviders["unknown-executor"] = "bps"
	host.mu.Unlock()
	if _, ok := host.PluginExecutorChannel("unknown-executor"); ok {
		t.Fatal("catalog provider became trusted identity")
	}
	opts := core.Options{SourceFormat: "openai", ResponseFormat: "openai", Headers: http.Header{"X-Channel": {"bps"}, "X-Provider": {"bps"}}}
	_, err := host.ExecutePluginExecutor(channelscope.With(context.Background(), "bps"), "unknown-executor", core.Request{Model: "bps-prefix-model", Payload: []byte("{}")}, opts)
	if err == nil || counters["unknown"].Load() != 0 {
		t.Fatal("unknown executor bypassed final adapter check")
	}
	host.mu.Lock()
	host.activePluginVersions["cpa-bps"] = "replaced"
	host.mu.Unlock()
	if _, ok := host.PluginExecutorChannel("cpa-bps"); ok {
		t.Fatal("stale registration was trusted")
	}
}
func TestScopedPluginLegacyUnscopedDispatchPreserved(t *testing.T) {
	_, h, counters := scopedPluginFixture(t)
	for _, target := range []string{"bps", "codex", "kilo", "unknown"} {
		wantStatus, wantCalls := http.StatusOK, int32(3)
		if target == "kilo" {
			wantStatus, wantCalls = http.StatusForbidden, 0
		}
		for _, mode := range []string{"execute", "count", "stream"} {
			if status := invokeScopedHandler(h, context.Background(), mode, "alias-"+target); status != wantStatus {
				t.Fatalf("旧 Key %s/%s：状态=%d，预期=%d", target, mode, status, wantStatus)
			}
		}
		if calls := counters[target].Load(); calls != wantCalls {
			t.Fatalf("旧 Key %s：上游次数=%d，预期=%d", target, calls, wantCalls)
		}
	}
}

func TestScopedPluginLegacyUnscopedDirectHostBoundary(t *testing.T) {
	host, _, counters := scopedPluginFixture(t)
	ctx := context.Background()
	req := core.Request{Model: "legacy-model", Payload: []byte("{}")}
	opts := core.Options{SourceFormat: "openai", ResponseFormat: "openai"}
	for channel, target := range map[string]string{"bps": "cpa-bps", "codex": "codex-executor", "kilo": "kilo-executor", "unknown": "unknown-executor"} {
		_, executeErr := host.ExecutePluginExecutor(ctx, target, req, opts)
		_, countErr := host.CountPluginExecutor(ctx, target, req, opts)
		stream, streamErr := host.ExecutePluginExecutorStream(ctx, target, req, opts)
		if streamErr == nil {
			for range stream.Chunks {
			}
		}
		for mode, err := range map[string]error{"execute": executeErr, "count": countErr, "stream": streamErr} {
			if channel == "kilo" {
				var scopeErr *coreauth.Error
				if !errors.As(err, &scopeErr) || scopeErr.HTTPStatus != http.StatusForbidden {
					t.Fatalf("旧 Key 直接宿主 %s：预期403，错误=%v", mode, err)
				}
			} else if err != nil {
				t.Fatalf("旧 Key 直接宿主 %s/%s：%v", channel, mode, err)
			}
		}
		wantCalls := int32(3)
		if channel == "kilo" {
			wantCalls = 0
		}
		if calls := counters[channel].Load(); calls != wantCalls {
			t.Fatalf("旧 Key 直接宿主 %s：上游次数=%d，预期=%d", channel, calls, wantCalls)
		}
	}
}

func TestScopedPluginLegacyUnscopedListMatchesExecutor(t *testing.T) {
	_, h, _ := scopedPluginFixture(t)
	client := t.Name()
	registry.GetGlobalRegistry().RegisterClient(client, "bps", []*registry.ModelInfo{{ID: "alias-kilo"}, {ID: "alias-unknown"}})
	defer registry.GetGlobalRegistry().UnregisterClient(client)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)
	h.WriteModelListResponse(c, "openai", []byte("{\"data\":[{\"id\":\"alias-bps\"},{\"id\":\"alias-codex\"},{\"id\":\"alias-kilo\"},{\"id\":\"alias-unknown\"},{\"id\":\"alias-cross\"}]}"))
	got := map[string]bool{}
	for _, model := range ojson.GetBytes(rec.Body.Bytes(), "data").Array() {
		got[model.Get("id").String()] = true
	}
	if len(got) != 4 || got["alias-kilo"] || !got["alias-bps"] || !got["alias-codex"] || !got["alias-unknown"] || !got["alias-cross"] {
		t.Fatalf("旧 Key 模型列表与最终执行器不符：%v", got)
	}
}

func TestScopedPluginLegacyProviderListUsesExecutorIdentity(t *testing.T) {
	for _, channel := range []string{"bps", "codex", "kilo", "unknown"} {
		t.Run(channel, func(t *testing.T) {
			host, h, _ := scopedPluginFixture(t)
			pluginID := channel + "-executor"
			if channel == "bps" {
				pluginID = "cpa-bps"
			}
			adapter, err := host.executorAdapterForPlugin(context.Background(), pluginID, "")
			if err != nil {
				t.Fatal(err)
			}
			adapter.provider = "bps"
			h.AuthManager.RegisterExecutor(adapter)
			client := t.Name()
			registry.GetGlobalRegistry().RegisterClient(client, "bps", []*registry.ModelInfo{{ID: "legacy-catalog-label-bps"}})
			defer registry.GetGlobalRegistry().UnregisterClient(client)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("GET", "/v1/models", nil)
			h.WriteModelListResponse(c, "openai", []byte("{\"data\":[{\"id\":\"legacy-catalog-label-bps\"}]}"))
			wantCount := 1
			if channel == "kilo" {
				wantCount = 0
			}
			if count := len(ojson.GetBytes(rec.Body.Bytes(), "data").Array()); count != wantCount {
				t.Fatalf("旧 Key %s：列表条数=%d，预期=%d", channel, count, wantCount)
			}
		})
	}
}

func TestScopedPluginAdapterBoundaryForAllDirectMethods(t *testing.T) {
	host, _, counters := scopedPluginFixture(t)
	opts := core.Options{SourceFormat: "openai", ResponseFormat: "openai", Headers: http.Header{"X-Channel": {"bps"}, "X-Provider": {"bps"}}}
	req := core.Request{Model: "bps-model", Payload: []byte("{}")}
	ctx := channelscope.With(context.Background(), "bps")
	for _, target := range []string{"codex-executor", "unknown-executor"} {
		if _, err := host.ExecutePluginExecutor(ctx, target, req, opts); err == nil {
			t.Fatal("execute bypass")
		}
		if _, err := host.CountPluginExecutor(ctx, target, req, opts); err == nil {
			t.Fatal("count bypass")
		}
		if _, err := host.ExecutePluginExecutorStream(ctx, target, req, opts); err == nil {
			t.Fatal("stream bypass")
		}
	}
	if counters["codex"].Load() != 0 || counters["unknown"].Load() != 0 {
		t.Fatal("rejected upstream invoked")
	}
}

func TestScopedPluginCatalogChecksActualRegisteredAdapter(t *testing.T) {
	host, h, _ := scopedPluginFixture(t)
	adapter, err := host.executorAdapterForPlugin(context.Background(), "codex-executor", "")
	if err != nil {
		t.Fatal(err)
	}
	adapter.provider = "bps"
	h.AuthManager.RegisterExecutor(adapter)
	client := t.Name()
	registry.GetGlobalRegistry().RegisterClient(client, "bps", []*registry.ModelInfo{{ID: "catalog-label-bps"}})
	defer registry.GetGlobalRegistry().UnregisterClient(client)
	for _, scope := range []string{"bps", "codex", "kilo"} {
		ctx := channelscope.With(context.Background(), scope)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/v1/models", nil).WithContext(ctx)
		h.WriteModelListResponse(c, "openai", []byte("{\"data\":[{\"id\":\"catalog-label-bps\"}]}"))
		if len(ojson.GetBytes(rec.Body.Bytes(), "data").Array()) != 0 {
			t.Fatal("model label overrode actual executor channel")
		}
	}
}

func TestScopedPluginBPSHostMappingUsesRegisteredRecord(t *testing.T) {
	host, _, _ := scopedPluginFixture(t)
	adapter, err := host.executorAdapterForPlugin(context.Background(), "cpa-bps", "")
	if err != nil {
		t.Fatal(err)
	}
	if adapter.executor.Identifier() == "bps" {
		t.Fatal("合成执行器标识应与通道 bps 不同")
	}
	if channel, known := host.PluginExecutorChannel("cpa-bps"); !known || channel != "bps" {
		t.Fatalf("宿主映射=%q，已知=%v", channel, known)
	}
	if channel, known := adapter.ExecutionChannel(); !known || channel != "bps" {
		t.Fatalf("最终适配器映射=%q，已知=%v", channel, known)
	}
}

func TestScopedPluginBPSMappingRejectsMissingExecutor(t *testing.T) {
	host := NewForTest(newTestSymbolLoader())
	plugin := validTestPlugin("cpa-bps")
	plugin.Capabilities.Executor = nil
	host.RegisterPluginForTest("cpa-bps", plugin)
	if _, known := host.PluginExecutorChannel("cpa-bps"); known {
		t.Fatal("无执行器能力的记录获得通道权限")
	}
	if _, err := host.executorAdapterForPlugin(context.Background(), "cpa-bps", ""); err == nil {
		t.Fatal("无执行器能力的记录获得适配器")
	}
}

func TestScopedPluginBPSMappingRejectsStaleAndMismatchedAdapter(t *testing.T) {
	for _, scope := range []string{"", "bps"} {
		for _, change := range []string{"path", "version", "executor"} {
			t.Run(scope+"/"+change, func(t *testing.T) {
				host, _, counters := scopedPluginFixture(t)
				adapter, err := host.executorAdapterForPlugin(context.Background(), "cpa-bps", "")
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "path":
					host.mu.Lock()
					host.activePluginPaths["cpa-bps"] = "test/replaced"
					host.mu.Unlock()
				case "version":
					host.mu.Lock()
					host.activePluginVersions["cpa-bps"] = "replaced"
					host.mu.Unlock()
				case "executor":
					adapter.executor = &fakeExecutor{identifier: "bps"}
				}
				if _, known := adapter.ExecutionChannel(); known {
					t.Fatal("失效记录或不同执行器实例获得通道权限")
				}
				ctx := channelscope.With(context.Background(), scope)
				req := core.Request{Model: "legacy-model", Payload: []byte("{}")}
				opts := core.Options{SourceFormat: "openai", ResponseFormat: "openai"}
				_, executeErr := adapter.Execute(ctx, nil, req, opts)
				_, countErr := adapter.CountTokens(ctx, nil, req, opts)
				_, streamErr := adapter.ExecuteStream(ctx, nil, req, opts)
				if executeErr == nil || countErr == nil || streamErr == nil || counters["bps"].Load() != 0 {
					t.Fatal("失效记录或不同执行器实例通过最终分发边界")
				}
			})
		}
	}
}
