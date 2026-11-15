package auth

import (
	"context"
	"math"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

// LockKiloConfigUpdate serializes a bootstrap configuration write with the first
// durable pool allocation, so a successful config edit cannot race pool ownership.
func (m *Manager) LockKiloConfigUpdate() func() {
	m.concurrencyPickMu.Lock()
	return m.concurrencyPickMu.Unlock
}

// EnsureKiloExecutor shares one pool between zero-account management and Service.
// The factory runs only when there is no existing executor.
func (m *Manager) EnsureKiloExecutor(factory func() ProviderExecutor) ProviderExecutor {
	m.concurrencyPickMu.Lock()
	defer m.concurrencyPickMu.Unlock()
	if e, ok := m.Executor("kilo"); ok {
		return e
	}
	e := factory()
	if checker, ok := e.(interface{ PendingKiloDeviceSaves() []string }); ok && len(checker.PendingKiloDeviceSaves()) > 0 {
		ctx := context.Background()
		if err := m.authLoadGate.Acquire(ctx, math.MaxInt64); err == nil {
			m.mu.RLock()
			store := m.store
			m.mu.RUnlock()
			if store != nil {
				if items, listErr := store.List(ctx); listErr == nil {
					_ = m.reconcileKiloDeviceSaves(e, items)
				}
			}
			m.authLoadGate.Release(math.MaxInt64)
		}
	}
	m.RegisterExecutor(e)
	return e
}

// ChangeKiloEgress fences selected snapshots and both account/proxy acquisition.
func (m *Manager) ChangeKiloEgress(c kilo.ProxyChanges) (kilo.ProxyStateView, error) {
	return m.changeKiloEgress(c, nil, nil)
}

func (m *Manager) ChangeKiloEgressObserved(ctx context.Context, c kilo.ProxyChanges) (kilo.ProxyStateView, error) {
	if !c.Create && c.URL == nil {
		return m.ChangeKiloEgress(c)
	}
	m.concurrencyPickMu.Lock()
	e, _ := m.Executor("kilo")
	writer, ok := e.(interface {
		PrepareKiloProxyChange(kilo.ProxyChanges) (kilo.ProxyChangeObserve, error)
	})
	if !ok {
		m.concurrencyPickMu.Unlock()
		return kilo.ProxyStateView{}, kilo.ErrStateStorage
	}
	observe, err := writer.PrepareKiloProxyChange(c)
	m.concurrencyPickMu.Unlock()
	if err != nil {
		return kilo.ProxyStateView{}, err
	}
	commit, err := observe(ctx)
	if err != nil {
		return kilo.ProxyStateView{}, err
	}
	return m.changeKiloEgress(c, e, commit)
}

func (m *Manager) changeKiloEgress(c kilo.ProxyChanges, expected ProviderExecutor, commit kilo.ProxyChangeCommit) (kilo.ProxyStateView, error) {
	m.concurrencyPickMu.Lock()
	defer m.concurrencyPickMu.Unlock()
	m.mu.RLock()

	accounts := map[string]kilo.EgressAccountStatus{}
	for id, a := range m.auths {
		if a != nil {
			_, manual := credentialConcurrencyLimit(a)
			accounts[id] = kilo.EgressAccountStatus{Provider: a.Provider, ManualLimit: manual}
		}
	}
	e := m.executors["kilo"]
	m.mu.RUnlock()
	m.concurrency.mu.Lock()
	for id, a := range accounts {
		if state := m.concurrency.states[id]; state != nil {
			a.Active = state.active
			accounts[id] = a
		}
	}
	m.concurrency.mu.Unlock()
	m.wireKiloDeviceProfiles(e)
	var view kilo.ProxyStateView
	var err error
	if commit != nil {
		if e != expected {
			return kilo.ProxyStateView{}, kilo.ErrStateConflict
		}
		view, err = commit(accounts)
	} else {
		writer, ok := e.(interface {
			ChangeKiloProxyState(kilo.ProxyChanges, map[string]kilo.EgressAccountStatus) (kilo.ProxyStateView, error)
		})
		if !ok {
			return kilo.ProxyStateView{}, kilo.ErrStateStorage
		}
		view, err = writer.ChangeKiloProxyState(c, accounts)
	}
	if err == nil && view.Revision != c.ExpectedRevision && !c.ClearRecycle {
		m.invalidateKiloProbeEgress(c.EgressAccount)
	}
	return view, err
}
