package management

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
)

// GetBPSCredentialUsage 按精确引用读取官方账号用量，不变更凭证状态。
func (h *Handler) GetBPSCredentialUsage(c *gin.Context) {
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
	result := helps.QueryBPSAccountUsage(c.Request.Context(), cfg, auth)
	current := h.authByIndex(index)
	if !sameBPSAccessSnapshot(auth, current) || bpsAccessProxy(auth, cfg) != bpsAccessProxy(current, h.configSnapshot()) {
		result.Fail("credential_changed")
	}
	c.JSON(http.StatusOK, result)
}
