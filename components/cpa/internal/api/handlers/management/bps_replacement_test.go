package management

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func enableBPSReplacement(h *Handler) {
	enabled := true
	h.cfg.Plugins = config.PluginsConfig{Enabled: false, Configs: map[string]config.PluginInstanceConfig{"cpa-bps": {Enabled: &enabled}}}
}

func TestBPSReplacementBlocksNativeEntriesAndLateSave(t *testing.T) {
	h, m := bpsTestHandler(t)
	old := bpsTestImport(t, h, "bps", bpsTestBody("bps", "old@example.com", "old-access"))
	if old.Code != 200 {
		t.Fatal(old.Body.String())
	}
	id := ojson.NewDoc(old.Body.Bytes()).Get("auth_id").String()
	enableBPSReplacement(h)
	for _, path := range []string{"/v8/management/oauth/auth-url?provider=bps", "/v8/management/oauth/auth-url?name=" + id} {
		w := bpsTestRequest(t, h.StartOAuthV8, "POST", path, `{}`, nil)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "bps_builtin_replaced") {
			t.Errorf("OAuth %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := bpsTestImport(t, h, "bps", bpsTestBody("bps", "new@example.com", "new-access"))
	if w.Code != 409 {
		t.Errorf("import: %d %s", w.Code, w.Body.String())
	}
	if _, err := h.saveImportedOAuth(context.Background(), "bps", bpsTestBody("bps", "late@example.com", "late-access")); err == nil {
		t.Error("late save accepted")
	}
	if len(m.List()) != 1 {
		t.Error("BPS state changed")
	}
	w = bpsTestImport(t, h, "codex", bpsTestBody("codex", "codex@example.com", "codex-access"))
	if w.Code != 200 {
		t.Errorf("Codex rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestBPSReplacementBlocksUploadAndEnableAllowsDisable(t *testing.T) {
	h, m := bpsTestHandler(t)
	old := bpsTestImport(t, h, "bps", bpsTestBody("bps", "old@example.com", "old-access"))
	id := ojson.NewDoc(old.Body.Bytes()).Get("auth_id").String()
	enableBPSReplacement(h)
	w := bpsTestRequest(t, h.UploadAuthFileV8, "POST", "/v8/management/credentials?name=new-bps.json", `{"type":"bps","email":"new@example.com"}`, nil)
	if w.Code != 409 {
		t.Errorf("upload: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(h.cfg.AuthDir, "new-bps.json")); !os.IsNotExist(err) {
		t.Error("rejected upload wrote file")
	}
	for _, disabled := range []bool{true, false} {
		w = bpsTestRequest(t, h.PatchAuthFileStatus, "PATCH", "/v8/management/credentials/status", bpsTestJSON(t, map[string]any{"name": id, "disabled": disabled}), nil)
		want := 200
		if !disabled {
			want = 409
		}
		if w.Code != want {
			t.Errorf("disabled=%v: %d %s", disabled, w.Code, w.Body.String())
		}
	}
	a, _ := m.GetByID(id)
	if a == nil || !a.Disabled {
		t.Error("old BPS enabled")
	}
}

func TestBPSPluginLoginReportsSavedAndAdopted(t *testing.T) {
	h, _ := bpsTestHandler(t)
	host := pluginhost.New()
	host.RegisterPluginForTest("cpa-bps", pluginapi.Plugin{Capabilities: pluginapi.Capabilities{AuthProvider: &testAuthProvider{identifier: "cpa-bps", pollLogin: func(context.Context, pluginapi.AuthLoginPollRequest) (pluginapi.AuthLoginPollResponse, error) {
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusSuccess, Auth: pluginapi.AuthData{Provider: "cpa-bps", ID: "cpa-bps-test.json", FileName: "cpa-bps-test.json", StorageJSON: []byte(`{"type":"cpa-bps","access_token":"synthetic"}`), Metadata: map[string]any{"type": "cpa-bps", "access_token": "synthetic"}}}, nil
	}}}})
	h.SetPluginHost(host)
	state := "state-bps-plugin-save-1234567890"
	if err := RegisterPluginOAuthSession(state, "cpa-bps", nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { CancelOAuthSession(state) })
	for _, path := range []string{"/v0/management/get-auth-status?state=", "/v8/management/oauth/status?state="} {
		w := bpsTestRequest(t, h.GetAuthStatus, "GET", path+state, "", nil)
		d := ojson.NewDoc(w.Body.Bytes())
		if w.Code != 200 || !d.Get("saved").Bool() || !d.Get("adopted").Bool() {
			t.Fatalf("missing persistence outcome: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestBPSReplacementFieldsCannotEnable(t *testing.T) {
	h, m := bpsTestHandler(t)
	old := bpsTestImport(t, h, "bps", bpsTestBody("bps", "old@example.com", "old-access"))
	id := ojson.NewDoc(old.Body.Bytes()).Get("auth_id").String()
	enableBPSReplacement(h)
	for _, value := range []any{false, "false", 0, nil} {
		w := bpsTestRequest(t, h.PatchAuthFileFields, "PATCH", "/v8/management/credentials/fields", bpsTestJSON(t, map[string]any{"name": id, "disabled": true}), nil)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		w = bpsTestRequest(t, h.PatchAuthFileFields, "PATCH", "/v8/management/credentials/fields", bpsTestJSON(t, map[string]any{"name": id, "disabled": value}), nil)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "bps_builtin_replaced") {
			t.Errorf("enable=%v: %d %s", value, w.Code, w.Body.String())
		}
	}
	w := bpsTestRequest(t, h.PatchAuthFileFields, "PATCH", "/v8/management/credentials/fields", bpsTestJSON(t, map[string]any{"name": id, "note": "preserved"}), nil)
	a, _ := m.GetByID(id)
	if w.Code != 200 || a == nil || !a.Disabled {
		t.Error("unrelated field edit changed disabled state")
	}
}
