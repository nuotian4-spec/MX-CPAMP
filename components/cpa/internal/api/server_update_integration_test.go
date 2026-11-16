//go:build linux && cgo

package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/updatestate"
)

type updateRouterFixture struct {
	server     *Server
	host       *pluginhost.Host
	store      *updatestate.Store
	configPath string
}

func newUpdateRouterFixture(t *testing.T, enabled bool, authentication string) *updateRouterFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	raw := fmt.Sprintf("remote-management: {secret-key: hash-test-key}\nplugins:\n  enabled: true\n  dir: %q\n  configs:\n    sample:\n      enabled: %t\n      custom: preserved\n", filepath.Join(root, "plugins"), enabled)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MX_UPDATE_LOCK_PATH", filepath.Join(root, ".mx-update.lock"))
	t.Setenv("MX_UPDATE_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("MX_UPDATE_CAPABILITY_EVIDENCE", "")
	t.Setenv("MANAGEMENT_PASSWORD", "")
	if authentication == "environment" {
		t.Setenv("MANAGEMENT_PASSWORD", "environment-test-key")
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	host := pluginhost.New()
	h := management.NewHandler(cfg, path, nil)
	h.SetPluginHost(host)
	if authentication == "local" {
		h.SetLocalPassword("local-test-key")
	}
	s := &Server{cfg: cfg, engine: gin.New(), mgmt: h}
	s.managementRoutesEnabled.Store(true)
	s.registerManagementRoutes()
	store, err := updatestate.New(os.Getenv("MX_UPDATE_LOCK_PATH"), os.Getenv("MX_UPDATE_STATE_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.ShutdownAll)
	return &updateRouterFixture{server: s, host: host, store: store, configPath: path}
}

func (f *updateRouterFixture) request(t *testing.T, method, path, key string, body []byte, want int) []byte {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1234"
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	f.server.engine.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, rec.Code, want, rec.Body)
	}
	return rec.Body.Bytes()
}

func syntheticUpdateZIP(t *testing.T, version, marker string, schema int) []byte {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "plugin.c")
	library := filepath.Join(root, "sample.so")
	registration := fmt.Sprintf(`{"ok":true,"result":{"schema_version":%d,"metadata":{"Name":"Synthetic Update Fixture","Version":%q,"Author":"test","GitHubRepository":"https://github.com/example/synthetic-update"},"capabilities":{"usage_plugin":true}}}`, schema, version)
	code := fmt.Sprintf(`#include <stdint.h>
#include <stdlib.h>
#include <string.h>
typedef struct { void *ptr; size_t len; } buffer;
typedef struct { uint32_t abi_version; void *host_ctx; void *call; void *free_buffer; } host_api;
typedef struct { uint32_t abi_version; int (*call)(const char*,const uint8_t*,size_t,buffer*); void (*free_buffer)(void*,size_t); void (*shutdown)(void); } plugin_api;
const char *fixture_marker=%q;
static int call(const char *method,const uint8_t *request,size_t length,buffer *out) {
  (void)request;(void)length; const char *text="{\"ok\":true,\"result\":{}}";
  if (!strcmp(method,"plugin.register") || !strcmp(method,"plugin.reconfigure")) text=%q;
  out->len=strlen(text);out->ptr=malloc(out->len);memcpy(out->ptr,text,out->len);return 0;
}
static void release(void *ptr,size_t len) {(void)len;free(ptr);}
static void stop(void) {}
int cliproxy_plugin_init(const host_api *host,plugin_api *out) {(void)host;out->abi_version=1;out->call=call;out->free_buffer=release;out->shutdown=stop;return 0;}
`, marker, registration)
	if err := os.WriteFile(source, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("gcc", "-shared", "-fPIC", "-o", library, source).CombinedOutput(); err != nil {
		t.Fatalf("synthetic gcc: %v %s", err, output)
	}
	data, err := os.ReadFile(library)
	if err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"id":"sample","version":%q,"platform":"linux/amd64","license_required":false,"files":{"sample.so":"%x"}}`, version, sha256.Sum256(data))
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range map[string][]byte{"PLUGIN.json": []byte(manifest), "sample.so": data, "install.sh": []byte("exit 99\n")} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func (f *updateRouterFixture) importAndWait(t *testing.T, archive []byte) updatestate.Operation {
	t.Helper()
	preview := f.request(t, "POST", "/v8/management/plugins/imports", "local-test-key", archive, 201)
	id := ojson.GetBytes(preview, "id").String()
	if ojson.GetBytes(preview, "compatibility").String() != "unverified" {
		t.Fatalf("preview=%s", preview)
	}
	f.request(t, "POST", "/v8/management/plugins/imports/"+id+"/install", "local-test-key", []byte(`{"confirm":true}`), 409)
	body := f.request(t, "POST", "/v8/management/plugins/imports/"+id+"/install", "local-test-key", []byte(`{"confirm":true,"confirm_unverified":true}`), 202)
	opID := ojson.GetBytes(body, "id").String()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body = f.request(t, "GET", "/v8/management/updates/operations/"+opID, "local-test-key", nil, 200)
		if updatestate.Terminal(ojson.GetBytes(body, "status").String()) || ojson.GetBytes(body, "status").String() == "recovery_required" {
			op, err := f.store.Get(opID)
			if err != nil {
				t.Fatal(err)
			}
			return op
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("operation did not finish: %s", body)
	return updatestate.Operation{}
}

func TestV8UpdateRouterAuthenticationAndSharedGate(t *testing.T) {
	for _, auth := range []string{"local", "environment", "hash"} {
		t.Run(auth, func(t *testing.T) {
			f := newUpdateRouterFixture(t, true, auth)
			key := map[string]string{"local": "local-test-key", "environment": "environment-test-key", "hash": "hash-test-key"}[auth]
			f.request(t, "POST", "/v8/management/plugins/imports", "", nil, 401)
			lease, err := f.store.Begin(updatestate.Operation{ID: "core-test-owner", Type: "core", Status: "installing"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lease.Finish(func(op *updatestate.Operation) { op.Status = "failed" }) }()
			for _, route := range []struct{ method, path string }{
				{"POST", "/v8/management/plugins/store/sample/install"},
				{"PUT", "/v8/management/config/plugins/configs/sample/enabled"},
				{"PUT", "/v8/management/config/plugins/configs/imports-tools"},
				{"DELETE", "/v8/management/plugins/sample"},
				{"PUT", "/v8/management/config"},
			} {
				body := f.request(t, route.method, route.path, key, []byte(`{"value":true}`), 409)
				if !strings.Contains(string(body), "core-test-owner") {
					t.Fatalf("missing lock owner: %s", body)
				}
			}
		})
	}
}

func TestV8UpdateRouterLoadsSyntheticLibraryAndRejectsActiveOverwrite(t *testing.T) {
	f := newUpdateRouterFixture(t, true, "local")
	op := f.importAndWait(t, syntheticUpdateZIP(t, "1.0.0", "first", 10))
	if op.Status != "completed" || !op.Registered || op.ActiveVersion != "1.0.0" {
		t.Fatalf("install=%+v", op)
	}
	version, path, registered := f.host.ActivePluginIdentity("sample")
	if !registered || version != "1.0.0" {
		t.Fatalf("active %s %s %t", version, path, registered)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	op = f.importAndWait(t, syntheticUpdateZIP(t, "1.0.0", "different-bytes", 10))
	if op.Status != "failed" || op.Stage != "restart_required" {
		t.Fatalf("overwrite=%+v", op)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("active library was overwritten")
	}
	op = f.importAndWait(t, syntheticUpdateZIP(t, "2.0.0", "future-schema", 99))
	if op.Status != "failed" || !op.Registered || op.ActiveVersion != "1.0.0" {
		t.Fatalf("rollback=%+v", op)
	}
	version, activePath, registered := f.host.ActivePluginIdentity("sample")
	if !registered || version != "1.0.0" || activePath != path {
		t.Fatalf("restored %s %s %t", version, activePath, registered)
	}
	latest := f.request(t, "GET", "/v8/management/updates/operations/latest?type=plugin", "local-test-key", nil, 200)
	if ojson.GetBytes(latest, "operation.id").String() != op.ID {
		t.Fatalf("latest=%s", latest)
	}
}

func TestV8UpdateRouterPreservesDisabledConfiguration(t *testing.T) {
	f := newUpdateRouterFixture(t, false, "local")
	op := f.importAndWait(t, syntheticUpdateZIP(t, "1.0.0", "disabled", 10))
	if op.Status != "completed_disabled" || op.Registered {
		t.Fatalf("disabled=%+v", op)
	}
	cfg, err := config.LoadConfig(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	item := cfg.Plugins.Configs["sample"]
	if item.Enabled == nil || *item.Enabled {
		t.Fatal("disabled configuration changed")
	}
	f.host.ApplyConfig(context.Background(), cfg)
	if _, _, registered := f.host.ActivePluginIdentity("sample"); registered {
		t.Fatal("disabled plugin registered")
	}
}

func TestV8CoreRouterUnixHostIndependentWorkerAndReconnect(t *testing.T) {
	script := os.Getenv("MX_UPDATE_FIXTURE_SCRIPT")
	if script == "" {
		t.Skip("set MX_UPDATE_FIXTURE_SCRIPT to the offline joint fixture")
	}
	ready := filepath.Join(t.TempDir(), "ready.json")
	logPath := filepath.Join(t.TempDir(), "host.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command("python3", script, "serve", ready)
	process.Stdout, process.Stderr = logFile, logFile
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait(); _ = logFile.Close() })
	deadline := time.Now().Add(10 * time.Second)
	var info []byte
	for time.Now().Before(deadline) {
		info, err = os.ReadFile(ready)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		output, _ := os.ReadFile(logPath)
		t.Fatalf("host readiness: %v %s", err, output)
	}
	root := ojson.GetBytes(info, "root").String()
	f := newUpdateRouterFixture(t, false, "local")
	t.Setenv("MX_UPDATE_SOCKET", ojson.GetBytes(info, "socket").String())
	t.Setenv("MX_UPDATE_SITE_ID", ojson.GetBytes(info, "site_id").String())
	t.Setenv("MX_UPDATE_LOCK_PATH", filepath.Join(root, ".mx-update.lock"))
	t.Setenv("MX_UPDATE_STATE_DIR", filepath.Join(root, ".mx-update-state"))
	f.request(t, "GET", "/v8/management/updates/core", "", nil, 401)
	discovery := f.request(t, "GET", "/v8/management/updates/core", "local-test-key", nil, 200)
	if ojson.GetBytes(discovery, "site_id").String() != "fixture" {
		t.Fatalf("discovery=%s", discovery)
	}
	preview := f.request(t, "POST", "/v8/management/updates/core/previews", "local-test-key", []byte(`{"release_id":"v1.2.3"}`), 200)
	candidate := ojson.GetBytes(preview, "id").String()
	url := "/v8/management/updates/core/previews/" + candidate + "/install"
	f.request(t, "POST", url, "local-test-key", []byte(`{}`), 400)
	body := f.request(t, "POST", url, "local-test-key", []byte(`{"confirm":true}`), 202)
	id := ojson.GetBytes(body, "id").String()
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body = f.request(t, "GET", "/v8/management/updates/operations/"+id, "local-test-key", nil, 200)
		if ojson.GetBytes(body, "status").String() == "installing" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if ojson.GetBytes(body, "status").String() != "installing" {
		t.Fatalf("worker did not own lock: %s", body)
	}
	f.request(t, "POST", "/v8/management/plugins/store/sample/install", "local-test-key", nil, 409)
	probe := exec.Command("python3", "-c", `import importlib.util,pathlib,sys
spec=importlib.util.spec_from_file_location('lock_probe',sys.argv[1]);u=importlib.util.module_from_spec(spec);spec.loader.exec_module(u)
site=pathlib.Path(sys.argv[2])/'site.json'
try:
 with u.site_lock(site,u.read(site)): pass
except (BlockingIOError,u.Failure): sys.exit(37)
`, filepath.Join(filepath.Dir(filepath.Dir(script)), "mx-update.py"), root)
	if err = probe.Run(); err == nil {
		t.Fatal("independent manual updater entered active worker lock")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 37 {
		t.Fatalf("cross-process lock: %v", err)
	}
	// Reconstruct the management facade while the independent host worker owns maintenance.
	h := management.NewHandler(f.server.cfg, f.configPath, nil)
	h.SetLocalPassword("local-test-key")
	f.server = &Server{cfg: f.server.cfg, engine: gin.New(), mgmt: h}
	f.server.managementRoutesEnabled.Store(true)
	f.server.registerManagementRoutes()
	reconnected := f.request(t, "GET", "/v8/management/updates/operations/"+id, "local-test-key", nil, 200)
	if ojson.GetBytes(reconnected, "id").String() != id || ojson.GetBytes(reconnected, "status").String() != "installing" {
		t.Fatalf("reconnect=%s", reconnected)
	}
	if err = os.WriteFile(filepath.Join(root, "continue"), []byte("continue"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body = f.request(t, "GET", "/v8/management/updates/operations/"+id, "local-test-key", nil, 200)
		if updatestate.Terminal(ojson.GetBytes(body, "status").String()) || ojson.GetBytes(body, "status").String() == "recovery_required" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if ojson.GetBytes(body, "status").String() != "completed" {
		output, _ := os.ReadFile(filepath.Join(root, "worker.log"))
		t.Fatalf("worker=%s log=%s", body, output)
	}
	proof, err := os.ReadFile(filepath.Join(root, "updater-proof.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !ojson.GetBytes(proof, "backup_exists").Bool() || ojson.GetBytes(proof, "stage").String() != "completed" || ojson.GetBytes(proof, "worker_pid").Int() == ojson.GetBytes(info, "pid").Int() {
		t.Fatalf("worker proof=%s", proof)
	}
	if _, err = os.Stat(filepath.Join(root, ".mx-update-state", "active.json")); !os.IsNotExist(err) {
		t.Fatalf("terminal owner not released: %v", err)
	}
	t.Log("formal v8 Router -> Unix HTTP -> independent worker -> real updater lifecycle; synthetic Docker/release boundary, stopped backup and persistent original-operation reconnect verified")
}
