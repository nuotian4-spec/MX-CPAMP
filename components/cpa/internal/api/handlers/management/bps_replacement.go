package management

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var errBPSBuiltinReplaced = errors.New("bps_builtin_replaced")

// Replacement follows configured intent, including while the plugin is unloaded.
func (h *Handler) builtinBPSReplaced(provider string) bool {
	if h == nil || !strings.EqualFold(strings.TrimSpace(provider), "bps") {
		return false
	}
	cfg := h.configSnapshot()
	if cfg == nil {
		return false
	}
	plugin := cfg.Plugins.Configs["cpa-bps"]
	return plugin.Enabled != nil && *plugin.Enabled
}

func (h *Handler) rejectBuiltinBPS(c *gin.Context, provider string) bool {
	if !h.builtinBPSReplaced(provider) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{"status": "error", "error": errBPSBuiltinReplaced.Error()})
	return true
}
