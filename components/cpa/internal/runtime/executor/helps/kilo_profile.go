package helps

import (
	"errors"
	"net/http"

	profile "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

var ErrKiloProfileHealthPending = errors.New("Kilo profile egress health pending")

// AcquireProfile follows the account's configured egress, without model admission
// or the inference-only full-pool direct fallback.
func (r *KiloRuntime) AcquireProfile(accountID string) (*http.Client, func(), error) {
	direct := r.state.Accounts[accountID].Mode == "direct"
	var lease *kilo.ProxyLease
	raw := "direct"
	if direct {
		lease = kilo.DirectLease()
	} else {
		group := ""
		if r.state.Accounts[accountID].Mode == "auto" {
			if !r.autoAssigned(accountID) {
				return nil, nil, profile.ProfileUnavailable()
			}
			for ip, g := range r.state.IPGroups {
				if g.Owner == accountID {
					group = ip
					break
				}
			}
		}
		if r.prepareHealth(accountID) {
			return nil, nil, ErrKiloProfileHealthPending
		}
		var err error
		lease, err = r.pool.TryAcquire(accountID, kilo.AcquireOptions{Group: group})
		if err != nil {
			return nil, nil, profile.ProfileUnavailable()
		}
		raw = r.urls[lease.Route().ProxyID]
	}
	transport, _, err := proxyutil.BuildHTTPTransport(raw)
	if err != nil || transport == nil {
		lease.Release()
		return nil, nil, profile.ProfileUnavailable()
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, func() { client.CloseIdleConnections(); lease.Release() }, nil
}
