package handlers

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestKiloSearchA2ScopedAliasAndPrivateRoute(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	cfg := &config.Config{Kilo: config.KiloConfig{StateDir: t.TempDir()}}
	manager.SetConfig(cfg)
	manager.RegisterExecutor(executor.NewKiloExecutor(cfg))
	a, err := manager.Register(context.Background(), &coreauth.Auth{ID: "a2-scoped-alias", Provider: "kilo", Status: coreauth.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient(a.ID, "kilo", []*registry.ModelInfo{{ID: "gpt-6.1-sol"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
	h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	provider, kind := "kilo", pluginapi.ModelRouteTargetProvider
	calls := 0
	h.SetModelRouterHost(&handlerRouterOnlyTestHost{hasRouters: true, route: func(ctx context.Context, req pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, bool) {
		calls++
		if req.RequestedModel != "a2-alias" {
			return pluginapi.ModelRouteResponse{}, false
		}
		return pluginapi.ModelRouteResponse{Handled: true, TargetKind: kind, Target: provider, TargetModel: "gpt-6.1-sol"}, true
	}})
	ctx := channelscope.With(context.Background(), "kilo")
	if !h.SupportsScopedKiloSearchModel(ctx, "a2-alias", false) || !h.SupportsScopedKiloSearchModel(ctx, "a2-alias", true) {
		t.Fatal("verified alias not advertised")
	}
	prepared, target, err := h.PrepareKiloAlphaExecution(ctx, "a2-alias", []byte(`{"model":"a2-alias"}`))
	if err != nil || target != "gpt-6.1-sol" {
		t.Fatalf("target=%s err=%v", target, err)
	}
	priorCalls := calls
	provider = "codex"
	decision := h.applyModelRouter(prepared, "codex", target, nil, false, modelExecutionOptions{ForcedProvider: "kilo"})
	if decision.Provider != "kilo" || decision.Model != target || calls != priorCalls {
		t.Fatal("private prepared route was re-resolved")
	}
	if h.SupportsScopedKiloSearchModel(ctx, "a2-alias", false) {
		t.Fatal("cross-channel alias advertised")
	}
	provider = "kilo"
	kind = pluginapi.ModelRouteTargetExecutor
	if h.SupportsScopedKiloSearchModel(ctx, "a2-alias", false) {
		t.Fatal("unverified plugin executor advertised")
	}
	if h.SupportsScopedKiloSearchModel(channelscope.With(ctx, "codex"), "gpt-6.1-sol", false) || h.SupportsScopedKiloSearchModel(ctx, "unknown-model", false) {
		t.Fatal("wrong scope or unknown model advertised")
	}
	forged := context.WithValue(ctx, "kiloSearchRouteContextKey", modelRouteDecision{Provider: "kilo"})
	if h.applyModelRouter(forged, "codex", "a2-alias", nil, false, modelExecutionOptions{}).ExecutorPluginID != "kilo" {
		t.Fatal("external string marker bypassed router")
	}
}
