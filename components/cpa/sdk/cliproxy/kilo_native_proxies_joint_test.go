package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// Both management routers and the inference executor are production code. Only
// the external CONNECT proxy, TLS upstream and diagnostic control are loopback fixtures.
func TestKiloNativeManagerProxiesJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerProxiesJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("proxy joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-list", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = kiloAccountsJointEnvironment("", "")
	listed, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("manager proxy target discovery failed: %v\n%s", err, listed)
	}
	found := false
	for _, line := range strings.Split(string(listed), "\n") {
		found = found || strings.TrimSpace(line) == managerTest
	}
	if !found {
		t.Fatal("configured Manager input does not contain the proxy joint test")
	}

	var directPeers sync.Map
	var proxyRequests, directRequests, directDials atomic.Int32
	var observationsMu sync.Mutex
	var observedAccounts []string
	var observedModes []string
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		// The TLS server observes the actual client socket, not the saved mode.
		// Proxy forwarding opens its own socket outside the runtime dial function.
		_, direct := directPeers.Load(r.RemoteAddr)
		mode := "proxy"
		if direct {
			mode = "direct"
			directRequests.Add(1)
		} else {
			proxyRequests.Add(1)
		}
		observationsMu.Lock()
		observedAccounts = append(observedAccounts, r.Header.Get("X-KILOCODE-MACHINEID"))
		observedModes = append(observedModes, mode)
		sequence := len(observedAccounts)
		observationsMu.Unlock()
		terminal := ojson.NewDocString(kiloTerminal)
		if err := terminal.Set("response.id", fmt.Sprintf("resp-proxy-joint-%d", sequence)); err != nil {
			t.Error(err)
			http.Error(w, "synthetic response serialization failed", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", terminal.String())
	}, true)
	f.allowDirect = true
	transport := http.DefaultTransport.(*http.Transport)
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err == nil && address == "ai-gateway.kilo.ai:443" {
			directPeers.Store(conn.LocalAddr().String(), true)
			directDials.Add(1)
		}
		return conn, err
	}
	accountID := m2Create(t, f, `,"name":"proxy joint account"`)
	store := kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}
	var poolObserved atomic.Bool
	var poolRevision atomic.Uint64
	var directObserved atomic.Bool
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		values := map[string]any{}
		switch r.URL.Path {
		case "/fixture":
			values["account_id"], values["proxy_url"] = accountID, f.cfg.Kilo.Proxies[0].URL
		case "/state":
			proxyCount, directCount := proxyRequests.Load(), directRequests.Load()
			accountActive, proxyActive := 0, 0
			for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
				accountActive += a.Active
			}
			for _, p := range f.executor.KiloProxySnapshot().Proxies {
				proxyActive += p.Active
			}
			if proxyCount > 0 {
				state, managed, err := store.Load(kilo.ProxyState{})
				if err != nil || !managed {
					t.Errorf("persisted joint state missing: managed=%t err=%v", managed, err)
					http.Error(w, "joint state unavailable", 500)
					return
				}
				policy := state.Accounts[accountID]
				if directCount == 0 {
					owned := false
					for _, p := range state.Proxies {
						owned = owned || (p.ID == policy.PrimaryProxyID && p.ID != "local-proxy" && p.AccountID == accountID && p.FirstUsed > 0)
					}
					if policy.Mode != "pool" || !owned || state.Limit != 2 || f.connects.Load() != 1 || directDials.Load() != 0 {
						t.Errorf("pool state/transport mismatch: policy=%+v owned=%t limit=%d CONNECT=%d directDial=%d", policy, owned, state.Limit, f.connects.Load(), directDials.Load())
						http.Error(w, "joint pool mismatch", 500)
						return
					}
					poolRevision.Store(state.Revision)
					poolObserved.Store(true)
				} else {
					directObserved.Store(policy.Mode == "direct" && policy.PrimaryProxyID == "")
				}
			}
			values = map[string]any{"proxy_requests": proxyCount, "direct_requests": directCount,
				"upstream_posts": f.posts.Load(), "proxy_connects": f.connects.Load(), "direct_dials": directDials.Load(),
				"results": len(f.hook.snapshot()), "account_active": accountActive, "proxy_active": proxyActive}
		default:
			http.NotFound(w, r)
			return
		}
		doc := ojson.NewDocString("{}")
		for key, value := range values {
			if err := doc.Set(key, value); err != nil {
				http.Error(w, "joint state serialization failed", 500)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(doc.Bytes())
	}))
	defer control.Close()
	command = exec.CommandContext(ctx, goBin, "test", "-count=1", "-json", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = kiloAccountsJointEnvironment(f.server.URL, control.URL)
	output, err := command.CombinedOutput()
	t.Logf("Manager proxies joint router result:\n%s", output)
	if err != nil {
		t.Fatalf("manager proxies joint test failed: %v", err)
	}
	passed := false
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		event := ojson.ParseBytes(line)
		if event.Get("Test").String() == managerTest {
			if event.Get("Action").String() == "skip" {
				t.Fatal("manager proxies joint test skipped")
			}
			passed = passed || event.Get("Action").String() == "pass"
		}
	}
	if !passed {
		t.Fatal("manager proxies joint test did not execute to PASS")
	}
	f.assertReleased(t, 2)
	if proxyRequests.Load() != 1 || directRequests.Load() != 1 || f.posts.Load() != 2 || f.connects.Load() != 1 || directDials.Load() != 1 {
		t.Fatalf("wire evidence: proxy=%d direct=%d POST=%d CONNECT=%d directDial=%d", proxyRequests.Load(), directRequests.Load(), f.posts.Load(), f.connects.Load(), directDials.Load())
	}
	state, managed, err := store.Load(kilo.ProxyState{})
	if err != nil || !managed || !poolObserved.Load() || !directObserved.Load() || state.Revision != poolRevision.Load()+1 || state.Limit != 2 {
		t.Fatalf("durable transition missing: managed=%t pool=%t direct=%t revision=%d previous=%d limit=%d err=%v", managed, poolObserved.Load(), directObserved.Load(), state.Revision, poolRevision.Load(), state.Limit, err)
	}
	if len(state.Proxies) != 2 || state.Accounts[accountID].Mode != "direct" || state.Accounts[accountID].PrimaryProxyID != "" {
		t.Fatalf("final durable state mismatch: proxies=%d account=%+v", len(state.Proxies), state.Accounts[accountID])
	}
	for _, p := range state.Proxies {
		if p.AccountID != "" || p.URL != f.cfg.Kilo.Proxies[0].URL {
			t.Fatal("direct did not release exclusive ownership or changed the stored URL")
		}
	}
	view, err := f.executor.KiloProxyState()
	if err != nil || view.Revision != state.Revision || view.RuntimeRevision != state.Revision {
		t.Fatalf("durable/runtime revision mismatch: view=%+v err=%v", view, err)
	}
	observationsMu.Lock()
	defer observationsMu.Unlock()
	account, _ := f.manager.GetByID(accountID)
	machine := coreauth.KiloMachineID(account)
	if machine == "" || len(observedAccounts) != 2 || observedAccounts[0] != machine || observedAccounts[1] != machine || observedModes[0] != "proxy" || observedModes[1] != "direct" {
		t.Fatalf("actual identity/routes: accounts=%v routes=%v", observedAccounts, observedModes)
	}
	for _, result := range f.hook.snapshot() {
		if result.AuthID != accountID || !result.Success {
			t.Fatalf("result identity/success mismatch: %+v", result)
		}
	}
	t.Logf("dual-router proxy lifecycle: POST=2 proxy=1 direct=1 CONNECT=1 directDial=1 results=2; same AuthID; durable/runtime revision=%d; ownership and both leases released", state.Revision)
}
