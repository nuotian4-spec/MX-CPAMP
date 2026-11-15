package auth

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"strings"
)

type executionChannelIdentity interface{ ExecutionChannel() (string, bool) }

// ProviderChannelAllowed preserves the provider used to select credentials while
// resolving the explicitly supported host identity through its live executor.
func (m *Manager) ProviderChannelAllowed(ctx context.Context, provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if executor, exists := m.Executor(provider); exists {
		if identity, ok := executor.(executionChannelIdentity); ok {
			channel, known := identity.ExecutionChannel()
			scope := channelscope.From(ctx)
			if scope == "" {
				return (!known || channel != "kilo") && channelscope.Allows(ctx, provider)
			}
			if !known || channel != scope {
				return false
			}
			return channelscope.Allows(ctx, provider) || (provider == "cpa-bps" && channel == "bps")
		}
	}
	return channelscope.Allows(ctx, provider)
}

// FilterChannelProviders never rewrites a provider or changes account ownership.
func (m *Manager) FilterChannelProviders(ctx context.Context, providers []string) []string {
	result := make([]string, 0, len(providers))
	for _, provider := range providers {
		if m.ProviderChannelAllowed(ctx, provider) {
			result = append(result, provider)
		}
	}
	return result
}
