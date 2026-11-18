package management

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/updatestate"
)

func registerTestUpdateSite(t *testing.T, h *Handler) {
	t.Helper()
	if h.updateStateStore != nil {
		return
	}
	root := t.TempDir()
	if h.configFilePath == "" {
		h.configFilePath = filepath.Join(root, "config.yaml")
		if err := os.WriteFile(h.configFilePath, []byte("plugins: {enabled: false}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := updatestate.New(filepath.Join(root, ".mx-update.lock"), filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	h.updateStateStore = store
	t.Cleanup(func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			h.updateOperationsMu.Lock()
			remaining := len(h.updateOperations)
			h.updateOperationsMu.Unlock()
			if remaining == 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
}

func newImportTestHandler(t *testing.T, enabled bool) *Handler {
	t.Helper()
	root := t.TempDir()
	raw := fmt.Sprintf("plugins:\n  enabled: true\n  dir: %q\n  configs:\n    sample:\n      enabled: %t\n      custom: preserved\n", filepath.ToSlash(filepath.Join(root, "plugins")), enabled)
	cfg, err := config.ParseConfigBytes([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "config.yaml")
	if err = os.WriteFile(filename, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: cfg, configFilePath: filename}
	registerTestUpdateSite(t, h)
	return h
}

func testImportZIP(t *testing.T, licensing string) []byte {
	t.Helper()
	library := append([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 62, 0}, []byte("inert synthetic library")...)
	manifest := fmt.Sprintf(`{"id":"sample","version":"1.0.0","platform":"linux/amd64",%s"files":{"sample.so":"%x"}}`, licensing, sha256.Sum256(library))
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range map[string][]byte{"PLUGIN.json": []byte(manifest), "sample.so": library, "install.sh": []byte("exit 99\n")} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func importRequest(h *Handler, method, path, id string, body []byte, handle func(*gin.Context)) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	c.Params = gin.Params{{Key: "import", Value: id}}
	c.Set(ConfigV8ContextKey, true)
	handle(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func stageTestImport(t *testing.T, h *Handler, licensing string) string {
	t.Helper()
	rec := importRequest(h, http.MethodPost, "/v8/management/plugins/imports", "", testImportZIP(t, licensing), func(c *gin.Context) { h.importPluginZIP(c, "linux", "amd64") })
	if rec.Code != http.StatusCreated {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body)
	}
	return ojson.GetBytes(rec.Body.Bytes(), "id").String()
}

func waitTestOperation(t *testing.T, h *Handler, id string) updatestate.Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		h.updateOperationsMu.Lock()
		_, running := h.updateOperations[id]
		h.updateOperationsMu.Unlock()
		if running {
			time.Sleep(time.Millisecond)
			continue
		}
		op, err := h.updateStateStore.Get(id)
		if err == nil && (updatestate.Terminal(op.Status) || op.Status == "recovery_required") {
			return op
		}
		time.Sleep(time.Millisecond)
	}
	last, err := h.updateStateStore.Get(id)
	t.Fatalf("operation did not publish a terminal outcome: %+v err=%v", last, err)
	return updatestate.Operation{}
}

func TestPluginImportPreviewCancelAndExplicitConfirmations(t *testing.T) {
	h := newImportTestHandler(t, false)
	id := stageTestImport(t, h, `"license_required":true,`)
	for _, tc := range []struct{ body, code string }{{`{}`, "confirmation_required"}, {`{"confirm":true}`, "unverified_compatibility_confirmation_required"}, {`{"confirm":true,"confirm_unverified":true}`, "license_network_confirmation_required"}} {
		rec := importRequest(h, http.MethodPost, "/v8/management/plugins/imports/"+id+"/install", id, []byte(tc.body), h.InstallPluginImport)
		if !strings.Contains(rec.Body.String(), tc.code) {
			t.Fatalf("body=%s want=%s", rec.Body, tc.code)
		}
	}
	if _, err := os.Stat(h.cfg.Plugins.Dir); !os.IsNotExist(err) {
		t.Fatalf("preview wrote plugin state: %v", err)
	}
	rec := importRequest(h, http.MethodDelete, "/v8/management/plugins/imports/"+id, id, nil, h.CancelPluginImport)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cancel=%d %s", rec.Code, rec.Body)
	}
	if _, _, err := h.readPluginImport(id); err == nil {
		t.Fatal("cancel left candidate available")
	}
}

func TestPluginImportUnknownLicenseRequiresSeparateConsent(t *testing.T) {
	h := newImportTestHandler(t, false)
	id := stageTestImport(t, h, "")
	record, _, err := h.readPluginImport(id)
	if err != nil || record.Preview.LicenseStatus != "unknown" {
		t.Fatalf("preview=%+v err=%v", record, err)
	}
	rec := importRequest(h, http.MethodPost, "/v8/management/plugins/imports/"+id+"/install", id, []byte(`{"confirm":true,"confirm_unverified":true}`), h.InstallPluginImport)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "license_network_confirmation_required") {
		t.Fatalf("unknown license bypassed: %d %s", rec.Code, rec.Body)
	}
}

func TestPluginImportDisabledPreservesConfigAndBlocksConcurrentWriters(t *testing.T) {
	h := newImportTestHandler(t, false)
	id := stageTestImport(t, h, `"license_required":false,`)
	entered, release := make(chan struct{}), make(chan struct{})
	h.configReloadHook = func(context.Context, *config.Config) { close(entered); <-release }
	body := []byte(`{"confirm":true,"confirm_unverified":true}`)
	rec := importRequest(h, http.MethodPost, "/v8/management/plugins/imports/"+id+"/install", id, body, h.InstallPluginImport)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("install=%d %s", rec.Code, rec.Body)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reload did not start")
	}
	duplicate := importRequest(h, http.MethodPost, "/v8/management/plugins/imports/"+id+"/install", id, body, h.InstallPluginImport)
	if duplicate.Code != http.StatusOK || ojson.GetBytes(duplicate.Body.Bytes(), "id").String() != "plugin-"+id {
		t.Fatalf("duplicate=%d %s", duplicate.Code, duplicate.Body)
	}
	cancel := importRequest(h, http.MethodDelete, "/v8/management/plugins/imports/"+id, id, nil, h.CancelPluginImport)
	if cancel.Code != http.StatusConflict {
		t.Fatalf("cancel during maintenance=%d", cancel.Code)
	}
	storeRequest := importRequest(h, http.MethodPost, "/v0/management/plugin-store/other/install", "", nil, h.InstallPluginFromStore)
	if storeRequest.Code != http.StatusConflict || !strings.Contains(storeRequest.Body.String(), "plugin-"+id) {
		t.Fatalf("store escaped gate: %d %s", storeRequest.Code, storeRequest.Body)
	}
	close(release)
	op := waitTestOperation(t, h, "plugin-"+id)
	if op.Status != "completed_disabled" || op.Registered {
		t.Fatalf("disabled result=%+v", op)
	}
	h.mu.Lock()
	item := h.cfg.Plugins.Configs["sample"]
	h.mu.Unlock()
	if item.Enabled == nil || *item.Enabled || !strings.Contains(marshalPluginRaw(t, item), "custom: preserved") {
		t.Fatal("disabled intent or custom configuration was replaced")
	}
	if pluginStoreDesiredVersion(item) != "1.0.0" {
		t.Fatal("local import did not pin version")
	}
	if _, err := os.Stat(filepath.Join(h.updateStateStore.Directory, "backups", "plugin-"+id, "config.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestPluginImportFailedActivationPreservesConcurrentSettings(t *testing.T) {
	h := newImportTestHandler(t, true)
	id := stageTestImport(t, h, `"license_required":false,`)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int
	h.configReloadHook = func(context.Context, *config.Config) {
		calls++
		if calls == 1 {
			close(entered)
			<-release
		}
	}
	rec := importRequest(h, http.MethodPost, "/v8/management/plugins/imports/"+id+"/install", id, []byte(`{"confirm":true,"confirm_unverified":true}`), h.InstallPluginImport)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("install=%d %s", rec.Code, rec.Body)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reload did not start")
	}
	h.mu.Lock()
	h.cfg.Debug = true
	h.cfg.ProxyURL = "http://127.0.0.1:7890"
	h.cfg.APIKeys = []string{"new-local-key"}
	err := config.SaveConfigPreserveComments(h.configFilePath, h.cfg, true)
	h.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	op := waitTestOperation(t, h, "plugin-"+id)
	if op.Status != "failed" || op.Registered {
		t.Fatalf("false active result=%+v", op)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.cfg.Debug || h.cfg.ProxyURL != "http://127.0.0.1:7890" || len(h.cfg.APIKeys) != 1 || h.cfg.APIKeys[0] != "new-local-key" {
		t.Fatal("plugin recovery replaced unrelated current settings")
	}
	if pluginStoreDesiredVersion(h.cfg.Plugins.Configs["sample"]) != "" {
		t.Fatal("failed candidate pin was not restored")
	}
	path := filepath.Join(h.cfg.Plugins.Dir, "linux", "amd64", "sample-v1.0.0.so")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed candidate remains selectable: %v", err)
	}
	if _, err := os.Stat(path + ".failed-plugin-" + id); err != nil {
		t.Fatalf("failed candidate was not retained for diagnosis: %v", err)
	}
}

func TestPluginImportKnownIncompatiblePairIsBlocked(t *testing.T) {
	h := newImportTestHandler(t, false)
	id := stageTestImport(t, h, `"license_required":false,`)
	record, _, _ := h.readPluginImport(id)
	filename := filepath.Join(t.TempDir(), "pairs.json")
	if err := updatestate.WriteJSON(filename, map[string]any{"plugin_pairs": []map[string]string{{"plugin_id": "sample", "version": "1.0.0", "platform": "linux/amd64", "library_sha256": record.Preview.LibrarySHA256, "host_commit": buildinfo.Commit, "result": "incompatible"}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MX_UPDATE_CAPABILITY_EVIDENCE", filename)
	blocked := stageTestImport(t, h, `"license_required":false,`)
	rec := importRequest(h, http.MethodPost, "/v8/management/plugins/imports/"+blocked+"/install", blocked, []byte(`{"confirm":true,"confirm_unverified":true}`), h.InstallPluginImport)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "plugin_import_not_installable") {
		t.Fatalf("known incompatible installed: %d %s", rec.Code, rec.Body)
	}
}

func TestUpdateGuardUsesExactPluginPathSegments(t *testing.T) {
	for _, tc := range []struct {
		path    string
		guarded bool
	}{{"/v0/management/plugins/imports-tools/config", true}, {"/v0/management/plugins/quota-tools/enabled", true}, {"/v0/management/plugins/imports/config", true}, {"/v8/management/plugins/imports", false}, {"/v8/management/plugins/a/quota", false}, {"/v0/management/plugins/a/quota/reset", false}, {"/v8/management/config/plugins/configs/a", true}} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
		if got := updateGuardedRequest(c); got != tc.guarded {
			t.Errorf("%s guarded=%v", tc.path, got)
		}
	}
}

func TestUpdateDeleteDoesNotRequireRemovedPluginRegistration(t *testing.T) {
	h := newImportTestHandler(t, false)
	lease, err := h.updateStateStore.Begin(updatestate.Operation{ID: "delete-test", Type: "plugin"})
	if err != nil {
		t.Fatal(err)
	}
	m := &updateMutation{lease: lease, pluginID: "sample", action: "remove"}
	delete(h.cfg.Plugins.Configs, "sample")
	h.completeUpdateReload(m, configReloadSnapshot{cfg: h.cfg.CloneForRuntime(), generation: 1})
	op, err := h.updateStateStore.Get("delete-test")
	if err != nil || op.Status != "completed" || op.Registered {
		t.Fatalf("delete=%+v %v", op, err)
	}
}
