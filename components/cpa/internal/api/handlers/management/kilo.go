package management

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type kiloAccountDTO struct {
	ClientIdentity           *kiloClientIdentityDTO          `json:"client_identity,omitempty"`
	QuotaSourceSummary       coreauth.KiloQuotaSourceSummary `json:"quota_source_summary"`
	QuotaSnapshot            coreauth.KiloQuotaView          `json:"quota_snapshot"`
	AutoRestriction          coreauth.KiloRestrictionView    `json:"auto_restriction"`
	SourcePolicy             coreauth.KiloSourcePolicyView   `json:"source_policy"`
	ID                       string                          `json:"id"`
	Name                     string                          `json:"name"`
	Enabled                  bool                            `json:"enabled"`
	Priority                 int                             `json:"priority"`
	OrganizationID           string                          `json:"organization_id"`
	AllowDirectWhenProxyFull bool                            `json:"allow_direct_when_proxy_full"`
	ConcurrencyLimit         *int                            `json:"concurrency_limit"`
	EffectiveLimit           int                             `json:"effective_limit"`
	Active                   int                             `json:"active"`
	Waiting                  int                             `json:"waiting"`
	QuotaSource              string                          `json:"quota_source"`
	CodexCredentialID        string                          `json:"codex_credential_id"`
}

// GetKiloAccounts exposes only public account fields from the live auth manager.
func (h *Handler) GetKiloAccounts(c *gin.Context) {
	accounts := make([]kiloAccountDTO, 0)
	if h.authManager != nil {
		snapshots := h.authManager.List()
		var bindings map[string]map[string]string
		bindingsAvailable := false
		if executor, ok := h.authManager.Executor("kilo"); ok {
			if source, ok := executor.(interface {
				KiloDeviceProfileBindings() (map[string]map[string]string, error)
			}); ok {
				var err error
				bindings, err = source.KiloDeviceProfileBindings()
				bindingsAvailable = err == nil
			}
		}
		quotaList := h.authManager.KiloQuotaList()
		concurrency := map[string]coreauth.CredentialConcurrencyEntry{}
		entries, _ := h.authManager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry)
		for _, entry := range entries {
			concurrency[entry.AuthID] = entry
		}
		for _, a := range snapshots {
			if !strings.EqualFold(a.Provider, "kilo") {
				continue
			}
			entry := concurrency[a.ID]
			view := h.kiloAccountView(a, entry, quotaList[a.ID])
			view.ClientIdentity = kiloClientIdentityView(a, bindings[a.ID], bindingsAvailable)
			accounts = append(accounts, view)
		}
	}
	c.JSON(http.StatusOK, gin.H{"accounts": accounts})
}

func (h *Handler) GetKiloStatus(c *gin.Context) {
	count, healthy, busy := 0, 0, 0
	configured := false
	healthEnabled, modelEnabled := false, false
	if h.authManager != nil {
		for _, a := range h.authManager.List() {
			if strings.EqualFold(a.Provider, "kilo") {
				count++
				if probes, err := h.authManager.KiloProbes(a.ID); err == nil {
					healthEnabled = healthEnabled || probes.Health.Enabled
					modelEnabled = modelEnabled || probes.ModelProbe.Enabled
				}
			}
		}
		if executor, ok := h.authManager.Executor("kilo"); ok {
			if source, ok := executor.(interface{ KiloProxySnapshot() kilo.PoolSnapshot }); ok {
				snapshot := source.KiloProxySnapshot()
				configured = count > 0 && len(snapshot.Proxies) > 0
				for _, p := range snapshot.Proxies {
					if p.Healthy {
						healthy++
					}
					if p.Active > 0 {
						busy++
					}
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"configured": configured, "account_count": count, "healthy_proxy_count": healthy, "busy_proxy_count": busy,
		"auto_account_probe_enabled": healthEnabled, "auto_model_probe_enabled": modelEnabled})
}

func (h *Handler) GetKiloCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"channel": "kilo", "public_protocols": []string{"responses", "chat_completions"},
		"tools_semantics": "source_parity", "images_semantics": "source_parity", "auto_account_probe_default": false,
		"auto_model_probe_default": false, "upstream_billing_verified": false})
}

func (h *Handler) kiloAccountView(a *coreauth.Auth, entry coreauth.CredentialConcurrencyEntry, snapshots ...coreauth.KiloQuotaListEntry) kiloAccountDTO {
	name := a.Label
	if name == "" {
		name = a.ID
	}
	priority, _ := strconv.Atoi(a.Attributes["priority"])
	org, _ := a.Metadata["organization_id"].(string)
	direct, _ := a.Metadata["allow_direct_when_proxy_full"].(bool)
	quotaSource, codexID := "none", ""
	var item coreauth.KiloQuotaListEntry
	if len(snapshots) > 0 {
		item = snapshots[0]
	} else {
		item = h.authManager.KiloQuotaList()[a.ID]
	}
	quota := item.Snapshot
	if quota.AccountID != "" {
		quotaSource, codexID = quota.Source.Kind, quota.Source.CredentialID
		if quotaSource == "cpa_codex" {
			quotaSource = "codex_ref"
		}
	}
	policy, _ := h.authManager.KiloSourcePolicy(a.ID)
	return kiloAccountDTO{SourcePolicy: policy, QuotaSourceSummary: item.Summary, QuotaSnapshot: item.Snapshot, AutoRestriction: h.authManager.KiloRestriction(a.ID), ID: a.ID, Name: name, Enabled: !a.Disabled, Priority: priority, OrganizationID: org, AllowDirectWhenProxyFull: direct, ConcurrencyLimit: entry.ConcurrencyLimit, EffectiveLimit: entry.EffectiveLimit, Active: entry.Active, Waiting: entry.PinnedWaiting, QuotaSource: quotaSource, CodexCredentialID: codexID}
}
