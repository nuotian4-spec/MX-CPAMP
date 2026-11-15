package management

import (
	"io"
	"net/http"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

func (h *Handler) PreviewKiloProxy(c *gin.Context) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !ojson.ValidBytes(raw) {
		kiloProxyError(c, kilo.ErrStateInvalid)
		return
	}
	root := ojson.ParseBytes(raw)
	url, valid := "", root.IsObject()
	root.ForEach(func(k, v ojson.Result) bool {
		if k.String() != "url" || v.Type != ojson.String {
			valid = false
		} else {
			url = v.String()
		}
		return true
	})
	setting, err := proxyutil.Parse(url)
	if !valid || err != nil || setting.Mode != proxyutil.ModeProxy {
		kiloProxyError(c, kilo.ErrStateInvalid)
		return
	}
	source, err := h.kiloPreviewSource()
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	view, err := source.PreviewKiloProxy(c.Request.Context(), url)
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, view)
}

func (h *Handler) kiloPreviewSource() (*executor.KiloExecutor, error) {
	if h.authManager == nil {
		return nil, kilo.ErrStateStorage
	}
	if current, ok := h.authManager.Executor("kilo"); ok {
		source, valid := current.(*executor.KiloExecutor)
		if !valid {
			return nil, kilo.ErrStateStorage
		}
		return source, nil
	}
	h.mu.Lock()
	cfg := h.cfg.CloneForRuntime()
	h.mu.Unlock()
	return executor.NewKiloExecutor(cfg), nil
}
