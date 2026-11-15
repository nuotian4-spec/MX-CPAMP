package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net"
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
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
)

func TestKiloNativeManagerIPAdmissionJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerIPAdmissionJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("IP admission joint runtime not configured")
	}
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	proxyURL := f.cfg.Kilo.Proxies[0].URL
	f.cfg.Kilo.Proxies = nil
	f.manager.SetConfig(f.cfg)
	f.executor = native.NewKiloExecutor(f.cfg)
	f.manager.RegisterExecutor(f.executor)
	var ip atomic.Value
	ip.Store("203.0.113.10")
	var concurrentProbes, failedConnects atomic.Int32
	bothProbes := make(chan struct{})
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		observed := ip.Load().(string)
		if observed == "203.0.113.11" {
			count := concurrentProbes.Add(1)
			if count == 2 {
				close(bothProbes)
			}
			if count <= 2 {
				select {
				case <-bothProbes:
				case <-r.Context().Done():
					return
				}
			}
		}
		_, _ = io.WriteString(w, observed)
	}
	pending := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "api.ipify.org:443" {
			t.Errorf("unexpected pending proxy request: %s %s", r.Method, r.Host)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		failedConnects.Add(1)
		http.Error(w, "synthetic observation failure", http.StatusBadGateway)
	}))
	defer pending.Close()
	transport := http.DefaultTransport.(*http.Transport)
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == pending.Listener.Addr().String() {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		return dial(ctx, network, address)
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /fixture":
			_, _ = fmt.Fprintf(w, `{"proxy_url":%q,"pending_proxy_url":%q}`, proxyURL, pending.URL)
		case "POST /probe-ip":
			body, err := io.ReadAll(r.Body)
			value := ojson.GetBytes(body, "ip").String()
			if err != nil || (value != "203.0.113.10" && value != "203.0.113.11") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			ip.Store(value)
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-json", "-count=1", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(kiloAccountsJointEnvironment(f.server.URL, control.URL), "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey, "KILO_JOINT_PROXY_URL="+proxyURL)
	output, err := command.CombinedOutput()
	t.Logf("Manager IP admission joint:\n%s", output)
	if err != nil {
		t.Fatalf("Manager IP admission joint: %v", err)
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
			t.Fatal("IP admission joint skipped")
		}
	}
	state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || !ran || !passed || state.Revision != 3 || len(state.Proxies) != 3 || len(state.IPHistory) != 2 || len(state.Accounts) != 0 || len(f.manager.List()) != 0 || f.probes.Load() != 7 || concurrentProbes.Load() != 3 || failedConnects.Load() != 2 || f.posts.Load() != 0 {
		t.Fatalf("joint evidence: err=%v run=%v pass=%v revision=%d proxies=%d history=%d accounts=%d probes=%d concurrent=%d failed=%d inference=%d", err, ran, passed, state.Revision, len(state.Proxies), len(state.IPHistory), len(state.Accounts), f.probes.Load(), concurrentProbes.Load(), failedConnects.Load(), f.posts.Load())
	}
	t.Log("dual Router admission: actual local CONNECT/TLS observation; preview read-only, one commit per create, pending persisted without registration, both concurrent probes entered before commit; no account or inference calls")
}
