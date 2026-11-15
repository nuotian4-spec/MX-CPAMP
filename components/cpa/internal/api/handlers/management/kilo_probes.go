package management

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (h *Handler) GetKiloProbes(c *gin.Context) {
	if h.authManager == nil {
		kiloAccountError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	view, err := h.authManager.KiloProbes(c.Param("id"))
	if err != nil {
		kiloAccountError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func kiloProbeFields(root ojson.Result, allowed ...string) bool {
	if !root.IsObject() {
		return false
	}
	valid := true
	root.ForEach(func(key, value ojson.Result) bool {
		found := false
		for _, name := range allowed {
			if key.String() == name {
				found = true
				break
			}
		}
		valid = valid && found
		return valid
	})
	return valid
}

func (h *Handler) PutKiloProbes(c *gin.Context) {
	if h.authManager == nil {
		kiloAccountError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	root := ojson.ParseBytes(raw)
	health, model := root.Get("health"), root.Get("model_probe")
	revision, revisionErr := strconv.ParseUint(root.Get("expected_revision").Raw, 10, 64)
	interval, intervalErr := strconv.Atoi(model.Get("interval_seconds").Raw)
	valid := err == nil && ojson.ValidBytes(raw) && revisionErr == nil && intervalErr == nil &&
		kiloProbeFields(root, "expected_revision", "health", "model_probe") &&
		kiloProbeFields(health, "enabled") && kiloProbeFields(model, "enabled", "model", "interval_seconds") &&
		health.Get("enabled").IsBool() && model.Get("enabled").IsBool() && model.Get("model").Type == ojson.String
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Kilo probe configuration"})
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
	config := coreauth.KiloProbeConfig{HealthEnabled: health.Get("enabled").Bool(), ModelEnabled: model.Get("enabled").Bool(), Model: model.Get("model").String(), IntervalSeconds: interval}
	_, err = h.authManager.ChangeKiloAccount(c.Request.Context(), c.Param("id"), coreauth.KiloAccountChanges{ProbeConfig: &config, ExpectedProbeRevision: &revision}, false, false)
	if errors.Is(err, coreauth.ErrKiloProbeConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, coreauth.ErrKiloProbeInvalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		kiloAccountError(c, err)
		return
	}
	h.GetKiloProbes(c)
}
