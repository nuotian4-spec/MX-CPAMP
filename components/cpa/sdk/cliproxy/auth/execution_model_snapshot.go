package auth

import (
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// ResolveExecutionModelWithConfig uses the caller's immutable configuration for aliases.
// Credential-local and Home upstream mappings retain their normal precedence.
func (m *Manager) ResolveExecutionModelWithConfig(cfg *internalconfig.Config, selected *Auth, routeModel string) string {
	routeModel = strings.TrimSpace(routeModel)
	if m == nil {
		return routeModel
	}
	if selected != nil {
		if model := strings.TrimSpace(selected.Attributes[homeUpstreamModelAttributeKey]); model != "" {
			return model
		}
	}
	model := rewriteModelForAuth(routeModel, selected)
	if channel := modelAliasChannel(selected); channel != "" {
		alias, matched := resolveUpstreamModelFromAliases(OAuthModelAliasesFromAttributes(authAttributes(selected)), model)
		if !matched && cfg != nil {
			alias = resolveUpstreamModelFromCompiledAliasTable(compileOAuthModelAliasTable(cfg.OAuthModelAlias), model, channel)
		}
		if alias.UpstreamModel != "" {
			model = alias.UpstreamModel
		}
	}
	if pool := resolveOpenAICompatUpstreamModelPool(cfg, selected, model); len(pool) > 0 {
		return pool[m.nextModelPoolOffset(selected, openAICompatModelPoolKey(selected, model), len(pool))%len(pool)]
	}
	resolved := m.applyAPIKeyModelAliasWithRouting(&apiKeyModelRoutingSnapshot{config: cfg}, selected, model)
	if strings.TrimSpace(resolved) != "" {
		return resolved
	}
	return model
}
