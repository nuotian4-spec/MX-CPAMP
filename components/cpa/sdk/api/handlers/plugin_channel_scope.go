package handlers

import (
	"context"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

type pluginChannelResolver interface{ PluginExecutorChannel(string) (string, bool) }

func (h *BaseAPIHandler) pluginChannelAllowed(ctx context.Context, pluginID string) bool {
	scope := channelscope.From(ctx)
	resolver, ok := h.pluginExecutorHost().(pluginChannelResolver)
	if !ok {
		return scope == ""
	}
	channel, known := resolver.PluginExecutorChannel(pluginID)
	if scope == "" {
		return !known || channel != "kilo"
	}
	return known && channel == scope
}

func (h *BaseAPIHandler) providerChannelAllowed(ctx context.Context, provider string) bool {
	if h.AuthManager != nil {
		return h.AuthManager.ProviderChannelAllowed(ctx, provider)
	}
	return channelscope.Allows(ctx, provider)
}

func (h *BaseAPIHandler) modelChannelAllowed(ctx context.Context, sourceFormat, model string) bool {
	doc := ojson.NewDocString("{}")
	_ = doc.Set("model", model)
	decision := h.applyModelRouter(ctx, sourceFormat, model, doc.Bytes(), false, modelExecutionOptions{})
	if decision.ExecutorPluginID != "" {
		return h.pluginChannelAllowed(ctx, decision.ExecutorPluginID)
	}
	if decision.Provider != "" {
		return h.providerChannelAllowed(ctx, decision.Provider)
	}
	providers := registry.GetGlobalRegistry().GetModelProviders(model)
	if len(providers) == 0 {
		return channelscope.From(ctx) == ""
	}
	for _, provider := range providers {
		if h.providerChannelAllowed(ctx, provider) {
			return true
		}
	}
	return false
}
