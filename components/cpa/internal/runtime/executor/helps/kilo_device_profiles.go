package helps

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (r *KiloRuntime) SetDeviceProfileStore(store kilo.DeviceProfileStore) { r.profileStore = store }
func (l *kiloLease) KiloDeviceProfile() kilo.DeviceProfile                 { return l.profile }
func deviceExitKey(state kilo.ProxyState, proxyID string) string {
	if proxyID == "" {
		return "direct"
	}
	for _, p := range state.Proxies {
		if p.ID != proxyID {
			continue
		}
		if ip, ok := kilo.FixedPublicIP(p.FixedIP); ok {
			return ip
		}
		if p.AdmissionState() != "pending" {
			if ip, ok := kilo.FixedPublicIP(p.AdmittedIP); ok {
				return ip
			}
		}
	}
	// Existing private/unconfirmed bootstrap proxies retain the first profile.
	return "legacy"
}
func (r *KiloRuntime) deviceContext() context.Context {
	if r.profileContext != nil {
		return r.profileContext
	}
	return context.Background()
}
func deviceProfileError(err error) error {
	if err == nil {
		return nil
	}
	return coreauth.NewRequestScopedError(err.Error(), 503)
}

func deviceProfileExits(state kilo.ProxyState, id string) []string {
	a := state.Accounts[id]
	exits := state.OwnedIPs(id)
	if a.Mode == "direct" {
		return []string{"direct"}
	}
	if a.Mode == "pool" {
		if a.DeviceProfiles["direct"] != "" {
			exits = append(exits, "direct")
		}
		for _, p := range state.Proxies {
			if p.AdmissionState() == "pending" || p.FixedIP != "" || (p.AccountID != "" && p.AccountID != id) || (a.CandidateProxyIDs != nil && !slices.Contains(a.CandidateProxyIDs, p.ID)) {
				continue
			}
			key := deviceExitKey(state, p.ID)
			if a.DeviceProfiles[key] != "" && !slices.Contains(exits, key) {
				exits = append(exits, key)
			}
		}
	}
	sort.Strings(exits)
	return exits
}

// Called only inside Manager admission or management, never by probe callbacks.
// Credential identities are saved first; only the subsequent state save publishes
// their egress associations. A failed state save leaves reusable idle identities.
func (r *KiloRuntime) prepareDeviceProfiles(next *kilo.ProxyState, forceID, forceExit string) (bool, error) {
	ids := make([]string, 0, len(next.Accounts))
	for id := range next.Accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	changed := false
	for _, id := range ids {
		if forceID != "" && id != forceID {
			continue
		}
		if _, pending := next.PendingDevices[id]; pending {
			continue
		}
		a := next.Accounts[id]
		exits := deviceProfileExits(*next, id)
		if id == forceID && forceExit != "" && !slices.Contains(exits, forceExit) {
			exits = append(exits, forceExit)
		}
		if forceID == "" {
			previous := deviceProfileExits(r.state, id)
			added := false
			for _, exit := range exits {
				added = added || !slices.Contains(previous, exit)
			}
			if !added {
				// Release only relationships removed by this transaction. Deleted
				// owners need no credential backfill to drain or return an exit.
				for _, exit := range previous {
					if !slices.Contains(exits, exit) && a.DeviceProfiles[exit] != "" {
						delete(a.DeviceProfiles, exit)
						next.Accounts[id] = a
						changed = true
					}
				}
				continue
			}
		}
		sort.Strings(exits)
		assigned := map[string]string{}
		missing := false
		for _, exit := range exits {
			if a.DeviceProfiles[exit] == "" {
				missing = true
			} else {
				assigned[exit] = a.DeviceProfiles[exit]
			}
		}
		if missing {
			if r.profileStore == nil {
				return false, kilo.ErrStateStorage
			}
			profiles, err := r.profileStore(r.deviceContext(), id, len(exits))
			if err != nil {
				return false, fmt.Errorf("%w: %v", kilo.ErrStateStorage, err)
			}
			used := map[string]bool{}
			known := map[string]bool{}
			for _, p := range profiles {
				known[p.ID] = true
			}
			for _, pid := range assigned {
				if !known[pid] || used[pid] {
					return false, coreauth.ErrKiloMachineIdentity
				}
				used[pid] = true
			}
			for _, exit := range exits {
				if assigned[exit] != "" {
					continue
				}
				for _, p := range profiles {
					if !used[p.ID] {
						assigned[exit] = p.ID
						used[p.ID] = true
						break
					}
				}
				if assigned[exit] == "" {
					return false, kilo.ErrStateStorage
				}
			}
		}
		if !maps.Equal(a.DeviceProfiles, assigned) {
			a.DeviceProfiles = assigned
			next.Accounts[id] = a
			changed = true
		}
	}
	return changed, nil
}
func (r *KiloRuntime) selectedDeviceProfile(auth *coreauth.Auth, route kilo.Route) (kilo.DeviceProfile, error) {
	key := deviceExitKey(r.state, route.ProxyID)
	next := r.state.Clone()
	if _, ok := next.Accounts[auth.ID]; !ok {
		mode := "pool"
		if route.Mode == kilo.RouteDirect {
			mode = "direct"
		}
		next.Accounts[auth.ID] = kilo.AccountEgress{Mode: mode}
	}
	changed, err := r.prepareDeviceProfiles(&next, auth.ID, key)
	if err != nil {
		return kilo.DeviceProfile{}, deviceProfileError(err)
	}
	if changed {
		if _, err = r.commitState(next); err != nil {
			return kilo.DeviceProfile{}, deviceProfileError(err)
		}
	}
	profileID := r.state.Accounts[auth.ID].DeviceProfiles[key]
	if selection, ok := auth.Runtime.(*coreauth.KiloDeviceSelection); ok && selection.RequiredProfileID != "" && selection.RequiredProfileID != profileID {
		return kilo.DeviceProfile{}, coreauth.NewRequestScopedError("Kilo continuation device has no eligible egress", 409)
	}
	if r.profileStore == nil {
		return kilo.DeviceProfile{}, deviceProfileError(kilo.ErrStateStorage)
	}
	profiles, err := r.profileStore(r.deviceContext(), auth.ID, 1)
	if err != nil {
		return kilo.DeviceProfile{}, deviceProfileError(err)
	}
	for _, profile := range profiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return kilo.DeviceProfile{}, deviceProfileError(coreauth.ErrKiloMachineIdentity)
}
func (r *KiloRuntime) requiredDeviceProxies(auth *coreauth.Auth, required string) []string {
	ids := []string{}
	a := r.state.Accounts[auth.ID]
	for _, p := range r.state.Proxies {
		key := deviceExitKey(r.state, p.ID)
		profile := a.DeviceProfiles[key]
		if profile == required || (profile == "" && len(a.DeviceProfiles) == 0 && required == coreauth.KiloFirstDeviceProfileID(auth)) {
			ids = append(ids, p.ID)
		}
	}
	return ids
}
