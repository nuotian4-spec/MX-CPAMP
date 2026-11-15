package management

import (
	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (h *Handler) GetKiloOrganizations(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.authManager == nil {
		kiloAccountError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	account, ok := h.authManager.GetByID(c.Param("id"))
	if !ok || account.Provider != "kilo" {
		kiloAccountError(c, coreauth.ErrKiloAccountMissing)
		return
	}
	source, err := h.kiloStateSource()
	if err != nil {
		kiloAccountError(c, err)
		return
	}
	choices, err := source.KiloOrganizations(c.Request.Context(), account)
	if err != nil {
		kiloAccountError(c, err)
		return
	}
	c.JSON(200, choices)
}
