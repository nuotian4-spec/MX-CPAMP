package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestI1PinnedDirectContinuation(t *testing.T) {
	entered := make(chan i1Observed, 5)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var posts atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		entered <- i1Observed{Machine: r.Header.Get("X-KILOCODE-MACHINEID"), Remote: r.RemoteAddr, Session: r.Header.Get("X-Session-Id")}
		if n == 1 {
			<-release
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("i1-direct-%d", n))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	f.allowDirect = true
	id := m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"proxy_limit":1}`, ojson.GetBytes(state, "revision").Uint()), 200)
	done := make(chan ux2Reply, 1)
	go func() {
		done <- ux2Request(context.Background(), f, "i1-busy-proxy", `{"model":"gpt-6.1-sol","input":"hold"}`)
	}()
	proxied := i1Observe(t, entered)
	result := ux2Request(context.Background(), f, "i1-direct", `{"model":"gpt-6.1-sol","input":"direct"}`)
	if result.status != 200 {
		t.Fatalf("direct=%+v", result)
	}
	direct := i1Observe(t, entered)
	if direct.Machine == proxied.Machine || f.connects.Load() != 1 {
		t.Fatal("direct fallback not isolated")
	}
	once.Do(func() { close(release) })
	if result = <-done; result.status != 200 {
		t.Fatalf("proxy=%+v", result)
	}
	result = ux2Request(context.Background(), f, "i1-direct", `{"model":"gpt-6.1-sol","input":"continue"}`)
	if result.status != 200 {
		t.Fatalf("direct continuation=%+v", result)
	}
	again := i1Observe(t, entered)
	if again != direct {
		t.Fatalf("direct continuation switched identity/connection: %+v %+v", direct, again)
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"allow_direct_when_proxy_full":false}`, 200)
	before := posts.Load()
	result = ux2Request(context.Background(), f, "i1-direct", `{"model":"gpt-6.1-sol","input":"disabled"}`)
	if result.status != 409 || posts.Load() != before {
		t.Fatalf("disabled route silently changed: %+v", result)
	}
	i1Released(t, f, 3)
}

func TestI1PostLeaseStateFailureReleasesBothSlots(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowDirect = true
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "203.0.113.10") }
	id := m2Create(t, f, `,"priority":20`)
	other := m2Create(t, f, `,"priority":0`)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"pool","exclusive_proxy_ids":[],"primary_proxy_id":"local-proxy"}`, ojson.GetBytes(state, "revision").Uint()), 200)
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+other+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, ojson.GetBytes(state, "revision").Uint()), 200)
	path := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := ojson.NewDoc(raw)
	_ = doc.Set("proxies.0.first_used", 1)
	if err = os.WriteFile(path, doc.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	m2Reload(t, f)
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(state, "revision").Uint()), 200)
	if err = os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"fail state save"}`)
	if status != 503 || f.posts.Load() != 0 {
		t.Fatalf("status=%d body=%s posts=%d", status, body, f.posts.Load())
	}
	i1Released(t, f, 0)
	for _, accountID := range []string{id, other} {
		a, _ := f.manager.GetByID(accountID)
		if a.Unavailable || a.LastError != nil {
			t.Fatal("local failure marked credential unavailable")
		}
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	status, body = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"slots recovered"}`)
	if status != 200 {
		t.Fatalf("recovered=%d %s", status, body)
	}
	i1Released(t, f, 1)
}

func TestI1DeviceSaveCommitsProfilesWithManualGroup(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var posts atomic.Int32
	i1NativeIPs(t, f, 2, func(_ int, w http.ResponseWriter, r *http.Request) { posts.Add(1); kiloSuccessStream(w, r) })
	id := "kilo-" + uuid.NewString() + ".json"
	owner := "device-i1-joint"
	draft := kilo.InferenceEgress{Mode: "auto", CapacityPolicy: kilo.CapacityPolicy{CapacityMode: "ip", AllocationMode: "specified", MaxIPs: 2, SpecifiedIPs: []string{"203.0.113.1", "203.0.113.2"}}}
	_, draft, err := f.executor.PrepareKiloDevice(owner, id, kilo.AuthorizationEgress{Mode: "direct"}, draft, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer f.executor.ReleaseKiloDevice(owner)
	if _, err = os.Stat(filepath.Join(f.cfg.AuthDir, id)); !os.IsNotExist(err) {
		t.Fatal("preview created credential")
	}
	key := kiloInitialKey
	a, err := f.manager.SaveKiloDeviceAccountWithEgress(context.Background(), id, coreauth.KiloAccountChanges{APIKey: &key}, "synthetic@example.invalid", owner, draft)
	if err != nil {
		t.Fatal(err)
	}
	if len(ojson.GetBytes(i1Credential(t, f, id), "device_profiles").Array()) != 2 {
		t.Fatal("device joint save did not persist complete profile set")
	}
	service := &Service{cfg: f.cfg, coreManager: f.manager}
	service.registerExecutorForAuth(a, false)
	if !service.registerModelsForAuth(context.Background(), a) {
		t.Fatal("saved account not registered")
	}
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"device saved"}`)
	if status != 200 || posts.Load() != 1 {
		t.Fatalf("saved=%d %s posts=%d", status, body, posts.Load())
	}
	i1Released(t, f, 1)
}

func TestI1PreviewPendingAndDuplicatePreserveProfiles(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "203.0.113.10") }
	id := m2Create(t, f, "")
	credential := i1Credential(t, f, id)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	rawURL := f.cfg.Kilo.Proxies[0].URL
	m2Request(t, f, "POST", kiloProxiesURL+"/preview", fmt.Sprintf(`{"url":%q}`, rawURL), 200)
	after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if !bytes.Equal(state, after) || !bytes.Equal(credential, i1Credential(t, f, id)) {
		t.Fatal("preview created identity or capacity")
	}
	state = m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":%d,"url":%q}`, ojson.GetBytes(state, "revision").Uint(), rawURL), 201)
	disk := m3bDisk(t, f)
	m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":%d,"url":%q}`, ojson.GetBytes(state, "revision").Uint(), rawURL), 409)
	if !bytes.Equal(disk, m3bDisk(t, f)) || !bytes.Equal(credential, i1Credential(t, f, id)) {
		t.Fatal("duplicate IP created identity or capacity")
	}
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "127.0.0.2") }
	after = m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":%d,"url":%q}`, ojson.GetBytes(state, "revision").Uint(), rawURL), 201)
	entries := ojson.GetBytes(after, "proxies").Array()
	pending := entries[len(entries)-1]
	if pending.Get("admission_status").String() != "pending" || pending.Get("healthy").Bool() || pending.Get("admitted_ip").String() != "" {
		t.Fatal("unconfirmed proxy became schedulable")
	}
	if len(ojson.GetBytes(after, "ip_groups").Array()) != len(ojson.GetBytes(state, "ip_groups").Array()) || !bytes.Equal(credential, i1Credential(t, f, id)) || f.posts.Load() != 0 {
		t.Fatal("pending proxy created identity or capacity")
	}
	i1Released(t, f, 0)
}

func TestI1CancelOneProfilePreservesOtherStream(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	seen := make(chan i1Observed, 2)
	finish := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(finish) })
	var posts atomic.Int32
	i1NativeIPs(t, f, 2, func(ip int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"i1-cancel-%d\",\"status\":\"in_progress\"}}\n\n", n)
		w.(http.Flusher).Flush()
		seen <- i1Observed{ip, r.Header.Get("X-KILOCODE-MACHINEID"), r.RemoteAddr, r.Header.Get("X-Session-Id")}
		select {
		case <-r.Context().Done():
			return
		case <-finish:
			kiloSuccessStream(w, r)
		}
	})
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"ip_concurrency_limit":1}`, ojson.GetBytes(state, "revision").Uint()), 200)
	id := m2Create(t, f, "")
	i1IPPolicy(t, f, id, true, 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstDone, secondDone := make(chan ux2Reply, 1), make(chan ux2Reply, 1)
	go func() {
		firstDone <- ux2Request(ctx, f, "i1-cancel", `{"model":"gpt-6.1-sol","input":"cancel","stream":true}`)
	}()
	first := i1Observe(t, seen)
	go func() {
		secondDone <- ux2Request(context.Background(), f, "i1-keep", `{"model":"gpt-6.1-sol","input":"keep","stream":true}`)
	}()
	second := i1Observe(t, seen)
	if first.Machine == second.Machine || first.IP == second.IP {
		t.Fatal("cancel fixture did not select distinct profiles")
	}
	cancel()
	if result := <-firstDone; result.err == nil {
		t.Fatal("cancelled stream unexpectedly completed")
	}
	ux2Wait(t, "cancelled profile retained a slot", func() bool {
		active := 0
		for _, p := range f.executor.KiloProxySnapshot().Proxies {
			active += p.Active
		}
		return active == 1 && len(f.hook.snapshot()) == 1
	})
	select {
	case result := <-secondDone:
		t.Fatalf("other stream terminated: %+v", result)
	default:
	}
	once.Do(func() { close(finish) })
	if result := <-secondDone; result.status != 200 || result.err != nil {
		t.Fatalf("retained stream=%+v", result)
	}
	i1Released(t, f, 2)
	a, _ := f.manager.GetByID(id)
	if posts.Load() != 2 || a.Unavailable || !a.NextRetryAfter.IsZero() {
		t.Fatal("cancellation replayed or penalized account")
	}
}

func TestI1DeletedLegacyIdentityDoesNotBlockOtherAccounts(t *testing.T) {
	for _, owner := range []bool{false, true} {
		t.Run(fmt.Sprint(owner), func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.allowDirect = true
			if owner {
				i1NativeIPs(t, f, 2, func(_ int, w http.ResponseWriter, r *http.Request) { kiloSuccessStream(w, r) })
			}
			removed := m2Create(t, f, "")
			if owner {
				i1IPPolicy(t, f, removed, true, 200)
			} else {
				state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
				m2Request(t, f, "PUT", kiloAccountsURL+"/"+removed+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, ojson.GetBytes(state, "revision").Uint()), 200)
			}
			healthy := m2Create(t, f, "")
			state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
			m2Request(t, f, "PUT", kiloAccountsURL+"/"+healthy+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, ojson.GetBytes(state, "revision").Uint()), 200)
			store := kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}
			saved, _, err := store.Load(kilo.ProxyState{})
			if err != nil {
				t.Fatal(err)
			}
			a := saved.Accounts[removed]
			a.DeviceProfiles = nil
			saved.Accounts[removed] = a
			if err = store.Save(saved); err != nil {
				t.Fatal(err)
			}
			doc := ojson.NewDoc(i1Credential(t, f, removed))
			_ = doc.Delete("device_profiles")
			if err = os.WriteFile(filepath.Join(f.cfg.AuthDir, removed), doc.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			m2Reload(t, f)
			m2Request(t, f, "DELETE", kiloAccountsURL+"/"+removed, "", 204)
			if owner {
				f.manager.SyncKiloAutoEgress(time.Now())
				after, _, err := store.Load(kilo.ProxyState{})
				if err != nil {
					t.Fatal(err)
				}
				for ip, g := range saved.IPGroups {
					got := after.IPGroups[ip]
					if got.Owner != "" || got.Draining || len(got.AccountIDs) != len(g.AccountIDs) || len(after.IPHistory) != len(saved.IPHistory) {
						t.Fatalf("deleted owner was not released with history preserved: %+v", got)
					}
				}
			}
			status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"healthy account"}`)
			if status != 200 || f.posts.Load() != 1 {
				t.Fatalf("orphan blocked healthy account: %d %s posts=%d", status, body, f.posts.Load())
			}
			if _, err = os.Stat(filepath.Join(f.cfg.AuthDir, removed)); !os.IsNotExist(err) {
				t.Fatal("deleted credential recreated")
			}
			i1Released(t, f, 1)
		})
	}
}
