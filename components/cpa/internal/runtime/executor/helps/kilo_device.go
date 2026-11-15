package helps

import (
	"net/http"
	"slices"

	device "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

// AcquireDevice uses the existing pool but never persists a temporary owner.
// Each HTTP operation owns its transport, so no idle session client survives it.
func (r *KiloRuntime) AcquireDevice(owner, mode string) (*http.Client, func(), error) {
	var lease *kilo.ProxyLease
	raw := "direct"
	if mode == "pool" {
		proxyID := ""
		if pin := r.devicePins[owner]; pin != nil {
			if pin.cancelled {
				return nil, nil, device.DeviceUnavailable()
			}
			proxyID = pin.authorization.ProxyID
		}
		// Start prepares health once; polls only consume the pinned route state.
		var err error
		lease, err = r.pool.TryAcquire(owner, kilo.AcquireOptions{Transient: true, ProxyID: proxyID})
		if err != nil {
			return nil, nil, device.DeviceUnavailable()
		}
		raw = r.urls[lease.Route().ProxyID]
	} else if mode == "direct" {
		lease = kilo.DirectLease()
	} else {
		return nil, nil, device.DeviceUnavailable()
	}
	transport, _, err := proxyutil.BuildHTTPTransport(raw)
	if err != nil || transport == nil {
		lease.Release()
		return nil, nil, device.DeviceUnavailable()
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, func() { client.CloseIdleConnections(); lease.Release() }, nil
}

// DeviceSaveEgress is read-only and does not probe or allocate resources.
func (r *KiloRuntime) DeviceSaveEgress(accountIDs ...string) (string, string) {
	accountID := ""
	if len(accountIDs) > 0 {
		accountID = accountIDs[0]
	}
	egress := r.state.Accounts[accountID]
	if egress.Mode == "direct" {
		return "available", ""
	}
	if egress.Mode == "auto" && egress.CapacityMode == "ip" {
		if r.ipPolicyReady(accountID) {
			return "available", ""
		}
		if r.autoError != nil {
			return "pending", *r.autoError
		}
		if egress.AllocationMode == "specified" {
			return "pending", "specified_conflict"
		}
		return "pending", "capacity_wait"
	}
	if egress.Mode == "auto" {
		if r.autoError != nil {
			return "pending", "auto_egress_storage_failed"
		}
		for ip, g := range r.state.IPGroups {
			if g.Owner != accountID {
				continue
			}
			if g.Draining {
				return "pending", "auto_egress_draining"
			}
			if !r.autoGroupHealthy(ip) {
				return "pending", "auto_egress_unavailable"
			}
			for _, p := range r.pool.Snapshot().Proxies {
				if p.Owner == accountID && p.Healthy && p.Active < p.Limit {
					return "available", ""
				}
			}
			return "pending", "auto_egress_capacity"
		}
		return "pending", "auto_egress_waiting"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := r.pool.Snapshot()
	configured, unchecked, healthy := false, false, false
	for _, p := range r.config.Proxies {
		if p.Disabled || (p.AccountID != "" && p.AccountID != accountID) || (egress.CandidateProxyIDs != nil && !slices.Contains(egress.CandidateProxyIDs, p.ID)) {
			continue
		}
		configured = true
		observation := r.health[p.ID]
		unchecked = unchecked || !observation.checked || observation.pending
		for _, live := range snapshot.Proxies {
			if live.ID != p.ID || !live.Healthy {
				continue
			}
			healthy = true
			if (live.Owner == "" || live.Owner == accountID) && live.Active < live.Limit {
				return "available", ""
			}
		}
	}
	if !configured {
		return "pending", "shared_proxy_not_configured"
	}
	if healthy {
		return "pending", "shared_proxy_capacity"
	}
	if unchecked {
		return "pending", "shared_proxy_unverified"
	}
	return "pending", "shared_proxy_unhealthy"
}
