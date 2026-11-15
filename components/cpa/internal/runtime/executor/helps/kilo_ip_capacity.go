package helps

import (
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"slices"
	"time"
)

// applyIPPolicy validates the entire specified set before changing any owner/history.
func (r *KiloRuntime) applyIPPolicy(next *kilo.ProxyState, id string, allowWaiting bool, saveOwner ...string) error {
	a := next.Accounts[id]
	policy := a.Normalized()
	if err := policy.Validate(a.Mode); err != nil {
		return err
	}
	owned := next.OwnedIPs(id)
	if policy.CapacityMode != "ip" {
		primary := r.state.Accounts[id].PrimaryProxyID
		for _, p := range next.Proxies {
			if p.ID == primary && slices.Contains(owned, p.FixedIP) {
				owned = append([]string{p.FixedIP}, slices.DeleteFunc(owned, func(ip string) bool { return ip == p.FixedIP })...)
				break
			}
		}
		for _, ip := range owned[min(1, len(owned)):] {
			g := next.IPGroups[ip]
			g.Draining = true
			g.Reason = "fixed_capacity"
			next.IPGroups[ip] = g
		}
		return nil
	}
	if policy.AllocationMode == "specified" {
		conflict := false
		for _, ip := range policy.SpecifiedIPs {
			g, ok := next.IPGroups[ip]
			if !ok || g.Retired || g.Draining || (g.Owner != "" && g.Owner != id) || (g.Owner == "" && (!r.autoGroupHealthy(ip) || r.autoGroupBusy(ip, nil, saveOwner...) || next.MaxUniqueAccounts < 1 || len(g.AccountIDs) >= next.MaxUniqueAccounts)) {
				conflict = true
			}
		}
		for _, ip := range owned {
			if !slices.Contains(policy.SpecifiedIPs, ip) && r.autoGroupBusy(ip, nil, saveOwner...) {
				conflict = true
			}
		}
		if conflict {
			if allowWaiting {
				return nil
			}
			return kilo.ErrStateConflict
		}
		for _, ip := range owned {
			if !slices.Contains(policy.SpecifiedIPs, ip) {
				returnAutoGroup(next, ip, time.Now().UTC())
			}
		}
		for _, ip := range policy.SpecifiedIPs {
			g := next.IPGroups[ip]
			g.Owner = id
			if !slices.Contains(g.AccountIDs, id) {
				g.AccountIDs = append(g.AccountIDs, id)
			}
			next.IPGroups[ip] = g
		}
	} else if len(owned) > policy.MaxIPs {
		for _, ip := range owned[policy.MaxIPs:] {
			g := next.IPGroups[ip]
			g.Draining = true
			g.Reason = "max_ips_reduced"
			next.IPGroups[ip] = g
		}
	}
	return nil
}
func (r *KiloRuntime) ipPolicyReady(id string) bool {
	a := r.state.Accounts[id].Normalized()
	if a.AllocationMode == "specified" {
		// Owners are published only after the complete set passes the transaction.
		// A remaining owner therefore proves application even after partial drain.
		for _, ip := range a.SpecifiedIPs {
			g := r.state.IPGroups[ip]
			if g.Owner == id && !g.Draining && !g.Retired && r.autoGroupHealthy(ip) {
				return true
			}
		}
		return false
	}
	return r.autoAssigned(id)
}

// expandIPCapacity runs inside the executor's existing admission lock.
func (r *KiloRuntime) expandIPCapacity(id string) bool {
	a := r.state.Accounts[id].Normalized()
	if a.CapacityMode != "ip" || a.AllocationMode != "auto" || !r.state.AutoAssignmentEnabled || r.state.MaxUniqueAccounts < 1 {
		return false
	}
	owned := r.state.OwnedIPs(id)
	if len(owned) >= a.MaxIPs {
		return false
	}
	if len(owned) > 0 {
		for other, status := range r.autoAccounts {
			policy := r.state.Accounts[other]
			if other != id && status.CanAllocate && policy.Mode == "auto" && policy.Normalized().AllocationMode == "auto" && len(r.state.OwnedIPs(other)) == 0 {
				return false
			}
		}
	}
	ips := []string{}
	for ip := range r.state.IPGroups {
		ips = append(ips, ip)
	}
	slices.Sort(ips)
	for _, ip := range ips {
		g := r.state.IPGroups[ip]
		if g.Owner != "" || g.Draining || g.Retired || len(g.AccountIDs) >= r.state.MaxUniqueAccounts || !r.autoGroupHealthy(ip) || r.autoGroupBusy(ip, nil) {
			continue
		}
		next := r.state.Clone()
		g.Owner = id
		if !slices.Contains(g.AccountIDs, id) {
			g.AccountIDs = append(g.AccountIDs, id)
		}
		next.IPGroups[ip] = g
		return r.autoCommit(next)
	}
	return false
}
func (r *KiloRuntime) ConfiguredIPCapacity(id string) (int, bool) {
	a := r.state.Accounts[id].Normalized()
	if a.CapacityMode != "ip" {
		return 0, false
	}
	return a.MaxIPs * r.state.IPCapacity(), true
}
func (r *KiloRuntime) capacityStateView(view *kilo.ProxyStateView) {
	for i := range view.Accounts {
		a := &view.Accounts[i]
		a.AssignedIPs = r.state.OwnedIPs(a.AccountID)
		a.EntryLimit = r.state.IPEntryLimit
		a.ManualLimit = r.autoAccounts[a.AccountID].ManualLimit
		a.EffectiveLimit = 20
		if a.ManualLimit != nil {
			a.EffectiveLimit = *a.ManualLimit
		}
		if a.CapacityMode == "ip" {
			a.ConfiguredCapacity = a.MaxIPs * r.state.IPCapacity()
			a.EffectiveLimit = a.ConfiguredCapacity
			if a.ManualLimit != nil {
				a.EffectiveLimit = min(a.EffectiveLimit, *a.ManualLimit)
			}
		}
		for _, g := range view.IPGroups {
			if g.Owner != a.AccountID {
				continue
			}
			a.InferenceActive += g.InferenceActive
			a.ManagementActive += g.ManagementActive
			if !g.Draining && !g.Retired && r.autoGroupHealthy(g.IP) {
				entries := 0
				for _, p := range view.Proxies {
					if p.FixedIP == g.IP && p.Healthy && p.Enabled {
						entries += p.EffectiveLimit
					}
				}
				a.CurrentCapacity += min(g.Capacity, entries)
			}
		}
		if a.CapacityMode == "ip" {
			a.CurrentCapacity = min(a.CurrentCapacity, a.EffectiveLimit)
		} else {
			a.CurrentCapacity = 0
		}
		a.EgressReady = a.Mode != "auto" || r.ipPolicyReady(a.AccountID)
		if len(a.AssignedIPs) > 0 {
			a.AssignedIP = a.AssignedIPs[0]
		}
	}
}

func (r *KiloRuntime) specifiedSetComplete(id string) bool {
	a := r.state.Accounts[id].Normalized()
	for _, ip := range a.SpecifiedIPs {
		g := r.state.IPGroups[ip]
		if g.Owner != id || g.Draining || g.Retired {
			return false
		}
	}
	return len(a.SpecifiedIPs) > 0
}
