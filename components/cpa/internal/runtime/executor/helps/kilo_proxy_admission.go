package helps

import (
	"errors"
	"slices"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

func (r *KiloRuntime) ProxyForChange(c kilo.ProxyChanges) (kilo.SavedProxy, error) {
	if c.ExpectedRevision != r.state.Revision {
		return kilo.SavedProxy{}, kilo.ErrStateConflict
	}
	if c.URL == nil {
		return kilo.SavedProxy{}, kilo.ErrStateInvalid
	}
	setting, err := proxyutil.Parse(*c.URL)
	if err != nil || setting.Mode != proxyutil.ModeProxy {
		return kilo.SavedProxy{}, kilo.ErrStateInvalid
	}
	if c.Create {
		return kilo.SavedProxy{URL: *c.URL}, nil
	}
	p, err := r.ProxyForProbe(c.ExpectedRevision, c.ID, false)
	if err != nil {
		return kilo.SavedProxy{}, err
	}
	if p.FixedIP != "" && p.URL != *c.URL {
		return kilo.SavedProxy{}, kilo.ErrStateConflict
	}
	return p, nil
}

func (r *KiloRuntime) ChangeObservedState(c kilo.ProxyChanges, accounts map[string]kilo.EgressAccountStatus, observation kilo.ProxyChangeObservation) (kilo.ProxyStateView, error) {
	return r.changeState(c, accounts, &observation)
}

func (r *KiloRuntime) PreviewProxyObservation(raw string, probeErr error) kilo.ProxyPreview {
	ip, reason := proxyObservationReason(probeErr, raw)
	view := kilo.ProxyPreview{Status: "pending", Reason: reason}
	if reason != "" {
		return view
	}
	at := time.Now().UTC()
	view.Status, view.ObservedExitIP, view.ObservedAt = "ready", &ip, &at
	next := r.state.Clone()
	var duplicate *kilo.ProxyIPAlreadyRegisteredError
	if err := r.admitProxy(&next, &kilo.SavedProxy{}, ip, at); errors.As(err, &duplicate) {
		view.Status, view.Reason, view.RegisteredProxyID = "duplicate", "duplicate_ip", duplicate.ProxyID
	}
	return view
}

func (r *KiloRuntime) proxyAdmissionHealthy(p kilo.SavedProxy, h kiloProxyHealth) bool {
	if p.AdmissionState() == "pending" || h.admissionReason != "" {
		return false
	}
	if p.AdmissionState() == "admitted" {
		ip, ok := kilo.FixedPublicIP(h.observedIP)
		return ok && ip == p.AdmittedIP
	}
	return true
}

func proxyObservationReason(probeErr error, raw string) (string, string) {
	if probeErr != nil {
		return "", "probe_failed"
	}
	if raw == "" {
		return "", "ip_unknown"
	}
	ip, ok := kilo.FixedPublicIP(raw)
	if !ok {
		return "", "not_public_ip"
	}
	return ip, ""
}

// seedLegacyIPHistory trusts prior formal allocation, not a hand-entered IP.
func seedLegacyIPHistory(next *kilo.ProxyState) {
	if next.IPHistory == nil {
		next.IPHistory = map[string]kilo.ProxyIPRegistration{}
	}
	for ip, group := range next.IPGroups {
		if _, exists := next.IPHistory[ip]; exists || len(group.AccountIDs) == 0 {
			continue
		}
		entry := kilo.ProxyIPRegistration{IP: ip, ProxyIDs: []string{}, Source: "legacy_confirmed"}
		for _, p := range next.Proxies {
			if p.AdmissionState() == "legacy" && p.FixedIP == ip {
				entry.ProxyIDs = append(entry.ProxyIDs, p.ID)
			}
		}
		for _, p := range next.Recycle {
			if p.Proxy.AdmissionState() == "legacy" && p.Proxy.FixedIP == ip {
				entry.ProxyIDs = append(entry.ProxyIDs, p.Proxy.ID)
			}
		}
		next.IPHistory[ip] = entry
	}
}

// admitProxy changes only the candidate state. Its caller commits it atomically.
func (r *KiloRuntime) admitProxy(next *kilo.ProxyState, p *kilo.SavedProxy, ip string, at time.Time) error {
	seedLegacyIPHistory(next)
	r.mu.Lock()
	if _, exists := next.IPHistory[ip]; !exists {
		ids := []string{}
		for _, old := range next.Proxies {
			h := r.health[old.ID]
			observed, ok := kilo.FixedPublicIP(h.observedIP)
			if old.AdmissionState() == "legacy" && old.FixedIP == ip && h.checked && h.healthy && !h.pending && ok && observed == ip {
				ids = append(ids, old.ID)
			}
		}
		if len(ids) > 0 {
			next.IPHistory[ip] = kilo.ProxyIPRegistration{IP: ip, ProxyIDs: ids, LastObservedAt: &at, Source: "legacy_confirmed"}
		}
	}
	r.mu.Unlock()
	entry, exists := next.IPHistory[ip]
	if exists && !slices.Contains(entry.ProxyIDs, p.ID) {
		owner := ""
		if len(entry.ProxyIDs) > 0 {
			owner = entry.ProxyIDs[0]
		}
		return &kilo.ProxyIPAlreadyRegisteredError{IP: ip, ProxyID: owner}
	}
	if !exists {
		entry = kilo.ProxyIPRegistration{IP: ip, ProxyIDs: []string{p.ID}, FirstAdmittedAt: &at, Source: "admission"}
		// Preserve old aliases only when their confirmed group and current observations agree.
		if p.AdmissionState() == "legacy" && p.FixedIP == ip {
			if _, confirmed := next.IPGroups[ip]; confirmed {
				r.mu.Lock()
				for _, other := range next.Proxies {
					h := r.health[other.ID]
					observed, ok := kilo.FixedPublicIP(h.observedIP)
					if other.ID != p.ID && other.AdmissionState() == "legacy" && other.FixedIP == ip && h.checked && h.healthy && !h.pending && ok && observed == ip {
						entry.ProxyIDs = append(entry.ProxyIDs, other.ID)
					}
				}
				r.mu.Unlock()
			}
		}
	}
	entry.LastObservedAt = &at
	next.IPHistory[ip] = entry
	p.AdmissionStatus, p.AdmissionReason, p.AdmittedIP, p.AdmittedAt = "admitted", "", ip, at
	return nil
}

func (r *KiloRuntime) quarantineObservationLocked(id string, h *kiloProxyHealth) {
	for _, p := range r.state.Proxies {
		if p.ID != id || p.AdmissionState() == "legacy" {
			continue
		}
		ip, reason := proxyObservationReason(nil, h.observedIP)
		if !h.healthy {
			reason = "probe_failed"
		}
		if reason == "" && p.AdmittedIP != "" && ip != p.AdmittedIP {
			reason = "ip_changed"
		}
		if reason != "" {
			h.admissionReason = reason
		}
		return
	}
}

// CommitProxyObservation is invoked under the executor state lock, never r.mu.
func (r *KiloRuntime) CommitProxyObservation(id, raw string, generation uint64) error {
	r.mu.Lock()
	h, exists := r.health[id]
	current := r.urls[id]
	r.mu.Unlock()
	if !exists || current != raw || h.generation != generation || h.admissionReason == "" {
		return nil
	}
	next := r.state.Clone()
	for i := range next.Proxies {
		p := &next.Proxies[i]
		if p.ID != id || p.URL != raw || p.AdmissionState() == "legacy" {
			continue
		}
		if p.AdmissionState() == "pending" && p.AdmissionReason == h.admissionReason {
			return nil
		}
		p.AdmissionStatus, p.AdmissionReason = "pending", h.admissionReason
		if _, err := r.commitState(next); err != nil {
			code := "storage_failed"
			r.autoError = &code
			return err
		}
		r.autoError = nil
		return nil
	}
	return nil
}

func (r *KiloRuntime) publishProxyObservation(id string, probeErr error, observedIP, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probeSequence++
	h := r.health[id]
	h.checked, h.pending, h.healthy, h.checkedAt, h.generation = true, false, probeErr == nil && reason == "", time.Now().UTC(), r.probeSequence
	h.admissionReason = reason
	if probeErr == nil && observedIP != "" {
		h.observedIP, h.observedAt = observedIP, h.checkedAt
	}
	r.health[id] = h
	r.refreshPoolHealthLocked()
}

func (r *KiloRuntime) commitPendingProbe(p kilo.SavedProxy, restore bool, reason string, probeErr error, observed string) (kilo.ProxyStateView, error) {
	next := r.state.Clone()
	if p.AdmissionState() == "legacy" {
		seedLegacyIPHistory(&next)
	}
	changed := false
	if restore {
		for i := range next.Recycle {
			if next.Recycle[i].Proxy.ID == p.ID {
				q := &next.Recycle[i].Proxy
				changed = q.AdmissionState() != "pending" || q.AdmissionReason != reason
				q.AdmissionStatus, q.AdmissionReason = "pending", reason
			}
		}
	} else {
		for i := range next.Proxies {
			if next.Proxies[i].ID == p.ID {
				q := &next.Proxies[i]
				changed = q.AdmissionState() != "pending" || q.AdmissionReason != reason
				q.AdmissionStatus, q.AdmissionReason = "pending", reason
			}
		}
		r.publishProxyObservation(p.ID, probeErr, observed, reason)
	}
	if changed {
		if _, err := r.commitState(next); err != nil {
			code := "storage_failed"
			r.autoError = &code
			return kilo.ProxyStateView{}, err
		}
		r.autoError = nil
	}
	return kilo.ProxyStateView{}, kilo.ErrProxyHealth
}
