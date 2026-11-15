package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloNativeManagerDeviceJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerDeviceJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("device joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-list", "^"+managerTest+"$", "./internal/http/router")
	command.Dir, command.Env = managerDir, kiloAccountsJointEnvironment("", "")
	listed, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("manager target discovery: %v\n%s", err, listed)
	}
	found := false
	for _, line := range strings.Split(string(listed), "\n") {
		found = found || strings.TrimSpace(line) == managerTest
	}
	if !found {
		t.Fatal("Manager device joint target missing")
	}
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowDirect = true
	proxyURL := f.cfg.Kilo.Proxies[0].URL
	f.cfg.Kilo.Proxies = nil
	var calls atomic.Int32
	f.deviceHandler = m4DeviceUpstream(t, &calls)
	var phases atomic.Int32
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/scenario" {
			w.WriteHeader(404)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mode := ojson.GetBytes(raw, "mode").String()
		switch mode {
		case "direct":
			if phases.Add(1) != 1 {
				t.Error("unexpected direct phase")
			}
			m4Request(t, f, "GET", kiloProxiesURL, "", 200)
		case "pool":
			if phases.Add(1) != 2 || len(f.manager.List()) != 1 || calls.Load() != 2 {
				t.Error("direct stage did not complete before pool")
			}
			doc := ojson.NewDocString(`{"name":"joint shared"}`)
			state := m4Request(t, f, "GET", kiloProxiesURL, "", 200)
			_ = doc.Set("expected_revision", ojson.GetBytes(state, "revision").Uint())
			_ = doc.Set("url", proxyURL)
			data := m4Request(t, f, "POST", kiloProxiesURL, doc.String(), 201)
			proxyID := ojson.GetBytes(data, "proxies.0.id").String()
			m4Request(t, f, "POST", kiloProxiesURL+"/"+proxyID+"/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(data, "revision").Uint()), 200)
		default:
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	command = exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager device joint result:\n%s", output)
	if err != nil {
		t.Fatalf("manager device joint failed: %v", err)
	}
	passed, ran := false, false
	for _, line := range strings.Split(string(output), "\n") {
		if !ojson.Valid(line) || ojson.Get(line, "Test").String() != managerTest {
			continue
		}
		switch ojson.Get(line, "Action").String() {
		case "run":
			ran = true
		case "pass":
			passed = true
		case "skip":
			t.Fatal("configured Manager joint target skipped")
		}
	}
	if !ran || !passed {
		t.Fatal("Manager child did not run and pass")
	}
	if phases.Load() != 2 || calls.Load() != 4 || f.deviceDirect.Load() != 2 || f.deviceConnects.Load() != 2 || f.posts.Load() != 0 || len(f.manager.List()) != 2 {
		t.Fatalf("counts phase=%d codes=%d direct=%d CONNECT=%d model=%d accounts=%d", phases.Load(), calls.Load(), f.deviceDirect.Load(), f.deviceConnects.Load(), f.posts.Load(), len(f.manager.List()))
	}
	machines := map[string]bool{}
	for _, a := range f.manager.List() {
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
		raw, readErr := os.ReadFile(filepath.Join(f.cfg.AuthDir, a.ID))
		machine := coreauth.KiloMachineID(a)
		if readErr != nil || machine == "" || machines[machine] || ojson.GetBytes(raw, "machine_id").String() != machine || ojson.GetBytes(raw, "api_key").String() != kiloDeviceToken || len(registry.GetGlobalRegistry().GetModelsForClient(a.ID)) == 0 {
			t.Fatal("durable credential/runtime publication mismatch")
		}
		machines[machine] = true
	}
	state, managed, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || !managed || len(state.Accounts) != 2 || len(state.PendingDevices) != 0 || len(state.Proxies) != 1 || state.Proxies[0].FirstUsed != 1 {
		t.Fatal("account policy persistence or first-used mismatch")
	}
	for _, p := range f.executor.KiloProxySnapshot().Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatal("joint bridge retained lease")
		}
	}
	m2Reload(t, f)
	if len(f.manager.List()) != 2 {
		t.Fatal("joint reload did not restore two credentials")
	}
	for _, a := range f.manager.List() {
		if !machines[coreauth.KiloMachineID(a)] {
			t.Fatal("reload machine changed")
		}
	}
	t.Logf("native Manager/Core routers PASS: device=4 direct=2 CONNECT=2 model=0 health=%d saved=2 UUIDs=2 first_used=1 device_pins_durable=0 pool_active=0", f.probes.Load())
}
