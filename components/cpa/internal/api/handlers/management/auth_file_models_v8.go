package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// GetAuthFileModelsV8 adds discovery state without changing the legacy response.
func (h *Handler) GetAuthFileModelsV8(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	id := name
	if h.authManager != nil {
		if a, ok := h.authManager.GetByID(name); ok {
			id = a.ID
		} else {
			for _, a := range h.authManager.List() {
				if a.FileName == name {
					id = a.ID
					break
				}
			}
		}
	}
	models := make([]gin.H, 0)
	for _, m := range registry.GetGlobalRegistry().GetModelsForClient(id) {
		entry := gin.H{"id": m.ID}
		if m.DisplayName != "" {
			entry["display_name"] = m.DisplayName
		}
		if m.Type != "" {
			entry["type"] = m.Type
		}
		if m.OwnedBy != "" {
			entry["owned_by"] = m.OwnedBy
		}
		models = append(models, entry)
	}
	discovery := coreauth.ModelDiscovery{Status: "unknown"}
	if h.authManager != nil {
		discovery = h.authManager.ModelDiscoveryForAuth(id)
	}
	c.JSON(http.StatusOK, gin.H{"models": models, "discovery": discovery})
}
