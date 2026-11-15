package management

import (
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (h *Handler) TestKiloAccount(c *gin.Context) {
	if h.authManager == nil {
		kiloAccountError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !ojson.ValidBytes(raw) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Kilo test request"})
		return
	}
	root := ojson.ParseBytes(raw)
	valid := root.IsObject() && root.Get("model").Type == ojson.String && root.Get("prompt").Type == ojson.String && strings.TrimSpace(root.Get("prompt").String()) != ""
	root.ForEach(func(key, value ojson.Result) bool {
		if key.String() != "model" && key.String() != "prompt" {
			valid = false
		}
		return true
	})
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Kilo test request"})
		return
	}
	account, exists := h.authManager.GetByID(c.Param("id"))
	if !exists || account.Provider != "kilo" {
		kiloAccountError(c, coreauth.ErrKiloAccountMissing)
		return
	}
	if _, err = h.kiloStateSource(); err != nil {
		kiloAccountError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	result, err := h.authManager.ExecuteKiloDiagnostic(c.Request.Context(), c.Param("id"), root.Get("model").String(), root.Get("prompt").String(), true)
	if err != nil {
		status := http.StatusServiceUnavailable
		if coded, ok := err.(interface{ StatusCode() int }); ok {
			status = coded.StatusCode()
		}
		if err == coreauth.ErrKiloAccountMissing || err == coreauth.ErrKiloIdentityChanged {
			kiloAccountError(c, err)
			return
		}
		c.JSON(status, gin.H{"error": "Kilo test was not started"})
		return
	}
	c.JSON(http.StatusOK, result)
}
