package helps

import (
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"reflect"
)

// BeginDeviceSave persists the complete policy before the credential can appear.
func (r *KiloRuntime) BeginDeviceSave(owner, id string, draft kilo.InferenceEgress) error {
	if err := r.ValidateDeviceDraft(owner, id, draft); err != nil {
		return err
	}
	if _, ok := r.state.PendingDevices[id]; ok {
		return nil
	}
	if _, ok := r.state.Accounts[id]; ok {
		return kilo.ErrStateConflict
	}
	next, err := deviceDraftState(r.state, id, draft)
	if err != nil {
		return err
	}
	for other, pin := range r.devicePins {
		if other == owner || pin.cancelled {
			continue
		}
		for _, pid := range draft.ExclusiveProxyIDs {
			if pid == pin.authorization.ProxyID {
				return kilo.ErrStateBusy
			}
		}
	}
	for _, live := range r.pool.Snapshot().Proxies {
		for _, pid := range draft.ExclusiveProxyIDs {
			if live.ID == pid && live.Active > 0 {
				return kilo.ErrStateBusy
			}
		}
	}
	pending := kilo.PendingDeviceSave{Draft: draft, PreviousPrimary: map[string]string{}}
	for other, before := range r.state.Accounts {
		if before.PrimaryProxyID != next.Accounts[other].PrimaryProxyID {
			pending.PreviousPrimary[other] = before.PrimaryProxyID
		}
	}
	if next.PendingDevices == nil {
		next.PendingDevices = map[string]kilo.PendingDeviceSave{}
	}
	next.PendingDevices[id] = pending
	_, err = r.commitState(next)
	return err
}

// FinishDeviceSave touches only this reservation, preserving unrelated pool changes.
func (r *KiloRuntime) FinishDeviceSave(id string, keep bool, owner ...string) error {
	pending, ok := r.state.PendingDevices[id]
	if !ok {
		return nil
	}
	next := r.state.Clone()
	delete(next.PendingDevices, id)
	if !keep {
		delete(next.Accounts, id)
		for i := range next.Proxies {
			if next.Proxies[i].AccountID == id {
				next.Proxies[i].AccountID = ""
			}
		}
		for other, pid := range pending.PreviousPrimary {
			a, exists := next.Accounts[other]
			if exists && a.PrimaryProxyID == "" {
				a.PrimaryProxyID = pid
				next.Accounts[other] = a
			}
		}
	}
	if keep && next.Accounts[id].CapacityMode == "ip" {
		if err := r.applyIPPolicy(&next, id, true, owner...); err != nil {
			return err
		}
	}
	if keep {
		if _, err := r.prepareDeviceProfiles(&next, id, ""); err != nil {
			return err
		}
	}
	_, err := r.commitState(next)
	return err
}
func (r *KiloRuntime) VerifyDeviceSave(id string, draft kilo.InferenceEgress) error {
	a, ok := r.state.Accounts[id]
	if !ok || !reflect.DeepEqual(a.CapacityPolicy.Normalized(), draft.CapacityPolicy.Normalized()) || a.Mode != draft.Mode || (a.Mode != "auto" && a.PrimaryProxyID != draft.PrimaryProxyID) || !reflect.DeepEqual(a.CandidateProxyIDs, draft.CandidateProxyIDs) {
		return kilo.ErrStateConflict
	}
	actual := map[string]bool{}
	for _, p := range r.state.Proxies {
		if p.AccountID == id {
			actual[p.ID] = true
		}
	}
	for _, pid := range draft.ExclusiveProxyIDs {
		if !actual[pid] {
			return kilo.ErrStateConflict
		}
		delete(actual, pid)
	}
	if len(actual) > 0 {
		return kilo.ErrStateConflict
	}
	return nil
}
func (r *KiloRuntime) ReconcileDeviceSaves(existing map[string]bool) error {
	for id := range r.state.PendingDevices {
		if err := r.FinishDeviceSave(id, existing[id]); err != nil {
			return err
		}
	}
	return nil
}
func (r *KiloRuntime) pendingDeviceProxy(id string) bool {
	for account, pending := range r.state.PendingDevices {
		if id == pending.Draft.PrimaryProxyID {
			return true
		}
		for _, pid := range pending.Draft.CandidateProxyIDs {
			if pid == id {
				return true
			}
		}
		for _, p := range r.state.Proxies {
			if p.ID == id && p.AccountID == account {
				return true
			}
		}
	}
	return false
}

func (r *KiloRuntime) PendingDeviceSaves() []string {
	ids := []string{}
	for id := range r.state.PendingDevices {
		ids = append(ids, id)
	}
	return ids
}
