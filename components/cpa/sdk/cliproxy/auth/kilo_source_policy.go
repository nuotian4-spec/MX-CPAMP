package auth

import (
	"context"
	"time"

	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
)

const kiloSourcePolicyKey = "kilo_source_policy"

// KiloSourcePolicyView describes an opt-in local rule, never an upstream refusal.
type KiloSourcePolicyView struct {
	changedAt       time.Time
	Enabled         bool       `json:"enabled"`
	IntervalSeconds int        `json:"interval_seconds"`
	Revision        uint64     `json:"revision"`
	Paused          bool       `json:"paused"`
	PauseGeneration uint64     `json:"pause_generation"`
	Scope           string     `json:"scope"`
	Status          string     `json:"status"`
	ObservedAt      *time.Time `json:"observed_at"`
	NextQueryAt     *time.Time `json:"next_query_at"`
	ErrorCode       *string    `json:"error_code"`
	Persisted       bool       `json:"persisted"`
}

type kiloPolicyRuntime struct {
	revision  uint64
	due       time.Time
	persisted bool
}
type kiloPolicyTicket struct {
	revision, pause, identity, epoch, binding uint64
	started                                   time.Time
	source                                    CodexUsageCredential
}

// publishKiloPolicyLocked preserves concurrent request accounting and the cached quota.
func (m *Manager) publishKiloPolicyLocked(id string, v KiloSourcePolicyView) {
	previous := m.auths[id]
	updated := previous.Clone()
	setKiloSourcePolicy(updated, v)
	updated.Generation++
	updated.ClearFileSnapshot()
	m.auths[id] = updated
	m.notifyAuthChangeLocked(id)
}

func kiloSourcePolicy(a *Auth) KiloSourcePolicyView {
	v := KiloSourcePolicyView{IntervalSeconds: 1800, Status: "disabled", Persisted: true}
	if a == nil || a.Provider != "kilo" {
		return v
	}
	p, _ := a.Metadata[kiloSourcePolicyKey].(map[string]any)
	if p == nil {
		return v
	}
	v.Enabled, _ = p["enabled"].(bool)
	v.Paused, _ = p["paused"].(bool)
	v.Revision = kiloUint(p["revision"])
	v.PauseGeneration = kiloUint(p["pause_generation"])
	if n := kiloUint(p["interval_seconds"]); n >= 60 && n <= 86400 {
		v.IntervalSeconds = int(n)
	}
	v.Scope, _ = p["scope"].(string)
	if raw, _ := p["changed_at"].(string); raw != "" {
		v.changedAt, _ = time.Parse(time.RFC3339Nano, raw)
	}
	v.Status, _ = p["status"].(string)
	if v.Status == "" {
		v.Status = "pending"
	}
	for key, dst := range map[string]**time.Time{"observed_at": &v.ObservedAt, "next_query_at": &v.NextQueryAt} {
		if raw, _ := p[key].(string); raw != "" {
			if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				*dst = &at
			}
		}
	}
	if code, _ := p["error_code"].(string); code != "" {
		v.ErrorCode = &code
	}
	return v
}
func setKiloSourcePolicy(a *Auth, v KiloSourcePolicyView) {
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	p := map[string]any{"enabled": v.Enabled, "interval_seconds": v.IntervalSeconds, "revision": v.Revision, "paused": v.Paused, "pause_generation": v.PauseGeneration, "scope": v.Scope, "status": v.Status}
	if !v.changedAt.IsZero() {
		p["changed_at"] = v.changedAt.UTC().Format(time.RFC3339Nano)
	}
	if v.ObservedAt != nil {
		p["observed_at"] = v.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	if v.NextQueryAt != nil {
		p["next_query_at"] = v.NextQueryAt.UTC().Format(time.RFC3339Nano)
	}
	if v.ErrorCode != nil {
		p["error_code"] = *v.ErrorCode
	}
	a.Metadata[kiloSourcePolicyKey] = p
}
func (m *Manager) KiloSourcePolicy(id string) (KiloSourcePolicyView, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.auths[id]
	if a == nil || a.Provider != "kilo" {
		return KiloSourcePolicyView{}, ErrKiloAccountMissing
	}
	v := kiloSourcePolicy(a)
	if r := m.kiloPolicyStates[id]; r != nil && r.revision == v.Revision {
		v.Persisted = r.persisted
	}
	return v, nil
}

// ChangeKiloSourcePolicy changes only this rule; manual and upstream restrictions survive.
func (m *Manager) ChangeKiloSourcePolicy(ctx context.Context, id string, expected uint64, enabled bool, interval int, confirmScope bool) (KiloSourcePolicyView, error) {
	if interval < 60 || interval > 86400 {
		return KiloSourcePolicyView{}, ErrKiloQuotaInvalid
	}
	m.concurrencyPickMu.Lock()
	defer m.concurrencyPickMu.Unlock()
	release, err := m.lockAuthMutationContext(ctx, id)
	if err != nil {
		return KiloSourcePolicyView{}, err
	}
	defer release()
	m.mu.Lock()
	a := m.auths[id]
	if a == nil || a.Provider != "kilo" {
		m.mu.Unlock()
		return KiloSourcePolicyView{}, ErrKiloAccountMissing
	}
	v := kiloSourcePolicy(a)
	if v.Revision != expected {
		m.mu.Unlock()
		return KiloSourcePolicyView{}, ErrKiloQuotaConflict
	}
	updated := a.Clone()
	v.Enabled, v.IntervalSeconds, v.Revision = enabled, interval, v.Revision+1
	v.changedAt = time.Now().UTC()
	v.Status, v.ObservedAt, v.NextQueryAt, v.ErrorCode = "pending", nil, nil, nil
	if confirmScope || !enabled {
		v.Scope = ""
	}
	if !enabled {
		v.Paused, v.Status = false, "disabled"
		v.PauseGeneration++
	}
	setKiloSourcePolicy(updated, v)
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return KiloSourcePolicyView{}, ErrKiloAccountStorage
	}
	if _, err = store.Save(WithAuthCreationIntent(ctx), updated); err != nil {
		return KiloSourcePolicyView{}, ErrKiloAccountStorage
	}
	m.mu.Lock()
	m.publishKiloPolicyLocked(id, v)
	if m.kiloPolicyStates == nil {
		m.kiloPolicyStates = map[string]*kiloPolicyRuntime{}
	}
	m.kiloPolicyStates[id] = &kiloPolicyRuntime{revision: v.Revision, persisted: true}
	m.wakeKiloRecoveryLocked()
	m.mu.Unlock()
	m.RefreshSchedulerEntry(id)
	return v, nil
}

func invalidateKiloSourcePolicy(a *Auth, change KiloAccountChanges, identityChanged bool) {
	if change.QuotaSource == nil && !identityChanged {
		return
	}
	v := kiloSourcePolicy(a)
	if !v.Enabled {
		return
	}
	v.Revision++
	v.changedAt = time.Now().UTC()
	v.Scope, v.Status, v.ObservedAt, v.NextQueryAt, v.ErrorCode = "", "source_pending", nil, nil, nil
	setKiloSourcePolicy(a, v)
}

func (m *Manager) observeKiloSourcePolicy(ctx context.Context, id string, ticket kiloPolicyTicket, view KiloQuotaView) {
	if ctx.Err() != nil {
		return
	}
	m.concurrencyPickMu.Lock()
	defer m.concurrencyPickMu.Unlock()
	release, err := m.lockAuthMutationContext(ctx, id)
	if err != nil {
		return
	}
	defer release()
	if ticket.source.AuthID != "" {
		// The target mutation already holds the load barrier. Acquire only the
		// source gate to avoid recursively waiting behind a pending full reload.
		value, _ := m.authMutationLocks.LoadOrStore(ticket.source.AuthID, make(chan struct{}, 1))
		gate := value.(chan struct{})
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return
		}
	}
	m.mu.Lock()
	a := m.auths[id]
	v := kiloSourcePolicy(a)
	if a == nil || !v.Enabled || v.Revision != ticket.revision || v.PauseGeneration != ticket.pause || kiloIdentityRevision(a) != ticket.identity || a.RegistrationEpoch != ticket.epoch || kiloQuotaBinding(a).Revision != ticket.binding {
		m.mu.Unlock()
		return
	}
	if v.ObservedAt != nil && !ticket.started.After(*v.ObservedAt) {
		m.mu.Unlock()
		return
	}
	if ticket.started.Before(v.changedAt) {
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	// Source replacement during another observer's storage I/O invalidates this result.
	if ticket.source.AuthID != "" && !m.codexUsageMaterialCurrentLocked(ticket.source) {
		code := "source_changed"
		view.Query.Status, view.Query.ErrorCode, view.Source.Kind = "failed", &code, "none"
	}
	due := now.Add(time.Duration(v.IntervalSeconds) * time.Second)
	v.NextQueryAt, v.ErrorCode = &due, view.Query.ErrorCode
	v.Status = "query_failed"
	if view.Query.ErrorCode != nil && *view.Query.ErrorCode == "query_egress_unavailable" {
		v.Status = "waiting_query_egress"
	}
	if view.Source.Kind == "none" {
		v.Status = "source_pending"
	}
	if view.Query.Status == "success" && !view.Stale && view.Query.ObservedAt != nil {
		scope, exhausted, reset := usage.SourcePolicyScope(view.UsageObservation, ticket.started, *view.Query.ObservedAt)
		v.Status = "source_pending"
		if scope != "" && (v.Scope == "" || v.Scope == scope) {
			v.Scope, v.ObservedAt, v.ErrorCode = scope, view.Query.ObservedAt, nil
			if exhausted && !v.Paused {
				v.PauseGeneration++
			}
			v.Paused, v.Status = exhausted, "ready"
			if exhausted {
				v.Status = "paused"
			}
			if reset != nil && reset.Before(due) {
				v.NextQueryAt = reset
			}
		}
	}
	updated := a.Clone()
	setKiloSourcePolicy(updated, v)
	if m.kiloPolicyStates == nil {
		m.kiloPolicyStates = map[string]*kiloPolicyRuntime{}
	}
	r := &kiloPolicyRuntime{revision: v.Revision, due: *v.NextQueryAt, persisted: false}
	m.kiloPolicyStates[id] = r
	// A new pause fences admission before persistence; a failed clear keeps the pause.
	if v.Paused {
		m.publishKiloPolicyLocked(id, v)
	}
	store := m.store
	m.mu.Unlock()
	m.RefreshSchedulerEntry(id)
	if store == nil {
		err = ErrKiloAccountStorage
	} else {
		_, err = store.Save(WithAuthCreationIntent(ctx), updated)
	}
	m.mu.Lock()
	if err == nil {
		// A source may change while Save is in flight. Never publish a stale clear.
		if ticket.source.AuthID != "" && !m.codexUsageMaterialCurrentLocked(ticket.source) && !v.Paused {
			v = kiloSourcePolicy(m.auths[id])
			v.Status = "source_pending"
			code := "source_changed"
			v.ErrorCode = &code
			r.persisted = false
		} else {
			r.persisted = true
		}
		m.publishKiloPolicyLocked(id, v)
	} else {
		live := kiloSourcePolicy(m.auths[id])
		code := "storage_failed"
		live.ErrorCode = &code
		live.Status = "storage_failed"
		m.publishKiloPolicyLocked(id, live)
	}
	m.mu.Unlock()
	m.RefreshSchedulerEntry(id)
}

// Policy work shares the existing recovery host. Restart ignores persisted query times.
func (m *Manager) processKiloSourcePolicies(ctx context.Context, now time.Time) {
	if ctx.Value(kiloSourceCycleKey{}) == nil {
		ctx = context.WithValue(ctx, kiloSourceCycleKey{}, kiloSourceCycle{})
	}
	for _, a := range m.List() {
		v := kiloSourcePolicy(a)
		if !v.Enabled {
			continue
		}
		m.mu.Lock()
		if m.kiloPolicyStates == nil {
			m.kiloPolicyStates = map[string]*kiloPolicyRuntime{}
		}
		r := m.kiloPolicyStates[a.ID]
		if r != nil && r.revision == v.Revision && r.due.After(now) {
			m.mu.Unlock()
			continue
		}
		m.kiloPolicyStates[a.ID] = &kiloPolicyRuntime{revision: v.Revision, due: now.Add(time.Duration(v.IntervalSeconds) * time.Second), persisted: r == nil || r.persisted}
		m.mu.Unlock()
		b := kiloQuotaBinding(a)
		if b.Kind == "none" {
			continue
		}
		_, _ = m.QueryKiloQuota(ctx, a.ID, b.Revision)
		if ctx.Err() != nil {
			return
		}
	}
}

func preserveKiloSourcePolicy(existing, incoming *Auth) {
	if existing == nil || incoming == nil || existing.Provider != "kilo" || incoming.Provider != "kilo" {
		return
	}
	old, next := kiloSourcePolicy(existing), kiloSourcePolicy(incoming)
	if old.Revision >= next.Revision {
		setKiloSourcePolicy(incoming, old)
	}
	if KiloIdentity(existing) != KiloIdentity(incoming) || kiloQuotaBinding(existing) != kiloQuotaBinding(incoming) {
		invalidateKiloSourcePolicy(incoming, KiloAccountChanges{}, true)
	}
}
