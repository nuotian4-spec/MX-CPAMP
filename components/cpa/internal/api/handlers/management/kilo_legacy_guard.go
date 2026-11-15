package management

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

var errKiloDedicated = errors.New("Kilo credentials require /v8/management/kilo/accounts; secret export is disabled")

func isKiloAccount(a *coreauth.Auth) bool {
	return a != nil && strings.EqualFold(strings.TrimSpace(a.Provider), "kilo")
}
func isKiloJSON(data []byte) bool {
	if !ojson.ValidBytes(data) {
		return false
	}
	root := ojson.ParseBytes(data)
	if !root.IsObject() {
		return false
	}
	provider := ""
	valid := true
	root.ForEach(func(key, value ojson.Result) bool {
		if !strings.EqualFold(key.String(), "type") {
			return true
		}
		if value.Type == ojson.Null {
			return true
		}
		if value.Type != ojson.String {
			valid = false
			return false
		}
		provider = value.String()
		return true
	})
	return valid && strings.EqualFold(strings.TrimSpace(provider), "kilo")
}
func (h *Handler) isKiloFile(name string) bool {
	if a := h.findAuthForDelete(name); isKiloAccount(a) {
		return true
	}
	if h.cfg == nil || isUnsafeAuthFileName(name) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(h.cfg.AuthDir, filepath.Base(name)))
	return err == nil && isKiloJSON(data)
}
func rejectKiloLegacy(c *gin.Context) { c.JSON(409, gin.H{"error": errKiloDedicated.Error()}) }
