package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
)

const kiloRecycleURL = "/v8/management/kilo/proxy-recycle"

func TestKiloM3BArchiveReloadProbeRestoreInference(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":0,"mode":"pool","exclusive_proxy_ids":["local-proxy"],"primary_proxy_id":"local-proxy"}`, 200)
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"before archive"}`)
	if status != 200 {
		t.Fatalf("first=%d %s", status, body)
	}
	m3Wait(t, func() bool { return f.executor.KiloProxySnapshot().Proxies[0].Active == 0 })
	view := m2Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":2}`, 200)
	if len(ojson.GetBytes(view, "proxies").Array()) != 0 || ojson.GetBytes(view, "recycle.0.id").String() != "local-proxy" || ojson.GetBytes(view, "recycle.0.original_account_id").String() != id || ojson.GetBytes(view, "accounts.0.primary_proxy_id").String() != "" {
		t.Fatalf("archive=%s", view)
	}
	path := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), f.cfg.Kilo.Proxies[0].URL) || strings.Contains(string(view), `"url"`) {
		t.Fatal("archive secret persistence/projection mismatch")
	}
	m2Reload(t, f)
	view = m2Request(t, f, "GET", kiloRecycleURL, "", 200)
	if ojson.GetBytes(view, "revision").Uint() != 3 || len(ojson.GetBytes(view, "proxies").Array()) != 0 {
		t.Fatalf("reload=%s", view)
	}
	probes := f.probes.Load()
	view = m2Request(t, f, "POST", kiloRecycleURL+"/local-proxy/restore", `{"expected_revision":3}`, 200)
	if f.probes.Load() != probes+1 || ojson.GetBytes(view, "revision").Uint() != 4 || ojson.GetBytes(view, "runtime_revision").Uint() != 4 || ojson.GetBytes(view, "proxies.0.account_id").String() != "" || !ojson.GetBytes(view, "proxies.0.healthy").Bool() || len(ojson.GetBytes(view, "recycle").Array()) != 0 {
		t.Fatalf("restore=%s probes=%d", view, f.probes.Load())
	}
	saved, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || saved.Proxies[0].FirstUsed != 1 || saved.Proxies[0].Order != 0 || saved.Proxies[0].Name != "local-proxy" || saved.Proxies[0].Disabled || saved.Accounts[id].PrimaryProxyID != "" {
		t.Fatalf("restore metadata/primary=%+v err=%v", saved, err)
	}
	status, body = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","previous_response_id":"resp-kilo","input":"after restore"}`)
	if status != 200 {
		t.Fatalf("restored inference=%d %s", status, body)
	}
	f.assertReleased(t, 2)
	if f.posts.Load() != 2 || f.probes.Load() != probes+1 {
		t.Fatalf("post/probe=%d/%d", f.posts.Load(), f.probes.Load())
	}
	t.Log(fmt.Sprintf("archive/reload/forced-probe/shared-restore/production-continuation PASS: POST=%d CONNECT=%d probes=%d results=%d", f.posts.Load(), f.connects.Load(), f.probes.Load(), len(f.hook.snapshot())))
}

func m3bDisk(t *testing.T, f *kiloNativeFixture) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func m3bAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("expected local network boundary not observed")
	}
}

func TestKiloM3BManagementGuardsAndClearIsolation(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	view := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q,"name":"archived"}`, f.cfg.Kilo.Proxies[0].URL), 201)
	pid := ojson.GetBytes(view, "proxies.1.id").String()
	m2Request(t, f, "DELETE", kiloProxiesURL+"/"+pid, `{"expected_revision":1}`, 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":2,"mode":"pool","exclusive_proxy_ids":["local-proxy"],"primary_proxy_id":"local-proxy"}`, 200)
	before := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
	disk := m3bDisk(t, f)
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key", "synthetic-bps-key", "synthetic-legacy-key"} {
		for _, route := range []struct{ method, path string }{{"GET", kiloRecycleURL}, {"DELETE", kiloRecycleURL}, {"DELETE", kiloProxiesURL + "/local-proxy"}, {"POST", kiloRecycleURL + "/" + pid + "/restore"}, {"POST", kiloProxiesURL + "/local-proxy/recheck"}} {
			status, _ := f.request(t, route.method, route.path, key, `{"expected_revision":3,"confirm":true}`)
			if status != 401 {
				t.Fatalf("client key reached %s %s: %d", route.method, route.path, status)
			}
			// Authenticate between cases to keep the shared IP failure limit independent.
			m2Request(t, f, "GET", kiloRecycleURL, "", 200)
		}
	}
	for _, body := range []string{`{}`, `{"expected_revision":3}`, `{"expected_revision":3,"confirm":false}`, `{"expected_revision":3,"confirm":"true"}`, `{"expected_revision":3,"confirm":true,"unknown":1}`, `{"expected_revision":null,"confirm":true}`, `{"expected_revision":-1,"confirm":true}`, `{"expected_revision":3.5,"confirm":true}`} {
		m2Request(t, f, "DELETE", kiloRecycleURL, body, 400)
	}
	for _, route := range []struct{ method, path string }{{"DELETE", kiloProxiesURL + "/local-proxy"}, {"POST", kiloRecycleURL + "/" + pid + "/restore"}, {"POST", kiloProxiesURL + "/local-proxy/recheck"}} {
		m2Request(t, f, route.method, route.path, `{"expected_revision":3,"url":"secret"}`, 400)
		m2Request(t, f, route.method, route.path, `{"expected_revision":2}`, 409)
	}
	m2Request(t, f, "DELETE", kiloRecycleURL, `{"expected_revision":2,"confirm":true}`, 409)
	m2Request(t, f, "POST", kiloRecycleURL+"/missing/restore", `{"expected_revision":3}`, 404)
	m2Request(t, f, "POST", kiloProxiesURL+"/missing/recheck", `{"expected_revision":3}`, 404)
	m2Request(t, f, "DELETE", kiloProxiesURL+"/missing", `{"expected_revision":3}`, 404)
	after := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
	if string(before) != string(after) || string(disk) != string(m3bDisk(t, f)) || f.probes.Load() != 0 || f.posts.Load() != 0 {
		t.Fatal("rejected management action changed state or reached network")
	}
	view = m2Request(t, f, "DELETE", kiloRecycleURL, `{"expected_revision":3,"confirm":true}`, 200)
	for _, field := range []string{"proxies", "accounts", "proxy_limit"} {
		if ojson.GetBytes(view, field).Raw != ojson.GetBytes(before, field).Raw {
			t.Fatalf("clear changed active field %s", field)
		}
	}
	if ojson.GetBytes(view, "revision").Uint() != 4 || len(ojson.GetBytes(view, "recycle").Array()) != 0 {
		t.Fatalf("clear=%s", view)
	}
	m2Reload(t, f)
	view = m2Request(t, f, "GET", kiloRecycleURL, "", 200)
	if len(ojson.GetBytes(view, "recycle").Array()) != 0 || ojson.GetBytes(view, "proxies.0.account_id").String() != id {
		t.Fatalf("clear reload=%s", view)
	}
	t.Log("all five routes enforce management key; invalid/stale/missing writes have zero effects; confirmed clear preserves active pool/account bindings")
}

func TestKiloM3BAtomicStorageFailures(t *testing.T) {
	for _, operation := range []string{"archive", "restore", "clear"} {
		t.Run(operation, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			m2Create(t, f, "")
			m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":0,"proxy_limit":1}`, 200)
			method, path, body := "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":1}`
			if operation != "archive" {
				m2Request(t, f, "DELETE", path, body, 200)
				if operation == "restore" {
					method, path, body = "POST", kiloRecycleURL+"/local-proxy/restore", `{"expected_revision":2}`
				} else {
					path, body = kiloRecycleURL, `{"expected_revision":2,"confirm":true}`
				}
			}
			before := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
			raw := m3bDisk(t, f)
			file := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
			if err := os.Rename(file, file+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0700); err != nil {
				t.Fatal(err)
			}
			m2Request(t, f, method, path, body, 503)
			after := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
			if string(before) != string(after) {
				t.Fatalf("failed %s published state: %s", operation, after)
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(file+".saved", file); err != nil {
				t.Fatal(err)
			}
			if string(raw) != string(m3bDisk(t, f)) {
				t.Fatal("failed write replaced previous disk state")
			}
			m2Reload(t, f)
			if reloaded := m2Request(t, f, "GET", kiloRecycleURL, "", 200); string(before) != string(reloaded) {
				t.Fatalf("failure changed reload: %s", reloaded)
			}
			wantProbes := int32(0)
			if operation == "restore" {
				wantProbes = 1
			}
			if f.probes.Load() != wantProbes || f.posts.Load() != 0 {
				t.Fatalf("unexpected network probes/posts=%d/%d", f.probes.Load(), f.posts.Load())
			}
		})
	}
}

func TestKiloM3BRestoreFailureAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelled), func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			started, probeDone := make(chan struct{}), make(chan struct{})
			f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
				defer close(probeDone)
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-KiloCode-MachineId") != "" {
					t.Error("health probe leaked account headers")
				}
				close(started)
				if cancelled {
					<-r.Context().Done()
					return
				}
				http.Error(w, "synthetic health unavailable", 502)
			}
			m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
			before := m2Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":0}`, 200)
			disk := m3bDisk(t, f)
			if !cancelled {
				m2Request(t, f, "POST", kiloRecycleURL+"/local-proxy/restore", `{"expected_revision":1}`, 502)
			} else {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+kiloRecycleURL+"/local-proxy/restore", strings.NewReader(`{"expected_revision":1}`))
				req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
				done := make(chan struct{})
				go func() {
					defer close(done)
					res, err := f.server.Client().Do(req)
					if err == nil {
						_ = res.Body.Close()
					}
				}()
				m3bAwait(t, started)
				cancel()
				m3bAwait(t, done)
			}
			m3bAwait(t, probeDone)
			after := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
			if string(before) != string(after) || string(disk) != string(m3bDisk(t, f)) || f.probes.Load() != 1 || f.posts.Load() != 0 || f.connects.Load() != 0 {
				t.Fatalf("failed/cancelled restore changed state: %s", after)
			}
		})
	}
}

func TestKiloM3BProbeConcurrentMutation(t *testing.T) {
	for _, mutation := range []string{"limit", "clear", "url"} {
		t.Run(mutation, func(t *testing.T) {
			started, finish := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-finish:
				default:
					close(finish)
				}
			}()
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-finish:
					_, _ = io.WriteString(w, "127.0.0.2")
				case <-r.Context().Done():
				}
			}
			id := m2Create(t, f, "")
			m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":0,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, 200)
			path, revision := kiloProxiesURL+"/local-proxy/recheck", 1
			if mutation != "url" {
				m2Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":1}`, 200)
				path, revision = kiloRecycleURL+"/local-proxy/restore", 2
			}
			done := make(chan int, 1)
			go func() {
				status, _ := f.request(t, "POST", path, kiloManagementKey, fmt.Sprintf(`{"expected_revision":%d}`, revision))
				done <- status
			}()
			m3bAwait(t, started)
			// This real inference must complete while the independent probe is held.
			f.allowDirect = true
			status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"independent admission"}`)
			if status != 200 {
				t.Fatalf("probe held admission lock: %d %s", status, body)
			}
			var view []byte
			switch mutation {
			case "limit":
				view = m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":2,"proxy_limit":1}`, 200)
			case "clear":
				view = m2Request(t, f, "DELETE", kiloRecycleURL, `{"expected_revision":2,"confirm":true}`, 200)
			case "url":
				view = m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", fmt.Sprintf(`{"expected_revision":1,"url":%q}`, f.cfg.Kilo.Proxies[0].URL+"/changed"), 200)
			}
			disk := m3bDisk(t, f)
			close(finish)
			if status := <-done; status != 409 {
				t.Fatalf("stale probe=%d", status)
			}
			after := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
			if string(view) != string(after) || string(disk) != string(m3bDisk(t, f)) {
				t.Fatalf("stale probe overwritten new state: %s", after)
			}
			f.assertReleased(t, 1)
			t.Log("held network probe did not hold account admission; concurrent revision change rejected late result with 409")
		})
	}
}

func TestKiloM3BRecheckInflightAndRecovery(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	var unhealthy atomic.Bool
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-finish; kiloSuccessStream(w, r) }, true)
	f.allowDirect = true
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		if unhealthy.Load() {
			http.Error(w, "synthetic unhealthy", 502)
			return
		}
		_, _ = io.WriteString(w, "127.0.0.2")
	}
	m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"held"}`)
		done <- status
	}()
	m3bAwait(t, started)
	m2Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":1}`, 409)
	before := m3bDisk(t, f)
	unhealthy.Store(true)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":1}`, 502)
	view := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
	if ojson.GetBytes(view, "proxies.0.healthy").Bool() || ojson.GetBytes(view, "proxies.0.active").Int() != 1 || ojson.GetBytes(view, "revision").Uint() != 1 || string(before) != string(m3bDisk(t, f)) || f.probes.Load() != 2 {
		t.Fatalf("recheck=%s probes=%d", view, f.probes.Load())
	}
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"unhealthy must not direct"}`)
	if status != 503 || f.posts.Load() != 1 {
		t.Fatalf("health failure direct fallback=%d posts=%d", status, f.posts.Load())
	}
	close(finish)
	if status := <-done; status != 200 {
		t.Fatalf("recheck killed active request=%d", status)
	}
	f.assertReleased(t, 1)
	unhealthy.Store(false)
	view = m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":1}`, 200)
	if !ojson.GetBytes(view, "proxies.0.healthy").Bool() || ojson.GetBytes(view, "revision").Uint() != 1 || f.probes.Load() != 3 {
		t.Fatalf("recovered=%s", view)
	}
	m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", `{"expected_revision":1,"enabled":false}`, 200)
	view = m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":2}`, 200)
	if ojson.GetBytes(view, "proxies.0.enabled").Bool() || ojson.GetBytes(view, "proxies.0.healthy").Bool() || ojson.GetBytes(view, "revision").Uint() != 2 || f.probes.Load() != 4 {
		t.Fatalf("disabled recheck=%s", view)
	}
	t.Log("inflight delete=409; forced failure changes only memory health, no direct fallback or inflight interruption; explicit recovery bypasses cache; disabled remains disabled")
}

func TestKiloM3BClearPreservesInflightBindings(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-finish; kiloSuccessStream(w, r) }, true)
	id := m2Create(t, f, "")
	view := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q}`, f.cfg.Kilo.Proxies[0].URL), 201)
	pid := ojson.GetBytes(view, "proxies.1.id").String()
	m2Request(t, f, "DELETE", kiloProxiesURL+"/"+pid, `{"expected_revision":1}`, 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":2,"mode":"pool","exclusive_proxy_ids":[],"primary_proxy_id":"local-proxy"}`, 200)
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"held during clear"}`)
		done <- status
	}()
	m3bAwait(t, started)
	before := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
	m2Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":4}`, 409)
	if rejected := m2Request(t, f, "GET", kiloRecycleURL, "", 200); string(rejected) != string(before) {
		t.Fatal("rejected inflight archive changed existing recycle")
	}
	view = m2Request(t, f, "DELETE", kiloRecycleURL, `{"expected_revision":4,"confirm":true}`, 200)
	for _, field := range []string{"proxies", "accounts", "proxy_limit"} {
		if ojson.GetBytes(view, field).Raw != ojson.GetBytes(before, field).Raw {
			t.Fatalf("clear changed live %s", field)
		}
	}
	if ojson.GetBytes(view, "revision").Uint() != 5 || ojson.GetBytes(view, "proxies.0.active").Int() != 1 || ojson.GetBytes(view, "proxies.0.owner").String() != id || len(ojson.GetBytes(view, "recycle").Array()) != 0 {
		t.Fatalf("live clear=%s", view)
	}
	close(finish)
	if status := <-done; status != 200 {
		t.Fatalf("clear interrupted active=%d", status)
	}
	f.assertReleased(t, 1)
	t.Log("clear changed only recycle+revision while owner/active/primary/account policy stayed identical; existing inference completed and released")
}

func TestKiloM3BCancelledRecheckPreservesHealth(t *testing.T) {
	started, probeDone := make(chan struct{}), make(chan struct{})
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	var probeCount atomic.Int32
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		if probeCount.Add(1) == 1 {
			_, _ = io.WriteString(w, "127.0.0.2")
			return
		}
		close(started)
		<-r.Context().Done()
		close(probeDone)
	}
	m2Create(t, f, "")
	m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", `{"expected_revision":0,"name":"healthy"}`, 200)
	before := m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":1}`, 200)
	disk := m3bDisk(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+kiloProxiesURL+"/local-proxy/recheck", strings.NewReader(`{"expected_revision":1}`))
	req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := f.server.Client().Do(req)
		if err == nil {
			_ = res.Body.Close()
		}
	}()
	m3bAwait(t, started)
	cancel()
	m3bAwait(t, done)
	m3bAwait(t, probeDone)
	view := m2Request(t, f, "GET", kiloRecycleURL, "", 200)
	if string(view) != string(before) || string(disk) != string(m3bDisk(t, f)) || !ojson.GetBytes(view, "proxies.0.healthy").Bool() || f.probes.Load() != 2 {
		t.Fatalf("cancel changed previous health=%s", view)
	}
}
