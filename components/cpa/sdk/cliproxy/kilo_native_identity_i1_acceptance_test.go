package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestI1AcceptedLegacyAliasesShareProfileAndCapacity(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	seen := make(chan i1Observed, 2)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var posts atomic.Int32
	i1NativeIPs(t, f, 1, func(ip int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		seen <- i1Observed{ip, r.Header.Get("X-KILOCODE-MACHINEID"), r.RemoteAddr, r.Header.Get("X-Session-Id")}
		<-release
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("i1-alias-%d", n))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	id := m2Create(t, f, "")
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"auto","exclusive_proxy_ids":[],"primary_proxy_id":"","capacity_mode":"ip","allocation_mode":"specified","max_ips":1,"specified_ips":["203.0.113.1"]}`, ojson.GetBytes(state, "revision").Uint()), 200)
	store := kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}
	saved, _, err := store.Load(kilo.ProxyState{})
	if err != nil {
		t.Fatal(err)
	}
	primary := saved.Proxies[1]
	u, err := url.Parse(primary.URL)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = alias.Close() })
	go func() {
		for {
			client, err := alias.Accept()
			if err != nil {
				return
			}
			go func() {
				remote, err := net.Dial("tcp", u.Host)
				if err != nil {
					_ = client.Close()
					return
				}
				go func() { _, _ = io.Copy(remote, client); _ = remote.Close(); _ = client.Close() }()
				_, _ = io.Copy(client, remote)
				_ = remote.Close()
				_ = client.Close()
			}()
		}
	}()
	previous := http.DefaultTransport
	transport := previous.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == alias.Addr().String() {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		return previous.(*http.Transport).DialContext(ctx, network, address)
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	// Existing confirmed aliases predate admission uniqueness; their owner history
	// is the supported migration source, not a newly admitted duplicate proxy.
	primary.AdmissionStatus, primary.AdmissionReason, primary.AdmittedIP = "", "", ""
	primary.AdmittedAt = time.Time{}
	saved.Proxies[1] = primary
	primary.ID, primary.URL, primary.Order = "legacy-alias", "http://"+alias.Addr().String(), 2
	saved.Proxies = append(saved.Proxies, primary)
	saved.IPHistory = nil
	saved.IPConcurrencyLimit = 2
	entryLimit := 1
	saved.IPEntryLimit = &entryLimit
	if err = store.Save(saved); err != nil {
		t.Fatal(err)
	}
	m2Reload(t, f)
	for _, pid := range []string{saved.Proxies[1].ID, "legacy-alias"} {
		state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
		m2Request(t, f, "POST", kiloProxiesURL+"/"+pid+"/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(state, "revision").Uint()), 200)
	}
	done := make(chan ux2Reply, 2)
	go func() {
		done <- ux2Request(context.Background(), f, "i1-alias-first", `{"model":"gpt-6.1-sol","input":"first alias"}`)
	}()
	first := i1Observe(t, seen)
	go func() {
		done <- ux2Request(context.Background(), f, "i1-alias-second", `{"model":"gpt-6.1-sol","input":"second alias"}`)
	}()
	second := i1Observe(t, seen)
	active := map[string]int{}
	for _, p := range f.executor.KiloProxySnapshot().Proxies {
		active[p.ID] = p.Active
	}
	view, err := f.executor.KiloProxyState()
	if err != nil {
		t.Fatal(err)
	}
	if active[saved.Proxies[1].ID] != 1 || active["legacy-alias"] != 1 || first.Machine == "" || first.Machine != second.Machine || first.Remote == second.Remote {
		t.Fatalf("aliases did not use distinct entries with one identity: %+v %+v active=%v", first, second, active)
	}
	if len(view.Accounts) != 1 || len(view.Accounts[0].AssignedIPs) != 1 || view.Accounts[0].CurrentCapacity != 2 || view.Accounts[0].ConfiguredCapacity != 2 || len(ojson.GetBytes(i1Credential(t, f, id), "device_profiles").Array()) != 1 {
		t.Fatalf("aliases multiplied identity or IP capacity: %+v", view.Accounts)
	}
	once.Do(func() { close(release) })
	for range 2 {
		if result := <-done; result.status != 200 || result.err != nil {
			t.Fatalf("alias request=%+v", result)
		}
	}
	if posts.Load() != 2 {
		t.Fatal("alias request replayed")
	}
	i1Released(t, f, 2)
}

func TestI1SecondProfileToolContinuationKeepsIdentity(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowDirect = true
	seen := make(chan i1Observed, 3)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var posts atomic.Int32
	i1NativeIPs(t, f, 2, func(ip int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if n == 3 {
			found := false
			for _, item := range ojson.GetBytes(raw, "input").Array() {
				if item.Get("type").String() == "function_call_output" && item.Get("call_id").String() == "call-i1-tools-2" && item.Get("output").String() == "second-profile-output" {
					found = true
				}
			}
			if !found {
				t.Error("second-profile tool output missing upstream")
			}
		}
		if r.Header.Get("X-Session-Id") != r.Header.Get("X-KILOCODE-TASKID") || r.Header.Get("X-Session-Id") != r.Header.Get("x-session-affinity") {
			t.Error("upstream session headers disagree")
		}
		seen <- i1Observed{ip, r.Header.Get("X-KILOCODE-MACHINEID"), r.RemoteAddr, r.Header.Get("X-Session-Id")}
		if n == 1 {
			<-release
		}
		id := fmt.Sprintf("i1-tools-%d", n)
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", id)
		_ = doc.Set("response.output", []any{map[string]any{"type": "function_call", "id": "fc-" + id, "call_id": "call-" + id, "name": "chain_tool", "arguments": "{}", "status": "completed"}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"ip_concurrency_limit":1}`, ojson.GetBytes(state, "revision").Uint()), 200)
	id := m2Create(t, f, "")
	i1IPPolicy(t, f, id, true, 200)
	firstDone := make(chan ux2Reply, 1)
	go func() {
		firstDone <- ux2Request(context.Background(), f, "i1-tools-first", `{"model":"gpt-6.1-sol","input":"hold first profile"}`)
	}()
	first := i1Observe(t, seen)
	result := ux2Request(context.Background(), f, "i1-tools-second", `{"model":"gpt-6.1-sol","input":"call a tool"}`)
	if result.status != 200 || result.err != nil {
		t.Fatalf("second profile=%+v", result)
	}
	second := i1Observe(t, seen)
	profiles := ojson.GetBytes(i1Credential(t, f, id), "device_profiles").Array()
	if len(profiles) != 2 || first.Machine != profiles[0].Get("machine_id").String() || second.Machine != profiles[1].Get("machine_id").String() || first.IP == second.IP {
		t.Fatalf("second request did not select second durable profile: %+v %+v", first, second)
	}
	once.Do(func() { close(release) })
	if result = <-firstDone; result.status != 200 || result.err != nil {
		t.Fatalf("first profile=%+v", result)
	}
	other := m2Create(t, f, `,"priority":100`)
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+other+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, ojson.GetBytes(state, "revision").Uint()), 200)
	result = ux2Request(context.Background(), f, "conflicting-inbound-session", `{"model":"gpt-6.1-sol","previous_response_id":"i1-tools-2","input":[{"type":"function_call_output","call_id":"call-i1-tools-2","output":"second-profile-output"}],"stream":true}`)
	if result.status != 200 || result.err != nil {
		t.Fatalf("tool continuation=%+v", result)
	}
	continued := i1Observe(t, seen)
	if continued != second || continued.Machine == first.Machine || posts.Load() != 3 {
		t.Fatalf("second-profile continuation moved: %+v want=%+v", continued, second)
	}
	i1Released(t, f, 3)
	for _, result := range f.hook.snapshot() {
		if result.AuthID != id {
			t.Fatalf("continuation switched account: %s", result.AuthID)
		}
	}
}

type i1LoadBarrierStore struct {
	coreauth.Store
	entered chan struct{}
	release chan struct{}
}

func (s *i1LoadBarrierStore) List(ctx context.Context) ([]*coreauth.Auth, error) {
	close(s.entered)
	select {
	case <-s.release:
		return s.Store.List(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Only synchronize the actual Manager callback; all executor methods stay native.
type i1ProfileStoreBarrier struct {
	*native.KiloExecutor
	before func(context.Context, string)
}

func (e *i1ProfileStoreBarrier) SetKiloDeviceProfileStore(store kilo.DeviceProfileStore) {
	e.KiloExecutor.SetKiloDeviceProfileStore(func(ctx context.Context, id string, count int) ([]kilo.DeviceProfile, error) {
		e.before(ctx, id)
		return store(ctx, id, count)
	})
}

func TestI1NativePostLeaseProfileGateBusyDoesNotFailOver(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowDirect = true
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "203.0.113.10") }
	id := m2Create(t, f, `,"priority":20`)
	other := m2Create(t, f, `,"priority":0`)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"pool","exclusive_proxy_ids":[],"primary_proxy_id":"local-proxy"}`, ojson.GetBytes(state, "revision").Uint()), 200)
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+other+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, ojson.GetBytes(state, "revision").Uint()), 200)
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(state, "revision").Uint()), 200)
	a, _ := f.manager.GetByID(id)
	_, release, busy, err := f.executor.TryAcquireEgress(a)
	if err != nil || busy || release == nil {
		t.Fatalf("native lease preparation: busy=%v err=%v", busy, err)
	}
	release()
	store := kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}
	saved, _, err := store.Load(kilo.ProxyState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Accounts[id].DeviceProfiles) != 1 || saved.Accounts[id].PrimaryProxyID != "local-proxy" || len(saved.Proxies) != 1 || saved.Proxies[0].FirstUsed == 0 {
		t.Fatalf("post-lease callback preconditions missing: %+v %+v", saved.Accounts[id], saved.Proxies)
	}
	// Complete mappings skip the early profile preparation callback. A healthy
	// established primary skips pool persistence; the next callback is therefore
	// selectedDeviceProfile, after ProxyPool.TryAcquire has incremented active.
	i1Released(t, f, 0)
	barrier := &i1LoadBarrierStore{Store: m2FileStore(f), entered: make(chan struct{}), release: make(chan struct{})}
	f.manager.SetStore(barrier)
	var unblock sync.Once
	var start sync.Once
	var started atomic.Bool
	loadDone := make(chan struct{})
	var loadErr error
	attempts := make(chan string, 4)
	t.Cleanup(func() {
		unblock.Do(func() { close(barrier.release) })
		if started.Load() {
			select {
			case <-loadDone:
			case <-time.After(10 * time.Second):
				t.Error("native Load did not finish after barrier release")
			}
		}
	})
	f.manager.RegisterExecutor(&i1ProfileStoreBarrier{KiloExecutor: f.executor, before: func(ctx context.Context, selected string) {
		attempts <- selected
		start.Do(func() {
			started.Store(true)
			go func() {
				loadErr = f.manager.Load(context.Background())
				close(loadDone)
			}()
		})
		select {
		case <-barrier.entered:
		case <-ctx.Done():
			t.Error("request canceled before Load held its gate")
		}
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := ux2Request(ctx, f, "i1-post-lease-gate", `{"model":"gpt-6.1-sol","input":"identity reload is busy"}`)
	if result.err != nil || result.status != 503 || !strings.Contains(string(result.body), "Kilo identity is being reloaded") || f.posts.Load() != 0 {
		t.Fatalf("native identity gate result=%+v posts=%d", result, f.posts.Load())
	}
	if len(attempts) != 1 || <-attempts != id {
		t.Fatal("identity gate retried or selected another account")
	}
	i1Released(t, f, 0)
	for _, accountID := range []string{id, other} {
		account, ok := f.manager.GetByID(accountID)
		if !ok || account.Disabled || account.Unavailable || account.LastError != nil {
			t.Fatalf("local identity gate changed account availability: %s", accountID)
		}
	}
	unblock.Do(func() { close(barrier.release) })
	select {
	case <-loadDone:
		if loadErr != nil {
			t.Fatal(loadErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("native Load did not finish after request completion")
	}
	t.Log("native post-lease callback: HTTP 503, zero model POST/results, both lease classes released, original account only, both accounts available, Load finished")
}
