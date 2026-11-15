package auth

import (
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

// SyncKiloAutoEgress uses the same gate as selection and manual egress edits.
func (m *Manager) SyncKiloAutoEgress(now time.Time) {
	m.concurrencyPickMu.Lock()
	defer m.concurrencyPickMu.Unlock()
	m.mu.RLock()
	e := m.executors["kilo"]
	accounts := map[string]kilo.EgressAccountStatus{}
	for id, a := range m.auths {
		if a == nil || a.Provider != "kilo" {
			continue
		}
		policy, restriction := kiloSourcePolicy(a), kiloRestriction(a)
		persisted := true
		if r := m.kiloRecoveryStates[id]; r != nil && restriction.Reason == kiloQuotaExhausted {
			persisted = persisted && r.persisted
		}
		if r := m.kiloPolicyStates[id]; r != nil && policy.Paused {
			persisted = persisted && r.persisted
		}
		blocked, _, _ := isAuthBlockedForModel(a, "", now)
		for _, state := range a.ModelStates {
			if state != nil && state.Unavailable && state.NextRetryAfter.After(now) {
				blocked = true
			}
		}
		_, manualLimit := credentialConcurrencyLimit(a)
		accounts[id] = kilo.EgressAccountStatus{ManualLimit: manualLimit, Provider: "kilo", CreatedAt: a.CreatedAt, CanAllocate: !blocked, ReturnRequested: a.Disabled || restriction.Reason == kiloQuotaExhausted || policy.Paused, RestrictionPersisted: persisted}
	}
	m.mu.RUnlock()
	m.concurrency.mu.Lock()
	for id, s := range m.concurrency.states {
		if s != nil && s.active > 0 {
			a := accounts[id]
			a.Active = s.active
			accounts[id] = a
		}
	}
	m.concurrency.mu.Unlock()
	m.wireKiloDeviceProfiles(e)
	if writer, ok := e.(interface {
		ReconcileKiloAutoEgress(map[string]kilo.EgressAccountStatus, time.Time)
	}); ok {
		writer.ReconcileKiloAutoEgress(accounts, now)
	}
}
