package executor

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

func (e *KiloExecutor) PrepareKiloDevice(owner, id string, a kilo.AuthorizationEgress, d kilo.InferenceEgress, cancel context.CancelFunc) (kilo.AuthorizationEgress, kilo.InferenceEgress, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		return a, d, kilo.ErrStateStorage
	}
	return e.runtime.PrepareDevice(owner, id, a, d, cancel)
}
func (e *KiloExecutor) ReleaseKiloDevice(owner string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtime != nil {
		e.runtime.ReleaseDevice(owner)
	}
}

// CommitKiloDevicePolicy fences policy mutation until the credential write settles.
// The caller already owns the existing Manager selection and auth mutation gates.
func (e *KiloExecutor) CommitKiloDevicePolicy(owner, id string, draft kilo.InferenceEgress, existing bool, write func() (bool, error), profiles kilo.DeviceProfileStore) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initErr != nil || e.runtime == nil || e.pending != nil {
		return kilo.ErrStateStorage
	}
	e.runtime.SetDeviceProfileStore(profiles)
	defer e.runtime.SetDeviceProfileStore(e.profileStore)
	if existing {
		if err := e.runtime.VerifyDeviceSave(id, draft); err != nil {
			return err
		}
	} else if err := e.runtime.BeginDeviceSave(owner, id, draft); err != nil {
		return err
	}
	if mayExist, err := write(); err != nil {
		if !mayExist {
			_ = e.runtime.FinishDeviceSave(id, false)
		}
		e.notifyLocked()
		return err
	}
	// The complete policy and credential are durable. A leftover reservation is
	// safely finalized from the complete credential enumeration after restart.
	if err := e.runtime.FinishDeviceSave(id, true, owner); err != nil {
		e.notifyLocked()
		return err
	}
	e.notifyLocked()
	return nil
}
func (e *KiloExecutor) VerifyKiloDevicePolicy(id string, draft kilo.InferenceEgress) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.runtime == nil || e.initErr != nil {
		return kilo.ErrStateStorage
	}
	return e.runtime.VerifyDeviceSave(id, draft)
}
func (e *KiloExecutor) ReconcileKiloDeviceSaves(existing map[string]bool, profiles kilo.DeviceProfileStore) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtime == nil {
		return kilo.ErrStateStorage
	}
	e.runtime.SetDeviceProfileStore(profiles)
	defer e.runtime.SetDeviceProfileStore(e.profileStore)
	if err := e.runtime.ReconcileDeviceSaves(existing); err != nil {
		e.initErr = err
		return err
	}
	return nil
}

func (e *KiloExecutor) PendingKiloDeviceSaves() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.runtime == nil {
		return nil
	}
	return e.runtime.PendingDeviceSaves()
}
