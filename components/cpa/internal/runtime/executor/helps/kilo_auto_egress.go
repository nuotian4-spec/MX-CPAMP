package helps

import (
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

func (r *KiloRuntime) autoGroupHealthy(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, p := range r.state.Proxies {
		if p.FixedIP != ip {
			continue
		}
		if p.Disabled {
			continue
		}
		if p.AdmissionState() == "pending" {
			return false
		}
		h := r.health[p.ID]
		if !h.checked || !h.healthy || h.pending || h.observedIP != ip || !r.proxyAdmissionHealthy(p, h) {
			return false
		}
		found = true
	}
	return found
}

// refreshPoolHealthLocked fences unconfirmed aliases and fixed-IP drift.
func (r *KiloRuntime) refreshPoolHealthLocked() {
	for _, p := range r.state.Proxies {
		h := r.health[p.ID]
		healthy := !p.Disabled && h.checked && h.healthy && !h.pending && r.proxyAdmissionHealthy(p, h)
		if p.FixedIP != "" {
			g := r.state.IPGroups[p.FixedIP]
			healthy = healthy && h.observedIP == p.FixedIP && !g.Draining && !g.Retired
		} else if _, reserved := r.state.IPGroups[h.observedIP]; reserved {
			healthy = false
		}
		_ = r.pool.SetHealthy(p.ID, healthy)
	}
}
func (r *KiloRuntime) autoGroupBusy(ip string, accounts map[string]kilo.EgressAccountStatus, saveOwner ...string) bool {
	live := map[string]int{}
	for _, p := range r.pool.Snapshot().Proxies {
		live[p.ID] = p.Active
	}
	for _, p := range r.state.Proxies {
		pinned := false
		for owner, pin := range r.devicePins {
			if !pin.cancelled && pin.authorization.ProxyID == p.ID && (len(saveOwner) == 0 || owner != saveOwner[0]) {
				pinned = true
			}
		}
		if p.FixedIP == ip && (live[p.ID] > 0 || pinned || r.pendingDeviceProxy(p.ID)) {
			return true
		}
	}
	return false
}
func (r *KiloRuntime) autoCommit(next kilo.ProxyState) bool {
	_, err := r.prepareDeviceProfiles(&next, "", "")
	if err != nil {
		r.profileError = err
	}
	if err == nil {
		_, err = r.commitState(next)
	}
	if err != nil {
		r.profileError = err
		code := "storage_failed"
		r.autoError = &code
		return false
	}
	r.autoError = nil
	return true
}

func returnAutoGroup(next *kilo.ProxyState, ip string, now time.Time) {
	g := next.IPGroups[ip]
	owner := g.Owner
	g.Owner, g.Draining = "", false
	g.Retired = next.MaxUniqueAccounts > 0 && len(g.AccountIDs) >= next.MaxUniqueAccounts
	g.Reason = ""
	if g.Retired {
		g.Reason = "unique_account_limit"
	}
	next.IPGroups[ip] = g
	a := next.Accounts[owner]
	for _, p := range next.Proxies {
		if p.FixedIP == ip && p.ID == a.PrimaryProxyID {
			a.PrimaryProxyID = ""
		}
	}
	a.CandidateProxyIDs = nil
	next.Accounts[owner] = a
	if g.Retired {
		kept := []kilo.SavedProxy{}
		for _, p := range next.Proxies {
			if p.FixedIP == ip {
				next.Recycle = append(next.Recycle, kilo.ArchivedProxy{Proxy: p, ArchivedAt: now})
			} else {
				kept = append(kept, p)
			}
		}
		next.Proxies = kept
	}
}

func (r *KiloRuntime) changeAutoState(next *kilo.ProxyState, c kilo.ProxyChanges, accounts map[string]kilo.EgressAccountStatus) error {
	if c.MaxUniqueAccounts != nil {
		if *c.MaxUniqueAccounts < 1 {
			return kilo.ErrStateInvalid
		}
		next.MaxUniqueAccounts = *c.MaxUniqueAccounts
	}
	if c.AutoAssignmentEnabled != nil {
		if *c.AutoAssignmentEnabled && next.MaxUniqueAccounts < 1 {
			return kilo.ErrStateInvalid
		}
		if *c.AutoAssignmentEnabled && !next.AutoAssignmentEnabled {
			next.AutoEnabledAt = time.Now().UTC()
		}
		next.AutoAssignmentEnabled = *c.AutoAssignmentEnabled
	}
	if c.FixedIP == nil {
		return nil
	}
	ip, ok := kilo.FixedPublicIP(*c.FixedIP)
	if !ok {
		return kilo.ErrStateInvalid
	}
	index := -1
	for i, p := range next.Proxies {
		if p.ID == c.ID {
			index = i
		}
	}
	if index < 0 {
		return kilo.ErrProxyMissing
	}
	p := next.Proxies[index]
	if p.AdmissionState() == "pending" {
		return kilo.ErrProxyHealth
	}
	if p.FixedIP != "" && p.FixedIP != ip {
		g := next.IPGroups[p.FixedIP]
		if g.Owner != "" || r.autoGroupBusy(p.FixedIP, accounts) {
			return kilo.ErrStateBusy
		}
	}
	r.mu.Lock()
	h := r.health[p.ID]
	r.mu.Unlock()
	if !h.checked || !h.healthy || h.pending || h.observedIP != ip {
		return kilo.ErrProxyHealth
	}
	if next.IPGroups == nil {
		next.IPGroups = map[string]kilo.IPGroup{}
	}
	g, exists := next.IPGroups[ip]
	if exists && (g.Owner != "" || g.Retired) {
		return kilo.ErrStateBusy
	}
	// One confirmation covers all currently observed entries for the same exit IP.
	for i, p := range next.Proxies {
		r.mu.Lock()
		observed := r.health[p.ID]
		r.mu.Unlock()
		if p.ID != c.ID && (observed.observedIP != ip || p.AdmissionState() == "pending") {
			continue
		}
		if p.AccountID != "" || r.proxyPinned(p.ID) || r.pendingDeviceProxy(p.ID) {
			return kilo.ErrStateBusy
		}
		for _, live := range r.pool.Snapshot().Proxies {
			if live.ID == p.ID && live.Active > 0 {
				return kilo.ErrStateBusy
			}
		}
		for id, a := range next.Accounts {
			if a.Mode != "auto" && (a.PrimaryProxyID == p.ID || slices.Contains(a.CandidateProxyIDs, p.ID)) {
				_ = id
				return kilo.ErrStateConflict
			}
		}
		next.Proxies[i].FixedIP = ip
	}
	if !exists {
		next.IPGroups[ip] = kilo.IPGroup{IP: ip, AccountIDs: []string{}}
	}
	for i := range next.Proxies {
		q := &next.Proxies[i]
		if q.FixedIP != ip || q.AdmissionState() == "pending" {
			continue
		}
		r.mu.Lock()
		h := r.health[q.ID]
		r.mu.Unlock()
		if h.checked && h.healthy && !h.pending && h.observedIP == ip {
			if err := r.admitProxy(next, q, ip, time.Now().UTC()); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReconcileAuto commits draining before release and never transfers active leases.
// The Manager holds its admission gate while the executor owns this state.
func (r *KiloRuntime) ReconcileAuto(accounts map[string]kilo.EgressAccountStatus, now time.Time) {
	r.autoAccounts = accounts
	for _, p := range r.state.Proxies {
		if p.FixedIP != "" {
			r.prepareProxyHealth(r.state.IPGroups[p.FixedIP].Owner, false, p.ID)
		}
	}
	next := r.state.Clone()
	changed := false
	for id, a := range accounts {
		if a.Provider != "kilo" || !r.state.AutoAssignmentEnabled || a.CreatedAt.Before(r.state.AutoEnabledAt) {
			continue
		}
		if _, exists := next.Accounts[id]; !exists {
			next.Accounts[id] = kilo.AccountEgress{Mode: "auto"}
			changed = true
		}
	}
	for ip, g := range next.IPGroups {
		if g.Owner == "" || g.Draining {
			continue
		}
		a, exists := accounts[g.Owner]
		fault := next.Accounts[g.Owner].CapacityMode == "ip" && r.autoGroupFailed(ip)
		if !exists || a.Provider != "kilo" || (a.ReturnRequested && a.RestrictionPersisted) || fault {
			g.Draining = true
			g.Reason = "quota_return"
			if fault {
				g.Reason = "egress_unavailable"
			}
			if !exists || a.Provider != "kilo" {
				g.Reason = "account_removed"
			}
			next.IPGroups[ip] = g
			changed = true
		}
	}
	if changed && !r.autoCommit(next) {
		return
	}
	// A committed draining state is never undone by a simultaneous recovery.
	for ip, g := range r.state.IPGroups {
		if !g.Draining || r.autoGroupBusy(ip, accounts) {
			continue
		}
		next = r.state.Clone()
		returnAutoGroup(&next, ip, now)
		if !r.autoCommit(next) {
			return
		}
	}
	for id, a := range r.state.Accounts {
		if a.CapacityMode == "ip" && a.AllocationMode == "specified" && accounts[id].CanAllocate && !r.specifiedSetComplete(id) {
			next = r.state.Clone()
			if r.applyIPPolicy(&next, id, false) == nil {
				if !r.autoCommit(next) {
					return
				}
			}
		}
	}
	if !r.state.AutoAssignmentEnabled {
		return
	}
	ids := []string{}
	for id, a := range r.state.Accounts {
		if a.Mode == "auto" && a.Normalized().AllocationMode != "specified" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	ips := []string{}
	for ip := range r.state.IPGroups {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	for _, id := range ids {
		a, exists := accounts[id]
		if !exists || !a.CanAllocate {
			continue
		}
		owned := false
		for _, g := range r.state.IPGroups {
			if g.Owner == id {
				owned = true
				break
			}
		}
		if owned {
			continue
		}
		if r.state.MaxUniqueAccounts < 1 {
			continue
		}
		for _, ip := range ips {
			g := r.state.IPGroups[ip]
			if g.Owner != "" || g.Draining || g.Retired || len(g.AccountIDs) >= r.state.MaxUniqueAccounts || !r.autoGroupHealthy(ip) || r.autoGroupBusy(ip, accounts) {
				continue
			}
			proxyIDs := []string{}
			conflict := false
			for _, p := range r.state.Proxies {
				if p.FixedIP == ip {
					if p.AccountID != "" {
						conflict = true
					}
					proxyIDs = append(proxyIDs, p.ID)
				}
			}
			if conflict || len(proxyIDs) == 0 {
				continue
			}
			next = r.state.Clone()
			g.Owner = id
			if !slices.Contains(g.AccountIDs, id) {
				g.AccountIDs = append(g.AccountIDs, id)
			}
			next.IPGroups[ip] = g
			policy := next.Accounts[id]
			policy.PrimaryProxyID = proxyIDs[0]
			next.Accounts[id] = policy
			if !r.autoCommit(next) {
				return
			}
			break
		}
	}
}

func (r *KiloRuntime) autoAssigned(id string) bool {
	for ip, g := range r.state.IPGroups {
		if g.Owner == id && !g.Draining && !g.Retired && r.autoGroupHealthy(ip) {
			return true
		}
	}
	return false
}
func (r *KiloRuntime) AutoStateView(view *kilo.ProxyStateView) {
	view.IPConcurrencyLimit = r.state.IPCapacity()
	view.IPEntryLimit = r.state.IPEntryLimit
	view.AutoAssignmentEnabled = r.state.AutoAssignmentEnabled
	view.MaxUniqueAccounts = r.state.MaxUniqueAccounts
	view.IPGroups = []kilo.IPGroupView{}
	view.AutoError = r.autoError
	for ip, g := range r.state.IPGroups {
		state := "available"
		if g.Retired {
			state = "retired"
		} else if g.Draining {
			state = "draining"
		} else if g.Owner != "" {
			state = "assigned"
			if r.state.MaxUniqueAccounts > 0 && len(g.AccountIDs) >= r.state.MaxUniqueAccounts {
				state = "retiring"
			}
		} else if !r.autoGroupHealthy(ip) {
			state = "unverified"
		} else if r.state.MaxUniqueAccounts < 1 || len(g.AccountIDs) >= r.state.MaxUniqueAccounts {
			state = "at_limit"
		}
		ids := []string{}
		for _, p := range r.state.Proxies {
			if p.FixedIP == ip {
				ids = append(ids, p.ID)
			}
		}
		g.AccountIDs = append([]string{}, g.AccountIDs...)
		groupView := kilo.IPGroupView{IPGroup: g, UniqueAccounts: len(g.AccountIDs), MaxUniqueAccounts: r.state.MaxUniqueAccounts, State: state, ProxyIDs: ids, Capacity: r.state.IPCapacity(), EntryLimit: r.state.IPEntryLimit}
		for _, p := range view.Proxies {
			if p.FixedIP == ip {
				groupView.InferenceActive += p.InferenceActive
				groupView.ManagementActive += p.ManagementActive
			}
		}
		groupView.Active = groupView.InferenceActive + groupView.ManagementActive
		view.IPGroups = append(view.IPGroups, groupView)
	}
	sort.Slice(view.IPGroups, func(i, j int) bool { return view.IPGroups[i].IP < view.IPGroups[j].IP })
	r.capacityStateView(view)
	for i := range view.Accounts {
		a := &view.Accounts[i]
		if a.Mode != "auto" {
			a.AllocationState = "manual"
			continue
		}
		a.AllocationState = "waiting"
		for _, g := range view.IPGroups {
			if g.Owner == a.AccountID {
				a.AllocationState = "assigned"
				a.AllocationReason = g.Reason
				if g.Draining {
					a.AllocationState = "draining"
				} else if !r.autoGroupHealthy(g.IP) {
					a.AllocationState = "egress_unavailable"
				}
			}
		}
		if a.CapacityMode == "ip" && a.EgressReady {
			a.AllocationState = "assigned"
		}
		if !a.EgressReady && len(a.AssignedIPs) == 0 {
			a.AllocationReason = "capacity_wait"
		}
		if a.AllocationMode == "specified" {
			if !a.EgressReady && len(a.AssignedIPs) == 0 {
				a.AllocationState = "waiting"
				a.AllocationReason = "specified_conflict"
			}
			if a.EgressReady {
				for _, ip := range a.SpecifiedIPs {
					g := r.state.IPGroups[ip]
					if g.Owner != a.AccountID || g.Draining || g.Retired || !r.autoGroupHealthy(ip) {
						a.AllocationReason = "partial_egress_unavailable"
						break
					}
				}
			}
		}
		if r.autoError != nil {
			a.AllocationReason = *r.autoError
		}
	}
}

// AcquireQuotaManagement needs only a free query route, never inference ownership.
func (r *KiloRuntime) AcquireQuotaManagement(accountID string) (*http.Client, func(), error) {
	owner := "management-" + uuid.NewString()
	for _, p := range r.state.Proxies {
		if p.Disabled || p.AccountID != "" || r.proxyPinned(p.ID) || r.pendingDeviceProxy(p.ID) {
			continue
		}
		if allowed := r.state.Accounts[accountID].CandidateProxyIDs; allowed != nil && !slices.Contains(allowed, p.ID) {
			continue
		}
		if p.FixedIP != "" {
			g := r.state.IPGroups[p.FixedIP]
			if (g.Owner != "" && (g.Owner != accountID || r.state.Accounts[accountID].CapacityMode != "ip")) || g.Draining || g.Retired || !r.autoGroupHealthy(p.FixedIP) {
				continue
			}
		}
		r.mu.Lock()
		h := r.health[p.ID]
		r.mu.Unlock()
		if !h.healthy || !h.checked {
			continue
		}
		leaseOwner := owner
		own := p.FixedIP != "" && r.state.IPGroups[p.FixedIP].Owner == accountID
		if own {
			leaseOwner = accountID
		}
		lease, err := r.pool.TryAcquire(leaseOwner, kilo.AcquireOptions{Transient: true, OwnedManagement: own, ProxyID: p.ID})
		if err != nil {
			continue
		}
		transport, mode, err := proxyutil.BuildHTTPTransport(p.URL)
		if err != nil || mode != proxyutil.ModeProxy || transport == nil {
			lease.Release()
			continue
		}
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		return client, func() { client.CloseIdleConnections(); lease.Release() }, nil
	}
	return nil, nil, kilo.ErrProxyUnavailable
}

// Unknown startup observations do not discard persisted owners.
func (r *KiloRuntime) autoGroupFailed(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	enabled := 0
	for _, p := range r.state.Proxies {
		if p.FixedIP != ip || p.Disabled {
			continue
		}
		enabled++
		h := r.health[p.ID]
		if h.checked && (!h.healthy || h.observedIP != ip) {
			return true
		}
	}
	return enabled == 0
}
