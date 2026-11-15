package openai

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	codexmodels "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/models"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	responsesclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/responses"
)

func (h *OpenAIAPIHandler) codexClientModelsResponse(clientVersion ...string) map[string]any {
	return h.codexClientModelsResponseForContext(context.Background(), clientVersion...)
}

func (h *OpenAIAPIHandler) codexClientModelsResponseForContext(ctx context.Context, clientVersion ...string) map[string]any {
	version := ""
	if len(clientVersion) > 0 {
		version = clientVersion[0]
	}
	optimizeMultiAgentV2 := false
	var applyPatchCapabilityForModel codexmodels.ApplyPatchCapabilityForModelFunc
	if h != nil {
		if cfg := h.CurrentConfig(); cfg != nil {
			optimizeMultiAgentV2 = cfg.Client.Codex.OptimizeMultiAgentV2
			if cfg.Client.Codex.EnableApplyPatch {
				applyPatchCapabilityForModel = h.SupportsApplyPatchModel
			}
		}
	}
	var toolSearchCapabilityForModel codexmodels.ToolSearchCapabilityForModelFunc
	if h != nil && responsesclaude.ClientToolSearchEnabled() {
		toolSearchCapabilityForModel = h.SupportsNativeToolSearchModel
	}
	modelRegistry := registry.GetGlobalRegistry()
	if channelscope.From(ctx) == "kilo" {
		models := make([]map[string]any, 0)
		for _, model := range h.Models() {
			id, _ := model["id"].(string)
			if h.SupportsScopedKiloSearchModel(ctx, id, false) {
				models = append(models, model)
			}
		}
		providers := func(id string) []string {
			if h.SupportsScopedKiloSearchModel(ctx, id, false) {
				return []string{"kilo"}
			}
			return nil
		}
		toolSearch := func(id string) bool { return h.SupportsScopedKiloSearchModel(ctx, id, true) }
		webSearch := func(id string) *bool { supported := h.SupportsScopedKiloSearchModel(ctx, id, false); return &supported }
		return codexmodels.BuildResponseForClientWithToolCapabilities(models, providers, webSearch, applyPatchCapabilityForModel, toolSearch, optimizeMultiAgentV2, version)
	}
	return codexmodels.BuildResponseForClientWithToolCapabilities(h.Models(), modelRegistry.GetModelProviders, modelRegistry.GetResponsesWebSearchCapability, applyPatchCapabilityForModel, toolSearchCapabilityForModel, optimizeMultiAgentV2, version)
}

// CodexClientModelsResponse builds a Codex client model response.
func CodexClientModelsResponse(models []map[string]any) map[string]any {
	return codexmodels.BuildResponse(models, nil, false)
}

// CodexClientModelsResponseWithMultiAgentV2 builds a Codex client model response
// and advertises multi-agent v2 for synthesized models when enabled.
func CodexClientModelsResponseWithMultiAgentV2(models []map[string]any, enabled bool) map[string]any {
	return codexmodels.BuildResponse(models, nil, enabled)
}

// CodexClientModelsResponseForClient builds a Codex client model response
// tailored for a specific client version.
func CodexClientModelsResponseForClient(models []map[string]any, clientVersion string, enabled bool) map[string]any {
	return codexmodels.BuildResponseForClient(models, nil, enabled, clientVersion)
}
