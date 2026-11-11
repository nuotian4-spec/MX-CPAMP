package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

const DefaultCredentialConcurrency = 20
const AttributeConcurrencyLimit = "concurrency_limit"
const credentialWaitLimit = 1000
const credentialWaitBudget = 30 * time.Second

type concurrencyIdentity struct{ provider, accountID, email, apiKey string }

func concurrencyAuthIdentity(a *Auth) concurrencyIdentity {
	if a == nil {
		return concurrencyIdentity{}
	}
	value := func(key string) string {
		v := authMetadataString(a, key)
		if v == "" {
			v = strings.TrimSpace(a.Attributes[key])
		}
		return v
	}
	return concurrencyIdentity{provider: a.Provider, accountID: value("account_id"), email: strings.ToLower(value("email")), apiKey: value("api_key")}
}
func (id concurrencyIdentity) conflicts(next concurrencyIdentity) bool {
	return id.provider != next.provider || (id.accountID != "" && next.accountID != "" && id.accountID != next.accountID) || (id.email != "" && next.email != "" && id.email != next.email) || (id.apiKey != "" && next.apiKey != "" && id.apiKey != next.apiKey)
}

type concurrencyCounter struct {
	active   int
	identity concurrencyIdentity
}
type localConcurrency struct {
	mu        sync.Mutex
	states    map[string]*concurrencyCounter
	changedCh chan struct{}
	waiting   int
	pinned    map[string]int
}

func (g *localConcurrency) initLocked() {
	if g.states == nil {
		g.states = make(map[string]*concurrencyCounter)
	}
	if g.pinned == nil {
		g.pinned = make(map[string]int)
	}
	if g.changedCh == nil {
		g.changedCh = make(chan struct{})
	}
}
func (g *localConcurrency) signalLocked() {
	g.initLocked()
	close(g.changedCh)
	g.changedCh = make(chan struct{})
}
func (g *localConcurrency) changed(id string, auth *Auth) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.initLocked()
	if state := g.states[id]; state != nil && (auth == nil || state.identity.conflicts(concurrencyAuthIdentity(auth))) {
		delete(g.states, id)
	}
	if state := g.states[id]; state != nil && auth != nil {
		next := concurrencyAuthIdentity(auth)
		if next.accountID != "" {
			state.identity.accountID = next.accountID
		}
		if next.email != "" {
			state.identity.email = next.email
		}
		if next.apiKey != "" {
			state.identity.apiKey = next.apiKey
		}
	}
	g.signalLocked()
}

// ParseConcurrencyLimit accepts only positive integral values in the API range.
func ParseConcurrencyLimit(value any) (int, error) {
	var raw string
	switch v := value.(type) {
	case int:
		raw = strconv.Itoa(v)
	case int64:
		raw = strconv.FormatInt(v, 10)
	case float64:
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("concurrency_limit must be a positive integer")
		}
		raw = strconv.FormatInt(int64(v), 10)
	case json.Number:
		raw = string(v)
	default:
		return 0, fmt.Errorf("concurrency_limit must be a positive integer")
	}
	v, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || v < 1 {
		return 0, fmt.Errorf("concurrency_limit must be an integer between 1 and 2147483647")
	}
	return int(v), nil
}
func credentialConcurrencyLimit(auth *Auth) (int, *int) {
	if auth != nil {
		if raw := auth.Attributes[AttributeConcurrencyLimit]; raw != "" {
			if n, err := strconv.ParseInt(raw, 10, 32); err == nil && n > 0 {
				v := int(n)
				return v, &v
			}
		}
		if raw := auth.Metadata[AttributeConcurrencyLimit]; raw != nil {
			if n, err := ParseConcurrencyLimit(raw); err == nil {
				return n, &n
			}
		}
	}
	return DefaultCredentialConcurrency, nil
}

type credentialLease struct {
	once  sync.Once
	gate  *localConcurrency
	state *concurrencyCounter
}

func (l *credentialLease) release() {
	if l == nil {
		return
	}
	l.once.Do(func() { l.gate.mu.Lock(); defer l.gate.mu.Unlock(); l.state.active--; l.gate.signalLocked() })
}

// Caller holds m.mu, so deletion cannot attach an admission to a removed identity.
func (m *Manager) tryCredentialLeaseLocked(auth *Auth) *credentialLease {
	g := &m.concurrency
	g.mu.Lock()
	defer g.mu.Unlock()
	g.initLocked()
	state := g.states[auth.ID]
	if state == nil {
		state = &concurrencyCounter{identity: concurrencyAuthIdentity(auth)}
		g.states[auth.ID] = state
	}
	limit, _ := credentialConcurrencyLimit(auth)
	if state.active >= limit {
		return nil
	}
	state.active++
	return &credentialLease{gate: g, state: state}
}

type concurrencyBudgetKey struct{}
type concurrencyBudget struct{ spent time.Duration }

func withConcurrencyBudget(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(concurrencyBudgetKey{}).(*concurrencyBudget); ok {
		return ctx
	}
	return context.WithValue(ctx, concurrencyBudgetKey{}, &concurrencyBudget{})
}
func concurrencyError(code string) error {
	return &Error{Code: code, Message: strings.ReplaceAll(code, "_", " "), HTTPStatus: http.StatusTooManyRequests}
}
func isConcurrencyError(err error) bool {
	if v, ok := err.(*Error); ok {
		return v.Code == "credential_concurrency_timeout" || v.Code == "credential_queue_full"
	}
	return false
}
func (m *Manager) waitCredentialCapacity(ctx context.Context, changed <-chan struct{}, pinned string) error {
	g := &m.concurrency
	g.mu.Lock()
	g.initLocked()
	if g.waiting >= credentialWaitLimit {
		g.mu.Unlock()
		return concurrencyError("credential_queue_full")
	}
	g.waiting++
	if pinned != "" {
		g.pinned[pinned]++
	}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.waiting--
		if pinned != "" {
			g.pinned[pinned]--
			if g.pinned[pinned] == 0 {
				delete(g.pinned, pinned)
			}
		}
		g.mu.Unlock()
	}()
	budget, _ := ctx.Value(concurrencyBudgetKey{}).(*concurrencyBudget)
	if budget == nil {
		budget = &concurrencyBudget{}
	}
	remaining := credentialWaitBudget - budget.spent
	if remaining <= 0 {
		return concurrencyError("credential_concurrency_timeout")
	}
	start := time.Now()
	defer func() { budget.spent += time.Since(start) }()
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return concurrencyError("credential_concurrency_timeout")
	case <-changed:
		return nil
	}
}
func (m *Manager) capacitySnapshot() (map[string]int, <-chan struct{}) {
	g := &m.concurrency
	g.mu.Lock()
	defer g.mu.Unlock()
	g.initLocked()
	out := make(map[string]int, len(g.states))
	for id, s := range g.states {
		out[id] = s.active
	}
	return out, g.changedCh
}

// Selection is serialized only until the lease is reserved; no I/O or waiting holds this lock.
func (m *Manager) pickNextMixedAdmitted(ctx context.Context, providers []string, model string, opts cliproxyexecutor.Options, tried map[string]struct{}) (*Auth, ProviderExecutor, string, *credentialLease, error) {
	if m.HomeEnabled() {
		a, e, p, err := m.pickNextMixed(ctx, providers, model, opts, tried)
		return a, e, p, nil, err
	}
	ctx = withConcurrencyBudget(ctx)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, "", nil, err
		}
		m.concurrencyPickMu.Lock()
		active, changed := m.capacitySnapshot()
		pinned := pinnedAuthIDFromMetadata(opts.Metadata)
		if pinned == "" {
			if affinity, ok := m.Selector().(*SessionAffinitySelector); ok {
				pinned, _ = affinity.boundAuthForRequest(providers, model, opts)
			}
		}
		excluded := maps.Clone(tried)
		if excluded == nil {
			excluded = make(map[string]struct{})
		}
		busy := false
		providerSet := make(map[string]bool, len(providers))
		for _, p := range providers {
			providerSet[canonicalSchedulingProvider(p)] = true
		}
		eligibility := authSelectionEligibilityForRequest(ctx, opts)
		m.mu.RLock()
		for id, a := range m.auths {
			if a == nil || a.Disabled || !providerSet[executorKeyFromAuth(a)] || !eligibility.allows(a) {
				continue
			}
			if _, used := tried[id]; used {
				continue
			}
			if pinned != "" && pinned != id {
				continue
			}
			if _, ok := m.executorLocked(executorKeyFromAuth(a)); !ok {
				continue
			}
			if model != "" && !m.authSupportsRouteModel(registry.GetGlobalRegistry(), a, model) {
				continue
			}
			limit, _ := credentialConcurrencyLimit(a)
			if active[id] < limit {
				continue
			}
			// A capacity wait is justified only by a credential otherwise available for this request.
			available, _, err := m.availableAuthsForSelector(ctx, m.selector, []*Auth{a}, "mixed", model, time.Now(), true, opts.Headers)
			if err == nil && len(available) > 0 {
				excluded[id] = struct{}{}
				busy = true
			}
		}
		m.mu.RUnlock()
		if pinned != "" {
			if _, full := excluded[pinned]; full {
				if _, already := tried[pinned]; !already && busy {
					m.concurrencyPickMu.Unlock()
					if err := m.waitCredentialCapacity(ctx, changed, pinned); err != nil {
						return nil, nil, "", nil, err
					}
					continue
				}
			}
		}
		a, e, p, err := m.pickNextMixed(ctx, providers, model, opts, excluded)
		var lease *credentialLease
		if err == nil && a != nil {
			m.mu.RLock()
			current := m.auths[a.ID]
			if current != nil {
				lease = m.tryCredentialLeaseLocked(current)
			}
			m.mu.RUnlock()
		}
		m.concurrencyPickMu.Unlock()
		if lease != nil {
			return a, e, p, lease, nil
		}
		if err != nil && !busy {
			return nil, nil, "", nil, err
		}
		if err := m.waitCredentialCapacity(ctx, changed, pinned); err != nil {
			return nil, nil, "", nil, err
		}
	}
}

// AcquireCredentialGeneration applies local admission to a pinned execution such as verification or a WS turn.
func (m *Manager) AcquireCredentialGeneration(ctx context.Context, id string) (func(), error) {
	if m.HomeEnabled() {
		return func() {}, nil
	}
	ctx = withConcurrencyBudget(ctx)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.concurrencyPickMu.Lock()
		_, changed := m.capacitySnapshot()
		m.mu.RLock()
		a := m.auths[id]
		if a == nil || a.Disabled {
			m.mu.RUnlock()
			m.concurrencyPickMu.Unlock()
			return nil, &Error{Code: "auth_unavailable", Message: "credential is no longer enabled", HTTPStatus: http.StatusConflict}
		}
		lease := m.tryCredentialLeaseLocked(a)
		m.mu.RUnlock()
		m.concurrencyPickMu.Unlock()
		if lease != nil {
			return lease.release, nil
		}
		if err := m.waitCredentialCapacity(ctx, changed, id); err != nil {
			return nil, err
		}
	}
}
func holdCredentialStream(ctx context.Context, result *cliproxyexecutor.StreamResult, lease *credentialLease) *cliproxyexecutor.StreamResult {
	if lease == nil {
		return result
	}
	if result == nil || result.Chunks == nil {
		lease.release()
		return result
	}
	source := result.Chunks
	out := make(chan cliproxyexecutor.StreamChunk)
	copy := *result
	copy.Chunks = out
	go func() {
		defer close(out)
		defer lease.release()
		for chunk := range source {
			select {
			case out <- chunk:
			case <-ctx.Done():
			}
		}
	}()
	return &copy
}

type CredentialConcurrencyEntry struct {
	AuthID           string `json:"auth_id"`
	AuthIndex        string `json:"auth_index"`
	Name             string `json:"name"`
	Provider         string `json:"provider"`
	Source           string `json:"source"`
	Editable         bool   `json:"editable"`
	ReadOnlyReason   string `json:"read_only_reason"`
	ConcurrencyLimit *int   `json:"concurrency_limit"`
	EffectiveLimit   int    `json:"effective_limit"`
	LimitSource      string `json:"limit_source"`
	Active           int    `json:"active"`
	PinnedWaiting    int    `json:"pinned_waiting"`
}

func (m *Manager) CredentialConcurrencySnapshot() map[string]any {
	if m.HomeEnabled() {
		return map[string]any{"mode": "home", "credentials": []CredentialConcurrencyEntry{}}
	}
	auths := m.List()
	g := &m.concurrency
	g.mu.Lock()
	defer g.mu.Unlock()
	g.initLocked()
	entries := make([]CredentialConcurrencyEntry, 0, len(auths))
	pinnedTotal := 0
	for _, n := range g.pinned {
		pinnedTotal += n
	}
	for _, a := range auths {
		limit, override := credentialConcurrencyLimit(a)
		name := a.FileName
		if name == "" {
			name = a.ID
		}
		entry := CredentialConcurrencyEntry{AuthID: a.ID, AuthIndex: a.Index, Name: name, Provider: a.Provider, Source: "file", Editable: true, ConcurrencyLimit: override, EffectiveLimit: limit, LimitSource: "default", PinnedWaiting: g.pinned[a.ID]}
		if override != nil {
			entry.LimitSource = "override"
		}
		if s := g.states[a.ID]; s != nil {
			entry.Active = s.active
		}
		source := a.AuthSourceKind()
		if source == AuthSourceConfig {
			entry.Source = "config"
		} else if source == AuthSourceMemory || source == "" {
			entry.Source = "runtime"
			entry.Editable = false
			entry.ReadOnlyReason = "runtime_source"
		}
		if reason := a.Attributes["concurrency_read_only"]; reason != "" {
			entry.Editable = false
			entry.ReadOnlyReason = reason
		}
		if IsPluginVirtualAuth(a) {
			entry.Source = "runtime"
			entry.Editable = false
			entry.ReadOnlyReason = "plugin_virtual"
		}
		entries = append(entries, entry)
	}
	return map[string]any{"mode": "local", "default_limit": DefaultCredentialConcurrency, "pool_waiting": g.waiting - pinnedTotal, "credentials": entries}
}

func (m *Manager) acquireCreditsCandidate(ctx context.Context, candidates []creditsCandidateEntry) (int, *credentialLease, error) {
	ctx = withConcurrencyBudget(ctx)
	for {
		if err := ctx.Err(); err != nil {
			return -1, nil, err
		}
		m.concurrencyPickMu.Lock()
		_, changed := m.capacitySnapshot()
		m.mu.RLock()
		found := false
		for i, candidate := range candidates {
			a := m.auths[candidate.auth.ID]
			if a == nil || a.Disabled {
				continue
			}
			found = true
			if lease := m.tryCredentialLeaseLocked(a); lease != nil {
				m.mu.RUnlock()
				m.concurrencyPickMu.Unlock()
				return i, lease, nil
			}
		}
		m.mu.RUnlock()
		m.concurrencyPickMu.Unlock()
		if !found {
			return -1, nil, &Error{Code: "auth_unavailable", Message: "credential is no longer enabled"}
		}
		pinned := ""
		if len(candidates) == 1 {
			pinned = candidates[0].auth.ID
		}
		if err := m.waitCredentialCapacity(ctx, changed, pinned); err != nil {
			return -1, nil, err
		}
	}
}
