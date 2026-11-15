package auth

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
)

var ErrKiloQuotaConflict = errors.New("quota_conflict")
var ErrKiloQuotaInvalid = errors.New("quota_invalid_request")

const kiloQuotaSourceKey = "kilo_quota_source"

// KiloQuotaBinding is stored only inside the existing Kilo credential.
type KiloQuotaBinding struct {
	Kind, CredentialID, AccountID, AccessToken string
	Revision                                   uint64
}
type KiloQuotaSourceView struct {
	Kind         string `json:"kind"`
	CredentialID string `json:"credential_id,omitempty"`
	Status       string `json:"status"`
}
type KiloQuotaQueryView struct {
	Status      string     `json:"status"`
	AttemptedAt *time.Time `json:"attempted_at"`
	ObservedAt  *time.Time `json:"observed_at"`
	ErrorCode   *string    `json:"error_code"`
}
type KiloQuotaView struct {
	AccountID       string              `json:"account_id"`
	BindingRevision uint64              `json:"binding_revision"`
	Source          KiloQuotaSourceView `json:"source"`
	Query           KiloQuotaQueryView  `json:"query"`
	usage.UsageObservation
	Stale bool `json:"stale"`
}
type KiloQuotaSourceChoice struct {
	CredentialID     string  `json:"credential_id"`
	Label            string  `json:"label"`
	AccountID        *string `json:"chatgpt_account_id"`
	Status           string  `json:"status"`
	HasQueryMaterial bool    `json:"has_query_material"`
}
type kiloQuotaRecord struct {
	view                        KiloQuotaView
	identity                    kilobinding.Identity
	credential, epoch, revision uint64
	disabled                    bool
	source                      CodexUsageCredential
}

func (r *kiloQuotaRecord) matches(a *Auth) bool {
	return r != nil && a != nil && a.Provider == "kilo" && r.identity == KiloIdentity(a) && r.credential == a.CredentialVersion && r.epoch == a.RegistrationEpoch && r.revision == kiloQuotaBinding(a).Revision && r.disabled == a.Disabled
}

type kiloUsageReader interface {
	KiloUsage(context.Context, string, string, string) (usage.UsageObservation, error)
}

func kiloQuotaBinding(a *Auth) KiloQuotaBinding {
	b := KiloQuotaBinding{Kind: "none"}
	if a == nil {
		return b
	}
	meta, _ := a.Metadata[kiloQuotaSourceKey].(map[string]any)
	if meta == nil {
		return b
	}
	b.Kind, _ = meta["kind"].(string)
	if b.Kind == "" {
		b.Kind = "none"
	}
	b.CredentialID, _ = meta["credential_id"].(string)
	b.AccountID, _ = meta["chatgpt_account_id"].(string)
	b.AccessToken, _ = meta["access_token"].(string)
	switch v := meta["revision"].(type) {
	case uint64:
		b.Revision = v
	case int:
		b.Revision = uint64(v)
	case float64:
		b.Revision = uint64(v)
	}
	return b
}
func (m *Manager) validateKiloQuotaBinding(b *KiloQuotaBinding) error {
	if b == nil {
		return nil
	}
	switch b.Kind {
	case "none":
		if b.CredentialID != "" || b.AccountID != "" || b.AccessToken != "" {
			return ErrKiloQuotaInvalid
		}
	case "cpa_codex":
		if b.CredentialID == "" || b.AccountID == "" || b.AccessToken != "" {
			return ErrKiloQuotaInvalid
		}
		m.mu.RLock()
		a := m.auths[b.CredentialID].Clone()
		m.mu.RUnlock()
		if codexUsageIdentity(a, b.AccountID) != "" {
			return ErrKiloQuotaConflict
		}
	case "imported":
		if strings.TrimSpace(b.AccessToken) == "" || b.CredentialID != "" || strings.ContainsAny(b.AccessToken+b.AccountID, "\r\n") {
			return ErrKiloQuotaInvalid
		}
	default:
		return ErrKiloQuotaInvalid
	}
	return nil
}
func applyKiloQuotaBinding(a *Auth, b KiloQuotaBinding) {
	meta := map[string]any{"kind": b.Kind, "revision": b.Revision}
	if b.Kind == "cpa_codex" {
		meta["credential_id"] = b.CredentialID
		meta["chatgpt_account_id"] = b.AccountID
	}
	if b.Kind == "imported" {
		meta["access_token"] = b.AccessToken
		if b.AccountID != "" {
			meta["chatgpt_account_id"] = b.AccountID
		}
	}
	a.Metadata[kiloQuotaSourceKey] = meta
}
func (m *Manager) KiloQuotaSources() []KiloQuotaSourceChoice {
	out := []KiloQuotaSourceChoice{}
	for _, a := range m.List() {
		if !strings.EqualFold(a.Provider, "codex") {
			continue
		}
		choice := KiloQuotaSourceChoice{CredentialID: a.ID, Label: a.Label, Status: m.codexUsageStatus(a, time.Now())}
		if account := authMetadataString(a, "account_id"); account != "" {
			choice.AccountID = &account
		}
		choice.HasQueryMaterial = choice.Status == "ready" || choice.Status == "refresh_required"
		out = append(out, choice)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CredentialID < out[j].CredentialID })
	return out
}
func (m *Manager) kiloQuotaViewLocked(a *Auth) KiloQuotaView {
	b := kiloQuotaBinding(a)
	v := KiloQuotaView{AccountID: a.ID, BindingRevision: b.Revision, Source: KiloQuotaSourceView{Kind: b.Kind, CredentialID: b.CredentialID, Status: "none"}, Query: KiloQuotaQueryView{Status: "never"}, UsageObservation: usage.UsageObservation{Windows: []usage.UsageWindow{}}, Stale: true}
	r := m.kiloQuotaRecords[a.ID]
	if r.matches(a) {
		v = r.view
	}
	if b.Kind == "imported" {
		v.Source.Status = "ready"
		if usage.KnownAccessExpired(b.AccessToken, time.Now()) || (v.Query.ErrorCode != nil && *v.Query.ErrorCode == "needs_reimport") {
			v.Source.Status = "needs_reimport"
		}
	} else if b.Kind == "cpa_codex" {
		source := m.auths[b.CredentialID]
		if code := codexUsageIdentity(source, b.AccountID); code != "" {
			v.Source.Status = code
			v.Stale = true
		} else {
			v.Source.Status = m.codexUsageStatus(source, time.Now())
			if r != nil && r.source.AuthID != "" && (r.source.RegistrationEpoch != source.RegistrationEpoch || r.source.CredentialVersion != source.CredentialVersion) {
				v.Stale = true
			}
		}
	}
	return v
}
func (m *Manager) KiloQuota(id string) (KiloQuotaView, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.auths[id]
	if a == nil || a.Provider != "kilo" {
		return KiloQuotaView{}, ErrKiloAccountMissing
	}
	return m.kiloQuotaViewLocked(a), nil
}

// QueryKiloQuota never holds account mutation or admission gates during I/O.
func (m *Manager) QueryKiloQuota(ctx context.Context, id string, revision uint64) (KiloQuotaView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	a := m.auths[id].Clone()
	if a == nil || a.Provider != "kilo" {
		m.mu.Unlock()
		return KiloQuotaView{}, ErrKiloAccountMissing
	}
	b := kiloQuotaBinding(a)
	restrictionGeneration := kiloRestriction(a).Generation
	policy := kiloSourcePolicy(a)
	ticket := kiloPolicyTicket{revision: policy.Revision, pause: policy.PauseGeneration, identity: kiloIdentityRevision(a), epoch: a.RegistrationEpoch, binding: b.Revision, started: time.Now().UTC()}
	if b.Kind == "none" || b.Revision != revision || m.kiloQuotaBusy[id] {
		m.mu.Unlock()
		return KiloQuotaView{}, ErrKiloQuotaConflict
	}
	if m.kiloQuotaBusy == nil {
		m.kiloQuotaBusy = map[string]bool{}
	}
	if m.kiloQuotaRecords == nil {
		m.kiloQuotaRecords = map[string]*kiloQuotaRecord{}
	}
	v := m.kiloQuotaViewLocked(a)
	now := time.Now().UTC()
	v.Query.Status = "running"
	v.Query.AttemptedAt = &now
	v.Query.ErrorCode = nil
	v.Stale = true
	record := &kiloQuotaRecord{view: v, identity: KiloIdentity(a), credential: a.CredentialVersion, epoch: a.RegistrationEpoch, revision: revision, disabled: a.Disabled}
	var initialSource *Auth
	if b.Kind == "cpa_codex" {
		initialSource = m.auths[b.CredentialID].Clone()
	}
	m.kiloQuotaRecords[id] = record
	m.kiloQuotaBusy[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.kiloQuotaBusy, id)
		if live := m.kiloQuotaRecords[id]; live == record && live.view.Query.Status == "running" {
			code := "source_changed"
			live.view.Query.Status = "failed"
			live.view.Query.ErrorCode = &code
			live.view.Stale = true
		}
		m.mu.Unlock()
	}()
	var material CodexUsageCredential
	var observation usage.UsageObservation
	var queryErr error
	var sourceObservedAt time.Time
	if b.Kind == "cpa_codex" {
		material, queryErr = m.ResolveCodexUsageCredential(ctx, b.CredentialID, b.AccountID)
	} else {
		material = CodexUsageCredential{AccessToken: b.AccessToken, AccountID: b.AccountID}
		if usage.KnownAccessExpired(b.AccessToken, time.Now()) {
			queryErr = usage.UsageError("needs_reimport")
		}
	}
	if queryErr == nil {
		m.mu.RLock()
		current := m.auths[id]
		valid := record.matches(current) && kiloRestriction(current).Generation == restrictionGeneration
		if b.Kind == "cpa_codex" {
			valid = valid && initialSource != nil && initialSource.RegistrationEpoch == material.RegistrationEpoch && m.codexUsageMaterialCurrentLocked(material)
		}
		m.mu.RUnlock()
		if !valid {
			return KiloQuotaView{}, ErrKiloQuotaConflict
		}
		result := m.queryKiloSource(ctx, id, b.Kind, material)
		observation, queryErr = result.observation, result.err
		ticket.started, sourceObservedAt = result.started, result.completed
		ticket.source = material
	}
	if ctx.Err() != nil {
		queryErr = usage.UsageError("usage_cancelled")
	}
	m.mu.Lock()
	var recoveryObservation *KiloQuotaView
	defer func() {
		m.mu.Unlock()
		if recoveryObservation != nil {
			m.observeKiloRecovery(ctx, id, restrictionGeneration, *recoveryObservation)
			m.observeKiloSourcePolicy(ctx, id, ticket, *recoveryObservation)
		}
	}()
	current := m.auths[id]
	if !record.matches(current) || kiloRestriction(current).Generation != restrictionGeneration {
		return KiloQuotaView{}, ErrKiloQuotaConflict
	}
	if b.Kind == "cpa_codex" {
		live := m.auths[b.CredentialID]
		if material.AuthID != "" {
			if !m.codexUsageMaterialCurrentLocked(material) {
				return KiloQuotaView{}, ErrKiloQuotaConflict
			}
		} else if initialSource != nil && (live == nil || live.RegistrationEpoch != initialSource.RegistrationEpoch || live.CredentialVersion != initialSource.CredentialVersion || live.Provider != initialSource.Provider || authMetadataString(live, "account_id") != authMetadataString(initialSource, "account_id")) {
			return KiloQuotaView{}, ErrKiloQuotaConflict
		}
	}
	if queryErr == nil {
		observed := sourceObservedAt
		v.UsageObservation = observation
		v.Query.Status = "success"
		v.Query.ObservedAt = &observed
		v.Stale = false
	} else {
		code := usage.UsageErrorCode(queryErr)
		if b.Kind == "imported" && code == "usage_unauthorized" {
			code = "needs_reimport"
		}
		v.Query.Status = "failed"
		if code == "usage_cancelled" {
			v.Query.Status = "cancelled"
		}
		v.Query.ErrorCode = &code
		v.Stale = true
	}
	material.AccessToken = ""
	record.view = v
	record.source = material
	out := m.kiloQuotaViewLocked(current)
	recoveryObservation = &out
	return out, nil
}
func (m *Manager) codexUsageMaterialCurrentLocked(material CodexUsageCredential) bool {
	a := m.auths[material.AuthID]
	return a != nil && !a.Disabled && codexUsageIdentity(a, material.AccountID) == "" && a.RegistrationEpoch == material.RegistrationEpoch && a.CredentialVersion == material.CredentialVersion && authMetadataString(a, "access_token") == material.AccessToken
}

// KiloQuotaSourceSummary is a local, credential-free display projection.
type KiloQuotaSourceSummary struct {
	Kind            string `json:"kind"`
	CredentialID    string `json:"credential_id"`
	DisplayLabel    string `json:"display_label"`
	Status          string `json:"status"`
	BindingRevision uint64 `json:"binding_revision"`
}
type KiloQuotaListEntry struct {
	Summary  KiloQuotaSourceSummary
	Snapshot KiloQuotaView
}

// KiloQuotaList reads all source identities and cached observations under one lock.
// It never refreshes credentials or calls an upstream endpoint.
func (m *Manager) KiloQuotaList() map[string]KiloQuotaListEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]KiloQuotaListEntry{}
	for id, a := range m.auths {
		if a == nil || a.Provider != "kilo" {
			continue
		}
		v := m.kiloQuotaViewLocked(a)
		summary := KiloQuotaSourceSummary{Kind: v.Source.Kind, CredentialID: v.Source.CredentialID, Status: v.Source.Status, BindingRevision: v.BindingRevision}
		if v.Source.Kind == "cpa_codex" {
			summary.DisplayLabel = v.Source.CredentialID
			if source := m.auths[v.Source.CredentialID]; source != nil && strings.EqualFold(source.Provider, "codex") && v.Source.Status != "source_changed" {
				if source.Label != "" {
					summary.DisplayLabel = source.Label
				}
			}
		}
		out[id] = KiloQuotaListEntry{Summary: summary, Snapshot: v}
	}
	return out
}
