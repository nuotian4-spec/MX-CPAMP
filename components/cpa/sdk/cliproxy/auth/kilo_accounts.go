package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	kiloprofile "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

var ErrKiloAccountMissing = errors.New("Kilo account not found")
var ErrKiloAccountBusy = errors.New("Kilo account has active requests")
var ErrKiloAccountStorage = errors.New("Kilo account storage is unavailable")

const KiloIdentityRevisionKey = "kilo_identity_revision"

// KiloAccountChanges is the narrow management write contract. Nil preserves a field.
type KiloAccountChanges struct {
	ProbeConfig              *KiloProbeConfig
	ExpectedProbeRevision    *uint64
	QuotaSource              *KiloQuotaBinding
	ExpectedBindingRevision  *uint64
	APIKey                   *string
	OrganizationID           *string
	Name                     *string
	Priority                 *int
	Enabled                  *bool
	ConcurrencySet           bool
	ConcurrencyLimit         *int
	AllowDirectWhenProxyFull *bool
}

func kiloIdentityRevision(a *Auth) uint64 {
	if a == nil {
		return 0
	}
	switch v := a.Metadata[KiloIdentityRevisionKey].(type) {
	case uint64:
		return v
	case int:
		return uint64(v)
	case float64:
		return uint64(v)
	}
	return 0
}

// ChangeKiloAccount serializes local disk publication with selection and admission.
// The existing pick mutex fences both the selected credential snapshot and its lease.
// The auth mutation gate also fences reloads, refresh and file watcher updates.
func (m *Manager) ChangeKiloAccount(ctx context.Context, id string, change KiloAccountChanges, create, remove bool) (*Auth, error) {
	return m.changeKiloAccount(ctx, id, change, create, remove, false, "")
}

// SaveKiloDeviceAccount uses a server-reserved ID and one credential commit.
func (m *Manager) SaveKiloDeviceAccount(ctx context.Context, id string, change KiloAccountChanges, email string) (*Auth, error) {
	value := strings.TrimSuffix(strings.TrimPrefix(id, "kilo-"), ".json")
	if _, err := uuid.Parse(value); err != nil || id != "kilo-"+value+".json" {
		return nil, ErrKiloAccountStorage
	}
	return m.changeKiloAccount(ctx, id, change, true, false, true, email)
}

func (m *Manager) changeKiloAccount(ctx context.Context, id string, change KiloAccountChanges, create, remove, reserved bool, email string) (*Auth, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return nil, ErrKiloAccountStorage
	}
	if change.APIKey != nil && strings.TrimSpace(*change.APIKey) == "" {
		return nil, errors.New("api_key must not be empty")
	}
	if change.OrganizationID != nil && (create || (*change.OrganizationID != "" && !kiloprofile.ValidOrganizationID(*change.OrganizationID))) {
		return nil, errors.New("invalid Kilo organization field")
	}
	if change.ConcurrencyLimit != nil {
		if _, err := ParseConcurrencyLimit(*change.ConcurrencyLimit); err != nil {
			return nil, err
		}
	}
	if change.ProbeConfig != nil {
		if err := m.validateKiloProbeConfig(*change.ProbeConfig); err != nil {
			return nil, err
		}
	}
	if create && !reserved {
		id = "kilo-" + uuid.NewString() + ".json"
	}
	var checked *Auth
	if !create && !remove {
		var err error
		checked, err = m.validateKiloOrganization(ctx, id, change)
		if err != nil {
			return nil, err
		}
	}
	m.concurrencyPickMu.Lock()
	pickLocked := true
	defer func() {
		if pickLocked {
			m.concurrencyPickMu.Unlock()
		}
	}()
	release, err := m.lockAuthMutationContext(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	current := m.auths[id].Clone()
	store := m.store
	m.mu.RUnlock()
	if reserved && current != nil {
		if current.Provider != "kilo" || change.APIKey == nil || authMetadataString(current, "api_key") != *change.APIKey {
			return nil, ErrKiloAccountStorage
		}
		if _, identityErr := KiloDeviceProfiles(current); identityErr != nil {
			return nil, identityErr
		}
		if _, joint := ctx.Value(kiloDevicePolicyKey{}).(kiloDevicePolicy); !joint {
			return current, nil
		}
	}
	if !create && (current == nil || !strings.EqualFold(current.Provider, "kilo")) {
		return nil, ErrKiloAccountMissing
	}
	if checked != nil && (current == nil || KiloIdentity(current) != KiloIdentity(checked) || current.RegistrationEpoch != checked.RegistrationEpoch || authMetadataString(current, "api_key") != authMetadataString(checked, "api_key")) {
		return nil, ErrKiloIdentityChanged
	}
	if store == nil {
		return nil, ErrKiloAccountStorage
	}
	if change.ProbeConfig != nil {
		if create || change.ExpectedProbeRevision == nil || *change.ExpectedProbeRevision != kiloProbeConfig(current).Revision {
			return nil, ErrKiloProbeConflict
		}
	}
	if change.QuotaSource != nil {
		if create || change.ExpectedBindingRevision == nil || *change.ExpectedBindingRevision != kiloQuotaBinding(current).Revision {
			return nil, ErrKiloQuotaConflict
		}
		if err = m.validateKiloQuotaBinding(change.QuotaSource); err != nil {
			return nil, err
		}
	}
	keyChanged := change.APIKey != nil && (current == nil || authMetadataString(current, "api_key") != *change.APIKey)
	organizationChanged := change.OrganizationID != nil && authMetadataString(current, "organization_id") != *change.OrganizationID
	identityChanged := keyChanged || organizationChanged
	if remove || identityChanged {
		m.concurrency.mu.Lock()
		state := m.concurrency.states[id]
		busy := state != nil && state.active > 0
		m.concurrency.mu.Unlock()
		if busy {
			return nil, ErrKiloAccountBusy
		}
	}
	cfg, _ := m.runtimeConfig.Load().(*config.Config)
	if cfg == nil || strings.TrimSpace(cfg.AuthDir) == "" {
		return nil, ErrKiloAccountStorage
	}
	dir, err := util.ResolveAuthDir(cfg.AuthDir)
	if err != nil {
		return nil, ErrKiloAccountStorage
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, ErrKiloAccountStorage
	}
	path := filepath.Join(dir, id)
	if !create {
		path = current.Attributes[AttributePath]
		if path == "" {
			path = filepath.Join(dir, current.FileName)
		}
		rel, e := filepath.Rel(dir, path)
		if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || current.AuthSourceKind() != AuthSourceFile {
			return nil, ErrKiloAccountStorage
		}
	}
	if reserved && current == nil {
		raw, readErr := os.ReadFile(path)
		if readErr == nil {
			root := ojson.ParseBytes(raw)
			meta, ok := root.Value().(map[string]any)
			if !ojson.ValidBytes(raw) || !ok || meta["type"] != "kilo" || change.APIKey == nil || meta["api_key"] != *change.APIKey {
				return nil, ErrKiloAccountStorage
			}
			current = &Auth{ID: id, FileName: id, Provider: "kilo", Status: StatusActive, Metadata: meta, Attributes: map[string]string{}}
		} else if !os.IsNotExist(readErr) {
			return nil, ErrKiloAccountStorage
		}
	}
	if remove {
		if err = store.Delete(ctx, path); err != nil {
			return nil, ErrKiloAccountStorage
		}
		m.mu.Lock()
		delete(m.auths, id)
		delete(m.kiloQuotaRecords, id)
		delete(m.kiloRecoveryStates, id)
		delete(m.modelPoolOffsets, id)
		if m.authEpochs == nil {
			m.authEpochs = map[string]uint64{}
		}
		m.authEpochs[id] = max(m.authEpochs[id], current.RegistrationEpoch) + 1
		epoch := m.authEpochs[id]
		m.notifyAuthChangeLocked(id)
		m.mu.Unlock()
		release()
		if m.scheduler != nil {
			m.scheduler.RecordRemovalTombstone(id, epoch)
		}
		m.structuralEpoch.Add(1)
		m.queueRefreshUnschedule(id)
		m.invalidateSessionAffinity(id)
		registry.GetGlobalRegistry().UnregisterClient(id)
		return nil, nil
	}
	updated := current
	now := time.Now()
	if create && current == nil {
		if change.APIKey == nil {
			return nil, errors.New("api_key is required")
		}
		updated = &Auth{ID: id, Provider: "kilo", FileName: id, Status: StatusActive, CreatedAt: now, AdoptedAt: now, Metadata: map[string]any{"type": "kilo", "allow_direct_when_proxy_full": false}, Attributes: map[string]string{}}
	}
	if updated.Metadata == nil {
		updated.Metadata = map[string]any{}
	}
	if change.ProbeConfig != nil {
		c := *change.ProbeConfig
		previous := kiloProbeConfig(current)
		c.Revision = previous.Revision
		if c != previous {
			c.Revision++
			applyKiloProbeConfig(updated, c)
		}
	}
	if change.QuotaSource != nil {
		binding := *change.QuotaSource
		binding.Revision = kiloQuotaBinding(current).Revision + 1
		applyKiloQuotaBinding(updated, binding)
	}
	if _, _, err = ensureKiloDeviceProfiles(updated, 1); err != nil {
		return nil, err
	}
	if create && reserved {
		updated.Metadata["email"] = email
		updated.Metadata["auth_method"] = "device"
	}
	if updated.Attributes == nil {
		updated.Attributes = map[string]string{}
	}
	updated.Attributes[AttributePath] = path
	updated.Attributes[AttributeSource] = path
	updated.Attributes[AttributeSourceBackend] = AuthSourceFile
	if change.APIKey != nil {
		updated.Metadata["api_key"] = *change.APIKey
		delete(updated.Attributes, "api_key")
	}
	if change.OrganizationID != nil {
		updated.Metadata["organization_id"] = *change.OrganizationID
	}
	if identityChanged {
		updated.Metadata[KiloIdentityRevisionKey] = kiloIdentityRevision(updated) + 1
		updated.CredentialVersion++
	}
	if change.Name != nil {
		updated.Label = *change.Name
		updated.Metadata["label"] = *change.Name
	}
	if change.Priority != nil {
		updated.Metadata["priority"] = strconv.Itoa(*change.Priority)
		ApplyAuthPriorityMetadata(updated, updated.Metadata)
	}
	if change.Enabled != nil {
		updated.Disabled = !*change.Enabled
		if updated.Disabled {
			updated.Status = StatusDisabled
		} else {
			updated.Status = StatusActive
		}
	}
	if change.ConcurrencySet {
		delete(updated.Attributes, AttributeConcurrencyLimit)
		delete(updated.Metadata, AttributeConcurrencyLimit)
		if change.ConcurrencyLimit != nil {
			updated.Metadata[AttributeConcurrencyLimit] = *change.ConcurrencyLimit
		}
	}
	if change.AllowDirectWhenProxyFull != nil {
		updated.Metadata["allow_direct_when_proxy_full"] = *change.AllowDirectWhenProxyFull
	}

	cooldownChanged := false
	if keyChanged {
		// A manually replaced identity does not inherit a rejected access token.
		updated.RejectedAccessToken = ""
		updated.InferenceVerified = false
		updated.LastInferenceVerifiedAt = time.Time{}
		if hasUnauthorizedAuthFailure(updated) {
			updated.LastError = nil
			updated.StatusMessage = ""
			if !updated.Quota.Exceeded && !updated.Quota.NextRecoverAt.After(now) {
				updated.Unavailable = false
				updated.NextRetryAfter = time.Time{}
				if !updated.Disabled {
					updated.Status = StatusActive
				}
			}
			cooldownChanged = true
		}
		if len(clearUnauthorizedModelStates(updated, now)) > 0 {
			cooldownChanged = true
		}
	}
	m.invalidateKiloRestriction(updated, change, identityChanged)
	invalidateKiloSourcePolicy(updated, change, identityChanged)
	updated.Metadata["disabled"] = updated.Disabled
	updated.UpdatedAt = now
	updated.Generation++
	updated.EnsureIndex()
	// This explicit save is fallible; the generic Register path tolerates store failures.
	write := func() (bool, error) {
		if _, saveErr := store.Save(WithAuthCreationIntent(ctx), updated); saveErr != nil {
			_, statErr := os.Stat(path)
			return !os.IsNotExist(statErr), ErrKiloAccountStorage
		}
		return true, nil
	}
	if policy, joint := ctx.Value(kiloDevicePolicyKey{}).(kiloDevicePolicy); reserved && joint {
		e, _ := m.Executor("kilo")
		writer, ok := e.(interface {
			CommitKiloDevicePolicy(string, string, kilo.InferenceEgress, bool, func() (bool, error), kilo.DeviceProfileStore) error
		})
		if !ok {
			return nil, ErrKiloAccountStorage
		}
		profiles := func(ctx context.Context, accountID string, count int) ([]kilo.DeviceProfile, error) {
			if accountID != id {
				return m.tryPersistKiloDeviceProfiles(ctx, accountID, count)
			}
			result, changed, err := ensureKiloDeviceProfiles(updated, count)
			if err != nil {
				return nil, err
			}
			if changed {
				if _, err = write(); err != nil {
					return nil, err
				}
			}
			return result, nil
		}
		if err = writer.CommitKiloDevicePolicy(policy.owner, id, policy.draft, current != nil, write, profiles); err != nil {
			return nil, err
		}
	} else if _, err = write(); err != nil {
		return nil, err
	}
	updated.ClearFileSnapshot()
	m.mu.Lock()
	if create {
		if m.authEpochs == nil {
			m.authEpochs = map[string]uint64{}
		}
		m.authEpochs[id]++
		updated.RegistrationEpoch = m.authEpochs[id]
	}
	m.auths[id] = updated.Clone()
	m.commitKiloRestrictionChangeLocked(updated, change, identityChanged)
	if change.QuotaSource != nil {
		delete(m.kiloQuotaRecords, id)
	}
	m.notifyAuthChangeLocked(id)
	committed := updated.Clone()
	m.mu.Unlock()
	release()
	m.concurrencyPickMu.Unlock()
	pickLocked = false
	m.finishAuthUpdate(ctx, committed, cooldownChanged)
	m.SyncKiloAutoEgress(time.Now())
	if identityChanged {
		m.invalidateSessionAffinity(id)
	}
	return committed, nil
}
