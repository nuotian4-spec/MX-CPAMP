package auth

import (
	"context"
	"time"

	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
)

type kiloSourceQueryKey struct {
	kind, source, account, token string
	epoch, version               uint64
}
type kiloSourceQueryResult struct {
	observation        usage.UsageObservation
	err                error
	started, completed time.Time
}
type kiloSourceFlight struct {
	done   chan struct{}
	result kiloSourceQueryResult
}
type kiloSourceCycleKey struct{}
type kiloSourceCycle map[kiloSourceQueryKey]kiloSourceQueryResult

type kiloManagementUsageReader interface {
	KiloManagementUsage(context.Context, string, string, string) (usage.UsageObservation, error)
}
type kiloCodexUsageReader interface {
	KiloCodexUsage(context.Context, *Auth, string, string) (usage.UsageObservation, error)
}

// queryKiloSource coalesces a source, not a target policy, and retains actual I/O time.
func (m *Manager) queryKiloSource(ctx context.Context, id, kind string, material CodexUsageCredential) kiloSourceQueryResult {
	key := kiloSourceQueryKey{kind: kind, source: material.AuthID, account: material.AccountID, token: material.AccessToken, epoch: material.RegistrationEpoch, version: material.CredentialVersion}
	cycle, _ := ctx.Value(kiloSourceCycleKey{}).(kiloSourceCycle)
	m.mu.Lock()
	if cached, ok := cycle[key]; ok {
		m.mu.Unlock()
		return cached
	}
	if f := m.kiloSourceFlights[key]; f != nil {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return kiloSourceQueryResult{err: usage.UsageError("usage_cancelled"), started: time.Now(), completed: time.Now()}
		case <-f.done:
			return f.result
		}
	}
	if m.kiloSourceFlights == nil {
		m.kiloSourceFlights = map[kiloSourceQueryKey]*kiloSourceFlight{}
	}
	f := &kiloSourceFlight{done: make(chan struct{})}
	m.kiloSourceFlights[key] = f
	var source *Auth
	if kind == "cpa_codex" {
		source = m.auths[material.AuthID].Clone()
	}
	m.mu.Unlock()
	result := kiloSourceQueryResult{started: time.Now().UTC()}
	e, _ := m.Executor("kilo")
	if kind == "cpa_codex" {
		if reader, ok := e.(kiloCodexUsageReader); ok && source != nil {
			result.observation, result.err = reader.KiloCodexUsage(ctx, source, material.AccessToken, material.AccountID)
		} else if reader, ok := e.(kiloUsageReader); ok {
			result.observation, result.err = reader.KiloUsage(ctx, id, material.AccessToken, material.AccountID)
		} else {
			result.err = usage.UsageError("query_egress_unavailable")
		}
	} else if reader, ok := e.(kiloManagementUsageReader); ok {
		result.observation, result.err = reader.KiloManagementUsage(ctx, id, material.AccessToken, material.AccountID)
	} else if reader, ok := e.(kiloUsageReader); ok {
		result.observation, result.err = reader.KiloUsage(ctx, id, material.AccessToken, material.AccountID)
	} else {
		result.err = usage.UsageError("query_egress_unavailable")
	}
	result.completed = time.Now().UTC()
	m.mu.Lock()
	f.result = result
	delete(m.kiloSourceFlights, key)
	if cycle != nil {
		cycle[key] = result
	}
	close(f.done)
	m.mu.Unlock()
	return result
}
