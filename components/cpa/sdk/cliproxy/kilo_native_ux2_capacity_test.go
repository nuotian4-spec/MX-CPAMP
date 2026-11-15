package cliproxy

import (
	"context"
	"fmt"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Each fixture IP is an explicit synthetic identity observed through real CONNECT/TLS.
func ux2NativeIPs(t *testing.T, f *kiloNativeFixture, n int, upstream func(int, http.ResponseWriter, *http.Request)) []string {
	t.Helper()
	old := http.DefaultTransport
	transport := old.(*http.Transport).Clone()
	allowed := map[string]bool{}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowed[addr] {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		return old.(*http.Transport).DialContext(ctx, network, addr)
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = old; transport.CloseIdleConnections() })
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	revision := ojson.GetBytes(state, "revision").Uint()
	state = m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", fmt.Sprintf(`{"expected_revision":%d,"enabled":false}`, revision), 200)
	ids := []string{}
	for i := 0; i < n; i++ {
		idx := i
		ip := fmt.Sprintf("203.0.113.%d", i+1)
		target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host == "api.kilo.ai" && r.URL.Path == "/api/profile" && f.profileHandler != nil {
				f.profileHandler(w, r)
				return
			}
			if r.Host == "api.kilo.ai" && f.deviceHandler != nil {
				f.deviceHandler(w, r)
				return
			}
			if r.Host == "api.ipify.org" {
				_, _ = io.WriteString(w, ip)
				return
			}
			if r.Host != "ai-gateway.kilo.ai" || r.Method != "POST" || (r.URL.Path != "/api/v1/responses" && !(f.allowAutoFree && r.URL.Path == "/api/v1/chat/completions")) {
				t.Errorf("unexpected route %s %s%s", r.Method, r.Host, r.URL.Path)
				http.Error(w, "route", 500)
				return
			}
			if r.Header.Get("Authorization") != "Bearer synthetic-kilo-token" {
				t.Error("credential not injected")
			}
			upstream(idx, w, r)
		}))
		t.Cleanup(target.Close)
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "CONNECT" || (r.Host != "api.ipify.org:443" && r.Host != "ai-gateway.kilo.ai:443" && r.Host != "api.kilo.ai:443") {
				http.Error(w, "target", 400)
				return
			}
			remote, err := net.Dial("tcp", target.Listener.Addr().String())
			if err != nil {
				http.Error(w, "local target", 502)
				return
			}
			client, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				remote.Close()
				return
			}
			_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			_ = rw.Flush()
			go func() { _, _ = io.Copy(remote, rw); _ = remote.Close(); _ = client.Close() }()
			_, _ = io.Copy(client, remote)
			_ = remote.Close()
			_ = client.Close()
		}))
		t.Cleanup(proxy.Close)
		allowed[proxy.Listener.Addr().String()] = true
		revision = ojson.GetBytes(state, "revision").Uint()
		state = m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":%d,"url":%q,"name":%q}`, revision, proxy.URL, ip), 201)
		entries := ojson.GetBytes(state, "proxies").Array()
		id := entries[len(entries)-1].Get("id").String()
		ids = append(ids, id)
		revision = ojson.GetBytes(state, "revision").Uint()
		m2Request(t, f, "POST", kiloProxiesURL+"/"+id+"/recheck", fmt.Sprintf(`{"expected_revision":%d}`, revision), 200)
		state = m2Request(t, f, "PATCH", kiloProxiesURL+"/"+id, fmt.Sprintf(`{"expected_revision":%d,"fixed_exit_ip":%q}`, revision, ip), 200)
	}
	revision = ojson.GetBytes(state, "revision").Uint()
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"auto_assignment_enabled":true,"max_unique_accounts":10}`, revision), 200)
	return ids
}
func ux2NativePolicy(t *testing.T, f *kiloNativeFixture, id string, maxIPs int) {
	t.Helper()
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"auto","exclusive_proxy_ids":[],"primary_proxy_id":"","capacity_mode":"ip","allocation_mode":"auto","max_ips":%d,"specified_ips":[]}`, ojson.GetBytes(state, "revision").Uint(), maxIPs), 200)
}
func ux2Wait(t *testing.T, why string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal(why)
}

type ux2Reply struct {
	status int
	body   []byte
	err    error
}

func ux2Request(ctx context.Context, f *kiloNativeFixture, session, body string) ux2Reply {
	req, err := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		return ux2Reply{err: err}
	}
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session_id", session)
	response, err := f.server.Client().Do(req)
	if err != nil {
		return ux2Reply{err: err}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	return ux2Reply{status: response.StatusCode, body: raw, err: err}
}
func TestUX2NativeAutomatic101ThroughDedicatedKeyCONNECTTLS(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	entered := make(chan int, 110)
	one := make(chan struct{}, 1)
	all := make(chan struct{})
	var closeAll sync.Once
	defer closeAll.Do(func() { close(all) })
	var posts, active, peak atomic.Int32
	var perIP [5]atomic.Int32
	ux2NativeIPs(t, f, 5, func(ip int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		count := active.Add(1)
		for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
		}
		perIP[ip].Add(1)
		entered <- int(n)
		select {
		case <-one:
		case <-all:
		case <-r.Context().Done():
			active.Add(-1)
			return
		}
		active.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("ux2-response-%d", n))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	id := m2Create(t, f, "")
	ux2NativePolicy(t, f, id, 5)
	state, err := f.executor.KiloProxyState()
	if err != nil || len(state.Accounts[0].AssignedIPs) != 1 {
		t.Fatalf("initial binding=%+v err=%v", state.Accounts, err)
	}
	replies := make(chan ux2Reply, 101)
	for i := 0; i < 101; i++ {
		go func(n int) {
			replies <- ux2Request(context.Background(), f, fmt.Sprintf("ux2-%d", n), `{"model":"gpt-6.1-sol","input":"capacity"}`)
		}(i)
	}
	for i := 0; i < 100; i++ {
		select {
		case <-entered:
		case <-time.After(15 * time.Second):
			t.Fatalf("entered %d, wanted100; posts=%d state=%+v", i, posts.Load(), f.manager.CredentialConcurrencySnapshot())
		}
	}
	ux2Wait(t, "101st did not wait", func() bool { return f.manager.CredentialConcurrencySnapshot()["pool_waiting"].(int) >= 1 })
	state, err = f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 100 || active.Load() != 100 || peak.Load() != 100 || len(state.Accounts[0].AssignedIPs) != 5 || state.Accounts[0].InferenceActive != 100 || state.Accounts[0].ConfiguredCapacity != 100 {
		t.Fatalf("plateau posts=%d active=%d peak=%d account=%+v", posts.Load(), active.Load(), peak.Load(), state.Accounts)
	}
	for i, g := range state.IPGroups {
		if g.InferenceActive != 20 || g.ManagementActive != 0 || perIP[i].Load() != 20 {
			t.Fatalf("group%d=%+v upstream=%d", i, g, perIP[i].Load())
		}
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan ux2Reply, 1)
	go func() {
		cancelled <- ux2Request(cancelCtx, f, "ux2-cancel", `{"model":"gpt-6.1-sol","input":"cancel queued"}`)
	}()
	ux2Wait(t, "cancellation request not queued", func() bool { return f.manager.CredentialConcurrencySnapshot()["pool_waiting"].(int) >= 2 })
	cancel()
	if result := <-cancelled; result.err == nil {
		t.Fatal("cancelled client succeeded")
	}
	ux2Wait(t, "cancelled wait leaked", func() bool { return f.manager.CredentialConcurrencySnapshot()["pool_waiting"].(int) == 1 })
	if posts.Load() != 100 {
		t.Fatal("cancelled request sent POST")
	}
	one <- struct{}{}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("101st did not enter freed slot")
	}
	if posts.Load() != 101 || peak.Load() > 100 {
		t.Fatalf("after release posts=%d peak=%d", posts.Load(), peak.Load())
	}
	closeAll.Do(func() { close(all) })
	for i := 0; i < 101; i++ {
		select {
		case result := <-replies:
			if result.err != nil || result.status != 200 {
				t.Fatalf("request%d %d %s %v", i, result.status, result.body, result.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("requests did not complete")
		}
	}
	ux2Wait(t, "leases/results not released", func() bool {
		v, e := f.executor.KiloProxyState()
		if e != nil || v.Accounts[0].InferenceActive != 0 || len(f.hook.snapshot()) != 101 {
			return false
		}
		entries := f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry)
		return len(entries) == 1 && entries[0].Active == 0 && entries[0].EffectiveLimit == 100
	})
	t.Logf("native dedicated key -> AuthManager -> KiloExecutor -> CONNECT/TLS PASS: initialIP1 expandedIP5 plateau100 eachIP20; POST101 only after release; peak%d; cancelled queued request POST0; result101 account/proxy active0; idle bindings5", peak.Load())
}

func TestUX2NativeDevicePoolSpecifiedFiveCommitAndConflictReceipt(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var calls atomic.Int32
	f.deviceHandler = m4DeviceUpstream(t, &calls)
	ids := ux2NativeIPs(t, f, 5, func(_ int, w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected inference")
		http.Error(w, "inference", 500)
	})
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	revision := ojson.GetBytes(state, "revision").Uint()
	draft := fmt.Sprintf(`"inference_egress":{"expected_revision":%d,"mode":"auto","capacity_mode":"ip","allocation_mode":"specified","max_ips":5,"specified_ips":["203.0.113.1","203.0.113.2","203.0.113.3","203.0.113.4","203.0.113.5"],"exclusive_proxy_ids":[],"primary_proxy_id":""}`, revision)
	start := m4Request(t, f, "POST", kiloDeviceURL, fmt.Sprintf(`{"name":"specified pool","authorization_egress":{"mode":"pool","proxy_id":%q,"expected_revision":%d},%s}`, ids[0], revision, draft), 200)
	flowID := ojson.GetBytes(start, "id").String()
	m4Request(t, f, "POST", kiloDeviceURL+"/"+flowID+"/poll", "{}", 200)
	saved := m4Request(t, f, "POST", kiloDeviceURL+"/"+flowID+"/save", "{}", 201)
	accountID := ojson.GetBytes(saved, "id").String()
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(accountID) })
	if !ojson.GetBytes(saved, "credential_saved").Bool() || !ojson.GetBytes(saved, "egress_ready").Bool() || ojson.GetBytes(saved, "egress_error").Type != ojson.Null {
		t.Fatalf("own authorization pin prevented commit: %s", saved)
	}
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if len(ojson.GetBytes(state, "accounts.0.assigned_ips").Array()) != 5 {
		t.Fatalf("not all five assigned: %s", state)
	}
	// A second completed authorization retains its credential while the exact set is occupied.
	f.allowDirect = true
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	draft = strings.Replace(draft, fmt.Sprintf(`"expected_revision":%d`, revision), fmt.Sprintf(`"expected_revision":%d`, ojson.GetBytes(state, "revision").Uint()), 1)
	start = m4Request(t, f, "POST", kiloDeviceURL, `{"name":"conflicted credential","authorization_egress":{"mode":"direct"},`+draft+`}`, 200)
	flowID = ojson.GetBytes(start, "id").String()
	m4Request(t, f, "POST", kiloDeviceURL+"/"+flowID+"/poll", "{}", 200)
	saved = m4Request(t, f, "POST", kiloDeviceURL+"/"+flowID+"/save", "{}", 201)
	waitingID := ojson.GetBytes(saved, "id").String()
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(waitingID) })
	if !ojson.GetBytes(saved, "credential_saved").Bool() || ojson.GetBytes(saved, "egress_ready").Bool() || ojson.GetBytes(saved, "egress_error").String() != "specified_conflict" {
		t.Fatalf("conflict receipt=%s", saved)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.AuthDir, waitingID)); err != nil {
		t.Fatal("approved credential not durable")
	}
	a, ok := f.manager.GetByID(waitingID)
	if !ok {
		t.Fatal("saved account missing")
	}
	if _, release, busy, err := f.executor.TryAcquireEgress(a); err != nil || !busy || release != nil {
		t.Fatal("unallocated account admitted")
	}
	v, err := f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range v.IPGroups {
		if g.Owner != accountID || g.UniqueAccounts != 1 {
			t.Fatal("conflict contaminated owner/history")
		}
	}
	t.Log("real pool device authorization on specifiedIP1 -> credential commit -> exact5 assigned in same save; second approved credential retained with credential_saved=true/egress_ready=false/specified_conflict; no partial owner/history or inference")
}
func TestUX2NativeLowAccountLimitStreamingDownscaleAndDisableDrain(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	entered := make(chan struct{}, 10)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var posts atomic.Int32
	ux2NativeIPs(t, f, 2, func(_ int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"ux2-stream-%d\"}}\n\n", n)
		w.(http.Flusher).Flush()
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("ux2-stream-%d", n))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	id := m2Create(t, f, `,"concurrency_limit":7`)
	ux2NativePolicy(t, f, id, 2)
	done := make(chan ux2Reply, 7)
	for i := 0; i < 7; i++ {
		go func(n int) {
			done <- ux2Request(context.Background(), f, fmt.Sprintf("stream-%d", n), `{"model":"gpt-6.1-sol","input":"stream","stream":true}`)
		}(i)
	}
	for i := 0; i < 7; i++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("seven streams not entered")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan ux2Reply, 1)
	go func() {
		waiting <- ux2Request(ctx, f, "stream-wait", `{"model":"gpt-6.1-sol","input":"wait","stream":true}`)
	}()
	ux2Wait(t, "eighth ignored manual7", func() bool { return f.manager.CredentialConcurrencySnapshot()["pool_waiting"].(int) == 1 })
	v, _ := f.executor.KiloProxyState()
	if posts.Load() != 7 || v.Accounts[0].InferenceActive != 7 || v.Accounts[0].CurrentCapacity != 7 || len(v.Accounts[0].AssignedIPs) != 1 {
		t.Fatalf("stream first event released or low limit ignored: %+v posts=%d", v.Accounts, posts.Load())
	}
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"ip_concurrency_limit":3}`, v.Revision), 200)
	if posts.Load() != 7 {
		t.Fatal("downscale admitted eighth")
	}
	cancel()
	<-waiting
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
	f.manager.SyncKiloAutoEgress(time.Now())
	v, _ = f.executor.KiloProxyState()
	if !v.IPGroups[0].Draining || v.IPGroups[0].InferenceActive != 7 || v.IPGroups[0].Owner != id {
		t.Fatal("disable broke active stream ownership")
	}
	once.Do(func() { close(release) })
	for i := 0; i < 7; i++ {
		result := <-done
		if result.err != nil || result.status != 200 || !strings.Contains(string(result.body), "response.completed") {
			t.Fatalf("stream interrupted=%d %v", result.status, result.err)
		}
	}
	ux2Wait(t, "streams not released", func() bool { v, _ := f.executor.KiloProxyState(); return v.Accounts[0].InferenceActive == 0 })
	f.manager.SyncKiloAutoEgress(time.Now())
	v, _ = f.executor.KiloProxyState()
	if len(v.Accounts[0].AssignedIPs) != 0 || posts.Load() != 7 {
		t.Fatal("disabled owner or replay remains")
	}
	t.Log("manual7 overrides target40; seven real SSE remain leased after first event; eighth waits/cancels POST0; IP downscale3 admits none; disable drains7 to terminal and releases owner without replay")
}

func TestUX2NativeContinuationKeepsAccountWithCapacityPolicy(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var posts atomic.Int32
	machines := make(chan string, 3)
	ux2NativeIPs(t, f, 2, func(_ int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		machines <- r.Header.Get("X-KiloCode-MachineId")
		w.Header().Set("Content-Type", "text/event-stream")
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("ux2-cont-%d", n))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	id := m2Create(t, f, "")
	ux2NativePolicy(t, f, id, 2)
	first := ux2Request(context.Background(), f, "ux2-cont-session", `{"model":"gpt-6.1-sol","input":"first"}`)
	if first.err != nil || first.status != 200 {
		t.Fatalf("first=%d %s %v", first.status, first.body, first.err)
	}
	other := m2Create(t, f, `,"priority":100`)
	ux2NativePolicy(t, f, other, 2)
	next := ux2Request(context.Background(), f, "ux2-cont-session", `{"model":"gpt-6.1-sol","previous_response_id":"ux2-cont-1","input":"next"}`)
	if next.err != nil || next.status != 200 {
		t.Fatalf("next=%d %s %v", next.status, next.body, next.err)
	}
	a, b := <-machines, <-machines
	if a == "" || a != b || posts.Load() != 2 {
		t.Fatal("continuation switched machine or replayed")
	}
	for _, result := range f.hook.snapshot() {
		if result.AuthID != id {
			t.Fatal("continuation switched to higher priority account")
		}
	}
	t.Log("capacity-mode continuation retains original AuthID/machine despite a new higher-priority account; exact2POST, no replay")
}
func TestUX2NativeConcurrentSpecifiedSetHasOneWinner(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	ux2NativeIPs(t, f, 5, func(_ int, w http.ResponseWriter, r *http.Request) { t.Error("unexpected inference") })
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"auto_assignment_enabled":false}`, ojson.GetBytes(state, "revision").Uint()), 200)
	a, b := m2Create(t, f, ""), m2Create(t, f, "")
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	body := fmt.Sprintf(`{"expected_revision":%d,"mode":"auto","exclusive_proxy_ids":[],"primary_proxy_id":"","capacity_mode":"ip","allocation_mode":"specified","max_ips":5,"specified_ips":["203.0.113.1","203.0.113.2","203.0.113.3","203.0.113.4","203.0.113.5"]}`, ojson.GetBytes(state, "revision").Uint())
	results := make(chan int, 2)
	start := make(chan struct{})
	for _, id := range []string{a, b} {
		go func(id string) {
			<-start
			status, _ := f.request(t, "PUT", kiloAccountsURL+"/"+id+"/egress", kiloManagementKey, body)
			results <- status
		}(id)
	}
	close(start)
	x, y := <-results, <-results
	if !((x == 200 && y == 409) || (x == 409 && y == 200)) {
		t.Fatalf("race statuses=%d/%d", x, y)
	}
	view, err := f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	owner := view.IPGroups[0].Owner
	for _, g := range view.IPGroups {
		if g.Owner != owner || g.UniqueAccounts != 1 || g.Active != 0 {
			t.Fatal("concurrent specified set split owners/history")
		}
	}
	t.Log("two real management PUTs race same exact5 set: one200 one409; one owner on everyIP, each history1, no partial assignment")
}

func TestUX2NativeToolResultKeepsCallAndAccountAfterIPExpansion(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var posts atomic.Int32
	seen := make(chan kiloSessionSeen, 2)
	wires := make(chan []byte, 2)
	ux2NativeIPs(t, f, 3, func(_ int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		body, _ := io.ReadAll(r.Body)
		wires <- body
		seen <- kiloSessionSeen{r.Header.Get("X-KiloCode-MachineId"), r.Header.Get("X-Session-Id")}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"ux2-tool-first","status":"completed","output":[{"type":"function_call","id":"fc-ux2","call_id":"ux2-call-1","name":"lookup","arguments":"{}","status":"completed"}]}}`+"\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: "+kiloTerminal+"\n\n")
	})
	id := m2Create(t, f, "")
	ux2NativePolicy(t, f, id, 2)
	first := ux2Request(context.Background(), f, "ux2-tool-session", `{"model":"gpt-6.1-sol","input":"call lookup"}`)
	if first.err != nil || first.status != 200 || !strings.Contains(string(first.body), "ux2-call-1") {
		t.Fatalf("tool first=%d %s %v", first.status, first.body, first.err)
	}
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"auto","exclusive_proxy_ids":[],"primary_proxy_id":"","capacity_mode":"ip","allocation_mode":"specified","max_ips":2,"specified_ips":["203.0.113.1","203.0.113.2"]}`, ojson.GetBytes(state, "revision").Uint()), 200)
	other := m2Create(t, f, `,"priority":100`)
	ux2NativePolicy(t, f, other, 1)
	second := ux2Request(context.Background(), f, "", `{"model":"gpt-6.1-sol","input":[{"type":"function_call_output","call_id":"ux2-call-1","output":"exact UX2 tool result"}]}`)
	if second.err != nil || second.status != 200 {
		t.Fatalf("tool second=%d %s %v", second.status, second.body, second.err)
	}
	a, b := <-seen, <-seen
	if a != b || a.Account == "" {
		t.Fatal("tool result changed account/session")
	}
	<-wires
	wire := <-wires
	found := false
	for _, item := range ojson.GetBytes(wire, "input").Array() {
		if item.Get("type").String() == "function_call_output" && item.Get("call_id").String() == "ux2-call-1" && item.Get("output").String() == "exact UX2 tool result" {
			found = true
		}
	}
	if !found || posts.Load() != 2 {
		t.Fatal("tool result/call changed or POST replayed")
	}
	for _, result := range f.hook.snapshot() {
		if result.AuthID != id {
			t.Fatal("tool result switched to higher-priority identity")
		}
	}
	t.Log("real function_call ux2-call-1 -> function_call_output preserves exact ID/result and AuthID/machine/session after original account grows1->2IP and higher-priority account appears; exact2POST")
}
