package auth

import (
	"context"
	"maps"
	"time"
)

// ModelDiscovery describes the latest completed plugin model discovery.
// It contains no upstream response body or credential material.
type ModelDiscovery struct {
	Status    string `json:"status"`
	CheckedAt string `json:"checked_at,omitempty"`
	PluginID  string `json:"plugin_id,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type modelDiscoveryRecord struct {
	result                               ModelDiscovery
	attempt                              uint64
	epoch, credentialVersion             uint64
	prefix, provider, proxyURL, authKind string
	attributes                           map[string]string
}

// NextModelDiscoveryAttempt orders attempts independently of their completion.
func (m *Manager) NextModelDiscoveryAttempt() uint64 { return m.modelDiscoverySequence.Add(1) }

// UpdateForModelDiscovery keeps a current discovery bound to its committed auth update.
func (m *Manager) UpdateForModelDiscovery(ctx context.Context, base, updated *Auth, attempt *uint64) (*Auth, bool, error) {
	return m.updateIfRegistrationCurrent(ctx, base, updated, attempt)
}

// RecordModelDiscovery must be called within the model registration fence.
func (m *Manager) RecordModelDiscovery(base *Auth, attempt uint64, result ModelDiscovery) bool {
	if m == nil || base == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if attempt <= m.modelDiscoveryInvalidBefore[base.ID] {
		return false
	}
	current := m.auths[base.ID]
	if current == nil || current.Disabled || current.Status == StatusDisabled ||
		current.CredentialVersion != base.CredentialVersion || current.ProxyURL != base.ProxyURL ||
		!modelRegistrationInputsMatch(current, base.RegistrationEpoch, base.Prefix, base.Provider, false, base.AuthKind(), base.Attributes) {
		return false
	}
	if m.modelDiscoveries == nil {
		m.modelDiscoveries = make(map[string]modelDiscoveryRecord)
	}
	if previous, ok := m.modelDiscoveries[base.ID]; ok && previous.attempt > attempt {
		return false
	}
	result.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.modelDiscoveries[base.ID] = modelDiscoveryRecord{result: result, attempt: attempt,
		epoch: base.RegistrationEpoch, credentialVersion: base.CredentialVersion,
		prefix: base.Prefix, provider: base.Provider, proxyURL: base.ProxyURL,
		authKind: base.AuthKind(), attributes: maps.Clone(base.Attributes)}
	return true
}

// ModelDiscoveryForAuth returns unknown after restart or changed registration inputs.
func (m *Manager) ModelDiscoveryForAuth(id string) ModelDiscovery {
	unknown := ModelDiscovery{Status: "unknown"}
	if m == nil {
		return unknown
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.auths[id]
	if a == nil {
		return unknown
	}
	if a.Disabled || a.Status == StatusDisabled {
		return ModelDiscovery{Status: "disabled"}
	}
	record, ok := m.modelDiscoveries[id]
	if !ok || a.CredentialVersion != record.credentialVersion || a.ProxyURL != record.proxyURL ||
		!modelRegistrationInputsMatch(a, record.epoch, record.prefix, record.provider, false, record.authKind, record.attributes) {
		return unknown
	}
	return record.result
}

func (m *Manager) invalidateModelDiscoveryLocked(id string) {
	delete(m.modelDiscoveries, id)
	if m.modelDiscoveryInvalidBefore == nil {
		m.modelDiscoveryInvalidBefore = make(map[string]uint64)
	}
	m.modelDiscoveryInvalidBefore[id] = m.modelDiscoverySequence.Add(1)
}
