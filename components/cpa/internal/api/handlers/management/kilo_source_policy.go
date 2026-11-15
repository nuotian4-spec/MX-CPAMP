package management

import (
	"io"
	"strconv"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (h *Handler) GetKiloSourcePolicy(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.authManager == nil {
		kiloQuotaError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	v, err := h.authManager.KiloSourcePolicy(c.Param("id"))
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	c.JSON(200, v)
}
func (h *Handler) PutKiloSourcePolicy(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.authManager == nil {
		kiloQuotaError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if err != nil || len(body) > 4096 || !ojson.ValidBytes(body) {
		kiloQuotaError(c, coreauth.ErrKiloQuotaInvalid)
		return
	}
	root := ojson.ParseBytes(body)
	valid := root.IsObject()
	root.ForEach(func(k, v ojson.Result) bool {
		switch k.String() {
		case "enabled", "confirm_scope":
			valid = valid && v.IsBool()
		case "expected_policy_revision", "interval_seconds":
			valid = valid && v.Type == ojson.Number
		default:
			valid = false
		}
		return true
	})
	revision, e1 := strconv.ParseUint(root.Get("expected_policy_revision").Raw, 10, 53)
	interval, e2 := strconv.Atoi(root.Get("interval_seconds").Raw)
	if !valid || e1 != nil || e2 != nil || !root.Get("enabled").IsBool() {
		kiloQuotaError(c, coreauth.ErrKiloQuotaInvalid)
		return
	}
	v, err := h.authManager.ChangeKiloSourcePolicy(c.Request.Context(), c.Param("id"), revision, root.Get("enabled").Bool(), interval, root.Get("confirm_scope").Bool())
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	c.JSON(200, v)
}
