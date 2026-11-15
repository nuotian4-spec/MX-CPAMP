package cliproxy

import (
	"context"
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
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

func TestKiloNativeManagerUX1DeviceJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerUX1DeviceJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("UX1 joint runtime not configured")
	}
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	selected, _ := ux1TwoProxies(t, f)
	t.Logf("public proxy DTO: %s", m4Request(t, f, "GET", kiloProxiesURL, "", 200))
	var calls, phases atomic.Int32
	f.deviceHandler = m4DeviceUpstream(t, &calls)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/scenario" || ojson.GetBytes(raw, "mode").String() != "ux1" {
			w.WriteHeader(400)
			return
		}
		phases.Add(1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("Manager UX1 joint result:\n%s", output)
	if err != nil {
		t.Fatalf("Manager UX1 joint: %v", err)
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
			t.Fatal("joint skipped")
		}
	}
	if !ran || !passed || phases.Load() != 1 || calls.Load() != 2 || selected.Load() != 2 || f.deviceConnects.Load() != 2 || f.deviceDirect.Load() != 0 || f.posts.Load() != 0 || len(f.manager.List()) != 1 {
		t.Fatalf("joint evidence ran=%v pass=%v phase=%d calls=%d selected=%d connect=%d direct=%d model=%d accounts=%d", ran, passed, phases.Load(), calls.Load(), selected.Load(), f.deviceConnects.Load(), f.deviceDirect.Load(), f.posts.Load(), len(f.manager.List()))
	}
	state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || len(state.PendingDevices) != 0 {
		t.Fatalf("pending state %v", err)
	}
	for _, a := range f.manager.List() {
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
		if len(state.Accounts[a.ID].CandidateProxyIDs) != 2 {
			t.Fatal("candidate set not persisted")
		}
	}
	ux1NoActive(t, f)
	t.Log("UX1 dual Router/Executor/Transport PASS; explicit second-proxy identity for Start/Poll, candidate persistence, repeated save identity, local quota DTO, shared ownership, active=0")
}
