package management

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// GetBPSCredentialAccess performs a read-only query for one exact registered credential.
func (h *Handler) GetBPSCredentialAccess(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	valid := err == nil
	for key, values := range query {
		if (key != "auth_index" && key != "auth_id") || len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			valid = false
		}
	}
	index := strings.TrimSpace(query.Get("auth_index"))
	if !valid || index == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_reference"})
		return
	}
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth_manager_unavailable"})
		return
	}
	auth := h.authByIndex(index)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential_not_found"})
		return
	}
	if auth.Provider != "bps" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider_mismatch"})
		return
	}
	if expected := strings.TrimSpace(query.Get("auth_id")); expected != "" && expected != auth.ID {
		c.JSON(http.StatusConflict, gin.H{"error": "credential_changed"})
		return
	}
	cfg := h.configSnapshot()
	result := helps.QueryBPSAccess(c.Request.Context(), cfg, auth)
	current := h.authByIndex(index)
	if !sameBPSAccessSnapshot(auth, current) || bpsAccessProxy(auth, cfg) != bpsAccessProxy(current, h.configSnapshot()) {
		result.Fail("credential_changed")
	}
	c.JSON(http.StatusOK, result)
}

func bpsAccessProxy(auth *coreauth.Auth, cfg *config.Config) string {
	if auth != nil && strings.TrimSpace(auth.ProxyURL) != "" {
		return strings.TrimSpace(auth.ProxyURL)
	}
	if cfg != nil {
		return strings.TrimSpace(cfg.ProxyURL)
	}
	return ""
}

func sameBPSAccessSnapshot(before, after *coreauth.Auth) bool {
	if before == nil || after == nil || before.ID != after.ID || after.Provider != "bps" || before.ProxyURL != after.ProxyURL || before.RegistrationEpoch != after.RegistrationEpoch || before.CredentialVersion != after.CredentialVersion {
		return false
	}
	for _, key := range []string{"access_token", "account_id", "sub"} {
		if metadataStringValue(before.Metadata, key) != metadataStringValue(after.Metadata, key) {
			return false
		}
	}
	return true
}
