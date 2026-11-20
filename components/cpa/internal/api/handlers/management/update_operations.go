package management

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/updatestate"
)

type updateContextKey struct{}

type updateMutation struct {
	mu              sync.Mutex
	lease           *updatestate.Lease
	scheduled       bool
	finished        bool
	pluginID        string
	version         string
	path            string
	licenseRequired bool
	licenseKnown    bool
	previous        *config.Config
	previousVersion string
	previousPath    string
	previousActive  bool
	action          string
	backupRoot      string
	replacedFiles   map[string]string
	createdPath     string
}

func (h *Handler) updateStore() (*updatestate.Store, error) {
	if h.updateStateStore != nil {
		return h.updateStateStore, nil
	}
	lockPath, state := os.Getenv("MX_UPDATE_LOCK_PATH"), os.Getenv("MX_UPDATE_STATE_DIR")
	if lockPath == "" || state == "" {
		return nil, fmt.Errorf("update site is not registered; install the host update integration first")
	}
	return updatestate.New(lockPath, state)
}

func updateGuardedRequest(c *gin.Context) bool {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
		return false
	}
	isV8 := strings.HasPrefix(c.Request.URL.Path, "/v8/management/")
	p := strings.TrimPrefix(strings.TrimPrefix(c.Request.URL.Path, "/v8/management"), "/v0/management")
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if isV8 && (parts[0] == "updates" || (len(parts) > 1 && parts[0] == "plugins" && parts[1] == "imports")) {
		return false
	}
	if len(parts) >= 3 && parts[0] == "plugins" && parts[2] == "quota" &&
		(len(parts) == 3 || (len(parts) == 4 && parts[3] == "reset")) {
		return false
	}
	return parts[0] == "config" || parts[0] == "config.yaml" || parts[0] == "plugin-store" || parts[0] == "plugins"
}

func (h *Handler) rememberUpdate(m *updateMutation) {
	h.updateOperationsMu.Lock()
	defer h.updateOperationsMu.Unlock()
	if h.updateOperations == nil {
		h.updateOperations = make(map[string]*updateMutation)
	}
	h.updateOperations[m.lease.Operation().ID] = m
}

func (h *Handler) forgetUpdate(m *updateMutation) {
	h.updateOperationsMu.Lock()
	defer h.updateOperationsMu.Unlock()
	delete(h.updateOperations, m.lease.Operation().ID)
}

func (h *Handler) ensureUpdateMutation(c *gin.Context) (*updateMutation, bool) {
	if current, ok := c.Request.Context().Value(updateContextKey{}).(*updateMutation); ok {
		return current, true
	}
	store, err := h.updateStore()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "update_site_unregistered", "message": err.Error()})
		return nil, false
	}
	action := "configure"
	if c.Request.Method == http.MethodPost && (c.FullPath() == "/v8/management/plugins/store/:id/install" || c.FullPath() == "/v0/management/plugin-store/:id/install") {
		action = "install"
	}
	if c.Request.Method == http.MethodDelete && strings.HasSuffix(c.FullPath(), "/plugins/:id") {
		action = "remove"
	}
	lease, err := store.Begin(updatestate.Operation{ID: uuid.NewString(), Type: "plugin", Stage: "preparing", PluginID: c.Param("id"), Action: action})
	if err != nil {
		writeUpdateBusy(c, err)
		return nil, false
	}
	m := &updateMutation{lease: lease, pluginID: c.Param("id"), action: action}
	if c.Request.Method == http.MethodDelete && strings.HasSuffix(c.FullPath(), "/plugins/:id") {
		m.action = "remove"
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), updateContextKey{}, m))
	c.Header("X-Update-Operation-ID", lease.Operation().ID)
	h.rememberUpdate(m)
	return m, true
}

func writeUpdateBusy(c *gin.Context, err error) {
	var busy *updatestate.BusyError
	if errors.As(err, &busy) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "update_busy", "operation_id": busy.OperationID})
		return
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "update_state_unavailable", "message": "Update state requires operator recovery"})
}

func (h *Handler) finishUpdateRequest(c *gin.Context, m *updateMutation) {
	m.mu.Lock()
	if m.scheduled || m.finished {
		m.mu.Unlock()
		return
	}
	m.finished = true
	m.mu.Unlock()
	status := "completed"
	if c.Writer.Status() >= 400 {
		status = "failed"
	}
	if err := m.lease.Finish(func(op *updatestate.Operation) { op.Status = status; op.Stage = status }); err != nil {
		_ = m.lease.Detach("Operation state could not be published")
	}
	h.forgetUpdate(m)
}

func (h *Handler) bindPluginUpdate(c *gin.Context, id, version, path string, licenseRequired bool, previous *config.Config) {
	m, _ := c.Request.Context().Value(updateContextKey{}).(*updateMutation)
	if m == nil {
		return
	}
	m.mu.Lock()
	m.pluginID = id
	m.version = version
	m.path = path
	m.licenseRequired = licenseRequired
	m.licenseKnown = licenseRequired
	m.previous = previous
	m.action = "install"
	h.mu.Lock()
	if h.pluginHost != nil {
		m.previousVersion, m.previousPath, m.previousActive = h.pluginHost.ActivePluginIdentity(id)
	}
	h.mu.Unlock()
	m.mu.Unlock()
	_ = m.lease.Update(func(op *updatestate.Operation) {
		op.PluginID = id
		op.TargetVersion = version
		op.Action = "install"
		op.Status = "installing"
		op.Stage = "installed"
	})
}

func (h *Handler) completeUpdateReload(m *updateMutation, snapshot configReloadSnapshot) {
	m.mu.Lock()
	id, version, expectedPath, licenseRequired, licenseKnown, action := m.pluginID, m.version, m.path, m.licenseRequired, m.licenseKnown, m.action
	m.mu.Unlock()
	op := m.lease.Operation()
	op.Status = "completed"
	op.Stage = "completed"
	op.LicenseStatus = "unknown"
	if licenseKnown && !licenseRequired {
		op.LicenseStatus = "not_required"
	}
	if id != "" {
		h.mu.Lock()
		host := h.pluginHost
		h.mu.Unlock()
		activeVersion, activePath, registered := "", "", false
		if host != nil {
			activeVersion, activePath, registered = host.ActivePluginIdentity(id)
		}
		op.ActiveVersion = activeVersion
		op.Registered = registered
		item, configured := snapshot.cfg.Plugins.Configs[id]
		enabled := snapshot.cfg.Plugins.Enabled && configured && pluginInstanceEnabled(item)
		if action == "remove" {
			if registered {
				op.Status = "failed"
				op.Message = "Removed plugin is still active"
			} else {
				op.Message = "Plugin removed and no longer registered"
			}
		} else if !enabled {
			if registered {
				op.Status = "failed"
				op.Message = "Disabled plugin is still active"
			} else {
				op.Status = "completed_disabled"
				op.Message = "Installed; original disabled state preserved"
			}
		} else if !registered || (version != "" && activeVersion != version) || (expectedPath != "" && filepath.Clean(activePath) != filepath.Clean(expectedPath)) {
			op.Status = "failed"
			op.Message = "Installed candidate did not become the active registered plugin"
		} else if licenseRequired || !licenseKnown {
			op.Status = "license_unverified"
			if licenseRequired {
				op.LicenseStatus = "unverified"
			}
			op.Message = "New version is active; license validity has not been verified"
		}
	}
	op.Stage = op.Status
	if op.Status == "failed" && m.previous != nil {
		if errRestore := m.restoreReplacedPlugins(); errRestore != nil {
			_ = m.lease.Detach("Candidate activation failed; previous library requires recovery from backup")
			h.forgetUpdate(m)
			return
		}
		h.mu.Lock()
		h.restorePluginConfigLocked(id, m.previous)
		errRestore := config.SaveConfigPreserveComments(h.configFilePath, h.cfg, true)
		restored := h.reloadSnapshotConfigLocked()
		h.mu.Unlock()
		if errRestore != nil {
			_ = m.lease.Detach("Candidate activation failed; original config backup requires recovery")
			h.forgetUpdate(m)
			return
		}
		h.reloadConfigAfterManagementSave(context.Background(), restored)
		h.mu.Lock()
		host := h.pluginHost
		h.mu.Unlock()
		op.ActiveVersion, op.Registered = "", false
		activePath := ""
		if host != nil {
			op.ActiveVersion, activePath, op.Registered = host.ActivePluginIdentity(id)
		}
		if op.Registered != m.previousActive || (op.Registered &&
			(op.ActiveVersion != m.previousVersion || filepath.Clean(activePath) != filepath.Clean(m.previousPath))) {
			_ = m.lease.Detach("Candidate activation failed; previous runtime identity was not restored")
			h.forgetUpdate(m)
			return
		}
		op.Message = "Candidate activation failed; previous plugin configuration and runtime identity restored"
	}
	if err := m.lease.Finish(func(value *updatestate.Operation) { *value = op }); err != nil {
		_ = m.lease.Detach("Reload finished; terminal record requires recovery")
	}
	m.mu.Lock()
	m.finished = true
	m.mu.Unlock()
	h.forgetUpdate(m)
}

// restorePluginConfigLocked preserves unrelated settings changed during maintenance.
func (h *Handler) restorePluginConfigLocked(id string, previous *config.Config) {
	if previous == nil || h.cfg == nil {
		return
	}
	if item, exists := previous.Plugins.Configs[id]; exists {
		ensurePluginConfigMap(h.cfg)
		h.cfg.Plugins.Configs[id] = item
	} else {
		delete(h.cfg.Plugins.Configs, id)
	}
}

func (h *Handler) GetUpdateOperation(c *gin.Context) {
	store, err := h.updateStore()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "update_site_unregistered"})
		return
	}
	op, err := store.Get(c.Param("operation"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "operation_not_found"})
		return
	}
	if !updatestate.Terminal(op.Status) && op.Type == "plugin" {
		h.updateOperationsMu.Lock()
		_, owned := h.updateOperations[op.ID]
		h.updateOperationsMu.Unlock()
		if !owned {
			op.Status = "recovery_required"
			op.Stage = "recovery_required"
			if op.Message == "" {
				op.Message = "Operation was interrupted; inspect runtime state before recovery"
			}
		}
	}
	c.JSON(http.StatusOK, op)
}

func (h *Handler) GetLatestUpdateOperation(c *gin.Context) {
	store, err := h.updateStore()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "update_site_unregistered"})
		return
	}
	op, err := store.Latest(c.Query("type"))
	if errors.Is(err, os.ErrNotExist) {
		c.JSON(http.StatusOK, gin.H{"operation": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "update_state_unavailable"})
		return
	}
	if !updatestate.Terminal(op.Status) && op.Type == "plugin" {
		h.updateOperationsMu.Lock()
		_, owned := h.updateOperations[op.ID]
		h.updateOperationsMu.Unlock()
		if !owned {
			op.Status = "recovery_required"
			op.Stage = "recovery_required"
			if op.Message == "" {
				op.Message = "Operation interrupted; runtime inspection is required"
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"operation": op})
}
