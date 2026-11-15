package management

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

func decodeKiloRecycleAction(c *gin.Context, clear bool) (uint64, error) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !ojson.ValidBytes(raw) {
		return 0, kilo.ErrStateInvalid
	}
	root := ojson.ParseBytes(raw)
	if !root.IsObject() {
		return 0, kilo.ErrStateInvalid
	}
	fields := map[string]ojson.Result{}
	root.ForEach(func(k, v ojson.Result) bool { fields[k.String()] = v; return true })
	v := fields["expected_revision"]
	if v.Type != ojson.Number {
		return 0, kilo.ErrStateInvalid
	}
	revision, err := strconv.ParseUint(v.Raw, 10, 64)
	if err != nil {
		return 0, kilo.ErrStateInvalid
	}
	delete(fields, "expected_revision")
	if clear {
		confirmed := fields["confirm"]
		if !confirmed.IsBool() || !confirmed.Bool() {
			return 0, kilo.ErrStateInvalid
		}
		delete(fields, "confirm")
	}
	if len(fields) != 0 {
		return 0, kilo.ErrStateInvalid
	}
	return revision, nil
}
func (h *Handler) ChangeKiloRecycle(c *gin.Context) {
	clear := c.Request.Method == http.MethodDelete && strings.HasSuffix(c.FullPath(), "/proxy-recycle")
	revision, err := decodeKiloRecycleAction(c, clear)
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	source, err := h.kiloStateSource()
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	var view kilo.ProxyStateView
	if c.Request.Method == http.MethodPost {
		view, err = source.ProbeKiloProxy(c.Request.Context(), revision, c.Param("id"), strings.HasSuffix(c.FullPath(), "/restore"))
	} else {
		view, err = h.authManager.ChangeKiloEgress(kilo.ProxyChanges{ExpectedRevision: revision, ID: c.Param("id"), Archive: !clear, ClearRecycle: clear})
	}
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
