package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// This bridge owns only external loopback substitutes; both management routers,
// file persistence, model registration, executor and transport are production code.
func TestKiloNativeManagerAccountsJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerAccountsJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("accounts joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-list", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = kiloAccountsJointEnvironment("", "")
	listed, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("manager target discovery failed: %v\n%s", err, listed)
	}
	found := false
	for _, line := range strings.Split(string(listed), "\n") {
		if strings.TrimSpace(line) == managerTest {
			found = true
		}
	}
	if !found {
		t.Skip("manager accounts joint test is not available; integration is not verified")
	}

	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var upstream atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if upstream.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		kiloSuccessStream(w, r)
	}, true)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var accountMu sync.Mutex
	created := map[string]bool{}
	f.postPersistGate = func(_ context.Context, a *coreauth.Auth) {
		accountMu.Lock()
		created[a.ID] = true
		accountMu.Unlock()
		if key, ok := a.Metadata["api_key"].(string); ok {
			f.expectedToken.Store(key)
		}
	}
	t.Cleanup(func() {
		accountMu.Lock()
		defer accountMu.Unlock()
		for id := range created {
			registry.GetGlobalRegistry().UnregisterClient(id)
		}
	})
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/started":
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			select {
			case <-started:
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Second):
				http.Error(w, "joint start barrier timed out", 504)
				return
			}
			_, _ = io.WriteString(w, "ok")
		case "/release":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			releaseOnce.Do(func() { close(release) })
			_, _ = io.WriteString(w, "ok")
		case "/state":
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			id := r.URL.Query().Get("auth_id")
			accountMu.Lock()
			known := created[id]
			accountMu.Unlock()
			if id != "" && !known {
				http.Error(w, "unknown synthetic auth ID", 404)
				return
			}
			_, exists := f.manager.GetByID(id)
			fileExists := false
			if known {
				info, err := os.Stat(filepath.Join(f.cfg.AuthDir, id))
				fileExists = err == nil && info.Mode().IsRegular()
			}
			active := 0
			entries, _ := f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry)
			for _, entry := range entries {
				if id == "" || entry.AuthID == id {
					active += entry.Active
				}
			}
			values := map[string]any{"auth_exists": exists, "file_exists": fileExists, "model_count": len(registry.GetGlobalRegistry().GetModelsForClient(id)), "active": active, "posts": f.posts.Load(), "connects": f.connects.Load(), "results": len(f.hook.snapshot())}
			doc := ojson.NewDocString("{}")
			for key, value := range values {
				if err := doc.Set(key, value); err != nil {
					http.Error(w, "joint state serialization failed", 500)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(doc.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer control.Close()
	command = exec.CommandContext(ctx, goBin, "test", "-count=1", "-json", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = kiloAccountsJointEnvironment(f.server.URL, control.URL)
	output, err := command.CombinedOutput()
	t.Logf("Manager accounts joint router result:\n%s", output)
	if err != nil {
		t.Fatalf("manager accounts joint test failed: %v", err)
	}
	passed := false
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		event := ojson.ParseBytes(line)
		if event.Get("Test").String() == managerTest && event.Get("Action").String() == "pass" {
			passed = true
		}
	}
	if !passed {
		t.Fatal("manager accounts joint test did not execute to PASS")
	}
	executor, ok := f.manager.Executor("kilo")
	if !ok {
		t.Fatal("actual Service never registered Kilo executor")
	}
	f.executor, ok = executor.(*native.KiloExecutor)
	if !ok {
		t.Fatalf("registered executor=%T", executor)
	}
	if f.posts.Load() < 1 || f.connects.Load() < 1 {
		t.Fatalf("actual upstream chain missing: POST=%d CONNECT=%d", f.posts.Load(), f.connects.Load())
	}
	f.assertReleased(t, int(f.posts.Load()))
	if len(f.manager.List()) != 0 {
		t.Fatal("joint CRUD left runtime accounts after final deletion")
	}
	accountMu.Lock()
	defer accountMu.Unlock()
	if len(created) == 0 {
		t.Fatal("joint test never persisted an account")
	}
	for id := range created {
		if _, err := os.Stat(filepath.Join(f.cfg.AuthDir, id)); !os.IsNotExist(err) {
			t.Fatalf("joint deletion left credential file for %s", id)
		}
		if len(registry.GetGlobalRegistry().GetModelsForClient(id)) != 0 {
			t.Fatalf("joint deletion left model registration for %s", id)
		}
	}
	t.Logf("dual-router account lifecycle: created=%d POST=%d CONNECT=%d results=%d; final runtime/files/models/leases clear", len(created), f.posts.Load(), f.connects.Load(), len(f.hook.snapshot()))
}

func kiloAccountsJointEnvironment(cpaURL, controlURL string) []string {
	values := map[string]string{"KILO_JOINT_CPA_URL": cpaURL, "KILO_JOINT_CONTROL_URL": controlURL, "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off", "GOWORK": "off", "GOFLAGS": "-mod=readonly"}
	environment := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		replaced := false
		for name := range values {
			if strings.EqualFold(key, name) {
				replaced = true
				break
			}
		}
		if !replaced {
			environment = append(environment, entry)
		}
	}
	for key, value := range values {
		environment = append(environment, fmt.Sprintf("%s=%s", key, value))
	}
	return environment
}
