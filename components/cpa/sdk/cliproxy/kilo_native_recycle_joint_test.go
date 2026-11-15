package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
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
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// Production management routers, persistence, admission and transports remain
// intact. The control server changes only the external IP fixture response.
func TestKiloNativeManagerRecycleJointBridge(t *testing.T) {
	const managerTest = "TestKiloManagerRecycleJointRuntime"
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("recycle joint runtime not configured; integration is not verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-list", "^"+managerTest+"$", "./internal/http/router")
	command.Dir, command.Env = managerDir, kiloAccountsJointEnvironment("", "")
	listed, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("manager recycle target discovery failed: %v\n%s", err, listed)
	}
	found := false
	for _, line := range strings.Split(string(listed), "\n") {
		found = found || strings.TrimSpace(line) == managerTest
	}
	if !found {
		t.Fatal("configured Manager input does not contain the recycle joint test")
	}

	var healthy atomic.Bool
	healthy.Store(true)
	var directPeers sync.Map
	var proxyRequests, directRequests, directDials atomic.Int32
	var observationsMu sync.Mutex
	var machines []string
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if _, direct := directPeers.Load(r.RemoteAddr); direct {
			directRequests.Add(1)
		} else {
			proxyRequests.Add(1)
		}
		observationsMu.Lock()
		machines = append(machines, r.Header.Get("X-KILOCODE-MACHINEID"))
		sequence := len(machines)
		observationsMu.Unlock()
		doc := ojson.NewDocString(kiloTerminal)
		if err := doc.Set("response.id", fmt.Sprintf("resp-recycle-joint-%d", sequence)); err != nil {
			t.Error(err)
			http.Error(w, "fixture serialization failed", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-KILOCODE-MACHINEID") != "" {
			t.Error("health request contains account headers")
		}
		if !healthy.Load() {
			http.Error(w, "synthetic IP target unavailable", 502)
			return
		}
		_, _ = io.WriteString(w, "127.0.0.2")
	}
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
	proxyURL := f.cfg.Kilo.Proxies[0].URL
	f.cfg.Kilo.Proxies = nil
	accountID := m2Create(t, f, `,"name":"recycle joint account"`)
	store := kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}
	stateFile := filepath.Join(store.Dir, kilo.StateFileName)
	var proofMu sync.Mutex
	var proxyID string
	var firstUsed uint64
	var archiveBytes []byte
	var archivedRevision, restoredRevision uint64
	var archiveProbes int32
	var poolObserved, failedRestoreObserved, restoredObserved, disabledObserved bool
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proofMu.Lock()
		defer proofMu.Unlock()
		fail := func(message string) { t.Error(message); http.Error(w, "joint evidence mismatch", 500) }
		values := map[string]any{}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/fixture":
			values["account_id"], values["proxy_url"] = accountID, proxyURL
		case r.Method == http.MethodPost && r.URL.Path == "/health-mode":
			raw, err := io.ReadAll(r.Body)
			if err != nil || !ojson.ValidBytes(raw) || !ojson.GetBytes(raw, "healthy").IsBool() {
				w.WriteHeader(400)
				return
			}
			next := ojson.GetBytes(raw, "healthy").Bool()
			if !next {
				state, managed, err := store.Load(kilo.ProxyState{})
				if err != nil || !managed || len(state.Proxies) != 0 || len(state.Recycle) != 1 || state.Recycle[0].Proxy.ID != proxyID || state.Recycle[0].Proxy.AccountID != accountID || state.Recycle[0].Proxy.FirstUsed != firstUsed || state.Accounts[accountID].PrimaryProxyID != "" {
					fail("archive did not preserve exact durable identity/first-use or clear primary")
					return
				}
				archiveBytes, err = os.ReadFile(stateFile)
				if err != nil {
					fail("archive file missing")
					return
				}
				archivedRevision, archiveProbes = state.Revision, f.probes.Load()
			} else if archiveBytes != nil {
				current, err := os.ReadFile(stateFile)
				if err != nil || !bytes.Equal(current, archiveBytes) || f.probes.Load() != archiveProbes+1 {
					fail("failed restore changed disk state or skipped fresh probe")
					return
				}
				failedRestoreObserved = true
			}
			healthy.Store(next)
			values["healthy"] = next
		case r.Method == http.MethodGet && r.URL.Path == "/state":
			count := proxyRequests.Load()
			state, managed, err := store.Load(kilo.ProxyState{})
			if err != nil || !managed {
				fail("expected managed durable proxy state")
				return
			}
			view, err := f.executor.KiloProxyState()
			if err != nil || view.Revision != state.Revision || view.RuntimeRevision != state.Revision {
				fail("durable/runtime revision mismatch")
				return
			}
			if count == 1 && len(state.Proxies) == 1 {
				p := state.Proxies[0]
				if p.AccountID == accountID {
					if p.FirstUsed == 0 || p.URL != proxyURL || state.Accounts[accountID].PrimaryProxyID != p.ID {
						fail("pre-archive production allocation was not persisted")
						return
					}
					poolObserved, proxyID, firstUsed = true, p.ID, p.FirstUsed
				} else if failedRestoreObserved {
					if p.ID != proxyID || p.FirstUsed != firstUsed || p.Disabled || p.URL != proxyURL || p.AccountID != "" || state.Accounts[accountID].PrimaryProxyID != "" || !view.Proxies[0].Healthy || state.Revision != archivedRevision+1 {
						fail("restore revived ownership/primary or changed durable identity")
						return
					}
					restoredObserved, restoredRevision = true, state.Revision
				}
			}
			if count == 2 && len(state.Proxies) == 1 && state.Proxies[0].Disabled && f.probes.Load() == 4 {
				if view.Proxies[0].Healthy {
					fail("disabled proxy became effectively healthy")
					return
				}
				disabledObserved = true
			}
			accountActive, proxyActive := 0, 0
			for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
				accountActive += a.Active
			}
			for _, p := range f.executor.KiloProxySnapshot().Proxies {
				proxyActive += p.Active
			}
			values = map[string]any{"health_requests": f.probes.Load(), "proxy_requests": count, "direct_requests": directRequests.Load(), "upstream_posts": f.posts.Load(), "proxy_connects": f.connects.Load(), "direct_dials": directDials.Load(), "results": len(f.hook.snapshot()), "account_active": accountActive, "proxy_active": proxyActive, "revision": state.Revision, "runtime_revision": view.RuntimeRevision}
		default:
			http.NotFound(w, r)
			return
		}
		doc := ojson.NewDocString("{}")
		for key, value := range values {
			if err := doc.Set(key, value); err != nil {
				fail("control serialization failed")
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(doc.Bytes())
	}))
	defer control.Close()
	command = exec.CommandContext(ctx, goBin, "test", "-count=1", "-json", "-run", "^"+managerTest+"$", "./internal/http/router")
	command.Dir, command.Env = managerDir, kiloAccountsJointEnvironment(f.server.URL, control.URL)
	output, err := command.CombinedOutput()
	t.Logf("Manager recycle joint router result:\n%s", output)
	if err != nil {
		t.Fatalf("manager recycle joint test failed: %v", err)
	}
	ran, passed := false, false
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		event := ojson.ParseBytes(line)
		if event.Get("Test").String() != managerTest {
			continue
		}
		action := event.Get("Action").String()
		if action == "skip" {
			t.Fatal("manager recycle joint test skipped")
		}
		ran, passed = ran || action == "run", passed || action == "pass"
	}
	if !ran || !passed {
		t.Fatal("manager recycle joint test did not execute to PASS")
	}
	f.assertReleased(t, 2)
	if f.posts.Load() != 2 || proxyRequests.Load() != 2 || directRequests.Load() != 0 || f.connects.Load() != 2 || directDials.Load() != 0 || f.probes.Load() != 4 {
		t.Fatalf("wire counts: POST=%d proxy=%d direct=%d CONNECT=%d directDial=%d health=%d", f.posts.Load(), proxyRequests.Load(), directRequests.Load(), f.connects.Load(), directDials.Load(), f.probes.Load())
	}
	state, managed, err := store.Load(kilo.ProxyState{})
	if err != nil || !managed || len(state.Proxies) != 1 || len(state.Recycle) != 0 {
		t.Fatalf("final durable state=%+v managed=%t err=%v", state, managed, err)
	}
	proofMu.Lock()
	defer proofMu.Unlock()
	if !poolObserved || !failedRestoreObserved || !restoredObserved || !disabledObserved {
		t.Fatalf("missing proof: pool=%t failed=%t restored=%t disabled=%t", poolObserved, failedRestoreObserved, restoredObserved, disabledObserved)
	}
	p := state.Proxies[0]
	policy := state.Accounts[accountID]
	if p.ID != proxyID || p.URL != proxyURL || p.FirstUsed != firstUsed || p.Disabled || p.AccountID != "" || policy.Mode != "pool" || policy.PrimaryProxyID != proxyID || state.Revision != restoredRevision+6 {
		t.Fatalf("final durable identity/policy=%+v %+v revision=%d restored=%d", p, policy, state.Revision, restoredRevision)
	}
	view, err := f.executor.KiloProxyState()
	if err != nil || view.Revision != state.Revision || view.RuntimeRevision != state.Revision || !view.Proxies[0].Healthy {
		t.Fatalf("final published state=%+v err=%v", view, err)
	}
	for _, result := range f.hook.snapshot() {
		if result.AuthID != accountID || !result.Success {
			t.Fatalf("result identity/success=%+v", result)
		}
	}
	observationsMu.Lock()
	defer observationsMu.Unlock()
	if len(machines) != 2 || machines[0] == "" || machines[0] != machines[1] {
		t.Fatalf("two production requests changed device identity: %v", machines)
	}
	t.Logf("dual-router recycle lifecycle: POST=2 proxy=2 direct=0 CONNECT=2 directDial=0 health=4 results=2; same AuthID/device; first_used=%d preserved; archived revision=%d restored=%d final durable/runtime=%d; clear only recycle; both leases released", firstUsed, archivedRevision, restoredRevision, state.Revision)
}
