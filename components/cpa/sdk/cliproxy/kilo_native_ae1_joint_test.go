package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloNativeManagerAE1JointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerAE1JointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("AE1 joint runtime not configured")
	}
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "203.0.113.10") }
	m5Codex(t, f, "source.json", "outer", "synthetic-outer", false, false)
	var used, queries atomic.Int32
	used.Store(100)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		queries.Add(1)
		_, _ = fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":%d,"reset_after_seconds":300},"secondary_window":{"limit_window_seconds":604800,"used_percent":1,"reset_after_seconds":600}}}`, used.Load())
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/fixture" {
			_, _ = io.WriteString(w, `{"proxy_id":"local-proxy","fixed_ip":"203.0.113.10","source_id":"source.json","source_account_id":"outer"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/scenario" {
			w.WriteHeader(404)
			return
		}
		switch ojson.GetBytes(body, "mode").String() {
		case "pause":
			used.Store(100)
		case "recover":
			used.Store(1)
		case "sync":
			f.manager.SyncKiloAutoEgress(time.Now())
		default:
			w.WriteHeader(400)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager AE1 joint:\n%s", output)
	if err != nil {
		t.Fatalf("Manager AE1 joint: %v", err)
	}
	ran, passed := false, false
	for _, line := range strings.Split(string(output), "\n") {
		if ojson.Get(line, "Test").String() != managerTest {
			continue
		}
		switch ojson.Get(line, "Action").String() {
		case "run":
			ran = true
		case "pass":
			passed = true
		case "skip":
			t.Fatal("AE1 joint skipped")
		}
	}
	m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	state, err := f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	accounts := 0
	for _, a := range f.manager.List() {
		if a.Provider == "kilo" {
			accounts++
		}
	}
	if !ran || !passed || queries.Load() != 2 || f.usageConnects.Load() != 2 || f.usageDirect.Load() != 0 || f.posts.Load() != 0 || accounts != 2 || len(state.IPGroups) != 1 || state.IPGroups[0].Owner != "" || state.IPGroups[0].UniqueAccounts != 1 || !state.IPGroups[0].Retired {
		t.Fatalf("AE1 joint evidence run=%v pass=%v queries=%d proxy=%d direct=%d posts=%d accounts=%d state=%+v", ran, passed, queries.Load(), f.usageConnects.Load(), f.usageDirect.Load(), f.posts.Load(), accounts, state)
	}
}
