package auth

import (
	"context"
	"time"

	"github.com/MoeForge/moego/ojson"
	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

const kiloRestrictionKey = "kilo_auto_restriction"
const kiloQuotaExhausted = "quota_exhausted"
const kiloAccountRestricted = "account_restricted"

// KiloRestrictionView is a material-free projection, independent of manual Disabled.
type KiloRestrictionView struct {
	Reason           string     `json:"reason"`
	Generation       uint64     `json:"generation"`
	IdentityRevision uint64     `json:"identity_revision"`
	SourceRevision   uint64     `json:"source_revision"`
	RecoverAt        *time.Time `json:"recover_at"`
	QueryStatus      string     `json:"query_status"`
	ErrorCode        *string    `json:"error_code"`
	Persisted        bool       `json:"persisted"`
}
type kiloRecoveryRuntime struct {
	generation  uint64
	persisted   bool
	queryStatus string
	errorCode   *string
	pending     bool
	fence       uint64
}
type kiloRecoveryLifecycle struct {
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
}

func kiloUint(value any) uint64 {
	switch n := value.(type) {
	case uint64:
		return n
	case int:
		return uint64(n)
	case float64:
		return uint64(n)
	}
	return 0
}
func kiloRestriction(a *Auth) KiloRestrictionView {
	v := KiloRestrictionView{Reason: "none", QueryStatus: "never", Persisted: true}
	if a == nil || a.Provider != "kilo" {
		return v
	}
	meta, _ := a.Metadata[kiloRestrictionKey].(map[string]any)
	reason, _ := meta["reason"].(string)
	if reason == kiloQuotaExhausted || reason == kiloAccountRestricted {
		v.Reason = reason
	}
	v.Generation = kiloUint(meta["generation"])
	v.IdentityRevision = kiloUint(meta["identity_revision"])
	v.SourceRevision = kiloUint(meta["source_revision"])
	if raw, _ := meta["recover_at"].(string); raw != "" {
		if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			v.RecoverAt = &at
		}
	}
	return v
}
func setKiloRestriction(a *Auth, v KiloRestrictionView) {
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	at := ""
	if v.RecoverAt != nil {
		at = v.RecoverAt.UTC().Format(time.RFC3339Nano)
	}
	a.Metadata[kiloRestrictionKey] = map[string]any{"reason": v.Reason, "generation": v.Generation, "identity_revision": v.IdentityRevision, "source_revision": v.SourceRevision, "recover_at": at}
}
func kiloAutoBlocked(a *Auth) bool {
	return kiloRestriction(a).Reason != "none" || kiloSourcePolicy(a).Paused
}
func kiloRestrictionReason(result Result) string {
	if result.Provider != "kilo" || result.Success || result.Error == nil {
		return ""
	}
	raw := result.Error.Message
	code, kind := ojson.Get(raw, "error.code").String(), ojson.Get(raw, "error.type").String()
	if code == "subscription_sharing_usage_limit_exceeded" {
		return kiloQuotaExhausted
	}
	if ojson.Get(raw, "upstream_status").Int() == 403 && (code == "account_restricted" || kind == "account_restricted") {
		return kiloAccountRestricted
	}
	return ""
}
func (m *Manager) kiloRuntimeLocked(id string, v KiloRestrictionView) *kiloRecoveryRuntime {
	if m.kiloRecoveryStates == nil {
		m.kiloRecoveryStates = map[string]*kiloRecoveryRuntime{}
	}
	r := m.kiloRecoveryStates[id]
	if r == nil || r.generation != v.Generation {
		fence := uint64(0)
		if r != nil {
			fence = r.fence
		}
		r = &kiloRecoveryRuntime{generation: v.Generation, persisted: true, queryStatus: "never", fence: fence}
		m.kiloRecoveryStates[id] = r
	}
	return r
}
func (m *Manager) KiloRestriction(id string) KiloRestrictionView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.auths[id]
	v := kiloRestriction(a)
	if a != nil && (a.Disabled || v.IdentityRevision != kiloIdentityRevision(a) || v.SourceRevision != kiloQuotaBinding(a).Revision) {
		v.RecoverAt = nil
	}
	if a != nil {
		binding := kiloQuotaBinding(a)
		if binding.Kind == "none" || (binding.Kind == "cpa_codex" && codexUsageIdentity(m.auths[binding.CredentialID], binding.AccountID) != "") {
			v.RecoverAt = nil
		}
	}
	if r := m.kiloRecoveryStates[id]; r != nil && r.generation == v.Generation {
		v.Persisted = r.persisted
		v.QueryStatus = r.queryStatus
		v.ErrorCode = r.errorCode
	}
	return v
}
func (m *Manager) wakeKiloRecoveryLocked() {
	if loop := m.kiloRecoveryLoop; loop != nil {
		select {
		case loop.wake <- struct{}{}:
		default:
		}
	}
}

// recordKiloRestriction intercepts only precise Kilo facts. It does not create a
// second generic model cooldown that recovery would later have to erase.
func (m *Manager) recordKiloRestriction(ctx context.Context, result Result) bool {
	reason := kiloRestrictionReason(result)
	if reason == "" {
		return false
	}
	m.concurrencyPickMu.Lock()
	release := m.lockAuthMutation(result.AuthID)
	m.mu.Lock()
	a := m.auths[result.AuthID]
	stale := a == nil || a.Provider != "kilo" || isStaleExecutionResult(result, a) || staleKiloDiagnostic(ctx, a)
	if r := m.kiloRecoveryStates[result.AuthID]; r != nil && result.ExecutionSeq > 0 && result.ExecutionSeq <= r.fence {
		stale = true
	}
	if stale {
		m.mu.Unlock()
		release()
		m.concurrencyPickMu.Unlock()
		m.hook.OnResult(ctx, result)
		m.publishErrorEvent(result, nil)
		return true
	}
	now := time.Now()
	v := kiloRestriction(a)
	if v.Reason == kiloAccountRestricted {
		reason = kiloAccountRestricted
	}
	if v.Reason != reason || v.IdentityRevision != kiloIdentityRevision(a) || v.SourceRevision != kiloQuotaBinding(a).Revision {
		v = KiloRestrictionView{Reason: reason, Generation: v.Generation + 1, IdentityRevision: kiloIdentityRevision(a), SourceRevision: kiloQuotaBinding(a).Revision}
		setKiloRestriction(a, v)
		r := m.kiloRuntimeLocked(a.ID, v)
		r.pending = reason == kiloQuotaExhausted
		r.queryStatus = "never"
	}
	r := m.kiloRuntimeLocked(a.ID, v)
	r.persisted = false
	a.recordRecentRequest(now, false)
	a.Failed++
	a.Generation++
	a.UpdatedAt = now
	saved := a.Clone()
	store := m.store
	// The protected runtime is published before fallible persistence.
	m.mu.Unlock()
	var err error
	if store == nil {
		err = ErrKiloAccountStorage
	} else {
		_, err = store.Save(WithAuthCreationIntent(context.WithoutCancel(ctx)), saved)
	}
	m.mu.Lock()
	r.persisted = err == nil
	if err != nil {
		code := "storage_failed"
		r.errorCode = &code
	} else {
		r.errorCode = nil
	}
	m.notifyAuthChangeLocked(a.ID)
	m.wakeKiloRecoveryLocked()
	snapshot := a.Clone()
	m.mu.Unlock()
	release()
	m.concurrencyPickMu.Unlock()
	m.publishKiloRestriction(ctx, snapshot)
	m.hook.OnResult(ctx, result)
	m.publishErrorEvent(result, snapshot)
	m.updateSessionAffinity(result)
	return true
}

// invalidateKiloRestriction is part of the existing explicit account transaction.
func (m *Manager) invalidateKiloRestriction(a *Auth, change KiloAccountChanges, identityChanged bool) {
	if change.Enabled == nil && change.QuotaSource == nil && !identityChanged {
		return
	}
	v := kiloRestriction(a)
	v.Generation++
	v.IdentityRevision = kiloIdentityRevision(a)
	v.SourceRevision = kiloQuotaBinding(a).Revision
	v.RecoverAt = nil
	if change.Enabled != nil && *change.Enabled {
		v.Reason = "none"
	}
	setKiloRestriction(a, v)
}
func (m *Manager) commitKiloRestrictionChangeLocked(a *Auth, change KiloAccountChanges, identityChanged bool) {
	v := kiloRestriction(a)
	if change.Enabled == nil && change.QuotaSource == nil && !identityChanged {
		if r := m.kiloRecoveryStates[a.ID]; r != nil && r.generation == v.Generation {
			r.persisted = true
		}
		return
	}
	delete(m.kiloRecoveryStates, a.ID)
	r := m.kiloRuntimeLocked(a.ID, v)
	r.fence = m.currentExecutionSeq()
	m.wakeKiloRecoveryLocked()
}

// observeKiloRecovery consumes the result of this query, not retained stale values.
func (m *Manager) observeKiloRecovery(ctx context.Context, id string, generation uint64, view KiloQuotaView) {
	if ctx.Err() != nil {
		return
	}
	release, err := m.lockAuthMutationContext(ctx, id)
	if err != nil {
		return
	}
	defer release()
	m.mu.Lock()
	a := m.auths[id]
	v := kiloRestriction(a)
	if a == nil || a.Disabled || v.Reason != kiloQuotaExhausted || v.Generation != generation || v.IdentityRevision != kiloIdentityRevision(a) || v.SourceRevision != view.BindingRevision {
		m.mu.Unlock()
		return
	}
	r := m.kiloRuntimeLocked(id, v)
	r.pending = false
	r.queryStatus = view.Query.Status
	r.errorCode = view.Query.ErrorCode
	if view.Query.Status != "success" || view.Stale {
		m.mu.Unlock()
		return
	}
	reset := usage.UsageRecoveryReset(view.UsageObservation, time.Now())
	if reset == nil {
		r.queryStatus = "no_future_reset"
		if v.RecoverAt == nil {
			m.mu.Unlock()
			return
		}
	}
	v.RecoverAt = reset
	setKiloRestriction(a, v)
	r.persisted = false
	updated := a.Clone()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		err = ErrKiloAccountStorage
	} else {
		_, err = store.Save(WithAuthCreationIntent(ctx), updated)
	}
	m.mu.Lock()
	if err == nil {
		updated.ClearFileSnapshot()
		m.auths[id] = updated
		r.persisted = true
		r.errorCode = nil
	} else {
		r.persisted = false
		code := "storage_failed"
		r.errorCode = &code
	}
	m.wakeKiloRecoveryLocked()
	m.mu.Unlock()
}

// ProcessKiloRecovery is one bounded host cycle. now allows deterministic due
// checks; no query is retried by the due loop, including across restarts.
func (m *Manager) ProcessKiloRecovery(ctx context.Context, now time.Time) {
	ctx = context.WithValue(ctx, kiloSourceCycleKey{}, kiloSourceCycle{})
	defer m.SyncKiloAutoEgress(now)
	m.kiloRecoveryCycleMu.Lock()
	defer m.kiloRecoveryCycleMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	m.mu.Lock()
	pending := map[string]KiloRestrictionView{}
	for id, r := range m.kiloRecoveryStates {
		if r == nil || !r.pending {
			continue
		}
		r.pending = false
		a := m.auths[id]
		v := kiloRestriction(a)
		if a == nil || a.Disabled || v.Reason != kiloQuotaExhausted || v.Generation != r.generation {
			continue
		}
		if kiloQuotaBinding(a).Kind == "none" {
			r.queryStatus = "no_source"
			continue
		}
		r.queryStatus = "running"
		pending[id] = v
	}
	m.mu.Unlock()
	for id, v := range pending {
		if ctx.Err() != nil {
			m.cancelKiloRecoveryPending(pending)
			return
		}
		_, err := m.QueryKiloQuota(ctx, id, v.SourceRevision)
		if ctx.Err() != nil {
			m.cancelKiloRecoveryPending(pending)
			return
		}
		if err != nil {
			m.mu.Lock()
			if r := m.kiloRecoveryStates[id]; r != nil && r.generation == v.Generation {
				r.queryStatus = "failed"
				code := "source_changed"
				r.errorCode = &code
			}
			m.mu.Unlock()
		}
	}
	m.processKiloSourcePolicies(ctx, now)
	for _, a := range m.List() {
		if ctx.Err() != nil {
			return
		}
		v := kiloRestriction(a)
		if v.Reason != kiloQuotaExhausted || v.RecoverAt == nil || v.RecoverAt.After(now) {
			continue
		}
		m.recoverKiloRestriction(ctx, a.ID, v, now)
	}
}
func (m *Manager) recoverKiloRestriction(ctx context.Context, id string, expected KiloRestrictionView, now time.Time) {
	m.concurrencyPickMu.Lock()
	defer m.concurrencyPickMu.Unlock()
	release, err := m.lockAuthMutationContext(ctx, id)
	if err != nil {
		return
	}
	defer release()
	m.mu.Lock()
	a := m.auths[id]
	v := kiloRestriction(a)
	if a == nil || a.Disabled || v.Reason != kiloQuotaExhausted || v.Generation != expected.Generation || v.IdentityRevision != kiloIdentityRevision(a) || v.SourceRevision != kiloQuotaBinding(a).Revision || v.RecoverAt == nil || v.RecoverAt.After(now) {
		m.mu.Unlock()
		return
	}
	binding := kiloQuotaBinding(a)
	if binding.Kind == "none" || (binding.Kind == "cpa_codex" && codexUsageIdentity(m.auths[binding.CredentialID], binding.AccountID) != "") {
		m.mu.Unlock()
		return
	}
	updated := a.Clone()
	v.Reason = "none"
	v.RecoverAt = nil
	v.Generation++
	setKiloRestriction(updated, v)
	updated.Generation++
	store := m.store
	m.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if store == nil {
		err = ErrKiloAccountStorage
	} else {
		_, err = store.Save(WithAuthCreationIntent(ctx), updated)
	}
	m.mu.Lock()
	if err != nil {
		r := m.kiloRuntimeLocked(id, expected)
		code := "storage_failed"
		r.errorCode = &code
		m.mu.Unlock()
		return
	}
	updated.ClearFileSnapshot()
	m.auths[id] = updated
	delete(m.kiloRecoveryStates, id)
	r := m.kiloRuntimeLocked(id, v)
	r.fence = m.currentExecutionSeq()
	m.notifyAuthChangeLocked(id)
	m.mu.Unlock()
	release()
	m.publishKiloRestriction(ctx, updated)
}

// StartKiloRecovery belongs to the service context, separate from token refresh.
func (m *Manager) StartKiloRecovery(parent context.Context) {
	ticker := time.NewTicker(time.Second)
	m.startKiloRecovery(parent, ticker.C, ticker.Stop)
}
func (m *Manager) startKiloRecovery(parent context.Context, ticks <-chan time.Time, stop func()) {
	m.StopKiloRecovery()
	ctx, cancel := context.WithCancel(parent)
	loop := &kiloRecoveryLifecycle{cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	m.mu.Lock()
	m.kiloRecoveryLoop = loop
	m.mu.Unlock()
	go func() {
		defer close(loop.done)
		defer stop()
		m.ProcessKiloRecovery(ctx, time.Now())
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticks:
				m.ProcessKiloRecovery(ctx, now)
			case <-loop.wake:
				m.ProcessKiloRecovery(ctx, time.Now())
			}
		}
	}()
}
func (m *Manager) StopKiloRecovery() {
	m.mu.Lock()
	loop := m.kiloRecoveryLoop
	m.kiloRecoveryLoop = nil
	m.mu.Unlock()
	if loop != nil {
		loop.cancel()
		<-loop.done
	}
}

func (m *Manager) cancelKiloRecoveryPending(pending map[string]KiloRestrictionView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, v := range pending {
		if r := m.kiloRecoveryStates[id]; r != nil && r.generation == v.Generation && r.queryStatus == "running" {
			r.queryStatus = "cancelled"
			code := "usage_cancelled"
			r.errorCode = &code
		}
	}
}

// Kilo file reloads and credential refreshes may not erase a newer local
// restriction. Identity/source edits retain the reason but invalidate the plan.
func preserveKiloRestriction(existing, incoming *Auth) {
	preserveKiloSourcePolicy(existing, incoming)
	if existing == nil || incoming == nil || existing.Provider != "kilo" || incoming.Provider != "kilo" {
		return
	}
	old, next := kiloRestriction(existing), kiloRestriction(incoming)
	changed := KiloIdentity(existing) != KiloIdentity(incoming) || authMetadataString(existing, "api_key") != authMetadataString(incoming, "api_key") || kiloQuotaBinding(existing) != kiloQuotaBinding(incoming)
	if changed {
		if old.Reason != "none" {
			next = old
		}
		next.Generation = max(old.Generation, next.Generation) + 1
		next.IdentityRevision = kiloIdentityRevision(incoming)
		next.SourceRevision = kiloQuotaBinding(incoming).Revision
		next.RecoverAt = nil
		setKiloRestriction(incoming, next)
	} else if old.Generation >= next.Generation {
		setKiloRestriction(incoming, old)
	}
}

func (m *Manager) publishKiloRestriction(ctx context.Context, a *Auth) {
	m.finishAuthUpdate(ctx, a, false)
	reg := registry.GetGlobalRegistry()
	models, epoch := reg.GetModelsAndEpochForClient(a.ID)
	projections := make([]registry.ClientModelProjection, 0, len(models))
	for _, model := range models {
		if model != nil {
			projections = append(projections, m.clientModelProjectionForAuth(a, model.ID, time.Now()))
		}
	}
	if len(projections) > 0 {
		reg.ApplyClientModelProjections(a.ID, epoch, a.Generation, projections)
	}
}
