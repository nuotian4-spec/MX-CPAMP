package management

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginstore"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/updatestate"
)

type pluginImportPreview struct {
	ID               string   `json:"id"`
	PluginID         string   `json:"plugin_id"`
	Version          string   `json:"version"`
	Platform         string   `json:"platform"`
	LibraryPath      string   `json:"library_path"`
	LibrarySHA256    string   `json:"library_sha256"`
	LicenseRequired  bool     `json:"license_required"`
	LicenseStatus    string   `json:"license_status"`
	CurrentVersion   string   `json:"current_version"`
	CurrentlyEnabled bool     `json:"currently_enabled"`
	Compatibility    string   `json:"compatibility"`
	Installable      bool     `json:"installable"`
	Blockers         []string `json:"blockers"`
	Integrity        string   `json:"integrity"`
	SourceTrust      string   `json:"source_trust"`
}

type pluginImportRecord struct {
	Preview  pluginImportPreview `json:"preview"`
	Baseline string              `json:"baseline"`
}

func (h *Handler) pluginImportRoot() (string, error) {
	var state string
	if h.updateStateStore != nil {
		state = h.updateStateStore.Directory
	} else {
		state = os.Getenv("MX_UPDATE_STATE_DIR")
	}
	if state == "" {
		if h.configFilePath == "" {
			return "", fmt.Errorf("configuration path is not available")
		}
		state = filepath.Join(filepath.Dir(h.configFilePath), ".mx-update-state")
	}
	root := filepath.Join(state, "imports")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func (h *Handler) importPath(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return "", fmt.Errorf("invalid import id")
	}
	root, err := h.pluginImportRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id), nil
}

func (h *Handler) importBaseline(id string) (string, string, bool, error) {
	raw, err := os.ReadFile(h.configFilePath)
	if err != nil {
		return "", "", false, err
	}
	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		return "", "", false, fmt.Errorf("configuration is not available")
	}
	item, configured := h.cfg.Plugins.Configs[id]
	enabled := !configured || pluginInstanceEnabled(item)
	host := h.pluginHost
	version := pluginStoreDesiredVersions(h.cfg.Plugins.Configs)[id]
	h.mu.Unlock()
	activePath := ""
	if host != nil {
		if activeVersion, path, registered := host.ActivePluginIdentity(id); registered {
			version = activeVersion
			activePath = path
		}
	}
	return config.ComputeSourceHash(raw) + ":" + version + ":" + activePath, version, enabled, nil
}

// ImportPluginZIP performs static inspection only, with no plugin initialization.
func (h *Handler) ImportPluginZIP(c *gin.Context) {
	h.importPluginZIP(c, runtime.GOOS, runtime.GOARCH)
}

func (h *Handler) importPluginZIP(c *gin.Context, goos, goarch string) {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, pluginstore.MaxImportArchiveBytes))
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "plugin_archive_too_large"})
		return
	}
	archive, err := pluginstore.PrepareImportArchive(data, goos, goarch)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "plugin_archive_invalid", "message": err.Error()})
		return
	}
	baseline, current, enabled, err := h.importBaseline(archive.PluginID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plugin_baseline_unavailable"})
		return
	}
	preview := pluginImportPreview{ID: uuid.NewString(), PluginID: archive.PluginID, Version: archive.Version, Platform: archive.Platform,
		LibraryPath: archive.LibraryPath, LibrarySHA256: archive.LibrarySHA256, LicenseRequired: archive.LicenseRequired,
		CurrentVersion: current, CurrentlyEnabled: enabled, Compatibility: h.pluginImportCompatibility(archive), Installable: true,
		Blockers: []string{}, Integrity: archive.Integrity, SourceTrust: "administrator_supplied"}
	preview.LicenseStatus = "unknown"
	if archive.LicenseKnown {
		preview.LicenseStatus = "not_required"
		if archive.LicenseRequired {
			preview.LicenseStatus = "required"
		}
	}
	if preview.Compatibility == "incompatible" {
		preview.Installable = false
		preview.Blockers = append(preview.Blockers, "known_incompatible_pair")
	}
	if _, err = h.updateStore(); err != nil {
		preview.Installable = false
		preview.Blockers = append(preview.Blockers, "update_site_unregistered")
	}
	directory, err := h.importPath(preview.ID)
	if err == nil {
		err = os.Mkdir(directory, 0o700)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(directory, "archive.zip"), archive.Archive, 0o600)
	}
	if err == nil {
		err = updatestate.WriteJSON(filepath.Join(directory, "preview.json"), pluginImportRecord{Preview: preview, Baseline: baseline})
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_import_save_failed"})
		return
	}
	c.JSON(http.StatusCreated, preview)
}

// Optional exact-pair evidence annotates a preview; its absence is not a whitelist denial.
func (h *Handler) pluginImportCompatibility(archive pluginstore.ImportedArchive) string {
	filename := os.Getenv("MX_UPDATE_CAPABILITY_EVIDENCE")
	if filename == "" {
		return "unverified"
	}
	data, err := os.ReadFile(filename)
	if err != nil || !ojson.ValidBytes(data) {
		return "unverified"
	}
	for _, pair := range ojson.GetBytes(data, "plugin_pairs").Array() {
		if pair.Get("plugin_id").String() == archive.PluginID && pair.Get("version").String() == archive.Version &&
			pair.Get("platform").String() == archive.Platform && pair.Get("library_sha256").String() == archive.LibrarySHA256 &&
			pair.Get("host_commit").String() == buildinfo.Commit {
			switch pair.Get("result").String() {
			case "registered":
				return "verified"
			case "incompatible":
				return "incompatible"
			}
		}
	}
	return "unverified"
}

func (h *Handler) readPluginImport(id string) (pluginImportRecord, string, error) {
	directory, err := h.importPath(id)
	if err != nil {
		return pluginImportRecord{}, "", err
	}
	data, err := os.ReadFile(filepath.Join(directory, "preview.json"))
	if err != nil || !ojson.ValidBytes(data) {
		return pluginImportRecord{}, "", fmt.Errorf("import not found")
	}
	doc := ojson.ParseBytes(data)
	p := doc.Get("preview")
	preview := pluginImportPreview{ID: p.Get("id").String(), PluginID: p.Get("plugin_id").String(), Version: p.Get("version").String(),
		Platform: p.Get("platform").String(), LibraryPath: p.Get("library_path").String(), LibrarySHA256: p.Get("library_sha256").String(),
		LicenseRequired: p.Get("license_required").Bool(), LicenseStatus: p.Get("license_status").String(), CurrentVersion: p.Get("current_version").String(), CurrentlyEnabled: p.Get("currently_enabled").Bool(),
		Compatibility: p.Get("compatibility").String(), Installable: p.Get("installable").Bool(), Blockers: []string{}, Integrity: p.Get("integrity").String(), SourceTrust: p.Get("source_trust").String()}
	for _, blocker := range p.Get("blockers").Array() {
		preview.Blockers = append(preview.Blockers, blocker.String())
	}
	if preview.ID != id {
		return pluginImportRecord{}, "", fmt.Errorf("import identity mismatch")
	}
	return pluginImportRecord{Preview: preview, Baseline: doc.Get("baseline").String()}, directory, nil
}

func (h *Handler) GetPluginImport(c *gin.Context) {
	record, _, err := h.readPluginImport(c.Param("import"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_import_not_found"})
		return
	}
	c.JSON(http.StatusOK, record.Preview)
}

func (h *Handler) CancelPluginImport(c *gin.Context) {
	h.pluginImportsMu.Lock()
	defer h.pluginImportsMu.Unlock()
	id := c.Param("import")
	_, directory, err := h.readPluginImport(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_import_not_found"})
		return
	}
	if store, errStore := h.updateStore(); errStore == nil {
		if _, errOperation := store.Get("plugin-" + id); errOperation == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "plugin_install_already_started", "operation_id": "plugin-" + id})
			return
		}
	}
	for _, name := range []string{"archive.zip", "preview.json"} {
		if err = os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_import_cancel_failed"})
			return
		}
	}
	if err = os.Remove(directory); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_import_cancel_failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) InstallPluginImport(c *gin.Context) {
	h.pluginImportsMu.Lock()
	defer h.pluginImportsMu.Unlock()
	id := c.Param("import")
	record, directory, err := h.readPluginImport(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_import_not_found"})
		return
	}
	store, err := h.updateStore()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "update_site_unregistered"})
		return
	}
	operationID := "plugin-" + id
	if previous, errPrevious := store.Get(operationID); errPrevious == nil {
		c.JSON(http.StatusOK, previous)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !ojson.ValidBytes(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_confirmation"})
		return
	}
	request := ojson.ParseBytes(body)
	if !request.Get("confirm").IsBool() || !request.Get("confirm").Bool() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "confirmation_required"})
		return
	}
	if record.Preview.Compatibility == "incompatible" || !record.Preview.Installable {
		c.JSON(http.StatusConflict, gin.H{"error": "plugin_import_not_installable"})
		return
	}
	if record.Preview.Compatibility != "verified" && (!request.Get("confirm_unverified").IsBool() || !request.Get("confirm_unverified").Bool()) {
		c.JSON(http.StatusConflict, gin.H{"error": "unverified_compatibility_confirmation_required"})
		return
	}
	if (record.Preview.LicenseRequired || record.Preview.LicenseStatus == "unknown") && (!request.Get("confirm_license_network").IsBool() || !request.Get("confirm_license_network").Bool()) {
		c.JSON(http.StatusConflict, gin.H{"error": "license_network_confirmation_required"})
		return
	}
	lease, err := store.Begin(updatestate.Operation{ID: operationID, Type: "plugin", Action: "install", PluginID: record.Preview.PluginID, TargetVersion: record.Preview.Version,
		Status: "queued", Stage: "preparing", Baseline: record.Baseline, LicenseStatus: "unverified"})
	if err != nil {
		if previous, errPrevious := store.Get(operationID); errPrevious == nil {
			c.JSON(http.StatusOK, previous)
			return
		}
		writeUpdateBusy(c, err)
		return
	}
	baseline, _, _, err := h.importBaseline(record.Preview.PluginID)
	if err != nil || baseline != record.Baseline {
		if errFinish := lease.Finish(func(op *updatestate.Operation) {
			op.Status = "failed"
			op.Stage = "baseline_changed"
			op.Message = "Runtime baseline changed; preview the package again"
		}); errFinish != nil {
			_ = lease.Detach("Baseline changed; terminal record requires recovery")
		}
		c.JSON(http.StatusConflict, gin.H{"error": "plugin_baseline_changed", "operation_id": operationID})
		return
	}
	h.mu.Lock()
	previous := h.cfg.CloneForRuntime()
	h.mu.Unlock()
	mutation := &updateMutation{lease: lease, scheduled: true, action: "install", pluginID: record.Preview.PluginID, version: record.Preview.Version, licenseRequired: record.Preview.LicenseRequired, previous: previous}
	mutation.licenseKnown = record.Preview.LicenseStatus == "required" || record.Preview.LicenseStatus == "not_required"
	h.mu.Lock()
	if h.pluginHost != nil {
		mutation.previousVersion, mutation.previousPath, mutation.previousActive = h.pluginHost.ActivePluginIdentity(mutation.pluginID)
	}
	h.mu.Unlock()
	h.rememberUpdate(mutation)
	op := lease.Operation()
	go h.runPluginImport(directory, record, mutation)
	c.JSON(http.StatusAccepted, op)
}

func (h *Handler) runPluginImport(directory string, record pluginImportRecord, mutation *updateMutation) {
	defer func() {
		if recover() != nil {
			_ = mutation.lease.Detach("Plugin install interrupted; inspect saved state before recovery")
			h.forgetUpdate(mutation)
		}
	}()
	fail := func(message string) {
		if errFinish := mutation.lease.Finish(func(op *updatestate.Operation) { op.Status = "failed"; op.Stage = "failed"; op.Message = message }); errFinish != nil {
			_ = mutation.lease.Detach("Install failed; terminal record requires recovery")
		}
		h.forgetUpdate(mutation)
	}
	if err := mutation.lease.Update(func(op *updatestate.Operation) { op.Status = "installing"; op.Stage = "backup" }); err != nil {
		_ = mutation.lease.Detach("Update state could not be persisted")
		h.forgetUpdate(mutation)
		return
	}
	if err := h.preparePluginBackup(mutation, record.Preview.PluginID); err != nil {
		fail("Original configuration and active library could not be backed up")
		return
	}
	h.mu.Lock()
	pluginsDir := h.cfg.Plugins.Dir
	host := h.pluginHost
	h.mu.Unlock()
	pluginsDir, err := config.ResolvePluginsDir(pluginsDir)
	if err != nil {
		fail("Plugin directory is invalid")
		return
	}
	archive, err := os.ReadFile(filepath.Join(directory, "archive.zip"))
	if err != nil {
		fail("Staged plugin archive is missing")
		return
	}
	platform := strings.Split(record.Preview.Platform, "/")
	if len(platform) != 2 {
		fail("Staged platform is invalid")
		return
	}
	result, err := pluginstore.InstallArchive(archive, pluginstore.Plugin{ID: record.Preview.PluginID, Version: record.Preview.Version}, pluginstore.InstallOptions{
		PluginsDir: pluginsDir, GOOS: platform[0], GOARCH: platform[1], PluginLoaded: func() bool { return pluginBusy(host, record.Preview.PluginID) },
		BeforeWrite: mutation.backupReplacedPlugin, BeforeCreate: mutation.recordCreatedPlugin,
	})
	if err != nil {
		if errors.Is(err, pluginstore.ErrLoadedPluginLocked) {
			if errFinish := mutation.lease.Finish(func(op *updatestate.Operation) {
				op.Status = "failed"
				op.Stage = "restart_required"
				op.Message = "Active same-version library differs; stop or restart the host before replacement"
			}); errFinish != nil {
				_ = mutation.lease.Detach("Restart-required outcome could not be persisted")
			}
			h.forgetUpdate(mutation)
			return
		}
		fail("Plugin library installation failed; original state is preserved in the operation backup")
		return
	}
	h.mu.Lock()
	// A local import pins the desired version without inventing a remote source.
	manifest := pluginstore.Manifest{SchemaVersion: pluginstore.SchemaVersionV2, ID: record.Preview.PluginID, Version: record.Preview.Version, SourceID: "local-import", SourceName: "Local ZIP", Install: pluginstore.InstallPlan{Type: pluginstore.InstallTypeLocal}}
	err = h.enablePluginConfigLocked(record.Preview.PluginID, manifest)
	if err == nil {
		err = config.SaveConfigPreserveComments(h.configFilePath, h.cfg, true)
	}
	if err != nil {
		h.restorePluginConfigLocked(record.Preview.PluginID, mutation.previous)
		h.mu.Unlock()
		if restoreErr := mutation.restoreReplacedPlugins(); restoreErr != nil {
			_ = mutation.lease.Detach("Configuration save failed; library and configuration require recovery")
			h.forgetUpdate(mutation)
			return
		}
		_ = mutation.lease.Detach("Library installed; configuration save requires recovery from the preserved backup")
		h.forgetUpdate(mutation)
		return
	}
	snapshot := h.reloadSnapshotConfigLocked()
	h.mu.Unlock()
	mutation.mu.Lock()
	mutation.path = result.Path
	mutation.mu.Unlock()
	ctx := context.WithValue(context.Background(), updateContextKey{}, mutation)
	h.reloadConfigAfterManagementSaveAsync(ctx, snapshot)
}
