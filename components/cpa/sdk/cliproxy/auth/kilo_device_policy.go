package auth

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	"os"
	"path/filepath"
)

type kiloDevicePolicyKey struct{}
type kiloDevicePolicy struct {
	owner string
	draft kilo.InferenceEgress
}

func (m *Manager) SaveKiloDeviceAccountWithEgress(ctx context.Context, id string, change KiloAccountChanges, email, owner string, draft kilo.InferenceEgress) (*Auth, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return m.SaveKiloDeviceAccount(context.WithValue(ctx, kiloDevicePolicyKey{}, kiloDevicePolicy{owner, draft}), id, change, email)
}

// Reconciliation follows a complete Store.List and confirms absence at the exact
// reserved file. A skipped, malformed or unreadable file is not proof of absence.
func (m *Manager) reconcileKiloDeviceSaves(e ProviderExecutor, items []*Auth) error {
	writer, ok := e.(interface {
		PendingKiloDeviceSaves() []string
		ReconcileKiloDeviceSaves(map[string]bool, kilo.DeviceProfileStore) error
	})
	if !ok {
		return nil
	}
	pending := writer.PendingKiloDeviceSaves()
	if len(pending) == 0 {
		return nil
	}
	existing := map[string]bool{}
	loaded := map[string]*Auth{}
	for _, a := range items {
		if a != nil && a.Provider == "kilo" {
			existing[a.ID] = true
			loaded[a.ID] = a
		}
	}
	cfg, _ := m.runtimeConfig.Load().(*config.Config)
	if cfg == nil {
		return ErrKiloAccountStorage
	}
	dir, err := util.ResolveAuthDir(cfg.AuthDir)
	if err != nil {
		return err
	}
	for _, id := range pending {
		if existing[id] {
			continue
		}
		if filepath.Base(id) != id {
			return ErrKiloAccountStorage
		}
		_, err = os.Stat(filepath.Join(dir, id))
		if !os.IsNotExist(err) {
			return ErrKiloAccountStorage
		}
	}
	// The caller owns the full auth load gate; do not reacquire a mutation gate.
	return writer.ReconcileKiloDeviceSaves(existing, func(ctx context.Context, id string, count int) ([]kilo.DeviceProfile, error) {
		a := loaded[id]
		if a == nil {
			return nil, ErrKiloAccountMissing
		}
		next := a.Clone()
		profiles, changed, err := ensureKiloDeviceProfiles(next, count)
		if err != nil {
			return nil, err
		}
		if changed {
			m.mu.RLock()
			store := m.store
			m.mu.RUnlock()
			if store == nil {
				return nil, ErrKiloAccountStorage
			}
			if _, err = store.Save(WithAuthCreationIntent(ctx), next); err != nil {
				return nil, ErrKiloAccountStorage
			}
			a.Metadata = next.Metadata
			a.ClearFileSnapshot()
		}
		return profiles, nil
	})
}
