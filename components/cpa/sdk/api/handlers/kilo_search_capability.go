package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
)

type kiloSearchRouteContextKey struct{}

// PrepareKiloAlphaExecution resolves and pins the same scoped target advertised
// by the client catalog. No route or executor is chosen from model names alone.
func (h *BaseAPIHandler) PrepareKiloAlphaExecution(ctx context.Context, model string, raw []byte) (context.Context, string, error) {
	target, ok := h.kiloScopedSearchModel(ctx, model, raw, false)
	if !ok {
		return ctx, "", fmt.Errorf("Kilo Alpha model is unknown, unsupported, or outside the authenticated channel")
	}
	return context.WithValue(ctx, kiloSearchRouteContextKey{}, modelRouteDecision{Provider: "kilo", Model: target}), target, nil
}

func (h *BaseAPIHandler) kiloScopedSearchModel(ctx context.Context, model string, raw []byte, discovery bool) (string, bool) {
	if h == nil || h.AuthManager == nil || channelscope.From(ctx) != "kilo" {
		return "", false
	}
	decision := h.applyModelRouter(ctx, "codex", model, raw, false, modelExecutionOptions{})
	if decision.ExecutorPluginID != "" || (decision.Provider != "" && decision.Provider != "kilo") {
		return "", false
	}
	if decision.Model != "" {
		model = decision.Model
	}
	model = strings.TrimSpace(model)
	return model, h.AuthManager.SupportsKiloSearchModel(ctx, thinking.ParseSuffix(model).ModelName, discovery)
}

// SupportsScopedKiloSearchModel is used while building the authenticated catalog.
func (h *BaseAPIHandler) SupportsScopedKiloSearchModel(ctx context.Context, model string, discovery bool) bool {
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("model", model)
	_, ok := h.kiloScopedSearchModel(ctx, model, doc.Bytes(), discovery)
	return ok
}
