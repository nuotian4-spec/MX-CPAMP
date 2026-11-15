package executor

import (
	"context"
	"strings"
	"time"

	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

func (e *KiloExecutor) ReconcileKiloAutoEgress(accounts map[string]kilo.EgressAccountStatus, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		return
	}
	before := e.runtime.StateView().Revision
	e.runtime.ReconcileAuto(accounts, now)
	if e.runtime.StateView().Revision != before {
		e.notifyLocked()
	}
}

func (e *KiloExecutor) KiloConfiguredIPCapacity(id string) (int, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.runtime == nil || e.initErr != nil {
		return 0, false
	}
	return e.runtime.ConfiguredIPCapacity(id)
}

// KiloCodexUsage follows the source credential's own management transport.
func (e *KiloExecutor) KiloCodexUsage(ctx context.Context, source *coreauth.Auth, token, account string) (usage.UsageObservation, error) {
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	raw := strings.TrimSpace(source.ProxyURL)
	if raw == "" && cfg != nil {
		raw = cfg.ProxyURL
	}
	transport, _, err := proxyutil.BuildHTTPTransport(raw)
	if err != nil {
		return usage.UsageObservation{}, usage.UsageError("query_egress_unavailable")
	}
	if transport != nil {
		transport.CloseIdleConnections()
	}
	client := helps.NewProxyAwareHTTPClient(ctx, cfg, source, 10*time.Second)
	return usage.FetchUsage(ctx, client, token, account)
}

// KiloManagementUsage holds a short proxy lease independently of inference admission.
func (e *KiloExecutor) KiloManagementUsage(ctx context.Context, accountID, token, outerAccount string) (usage.UsageObservation, error) {
	e.mu.Lock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		e.mu.Unlock()
		return usage.UsageObservation{}, usage.UsageError("query_egress_unavailable")
	}
	client, release, err := e.runtime.AcquireQuotaManagement(accountID)
	if err != nil {
		e.mu.Unlock()
		return usage.UsageObservation{}, usage.UsageError("query_egress_unavailable")
	}
	e.active++
	e.mu.Unlock()
	defer func() {
		release()
		e.mu.Lock()
		defer e.mu.Unlock()
		e.active--
		if e.active == 0 {
			e.applyPendingLocked()
		}
		e.notifyLocked()
	}()
	return usage.FetchUsage(ctx, client, token, outerAccount)
}
