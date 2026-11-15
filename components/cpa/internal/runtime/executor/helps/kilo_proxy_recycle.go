package helps

import (
	"context"
	"errors"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

func (r *KiloRuntime) ProxyForProbe(revision uint64, id string, restore bool) (kilo.SavedProxy, error) {
	if r.state.Revision != revision {
		return kilo.SavedProxy{}, kilo.ErrStateConflict
	}
	if restore {
		for _, entry := range r.state.Recycle {
			if entry.Proxy.ID == id {
				return entry.Proxy, nil
			}
		}
	} else {
		for _, p := range r.state.Proxies {
			if p.ID == id {
				return p, nil
			}
		}
	}
	return kilo.SavedProxy{}, kilo.ErrProxyMissing
}

// CompleteProxyProbe is called under the same executor lock as admission.
// The network phase holds neither this lock nor the Manager selection lock.
func (r *KiloRuntime) CompleteProxyProbe(revision uint64, expected kilo.SavedProxy, restore bool, probeErr error, observedIPs ...string) (kilo.ProxyStateView, error) {
	p, err := r.ProxyForProbe(revision, expected.ID, restore)
	if err != nil {
		return kilo.ProxyStateView{}, err
	}
	if p != expected {
		return kilo.ProxyStateView{}, kilo.ErrStateConflict
	}

	if errors.Is(probeErr, context.Canceled) {
		return kilo.ProxyStateView{}, probeErr
	}
	// An omitted observation remains the old internal connectivity-only contract.
	legacyConnectivity := p.AdmissionState() == "legacy" && len(observedIPs) == 0
	raw := ""
	if len(observedIPs) > 0 {
		raw = observedIPs[0]
	}
	ip, reason := proxyObservationReason(probeErr, raw)
	if !legacyConnectivity && reason != "" {
		return r.commitPendingProbe(p, restore, reason, probeErr, raw)
	}
	if legacyConnectivity && probeErr != nil {
		if !restore {
			r.publishProxyObservation(p.ID, probeErr, "", "")
		}
		return kilo.ProxyStateView{}, kilo.ErrProxyHealth
	}
	next := r.state.Clone()
	if !legacyConnectivity {
		if p.AdmittedIP != "" && p.AdmittedIP != ip && !restore {
			r.publishProxyObservation(p.ID, probeErr, ip, "ip_changed")
			for _, live := range r.pool.Snapshot().Proxies {
				if live.ID == p.ID && live.Active > 0 {
					return r.commitPendingProbe(p, restore, "ip_changed", probeErr, ip)
				}
			}
			if r.proxyPinned(p.ID) || r.pendingDeviceProxy(p.ID) {
				return r.commitPendingProbe(p, restore, "ip_changed", probeErr, ip)
			}
		}
		if p.FixedIP != "" && ip != p.FixedIP {
			g := next.IPGroups[p.FixedIP]
			if g.Owner != "" || r.autoGroupBusy(p.FixedIP, nil) || restore {
				return r.commitPendingProbe(p, restore, "ip_changed", probeErr, ip)
			}
		}
		if err := r.admitProxy(&next, &p, ip, time.Now().UTC()); err != nil {
			if expected.AdmissionState() != "pending" {
				_, persistErr := r.commitPendingProbe(expected, restore, "duplicate_ip", probeErr, ip)
				if persistErr != nil && !errors.Is(persistErr, kilo.ErrProxyHealth) {
					return kilo.ProxyStateView{}, persistErr
				}
			}
			return kilo.ProxyStateView{}, err
		}
		if p.FixedIP != "" && p.FixedIP != ip {
			p.FixedIP = ip
			if _, exists := next.IPGroups[ip]; !exists {
				next.IPGroups[ip] = kilo.IPGroup{IP: ip, AccountIDs: []string{}}
			}
		}
	}
	if restore {
		if p.FixedIP != "" {
			g := next.IPGroups[p.FixedIP]
			if g.Owner != "" || next.MaxUniqueAccounts < 1 || len(g.AccountIDs) >= next.MaxUniqueAccounts {
				return kilo.ProxyStateView{}, kilo.ErrStateConflict
			}
			g.Retired, g.Reason = false, ""
			next.IPGroups[p.FixedIP] = g
		}
		for i, entry := range next.Recycle {
			if entry.Proxy.ID == p.ID {
				next.Recycle = append(next.Recycle[:i], next.Recycle[i+1:]...)
				break
			}
		}
		p.AccountID, p.Disabled = "", false
		next.Proxies = append(next.Proxies, p)
	} else {
		for i := range next.Proxies {
			if next.Proxies[i].ID == p.ID {
				next.Proxies[i] = p
				break
			}
		}
	}
	if restore || !legacyConnectivity {
		if _, err = r.commitState(next); err != nil {
			return kilo.ProxyStateView{}, err
		}
	}
	r.publishProxyObservation(p.ID, probeErr, ip, "")
	return r.StateView(), nil
}
