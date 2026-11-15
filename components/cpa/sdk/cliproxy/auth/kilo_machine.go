package auth

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
)

const KiloMachineIDKey = "machine_id"
const KiloDeviceProfilesKey = "device_profiles"

var ErrKiloMachineIdentity = fmt.Errorf("Kilo machine identity is invalid: %w", ErrKiloIdentityChanged)

// KiloDeviceSelection exists only on the selected request snapshot.
type KiloDeviceSelection struct {
	Context           context.Context
	RequiredProfileID string
}

func validKiloMachine(value string) string {
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
		return ""
	}
	return id.String()
}
func KiloMachineID(a *Auth) string {
	if a == nil {
		return ""
	}
	if selected, ok := a.Runtime.(interface{ KiloDeviceProfile() kilo.DeviceProfile }); ok {
		return validKiloMachine(selected.KiloDeviceProfile().MachineID)
	}
	value, _ := a.Metadata[KiloMachineIDKey].(string)
	return validKiloMachine(value)
}
func KiloFirstDeviceProfileID(a *Auth) string {
	if a == nil {
		return ""
	}
	value, _ := a.Metadata[KiloMachineIDKey].(string)
	return validKiloMachine(value)
}
func KiloDeviceProfileID(a *Auth) string {
	if a == nil {
		return ""
	}
	if selected, ok := a.Runtime.(interface{ KiloDeviceProfile() kilo.DeviceProfile }); ok {
		return selected.KiloDeviceProfile().ID
	}
	return KiloFirstDeviceProfileID(a)
}
func KiloDeviceProfiles(a *Auth) ([]kilo.DeviceProfile, error) {
	if a == nil {
		return nil, ErrKiloAccountMissing
	}
	machine, exists := a.Metadata[KiloMachineIDKey]
	first := ""
	if exists {
		value, ok := machine.(string)
		if !ok || validKiloMachine(value) == "" {
			return nil, ErrKiloMachineIdentity
		}
		first = validKiloMachine(value)
	}
	raw, exists := a.Metadata[KiloDeviceProfilesKey]
	if !exists {
		if first == "" {
			return nil, nil
		}
		return []kilo.DeviceProfile{{ID: first, MachineID: first}}, nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, ErrKiloMachineIdentity
	}
	profiles := make([]kilo.DeviceProfile, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		value, ok := item.(map[string]any)
		if !ok {
			return nil, ErrKiloMachineIdentity
		}
		id, _ := value["id"].(string)
		machine, _ := value["machine_id"].(string)
		if validKiloMachine(id) == "" || validKiloMachine(machine) == "" || seen[id] {
			return nil, ErrKiloMachineIdentity
		}
		seen[id] = true
		profiles = append(profiles, kilo.DeviceProfile{ID: id, MachineID: machine})
	}
	if first != "" && (profiles[0].ID != first || profiles[0].MachineID != first) {
		return nil, ErrKiloMachineIdentity
	}
	return profiles, nil
}
func ensureKiloDeviceProfiles(a *Auth, count int) ([]kilo.DeviceProfile, bool, error) {
	profiles, err := KiloDeviceProfiles(a)
	if err != nil {
		return nil, false, err
	}
	_, stored := a.Metadata[KiloDeviceProfilesKey]
	_, hasMachine := a.Metadata[KiloMachineIDKey]
	changed := !stored || !hasMachine
	for len(profiles) < max(1, count) {
		machine := uuid.NewString()
		profiles = append(profiles, kilo.DeviceProfile{ID: machine, MachineID: machine})
		changed = true
	}
	if changed {
		if a.Metadata == nil {
			a.Metadata = map[string]any{}
		}
		a.Metadata[KiloMachineIDKey] = profiles[0].MachineID
		items := make([]any, 0, len(profiles))
		for _, p := range profiles {
			items = append(items, map[string]any{"id": p.ID, "machine_id": p.MachineID})
		}
		a.Metadata[KiloDeviceProfilesKey] = items
	}
	return profiles, changed, nil
}

// Selection and management own concurrencyPickMu before invoking this callback.
// The callback takes only the per-auth mutation gate, followed by Manager.mu.
func (m *Manager) wireKiloDeviceProfiles(e ProviderExecutor) {
	if writer, ok := e.(interface{ SetKiloDeviceProfileStore(kilo.DeviceProfileStore) }); ok {
		writer.SetKiloDeviceProfileStore(m.tryPersistKiloDeviceProfiles)
	}
}
func (m *Manager) persistKiloDeviceProfiles(ctx context.Context, id string, count int) ([]kilo.DeviceProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := m.lockAuthMutationContext(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	return m.saveKiloDeviceProfiles(ctx, id, count)
}

// Runtime callbacks must not wait behind Load, which may itself wait for the
// executor lock. Reuse the existing gates without blocking under that lock.
func (m *Manager) tryPersistKiloDeviceProfiles(ctx context.Context, id string, count int) ([]kilo.DeviceProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !m.authLoadGate.TryAcquire(1) {
		return nil, NewRequestScopedError("Kilo identity is being reloaded", 503)
	}
	value, _ := m.authMutationLocks.LoadOrStore(id, make(chan struct{}, 1))
	gate := value.(chan struct{})
	select {
	case gate <- struct{}{}:
		defer func() { <-gate; m.authLoadGate.Release(1) }()
	default:
		m.authLoadGate.Release(1)
		return nil, NewRequestScopedError("Kilo identity is being updated", 503)
	}
	return m.saveKiloDeviceProfiles(ctx, id, count)
}

func (m *Manager) saveKiloDeviceProfiles(ctx context.Context, id string, count int) ([]kilo.DeviceProfile, error) {
	m.mu.RLock()
	current, store := m.auths[id].Clone(), m.store
	m.mu.RUnlock()
	if current == nil {
		return nil, ErrKiloAccountMissing
	}
	profiles, changed, err := ensureKiloDeviceProfiles(current, count)
	if err != nil {
		return nil, NewRequestScopedError(err.Error(), 503)
	}
	if changed {
		if store == nil {
			return nil, NewRequestScopedError("Kilo machine identity storage is unavailable", 503)
		}
		if _, err = store.Save(WithAuthCreationIntent(ctx), current); err != nil {
			return nil, NewRequestScopedError("Kilo machine identity storage is unavailable", 503)
		}
		m.mu.Lock()
		latest := m.auths[id].Clone()
		if latest != nil {
			if latest.Metadata == nil {
				latest.Metadata = map[string]any{}
			}
			latest.Metadata[KiloMachineIDKey] = current.Metadata[KiloMachineIDKey]
			latest.Metadata[KiloDeviceProfilesKey] = current.Metadata[KiloDeviceProfilesKey]
			latest.ClearFileSnapshot()
			m.auths[id] = latest
			m.notifyAuthChangeLocked(id)
		}
		m.mu.Unlock()
	}
	return profiles, nil
}
func (m *Manager) ensureKiloMachineLocked(ctx context.Context, selected *Auth) (*Auth, error) {
	if selected == nil || selected.Provider != "kilo" {
		return selected, nil
	}
	if _, err := m.persistKiloDeviceProfiles(ctx, selected.ID, 1); err != nil {
		return nil, err
	}
	current, ok := m.GetByID(selected.ID)
	if !ok || current == nil {
		return nil, ErrKiloAccountMissing
	}
	result := selected.Clone()
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata[KiloMachineIDKey] = current.Metadata[KiloMachineIDKey]
	result.Metadata[KiloDeviceProfilesKey] = current.Metadata[KiloDeviceProfilesKey]
	selection := &KiloDeviceSelection{Context: ctx}
	if plan := kilobinding.FromContext(ctx); plan != nil && plan.Record.ID != "" {
		selection.RequiredProfileID = plan.Record.Identity.DeviceProfileID
		if selection.RequiredProfileID == "" {
			selection.RequiredProfileID = KiloFirstDeviceProfileID(current)
		}
	}
	result.Runtime = selection
	e, _ := m.Executor("kilo")
	m.wireKiloDeviceProfiles(e)
	return result, nil
}
