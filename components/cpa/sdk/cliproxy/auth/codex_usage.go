package auth

import (
	"context"
	"strings"
	"time"

	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

// CodexUsageCredential is process-local material, never a public or persisted DTO.
type CodexUsageCredential struct {
	AuthID, AccountID, AccessToken       string
	RegistrationEpoch, CredentialVersion uint64
}

func codexUsageIdentity(a *Auth, expected string) string {
	if a == nil {
		return "source_missing"
	}
	if !strings.EqualFold(a.Provider, "codex") {
		return "source_changed"
	}
	account := authMetadataString(a, "account_id")
	if account == "" {
		return "identity_unknown"
	}
	if account != expected {
		return "source_changed"
	}
	return ""
}

func (m *Manager) codexUsageStatus(a *Auth, now time.Time) string {
	if a == nil || a.Disabled {
		return "unavailable"
	}
	if authMetadataString(a, "account_id") == "" {
		return "identity_unknown"
	}
	if a.RequiresSignIn {
		return "needs_sign_in"
	}
	_, grant, due := CodexOuterNeedsRefresh(a, now)
	if grant && due {
		return "refresh_required"
	}
	if cfg, _ := m.runtimeConfig.Load().(*config.Config); cfg != nil && cfg.Home.Enabled {
		exp, known := a.OuterExpirationTime()
		if authMetadataString(a, "access_token") == "" || (known && !exp.After(now.Add(CodexOuterRefreshLead))) {
			return "refresh_required"
		}
	}
	if authMetadataString(a, "access_token") == "" {
		return "no_outer_material"
	}
	if exp, ok := a.OuterExpirationTime(); ok && !exp.After(now) {
		return "no_outer_material"
	}
	return "ready"
}

// ResolveCodexUsageCredential reuses the original non-forced refresh transaction.
// Refresh keeps its own proxy, journal, epoch fencing, SIWC and Home branches.
func (m *Manager) ResolveCodexUsageCredential(ctx context.Context, id, expectedAccount string) (CodexUsageCredential, error) {
	var empty CodexUsageCredential
	a, _ := m.GetByID(id)
	if code := codexUsageIdentity(a, expectedAccount); code != "" {
		return empty, usage.UsageError(code)
	}
	if a.Disabled {
		return empty, usage.UsageError("source_unavailable")
	}
	epoch := a.RegistrationEpoch
	now := time.Now()
	_, _, outerDue := CodexOuterNeedsRefresh(a, now)
	_, _, siwcDue := CodexSIWCNeedsRefresh(a, now)
	cfg, _ := m.runtimeConfig.Load().(*config.Config)
	home := cfg != nil && cfg.Home.Enabled
	homeDue := false
	if home {
		exp, ok := a.OuterExpirationTime()
		homeDue = authMetadataString(a, "access_token") == "" || (ok && !exp.After(now.Add(CodexOuterRefreshLead)))
	}
	if (outerDue || siwcDue || homeDue) && m.shouldRefresh(a, now) {
		if !home {
			raw := strings.TrimSpace(a.ProxyURL)
			if raw == "" && cfg != nil {
				raw = cfg.ProxyURL
			}
			transport, _, err := proxyutil.BuildHTTPTransport(raw)
			if err != nil {
				return empty, usage.UsageError("refresh_proxy_invalid")
			}
			if transport != nil {
				transport.CloseIdleConnections()
			}
		}
		if _, err := m.refreshAuthForRequestAtEpoch(ctx, id, "", epoch); err != nil {
			latest, _ := m.GetByID(id)
			if codexUsageIdentity(latest, expectedAccount) == "" && latest.RegistrationEpoch == epoch {
				empty = codexUsageSnapshot(latest, expectedAccount)
			}
			if ctx.Err() != nil {
				return empty, usage.UsageError("usage_cancelled")
			}
			return empty, usage.UsageError("refresh_failed")
		}
	}
	a, _ = m.GetByID(id)
	if code := codexUsageIdentity(a, expectedAccount); code != "" {
		return empty, usage.UsageError(code)
	}
	if a.RegistrationEpoch != epoch {
		return empty, usage.UsageError("source_changed")
	}
	current := codexUsageSnapshot(a, expectedAccount)
	if ctx.Err() != nil {
		return current, usage.UsageError("usage_cancelled")
	}
	if a.Disabled {
		return current, usage.UsageError("source_unavailable")
	}
	token := authMetadataString(a, "access_token")
	if token == "" {
		return current, usage.UsageError("no_outer_material")
	}
	if exp, ok := a.OuterExpirationTime(); ok && !exp.After(time.Now()) {
		return current, usage.UsageError("no_outer_material")
	}
	if token == a.RejectedAccessToken {
		return current, usage.UsageError("source_unavailable")
	}
	return current, nil
}

func codexUsageSnapshot(a *Auth, account string) CodexUsageCredential {
	return CodexUsageCredential{a.ID, account, authMetadataString(a, "access_token"), a.RegistrationEpoch, a.CredentialVersion}
}
