// Package kilo provides the in-memory capacity core for native Kilo integration.
package kilo

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
)

const (
	DefaultProxyLimit = 10
	MaxProxyLimit     = 100
)

var (
	ErrProxyUnavailable = errors.New("kilo: no healthy eligible proxy")
	ErrProxyCapacity    = errors.New("kilo: healthy proxy capacity exhausted")
)

// Proxy contains no transport credentials. An empty AccountID makes it public.
// IDs identify unique proxy entries across the public and private configuration.
type Proxy struct {
	GroupLimit   int
	EntryLimit   int
	Group        string
	AutoReserved bool
	ID           string
	AccountID    string
	Healthy      bool
	FirstUsed    uint64
}

type RouteMode string

const (
	RouteProxy  RouteMode = "proxy"
	RouteDirect RouteMode = "direct"
)

// RouteDirect must be mapped to explicit direct transport, never an empty URL
// that could inherit a global or environment proxy.
type Route struct {
	Mode    RouteMode
	ProxyID string
}

type AcquireOptions struct {
	ProxyIDs        []string
	Groups          []string
	OwnedManagement bool
	Group           string
	// Transient leases do not publish temporary account preferences to disk.
	Transient                bool
	ProxyID                  string
	AllowDirectWhenProxyFull bool
	// A failed connection may retry healthy proxies but must never enable direct.
	RetryAfterProxyFailure bool
}

type proxyState struct {
	proxy      Proxy
	owner      string
	active     int
	management int
}

// ProxyPool only accounts for proxy slots. The caller must already hold CPA's
// AuthID admission lease, including for direct routes, retries and probes.
// It neither acquires nor releases that lease and does not reset its wait budget.
type ProxyPool struct {
	mu         sync.Mutex
	entries    []*proxyState
	byID       map[string]*proxyState
	limit      int
	changed    chan struct{}
	primary    map[string]string
	extras     map[string][]string
	candidates map[string][]string
	persist    func(accountID, proxyID string, primary bool) (uint64, error)
}

func NewProxyPool(proxies []Proxy, limit int) (*ProxyPool, error) {
	if limit == 0 {
		limit = DefaultProxyLimit
	}
	if err := validateLimit(limit); err != nil {
		return nil, err
	}
	p := &ProxyPool{limit: limit, byID: make(map[string]*proxyState), changed: make(chan struct{}), primary: map[string]string{}, extras: map[string][]string{}}
	for _, proxy := range proxies {
		if proxy.ID == "" {
			return nil, errors.New("kilo: proxy ID is required")
		}
		if _, exists := p.byID[proxy.ID]; exists {
			return nil, errors.New("kilo: duplicate proxy ID")
		}
		state := &proxyState{proxy: proxy, owner: proxy.AccountID}
		p.entries = append(p.entries, state)
		p.byID[proxy.ID] = state
	}
	return p, nil
}

func validateLimit(limit int) error {
	if limit < 1 || limit > MaxProxyLimit {
		return fmt.Errorf("kilo: proxy limit must be between 1 and %d", MaxProxyLimit)
	}
	return nil
}

// TryAcquire prefers private proxies and preserves configuration order within
// each source. Busy public proxies are unavailable to another account even when
// their owner's per-proxy limit has spare slots. Failed proxies add no capacity.
func (p *ProxyPool) TryAcquire(accountID string, options AcquireOptions) (*ProxyLease, error) {
	if accountID == "" {
		return nil, errors.New("kilo: account ID is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	healthy := 0
	allFull := true
	privateHealthy := false
	allowed := func(id string) bool {
		if options.ProxyIDs != nil && !slices.Contains(options.ProxyIDs, id) {
			return false
		}
		if options.ProxyID != "" {
			return id == options.ProxyID
		}
		if options.Transient {
			return true
		}
		ids := p.candidates[accountID]
		return ids == nil || slices.Contains(ids, id)
	}
	for _, state := range p.entries {
		if !options.Transient && allowed(state.proxy.ID) && state.proxy.Healthy && state.proxy.AccountID == accountID {
			privateHealthy = true
		}
	}
	eligible := func(s *proxyState) bool {
		if s != nil && options.Groups != nil && !slices.Contains(options.Groups, s.proxy.Group) {
			return false
		}
		if s != nil && options.Group != "" && s.proxy.Group != options.Group {
			return false
		}
		if s != nil && s.proxy.AutoReserved && s.proxy.AccountID == "" && !options.Transient {
			return false
		}
		return s != nil && allowed(s.proxy.ID) && s.proxy.Healthy && (s.proxy.AccountID == "" || ((!options.Transient || options.OwnedManagement) && s.proxy.AccountID == accountID))
	}
	available := func(s *proxyState) bool {
		if !eligible(s) || (s.owner != "" && s.owner != accountID) {
			return false
		}
		if s.proxy.Group != "" {
			for _, other := range p.entries {
				if other.proxy.Group == s.proxy.Group && other.owner != "" && other.owner != accountID {
					return false
				}
			}
		}
		return true
	}
	full := func(s *proxyState) bool {
		limit := p.entryLimit(s)
		if s.active >= limit {
			return true
		}
		if s.proxy.Group != "" && s.proxy.GroupLimit > 0 {
			total := 0
			for _, other := range p.entries {
				if other.proxy.Group == s.proxy.Group {
					total += other.active
				}
			}
			return total >= s.proxy.GroupLimit
		}
		return false
	}
	primary := p.byID[p.primary[accountID]]
	primaryValid := !options.Transient && available(primary) && (!privateHealthy || primary.proxy.AccountID == accountID)
	// A healthy extra becomes primary even while its existing leases fill it.
	if !primaryValid && !options.Transient {
		for _, id := range p.extras[accountID] {
			state := p.byID[id]
			if !available(state) || (privateHealthy && state.proxy.AccountID != accountID) {
				continue
			}
			if p.persist != nil && !options.Transient {
				first, err := p.persist(accountID, id, true)
				if err != nil {
					return nil, err
				}
				state.proxy.FirstUsed = first
			}
			for other, pid := range p.primary {
				if other != accountID && pid == id {
					delete(p.primary, other)
				}
			}
			p.primary[accountID] = id
			p.extras[accountID] = slices.DeleteFunc(p.extras[accountID], func(pid string) bool { return pid == id })
			primary, primaryValid = state, true
			break
		}
	}
	candidates := make([]*proxyState, 0, len(p.entries))
	if primaryValid {
		candidates = append(candidates, primary)
	}
	for _, id := range p.extras[accountID] {
		state := p.byID[id]
		if available(state) && (primaryValid || !privateHealthy || state.proxy.AccountID == accountID) {
			candidates = append(candidates, state)
		}
	}
	for _, private := range []bool{true, false} {
		tier := []*proxyState{}
		for _, state := range p.entries {
			if !eligible(state) || (state.proxy.AccountID != "") != private {
				continue
			}
			healthy++
			if !full(state) {
				allFull = false
			}
			tier = append(tier, state)
		}
		if !private {
			sort.SliceStable(tier, func(i, j int) bool {
				a, b := tier[i].proxy.FirstUsed, tier[j].proxy.FirstUsed
				if a == 0 {
					return false
				}
				if b == 0 {
					return true
				}
				return a < b
			})
		}
		candidates = append(candidates, tier...)
	}
	for _, state := range candidates {
		if !available(state) || full(state) {
			continue
		}
		promote := !primaryValid
		if p.persist != nil && ((!options.Transient && promote) || state.proxy.FirstUsed == 0) {
			first, err := p.persist(accountID, state.proxy.ID, promote && !options.Transient)
			if err != nil {
				return nil, err
			}
			state.proxy.FirstUsed = first
		}
		if promote && !options.Transient {
			for id, pid := range p.primary {
				if id != accountID && pid == state.proxy.ID {
					delete(p.primary, id)
				}
			}
			p.primary[accountID] = state.proxy.ID
			p.extras[accountID] = slices.DeleteFunc(p.extras[accountID], func(id string) bool { return id == state.proxy.ID })
		} else if !options.Transient && state != primary && !slices.Contains(p.extras[accountID], state.proxy.ID) {
			p.extras[accountID] = append(p.extras[accountID], state.proxy.ID)
		}
		state.owner = accountID
		state.active++
		if options.Transient {
			state.management++
		}
		p.signalLocked()
		return &ProxyLease{pool: p, state: state, transient: options.Transient, route: Route{Mode: RouteProxy, ProxyID: state.proxy.ID}}, nil
	}

	if healthy == 0 {
		return nil, ErrProxyUnavailable
	}
	if allFull && options.AllowDirectWhenProxyFull && !options.RetryAfterProxyFailure {
		return &ProxyLease{route: Route{Mode: RouteDirect}}, nil
	}
	return nil, ErrProxyCapacity
}

// Reconfigure preserves existing lease pointers and only changes future admission.
// The caller fences admission and validates sensitive changes before committing.
func (p *ProxyPool) Reconfigure(proxies []Proxy, limit int, accounts map[string]AccountEgress, persist func(string, string, bool) (uint64, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entries := make([]*proxyState, 0, len(proxies))
	for _, proxy := range proxies {
		state := p.byID[proxy.ID]
		if state == nil {
			state = &proxyState{}
			p.byID[proxy.ID] = state
		}
		state.proxy = proxy
		if state.active == 0 {
			state.owner = proxy.AccountID
		}
		entries = append(entries, state)
	}
	nextByID := make(map[string]*proxyState, len(entries))
	for _, state := range entries {
		nextByID[state.proxy.ID] = state
	}
	p.byID = nextByID
	p.entries, p.limit, p.persist = entries, limit, persist
	p.primary = map[string]string{}
	p.candidates = map[string][]string{}
	for id, a := range accounts {
		p.primary[id] = a.PrimaryProxyID
		p.candidates[id] = slices.Clone(a.CandidateProxyIDs)
	}
	for id, extras := range p.extras {
		p.extras[id] = slices.DeleteFunc(extras, func(pid string) bool {
			s := p.byID[pid]
			return s == nil || s.active == 0 || (s.proxy.AccountID != "" && s.proxy.AccountID != id) || p.primary[id] == pid
		})
	}
	p.signalLocked()
}

// Changes must be read before TryAcquire when a caller needs to wait. A closed
// channel means retry under the existing admission budget, not a new timeout.
func (p *ProxyPool) Changes() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.changed
}

func (p *ProxyPool) signalLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// SetLimit affects future acquisitions without interrupting in-flight leases.
func (p *ProxyPool) SetLimit(limit int) error {
	if err := validateLimit(limit); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.limit != limit {
		p.limit = limit
		p.signalLocked()
	}
	return nil
}

// SetHealthy updates observed health without terminating active requests.
// Network probing and retry policy belong to the caller.
func (p *ProxyPool) SetHealthy(proxyID string, healthy bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, exists := p.byID[proxyID]
	if !exists {
		return errors.New("kilo: proxy not found")
	}
	if state.proxy.Healthy != healthy {
		state.proxy.Healthy = healthy
		p.signalLocked()
	}
	return nil
}

type ProxySnapshot struct {
	Group            string
	InferenceActive  int
	ManagementActive int
	ID               string
	AccountID        string
	Owner            string
	Healthy          bool
	Active           int
	Limit            int
}

type PoolSnapshot struct {
	Limit   int
	Proxies []ProxySnapshot
}

// Snapshot returns a detached, credential-free view in configuration order.
func (p *ProxyPool) Snapshot() PoolSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := PoolSnapshot{Limit: p.limit, Proxies: make([]ProxySnapshot, 0, len(p.entries))}
	for _, state := range p.entries {
		result.Proxies = append(result.Proxies, ProxySnapshot{ID: state.proxy.ID,
			AccountID: state.proxy.AccountID, Owner: state.owner, Healthy: state.proxy.Healthy,
			Active: state.active, Limit: p.entryLimit(state), Group: state.proxy.Group, InferenceActive: state.active - state.management, ManagementActive: state.management})
	}
	return result
}

// ProxyLease releases only its proxy slot. The caller retains the same external
// account admission until the request actually ends, for either route mode.
type ProxyLease struct {
	transient bool
	once      sync.Once
	pool      *ProxyPool
	state     *proxyState
	route     Route
}

func (l *ProxyLease) Route() Route { return l.route }

// Release is safe to repeat and call concurrently, including on direct leases.
func (l *ProxyLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.pool == nil {
			return
		}
		l.pool.mu.Lock()
		defer l.pool.mu.Unlock()
		owner := l.state.owner
		l.state.active--
		if l.transient {
			l.state.management--
		}
		if l.state.active == 0 {
			l.pool.extras[owner] = slices.DeleteFunc(l.pool.extras[owner], func(id string) bool { return id == l.state.proxy.ID })
			if l.transient {
				delete(l.pool.primary, owner)
				delete(l.pool.extras, owner)
			}
		}
		if l.state.active == 0 && l.state.proxy.AccountID == "" {
			l.state.owner = ""
		}
		l.pool.signalLocked()
	})
}

func (p *ProxyPool) entryLimit(s *proxyState) int {
	if s.proxy.EntryLimit > 0 {
		return s.proxy.EntryLimit
	}
	return p.limit
}

func DirectLease() *ProxyLease { return &ProxyLease{route: Route{Mode: RouteDirect}} }
