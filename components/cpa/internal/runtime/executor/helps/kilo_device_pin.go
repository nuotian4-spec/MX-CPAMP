package helps

import (
	"context"
	"reflect"
	"slices"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

type kiloDevicePin struct {
	authorization kilo.AuthorizationEgress
	inference     kilo.InferenceEgress
	accountID     string
	proxies       map[string]kilo.SavedProxy
	cancel        context.CancelFunc
	cancelled     bool
}

func sameDeviceProxy(a, b kilo.SavedProxy) bool {
	return a.ID == b.ID && a.URL == b.URL && a.AccountID == b.AccountID && a.Disabled == b.Disabled && a.AdmissionState() == b.AdmissionState() && a.AdmittedIP == b.AdmittedIP
}

func deviceDraftState(state kilo.ProxyState, id string, draft kilo.InferenceEgress) (kilo.ProxyState, error) {
	next := state.Clone()
	wanted := map[string]bool{}
	for _, pid := range draft.ExclusiveProxyIDs {
		if pid == "" || wanted[pid] {
			return next, kilo.ErrStateInvalid
		}
		wanted[pid] = true
	}
	for i := range next.Proxies {
		p := &next.Proxies[i]
		if p.AdmissionState() == "pending" && (wanted[p.ID] || draft.PrimaryProxyID == p.ID || slices.Contains(draft.CandidateProxyIDs, p.ID)) {
			return next, kilo.ErrStateConflict
		}
		if p.FixedIP != "" && draft.Mode != "auto" && (draft.PrimaryProxyID == p.ID || slices.Contains(draft.CandidateProxyIDs, p.ID)) {
			return next, kilo.ErrStateConflict
		}
		if wanted[p.ID] {
			if p.FixedIP != "" {
				return next, kilo.ErrStateConflict
			}
			if p.AccountID != "" && p.AccountID != id {
				return next, kilo.ErrStateConflict
			}
			p.AccountID = id
			delete(wanted, p.ID)
			for other, a := range next.Accounts {
				if other != id && a.PrimaryProxyID == p.ID {
					a.PrimaryProxyID = ""
					next.Accounts[other] = a
				}
			}
		} else if p.AccountID == id {
			p.AccountID = ""
		}
	}
	if len(wanted) > 0 {
		return next, kilo.ErrStateInvalid
	}
	next.Accounts[id] = kilo.AccountEgress{CapacityPolicy: draft.CapacityPolicy, Mode: draft.Mode, CandidateProxyIDs: slices.Clone(draft.CandidateProxyIDs), PrimaryProxyID: draft.PrimaryProxyID}
	if err := next.Validate(); err != nil {
		return next, err
	}
	return next, nil
}

func (r *KiloRuntime) deviceAdmissionAllowed(authorizationProxyID string, draft kilo.InferenceEgress) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.state.Proxies {
		selected := p.ID == authorizationProxyID || p.ID == draft.PrimaryProxyID || slices.Contains(draft.CandidateProxyIDs, p.ID) || slices.Contains(draft.ExclusiveProxyIDs, p.ID)
		if selected && (p.AdmissionState() == "pending" || r.health[p.ID].admissionReason != "") {
			return false
		}
	}
	return true
}

func (r *KiloRuntime) PrepareDevice(owner, id string, authorization kilo.AuthorizationEgress, inference kilo.InferenceEgress, cancel context.CancelFunc) (kilo.AuthorizationEgress, kilo.InferenceEgress, error) {
	fail := func(err error) (kilo.AuthorizationEgress, kilo.InferenceEgress, error) {
		return authorization, inference, err
	}
	if authorization.ExpectedRevision != nil && *authorization.ExpectedRevision != r.state.Revision {
		return fail(kilo.ErrStateConflict)
	}
	if inference.ExpectedRevision != nil && *inference.ExpectedRevision != r.state.Revision {
		return fail(kilo.ErrStateConflict)
	}
	if inference.Mode == "" {
		inference.Mode = "pool"
	}
	if inference.Default && r.state.AutoAssignmentEnabled {
		inference.Mode = "auto"
	}
	inference.Default = false
	inference.CapacityPolicy = inference.CapacityPolicy.Normalized()
	if inference.Mode == "auto" && r.state.MaxUniqueAccounts < 1 {
		return fail(kilo.ErrStateInvalid)
	}
	if !r.deviceAdmissionAllowed(authorization.ProxyID, inference) {
		return fail(kilo.ErrStateConflict)
	}
	if _, err := deviceDraftState(r.state, id, inference); err != nil {
		return fail(err)
	}
	if authorization.Mode == "pool" {
		if authorization.ProxyID == "" {
			if r.prepareProxyHealth(owner, false) {
				return fail(kilo.ErrProxyHealth)
			}
			lease, err := r.pool.TryAcquire(owner, kilo.AcquireOptions{Transient: true})
			if err != nil {
				return fail(err)
			}
			authorization.ProxyID = lease.Route().ProxyID
			lease.Release()
		}
		found := false
		for _, p := range r.state.Proxies {
			if p.ID == authorization.ProxyID {
				found = !p.Disabled && p.AccountID == "" && p.AdmissionState() != "pending"
			}
		}
		if !found {
			return fail(kilo.ErrStateConflict)
		}
		// Stale IP observations do not invalidate a known healthy auth route.
		if r.prepareProxyHealth(owner, false, authorization.ProxyID) {
			return fail(kilo.ErrProxyHealth)
		}
	} else if authorization.Mode != "direct" || authorization.ProxyID != "" {
		return fail(kilo.ErrStateInvalid)
	}
	revision := r.state.Revision
	authorization.ExpectedRevision = &revision
	if inference.ExpectedRevision == nil {
		v := revision
		inference.ExpectedRevision = &v
	}
	pin := &kiloDevicePin{authorization: authorization, inference: inference, accountID: id, cancel: cancel, proxies: map[string]kilo.SavedProxy{}}
	for _, p := range r.state.Proxies {
		if p.ID == authorization.ProxyID || p.ID == inference.PrimaryProxyID || slices.Contains(inference.CandidateProxyIDs, p.ID) || slices.Contains(inference.ExclusiveProxyIDs, p.ID) {
			pin.proxies[p.ID] = p
		}
	}
	if r.devicePins == nil {
		r.devicePins = map[string]*kiloDevicePin{}
	}
	r.devicePins[owner] = pin
	return authorization, inference, nil
}
func (r *KiloRuntime) ReleaseDevice(owner string) { delete(r.devicePins, owner) }
func (r *KiloRuntime) proxyPinned(id string) bool {
	for _, pin := range r.devicePins {
		if !pin.cancelled && pin.authorization.ProxyID == id {
			return true
		}
	}
	return false
}
func (r *KiloRuntime) cancelDisabledPins(next kilo.ProxyState) {
	for _, pin := range r.devicePins {
		for _, p := range next.Proxies {
			if p.ID == pin.authorization.ProxyID && (p.Disabled || p.AdmissionState() == "pending") {
				if pin.cancel != nil {
					pin.cancel()
				}
				pin.cancelled = true
			}
		}
	}
}
func (r *KiloRuntime) ValidateDeviceDraft(owner, id string, draft kilo.InferenceEgress) error {
	pin := r.devicePins[owner]
	authorizationProxyID := ""
	if pin != nil {
		authorizationProxyID = pin.authorization.ProxyID
	}
	if !r.deviceAdmissionAllowed(authorizationProxyID, draft) {
		return kilo.ErrStateConflict
	}
	if pending, ok := r.state.PendingDevices[id]; ok {
		if reflect.DeepEqual(pending.Draft, draft) {
			return nil
		}
		return kilo.ErrStateConflict
	}
	if pin == nil || pin.accountID != id || !reflect.DeepEqual(pin.inference, draft) {
		return kilo.ErrStateConflict
	}
	current := map[string]kilo.SavedProxy{}
	for _, p := range r.state.Proxies {
		current[p.ID] = p
	}
	for pid, p := range pin.proxies {
		if !sameDeviceProxy(p, current[pid]) {
			return kilo.ErrStateConflict
		}
	}
	_, err := deviceDraftState(r.state, id, draft)
	return err
}
