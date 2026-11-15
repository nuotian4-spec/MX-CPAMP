package auth

import (
	"context"
	"errors"
	"fmt"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"slices"
	"strings"
	"time"
)

// PermissionDenial is bounded evidence supplied by the native BPS executor.
// It is independent of the current request's retry/truncation classification.
type PermissionDenial struct {
	Source             string `json:"source"`
	UpstreamHTTPStatus int    `json:"upstream_http_status"`
	SemanticStatus     int    `json:"semantic_status"`
	ReasonCode         string `json:"reason_code"`
	CredentialScope    bool   `json:"-"`
}

type PermissionSuspension struct {
	Scope              string    `json:"scope"`
	ModelKey           string    `json:"model_key,omitempty"`
	Source             string    `json:"source"`
	UpstreamHTTPStatus int       `json:"upstream_http_status"`
	SemanticStatus     int       `json:"semantic_status"`
	ReasonCode         string    `json:"reason_code"`
	FirstSeenAt        time.Time `json:"first_seen_at"`
	LastSeenAt         time.Time `json:"last_seen_at"`
}

type PermissionScope struct {
	Scope    string `json:"scope"`
	ModelKey string `json:"model_key,omitempty"`
}

func permissionDenialFromError(err error) *PermissionDenial {
	var evidence interface{ BPSPermissionDenial() (string, int, bool) }
	if !errors.As(err, &evidence) {
		return nil
	}
	source, status, credential := evidence.BPSPermissionDenial()
	if (source != "http" && source != "inband") || status < 100 || status > 599 {
		return nil
	}
	return &PermissionDenial{Source: source, UpstreamHTTPStatus: status, SemanticStatus: 403, ReasonCode: "unknown", CredentialScope: credential}
}

func PermissionSuspensionSnapshot(auth *Auth) []PermissionSuspension {
	result := make([]PermissionSuspension, 0)
	if auth != nil && auth.Provider == "bps" {
		result = append(result, auth.PermissionSuspensions...)
	}
	slices.SortFunc(result, func(a, b PermissionSuspension) int {
		if a.Scope != b.Scope {
			return strings.Compare(a.Scope, b.Scope)
		}
		return strings.Compare(a.ModelKey, b.ModelKey)
	})
	return result
}

func permissionSuspended(auth *Auth, model string) bool {
	if auth == nil || auth.Provider != "bps" {
		return false
	}
	if auth.permissionStateUnknown {
		return true
	}
	for _, state := range auth.PermissionSuspensions {
		if state.Scope == "credential" || (model != "" && sameCanonicalModelKey(state.ModelKey, model)) {
			return true
		}
	}
	return false
}

func recordPermissionDenial(auth *Auth, model string, result Result, now time.Time) {
	if auth.Provider != "bps" || result.Success || result.PermissionDenial == nil {
		return
	}
	evidence := result.PermissionDenial
	if evidence.SemanticStatus != 403 {
		return
	}
	scope := "model"
	if evidence.CredentialScope {
		scope, model = "credential", ""
	}
	if scope == "model" && model == "" {
		return
	}
	state := PermissionSuspension{Scope: scope, ModelKey: canonicalModelKey(model), Source: evidence.Source, UpstreamHTTPStatus: evidence.UpstreamHTTPStatus, SemanticStatus: 403, ReasonCode: "unknown", FirstSeenAt: now.UTC(), LastSeenAt: now.UTC()}
	for i, previous := range auth.PermissionSuspensions {
		if previous.Scope == scope && (scope == "credential" || sameCanonicalModelKey(previous.ModelKey, state.ModelKey)) {
			state.FirstSeenAt = previous.FirstSeenAt
			auth.PermissionSuspensions[i] = state
			auth.permissionStateRecorded = true
			return
		}
	}
	auth.PermissionSuspensions = append(auth.PermissionSuspensions, state)
	auth.permissionStateRecorded = true
}

// checkPermissionDispatch reads live state after queuing/preparation, rather than
// relying on the detached auth snapshot originally selected for the request.
func (m *Manager) checkPermissionDispatch(auth *Auth, model string) error {
	if auth == nil || auth.Provider != "bps" {
		return nil
	}
	m.mu.RLock()
	current := m.auths[auth.ID]
	unknown := m.permissionLoadFailed
	blocked := permissionSuspended(current, model)
	m.mu.RUnlock()
	if unknown {
		return NewRequestScopedError("BPS permission state is unavailable", 503)
	}
	if blocked {
		return NewRequestScopedError("BPS permission suspension requires administrator release", 403)
	}
	return nil
}

func (m *Manager) executePermissionChecked(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if err := m.checkPermissionDispatch(auth, req.Model); err != nil {
		return cliproxyexecutor.Response{}, err
	}
	if err := beginBPSDispatch(ctx, auth, req, opts); err != nil {
		return cliproxyexecutor.Response{}, err
	}
	return executor.Execute(m.permissionDispatchContext(ctx, auth, req.Model), auth, req, opts)
}

func (m *Manager) executeStreamPermissionChecked(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if err := m.checkPermissionDispatch(auth, req.Model); err != nil {
		return nil, err
	}
	return executor.ExecuteStream(m.permissionDispatchContext(ctx, auth, req.Model), auth, req, opts)
}

func (m *Manager) streamAttemptPermissionChecked(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error, context.CancelFunc) {
	if err := m.checkPermissionDispatch(auth, req.Model); err != nil {
		return nil, err, func() {}
	}
	if err := beginBPSDispatch(ctx, auth, req, opts); err != nil {
		return nil, err, func() {}
	}
	return executeStreamAttempt(m.permissionDispatchContext(ctx, auth, req.Model), executor, auth, req, opts)
}

type permissionDispatchKey struct{}

func (m *Manager) permissionDispatchContext(ctx context.Context, auth *Auth, model string) context.Context {
	if auth == nil || auth.Provider != "bps" {
		return ctx
	}
	return context.WithValue(ctx, permissionDispatchKey{}, func() error { return m.checkPermissionDispatch(auth, model) })
}

// CheckBPSPermissionDispatch rechecks after request/image preparation and before
// the native executor starts the HTTP inference request.
func CheckBPSPermissionDispatch(ctx context.Context) error {
	if gate, ok := ctx.Value(permissionDispatchKey{}).(func() error); ok {
		return gate()
	}
	return nil
}

func (m *Manager) PermissionPersistenceStatus() string {
	if m == nil {
		return "unknown"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.permissionLoadFailed {
		return "unknown"
	}
	if m.permissionSaveFailed {
		return "error"
	}
	if m.cooldownStore == nil {
		return "memory_only"
	}
	return "durable"
}

func permissionStateRecord(auth *Auth) (CooldownStateRecord, bool) {
	if auth == nil || auth.Provider != "bps" || !auth.permissionStateRecorded {
		return CooldownStateRecord{}, false
	}
	return CooldownStateRecord{Provider: auth.Provider, AuthID: auth.ID, AuthFile: auth.FileName, PermissionEpoch: auth.RegistrationEpoch, PermissionSuspensions: PermissionSuspensionSnapshot(auth), PermissionReleasedAt: auth.permissionReleasedAt}, true
}

func (m *Manager) restorePermissionRecordLocked(record CooldownStateRecord) bool {
	if record.PermissionEpoch == 0 || record.Provider != "bps" {
		return false
	}
	auth := m.auths[record.AuthID]
	if auth == nil || auth.Provider != "bps" || auth.permissionStateRecorded {
		return false
	}
	auth.PermissionSuspensions = append([]PermissionSuspension(nil), record.PermissionSuspensions...)
	auth.permissionReleasedAt = record.PermissionReleasedAt
	auth.permissionStateRecorded = true
	auth.RegistrationEpoch = max(auth.RegistrationEpoch, record.PermissionEpoch) + 1
	if m.authEpochs == nil {
		m.authEpochs = make(map[string]uint64)
	}
	m.authEpochs[auth.ID] = auth.RegistrationEpoch
	auth.Generation++
	return true
}

// ReleasePermissionSuspensions persists the selected release before publishing it.
// All three existing auth versions are CAS inputs; no new revision counter exists.
func (m *Manager) ReleasePermissionSuspensions(ctx context.Context, id, provider string, epoch, version, generation uint64, scopes []PermissionScope) (*Auth, error) {
	if m == nil {
		return nil, NewRequestScopedError("permission state is unavailable", 503)
	}
	if provider != "bps" || id == "" || epoch == 0 || version == 0 || generation == 0 || len(scopes) == 0 {
		return nil, NewRequestScopedError("invalid permission release", 400)
	}
	m.configCooldownMu.Lock()
	defer m.configCooldownMu.Unlock()
	release, err := m.lockAuthMutationContext(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	m.mu.RLock()
	current := m.auths[id].Clone()
	store, unknown := m.cooldownStore, m.permissionLoadFailed
	m.mu.RUnlock()
	if current == nil || current.Provider != provider {
		return nil, NewRequestScopedError("credential not found", 404)
	}
	if store == nil || unknown || m.HomeEnabled() {
		return nil, NewRequestScopedError("permission state is unavailable", 503)
	}
	atomicStore, ok := store.(interface {
		SavePermissionRelease(context.Context, string, []CooldownStateRecord) error
	})
	if !ok {
		return nil, NewRequestScopedError("atomic permission release is unavailable", 503)
	}
	if current.RegistrationEpoch != epoch || current.CredentialVersion != version || current.Generation != generation {
		return nil, NewRequestScopedError("permission snapshot has changed", 409)
	}
	remove := make(map[int]bool)
	for _, scope := range scopes {
		if (scope.Scope != "model" && scope.Scope != "credential") || (scope.Scope == "model" && strings.TrimSpace(scope.ModelKey) == "") || (scope.Scope == "credential" && scope.ModelKey != "") {
			return nil, NewRequestScopedError("invalid permission scope", 400)
		}
		found := false
		for i, state := range current.PermissionSuspensions {
			if state.Scope == scope.Scope && state.ModelKey == scope.ModelKey {
				remove[i], found = true, true
			}
		}
		if !found {
			return nil, NewRequestScopedError("permission scope has changed", 409)
		}
	}
	remaining := make([]PermissionSuspension, 0)
	for i, state := range current.PermissionSuspensions {
		if !remove[i] {
			remaining = append(remaining, state)
		}
	}
	current.PermissionSuspensions = remaining
	current.permissionReleasedAt = time.Now().UTC()
	current.permissionStateRecorded = true
	current.Generation++
	records := m.cooldownStateRecordsSnapshot()
	kept := records[:0]
	for _, record := range records {
		if record.AuthID == id && record.PermissionEpoch > 0 {
			continue
		}
		kept = append(kept, record)
	}
	record, _ := permissionStateRecord(current)
	kept = append(kept, record)
	if errSave := atomicStore.SavePermissionRelease(ctx, id, kept); errSave != nil {
		m.mu.Lock()
		m.permissionSaveFailed = true
		m.mu.Unlock()
		return nil, NewRequestScopedError("permission state persistence failed", 503)
	}
	m.mu.Lock()
	m.auths[id] = current
	m.permissionSaveFailed = false
	m.notifyAuthChangeLocked(id)
	m.mu.Unlock()
	release()
	if m.scheduler != nil {
		m.scheduler.upsertAuth(current.Clone())
	}
	return current.Clone(), nil
}

func permissionRecordEqual(a, b CooldownStateRecord) bool {
	return a.PermissionEpoch == b.PermissionEpoch && a.PermissionReleasedAt.Equal(b.PermissionReleasedAt) && slices.Equal(a.PermissionSuspensions, b.PermissionSuspensions)
}

func validatePermissionRecords(records []CooldownStateRecord) error {
	for _, record := range records {
		for _, state := range record.PermissionSuspensions {
			if record.Provider != "bps" || record.PermissionEpoch == 0 || state.SemanticStatus != 403 || (state.Source != "http" && state.Source != "inband") || (state.Scope != "credential" && state.Scope != "model") || (state.Scope == "model" && state.ModelKey == "") {
				return fmt.Errorf("invalid permission suspension record")
			}
		}
	}
	return nil
}

type permissionOnlyStateStore struct{ CooldownStateStore }

// PermissionOnlyStateStore reuses the runtime store when timed cooldown saving
// is disabled, without enabling persistence for other providers or cooldowns.
func PermissionOnlyStateStore(store CooldownStateStore) CooldownStateStore {
	if store == nil {
		return nil
	}
	return permissionOnlyStateStore{store}
}

func permissionOnlyRecords(records []CooldownStateRecord) []CooldownStateRecord {
	result := make([]CooldownStateRecord, 0)
	for _, record := range records {
		if record.Provider == "bps" && record.PermissionEpoch > 0 {
			result = append(result, record)
		}
	}
	return result
}

func (s permissionOnlyStateStore) Load(ctx context.Context) ([]CooldownStateRecord, error) {
	records, err := s.CooldownStateStore.Load(ctx)
	return permissionOnlyRecords(records), err
}

func (s permissionOnlyStateStore) Save(ctx context.Context, records []CooldownStateRecord) error {
	return s.CooldownStateStore.Save(ctx, permissionOnlyRecords(records))
}

func (s permissionOnlyStateStore) SavePermissionRelease(ctx context.Context, id string, records []CooldownStateRecord) error {
	store, ok := s.CooldownStateStore.(interface {
		SavePermissionRelease(context.Context, string, []CooldownStateRecord) error
	})
	if !ok {
		return fmt.Errorf("atomic permission release is unavailable")
	}
	return store.SavePermissionRelease(ctx, id, permissionOnlyRecords(records))
}
