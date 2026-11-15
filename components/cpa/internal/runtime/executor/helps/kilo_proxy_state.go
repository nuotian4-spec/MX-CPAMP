package helps

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

// The executor mutex owns persisted state; the runtime mutex owns transports and health.
func NewManagedKiloRuntime(cfg *config.Config) (*KiloRuntime, error) {
	if cfg.ValidateKiloState() != nil {
		return nil, kilo.ErrStateStorage
	}
	initial := kilo.ProxyState{Limit: cfg.Kilo.ProxyLimit, Accounts: map[string]kilo.AccountEgress{}}
	if initial.Limit == 0 {
		initial.Limit = kilo.DefaultProxyLimit
	}
	for i, p := range cfg.Kilo.Proxies {
		initial.Proxies = append(initial.Proxies, kilo.SavedProxy{ID: p.ID, URL: p.URL, Name: p.ID, AccountID: p.AccountID, Disabled: p.Disabled, Order: i})
	}
	store := kilo.ProxyStateStore{Dir: config.KiloStateDirectory(cfg)}
	state, managed, err := store.Load(initial)
	if err != nil {
		return nil, err
	}
	r, err := NewKiloRuntime(kiloStateConfig(state, cfg.Kilo.StateDir))
	if err != nil {
		return nil, err
	}
	r.state, r.managed, r.store = state, managed, store
	r.applyState(state)
	return r, nil
}
func kiloStateConfig(s kilo.ProxyState, dir string) config.KiloConfig {
	cfg := config.KiloConfig{StateDir: dir, ProxyLimit: s.Limit}
	for _, p := range s.Proxies {
		cfg.Proxies = append(cfg.Proxies, config.KiloProxy{ID: p.ID, URL: p.URL, AccountID: p.AccountID, Disabled: p.Disabled})
	}
	return cfg
}
func (r *KiloRuntime) applyState(next kilo.ProxyState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keep := map[string]bool{}
	for _, p := range next.Proxies {
		keep[p.ID] = true
	}
	for id := range r.urls {
		if keep[id] {
			continue
		}
		delete(r.urls, id)
		delete(r.health, id)
		for key, client := range r.clients {
			parts := strings.Split(key, "\x00")
			if len(parts) > 2 && parts[2] == id {
				client.CloseIdleConnections()
				delete(r.clients, key)
			}
		}
	}
	proxies := make([]kilo.Proxy, 0, len(next.Proxies))
	for _, p := range next.Proxies {
		if r.urls[p.ID] != p.URL {
			delete(r.health, p.ID)
			for key, client := range r.clients {
				client.CloseIdleConnections()
				delete(r.clients, key)
			}
		}
		r.urls[p.ID] = p.URL
		owner := p.AccountID
		healthy := !p.Disabled && r.health[p.ID].healthy && r.proxyAdmissionHealthy(p, r.health[p.ID])
		if p.FixedIP != "" {
			g := next.IPGroups[p.FixedIP]
			owner = g.Owner
			healthy = healthy && r.health[p.ID].observedIP == p.FixedIP && !g.Draining && !g.Retired
		}
		groupLimit, entryLimit := 0, 0
		if p.FixedIP != "" {
			groupLimit = next.IPCapacity()
			if owner == "" || next.Accounts[owner].CapacityMode == "ip" {
				entryLimit = groupLimit
				if next.IPEntryLimit != nil {
					entryLimit = *next.IPEntryLimit
				}
			}
		}
		proxies = append(proxies, kilo.Proxy{GroupLimit: groupLimit, EntryLimit: entryLimit, ID: p.ID, AccountID: owner, Healthy: healthy, FirstUsed: p.FirstUsed, Group: p.FixedIP, AutoReserved: p.FixedIP != ""})
	}
	r.config = kiloStateConfig(next, r.config.StateDir)
	r.pool.Reconfigure(proxies, next.Limit, next.Accounts, r.persistAllocation)
	r.state = next
	r.refreshPoolHealthLocked()
}
func (r *KiloRuntime) persistAllocation(accountID, proxyID string, primary bool) (uint64, error) {
	next := r.state.Clone()
	var first, maxFirst uint64
	for _, p := range next.Proxies {
		if p.FirstUsed > maxFirst {
			maxFirst = p.FirstUsed
		}
	}
	for _, entry := range next.Recycle {
		if entry.Proxy.FirstUsed > maxFirst {
			maxFirst = entry.Proxy.FirstUsed
		}
	}
	for i := range next.Proxies {
		if next.Proxies[i].ID == proxyID {
			if next.Proxies[i].FirstUsed == 0 {
				next.Proxies[i].FirstUsed = maxFirst + 1
			}
			first = next.Proxies[i].FirstUsed
		}
	}
	if primary && next.Accounts[accountID].Mode != "auto" {
		for id, a := range next.Accounts {
			if _, pending := next.PendingDevices[id]; !pending && id != accountID && a.PrimaryProxyID == proxyID {
				a.PrimaryProxyID = ""
				next.Accounts[id] = a
			}
		}
		egress := next.Accounts[accountID]
		egress.Mode, egress.PrimaryProxyID = "pool", proxyID
		next.Accounts[accountID] = egress
	}
	if !strings.HasPrefix(accountID, "device-") {
		if _, err := r.prepareDeviceProfiles(&next, accountID, deviceExitKey(next, proxyID)); err != nil {
			return 0, err
		}
	}
	next.Revision++
	if err := r.store.Save(next); err != nil {
		return 0, err
	}
	r.state, r.managed = next, true
	return first, nil
}

// DeviceProfileBindings requires the executor read lock, like StateView.
func (r *KiloRuntime) DeviceProfileBindings() map[string]map[string]string {
	bindings := make(map[string]map[string]string, len(r.state.Accounts))
	for id, account := range r.state.Accounts {
		bindings[id] = maps.Clone(account.DeviceProfiles)
	}
	return bindings
}

func (r *KiloRuntime) StateView() kilo.ProxyStateView {
	view := kilo.ProxyStateView{Revision: r.state.Revision, RuntimeRevision: r.state.Revision, Managed: r.managed, ProxyLimit: r.state.Limit, Proxies: []kilo.ProxyView{}, Accounts: []kilo.AccountEgressView{}, Recycle: []kilo.ArchivedProxyView{}}
	snapshot := r.pool.Snapshot()
	live := map[string]kilo.ProxySnapshot{}
	for _, p := range snapshot.Proxies {
		live[p.ID] = p
	}
	for _, p := range r.state.Proxies {
		setting, _ := proxyutil.Parse(p.URL)
		s := live[p.ID]
		ip, at, observation := r.proxyObservation(p.ID)
		ownerKind := "none"
		if s.Owner != "" {
			ownerKind = "account"
			if strings.HasPrefix(s.Owner, "device-") {
				ownerKind = "device"
			}
		}
		view.Proxies = append(view.Proxies, kilo.ProxyView{ProxyAdmissionView: p.AdmissionView(), ID: p.ID, Name: p.Name, Order: p.Order, Enabled: !p.Disabled, AccountID: p.AccountID, FixedIP: p.FixedIP, Scheme: setting.URL.Scheme, Host: setting.URL.Hostname(), Port: setting.URL.Port(), HasAuth: setting.URL.User != nil, Healthy: s.Healthy, Active: s.Active, InferenceActive: s.InferenceActive, ManagementActive: s.ManagementActive, EffectiveLimit: s.Limit, Owner: s.Owner, OwnerKind: ownerKind, ObservedExitIP: ip, ObservedAt: at, ObservationStatus: observation})
	}
	for _, entry := range r.state.Recycle {
		p := entry.Proxy
		setting, _ := proxyutil.Parse(p.URL)
		view.Recycle = append(view.Recycle, kilo.ArchivedProxyView{ProxyAdmissionView: p.AdmissionView(), ID: p.ID, Name: p.Name, Order: p.Order, Enabled: !p.Disabled, OriginalAccountID: p.AccountID, FixedIP: p.FixedIP, RetirementReason: r.state.IPGroups[p.FixedIP].Reason, Scheme: setting.URL.Scheme, Host: setting.URL.Hostname(), Port: setting.URL.Port(), HasAuth: setting.URL.User != nil, ArchivedAt: entry.ArchivedAt.UTC().Format(time.RFC3339Nano)})
	}
	for id, a := range r.state.Accounts {
		view.Accounts = append(view.Accounts, kilo.AccountEgressView{CapacityPolicy: a.CapacityPolicy.Normalized(), AccountID: id, Mode: a.Mode, PrimaryProxyID: a.PrimaryProxyID, CandidateProxyIDs: a.CandidateProxyIDs})
	}
	sort.Slice(view.Accounts, func(i, j int) bool { return view.Accounts[i].AccountID < view.Accounts[j].AccountID })
	r.AutoStateView(&view)
	return view
}

func (r *KiloRuntime) StateDirectory() string { return r.store.Dir }
func (r *KiloRuntime) ChangeState(c kilo.ProxyChanges, accounts map[string]kilo.EgressAccountStatus) (kilo.ProxyStateView, error) {
	return r.changeState(c, accounts, nil)
}
func (r *KiloRuntime) changeState(c kilo.ProxyChanges, accounts map[string]kilo.EgressAccountStatus, observation *kilo.ProxyChangeObservation) (kilo.ProxyStateView, error) {
	fail := func(err error) (kilo.ProxyStateView, error) { return kilo.ProxyStateView{}, err }
	if c.ExpectedRevision != r.state.Revision {
		return fail(kilo.ErrStateConflict)
	}
	for id, status := range accounts {
		if r.autoAccounts == nil {
			r.autoAccounts = map[string]kilo.EgressAccountStatus{}
		}
		previous := r.autoAccounts[id]
		previous.ManualLimit = status.ManualLimit
		r.autoAccounts[id] = previous
	}
	next := r.state.Clone()
	if err := r.changeAutoState(&next, c, accounts); err != nil {
		return fail(err)
	}
	if c.ClearRecycle {
		next.Recycle = nil
		return r.commitState(next)
	}
	if c.Archive {
		index := -1
		for i, p := range next.Proxies {
			if p.ID == c.ID {
				index = i
				break
			}
		}
		if index < 0 {
			return fail(kilo.ErrProxyMissing)
		}
		p := next.Proxies[index]
		if g := next.IPGroups[p.FixedIP]; p.FixedIP != "" && g.Owner != "" {
			return fail(kilo.ErrStateBusy)
		}
		if r.proxyPinned(p.ID) || r.pendingDeviceProxy(p.ID) {
			return fail(kilo.ErrStateBusy)
		}
		for _, live := range r.pool.Snapshot().Proxies {
			if live.ID == p.ID && live.Active > 0 {
				return fail(kilo.ErrStateBusy)
			}
		}
		if accounts[p.AccountID].Active > 0 {
			return fail(kilo.ErrStateBusy)
		}
		for id, a := range next.Accounts {
			if a.PrimaryProxyID == p.ID {
				if accounts[id].Active > 0 {
					return fail(kilo.ErrStateBusy)
				}
				a.PrimaryProxyID = ""
				next.Accounts[id] = a
			}
		}
		next.Recycle = append(next.Recycle, kilo.ArchivedProxy{Proxy: p, ArchivedAt: time.Now().UTC()})
		next.Proxies = append(next.Proxies[:index], next.Proxies[index+1:]...)
		return r.commitState(next)
	}

	if c.IPConcurrencyLimit != nil {
		next.IPConcurrencyLimit = *c.IPConcurrencyLimit
	}
	if c.SetIPEntryLimit {
		next.IPEntryLimit = c.IPEntryLimit
	}
	if c.Limit != nil {
		next.Limit = *c.Limit
	}
	if c.Create {
		if c.URL == nil {
			return fail(kilo.ErrStateInvalid)
		}
		c.ID = "kilo-proxy-" + uuid.NewString()
		next.Proxies = append(next.Proxies, kilo.SavedProxy{ID: c.ID, Order: len(next.Proxies), AdmissionStatus: "pending", AdmissionReason: "unverified"})
	}
	if c.ID != "" {
		found := false
		for i := range next.Proxies {
			p := &next.Proxies[i]
			if p.ID != c.ID {
				continue
			}
			found = true
			if c.URL != nil {
				if p.URL != *c.URL {
					p.AdmissionStatus, p.AdmissionReason = "pending", "unverified"
				}
				p.URL = *c.URL
			}
			if observation != nil {
				ip, reason := proxyObservationReason(observation.Err, observation.IP)
				if reason != "" {
					seedLegacyIPHistory(&next)
					p.AdmissionStatus, p.AdmissionReason = "pending", reason
				} else if err := r.admitProxy(&next, p, ip, time.Now().UTC()); err != nil {
					return fail(err)
				}
			}
			if c.Name != nil {
				p.Name = *c.Name
			}
			if c.Order != nil {
				p.Order = *c.Order
			}
			if c.Enabled != nil {
				p.Disabled = !*c.Enabled
			}
			if c.AccountID != nil {
				if *c.AccountID != "" && p.AdmissionState() == "pending" {
					return fail(kilo.ErrStateConflict)
				}
				p.AccountID = *c.AccountID
			}
		}
		if !found {
			return fail(kilo.ErrProxyMissing)
		}
	}
	if c.EgressAccount != "" {
		if c.Mode == "auto" && next.MaxUniqueAccounts < 1 {
			return fail(kilo.ErrStateInvalid)
		}
		for ip, g := range next.IPGroups {
			if g.Owner == c.EgressAccount && c.Mode != "auto" {
				if r.autoGroupBusy(ip, accounts) {
					return fail(kilo.ErrStateBusy)
				}
				returnAutoGroup(&next, ip, time.Now().UTC())
			}
		}
		if _, pending := r.state.PendingDevices[c.EgressAccount]; pending {
			return fail(kilo.ErrStateBusy)
		}
		if accounts[c.EgressAccount].Provider != "kilo" {
			return fail(kilo.ErrStateInvalid)
		}
		if accounts[c.EgressAccount].Active > 0 && !(c.Mode == "auto" && next.Accounts[c.EgressAccount].Mode == "auto") {
			return fail(kilo.ErrStateBusy)
		}
		wanted := map[string]bool{}
		for _, id := range c.ExclusiveProxyIDs {
			if id == "" || wanted[id] {
				return fail(kilo.ErrStateInvalid)
			}
			wanted[id] = true
		}
		for i := range next.Proxies {
			p := &next.Proxies[i]
			if wanted[p.ID] {
				if p.AdmissionState() == "pending" {
					return fail(kilo.ErrStateConflict)
				}
				if p.FixedIP != "" {
					return fail(kilo.ErrStateConflict)
				}
				if p.AccountID != "" && p.AccountID != c.EgressAccount {
					return fail(kilo.ErrStateInvalid)
				}
				p.AccountID = c.EgressAccount
				delete(wanted, p.ID)
			} else if p.AccountID == c.EgressAccount {
				p.AccountID = ""
			}
		}
		if len(wanted) > 0 {
			return fail(kilo.ErrStateInvalid)
		}
		for _, p := range next.Proxies {
			if p.AdmissionState() == "pending" && (c.PrimaryProxyID == p.ID || slices.Contains(c.CandidateProxyIDs, p.ID)) {
				return fail(kilo.ErrStateConflict)
			}
		}
		next.Accounts[c.EgressAccount] = kilo.AccountEgress{CapacityPolicy: c.CapacityPolicy, Mode: c.Mode, PrimaryProxyID: c.PrimaryProxyID, CandidateProxyIDs: c.CandidateProxyIDs}
	}
	if c.EgressAccount != "" && c.Mode == "auto" {
		if err := r.applyIPPolicy(&next, c.EgressAccount, false); err != nil {
			return fail(err)
		}
	}
	old := map[string]kilo.SavedProxy{}
	for _, p := range r.state.Proxies {
		old[p.ID] = p
	}
	live := map[string]kilo.ProxySnapshot{}
	for _, p := range r.pool.Snapshot().Proxies {
		live[p.ID] = p
	}
	if c.EgressAccount != "" {
		previous := r.state.Accounts[c.EgressAccount]
		if !(c.Mode == "auto" && previous.Mode == "auto") && previous.PrimaryProxyID != c.PrimaryProxyID && (live[previous.PrimaryProxyID].Active > 0 || live[c.PrimaryProxyID].Active > 0) {
			return fail(kilo.ErrStateBusy)
		}
	}
	for _, p := range next.Proxies {
		previous := old[p.ID]
		if p.FixedIP != "" {
			if p.AccountID != "" || p.URL != previous.URL {
				return fail(kilo.ErrStateConflict)
			}
			for _, a := range next.Accounts {
				if a.Mode != "auto" && (a.PrimaryProxyID == p.ID || slices.Contains(a.CandidateProxyIDs, p.ID)) {
					return fail(kilo.ErrStateConflict)
				}
			}
		}
		if p.AccountID != "" && p.AccountID != previous.AccountID && accounts[p.AccountID].Provider != "kilo" {
			return fail(kilo.ErrStateInvalid)
		}
		if p.URL != previous.URL || p.AccountID != previous.AccountID {
			if previous.FixedIP != "" {
				return fail(kilo.ErrStateConflict)
			}
			if r.proxyPinned(p.ID) || r.pendingDeviceProxy(p.ID) {
				return fail(kilo.ErrStateBusy)
			}
			for id, a := range next.Accounts {
				if a.PrimaryProxyID == p.ID && accounts[id].Active > 0 {
					return fail(kilo.ErrStateBusy)
				}
			}
			if live[p.ID].Active > 0 || accounts[p.AccountID].Active > 0 || accounts[previous.AccountID].Active > 0 {
				return fail(kilo.ErrStateBusy)
			}
			if p.AccountID != previous.AccountID {
				for id, a := range next.Accounts {
					if a.PrimaryProxyID == p.ID && p.AccountID != "" && p.AccountID != id {
						if accounts[id].Active > 0 {
							return fail(kilo.ErrStateBusy)
						}
						a.PrimaryProxyID = ""
						next.Accounts[id] = a
					}
				}
			}
		}
	}
	if _, err := r.prepareDeviceProfiles(&next, c.EgressAccount, ""); err != nil {
		return fail(err)
	}
	view, err := r.commitState(next)
	if err == nil && observation != nil {
		ip, reason := proxyObservationReason(observation.Err, observation.IP)
		r.publishProxyObservation(c.ID, observation.Err, ip, reason)
		view = r.StateView()
	}
	return view, err
}

func (r *KiloRuntime) commitState(next kilo.ProxyState) (kilo.ProxyStateView, error) {
	if next.Validate() != nil {
		return kilo.ProxyStateView{}, kilo.ErrStateInvalid
	}
	next.Sort()
	next.Revision++
	if err := r.store.Save(next); err != nil {
		return kilo.ProxyStateView{}, err
	}
	r.cancelDisabledPins(next)
	r.applyState(next)
	r.managed = true
	return r.StateView(), nil
}

func (r *KiloRuntime) proxyObservation(id string) (*string, *time.Time, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.health[id]
	var ip *string
	var at *time.Time
	if h.observedIP != "" {
		v := h.observedIP
		ip = &v
		t := h.observedAt.UTC()
		at = &t
	}
	status := "unknown"
	if h.pending {
		status = "pending"
	} else if h.checked && !h.healthy {
		status = "failed"
	} else if ip != nil {
		status = "ok"
		if time.Since(h.observedAt) >= time.Minute {
			status = "stale"
		}
	}
	return ip, at, status
}
