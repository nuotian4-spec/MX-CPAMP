package cliproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"gopkg.in/yaml.v3"
)

const kiloProxiesURL = "/v8/management/kilo/proxies"

func TestKiloM3ManagementPoolTransportReload(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	data := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(data, "revision").Uint() != 0 || ojson.GetBytes(data, "managed").Bool() {
		t.Fatalf("initial view=%s", data)
	}
	data = m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":0,"url":%q,"name":"second"}`, f.cfg.Kilo.Proxies[0].URL), 201)
	pid := ojson.GetBytes(data, "proxies.1.id").String()
	if !strings.HasPrefix(pid, "kilo-proxy-") {
		t.Fatalf("id=%q", pid)
	}
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":0,"proxy_limit":1}`, 409)
	id := m2Create(t, f, `,"name":"managed"`)
	data = m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", fmt.Sprintf(`{"expected_revision":1,"mode":"pool","exclusive_proxy_ids":[%q],"primary_proxy_id":%q}`, pid, pid), 200)
	if ojson.GetBytes(data, "runtime_revision").Uint() != 2 {
		t.Fatalf("view=%s", data)
	}
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"hello"}`)
	if status != 200 {
		t.Fatalf("inference=%d %s", status, body)
	}
	data = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(data, "revision").Uint() != 3 || ojson.GetBytes(data, "runtime_revision").Uint() != 3 {
		t.Fatalf("auto revision=%s", data)
	}
	if f.posts.Load() != 1 || f.connects.Load() != 1 {
		t.Fatalf("posts=%d connects=%d", f.posts.Load(), f.connects.Load())
	}
	raw, err := os.ReadFile(filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), pid) || strings.Contains(string(data), `"url"`) {
		t.Fatalf("state/view=%s %s", raw, data)
	}
	f.cfg.Kilo.Proxies = nil
	f.cfg.Kilo.ProxyLimit = 99
	m2Reload(t, f)
	data = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(data, "revision").Uint() != 3 || ojson.GetBytes(data, "proxy_limit").Int() != 2 || ojson.GetBytes(data, "proxies.1.account_id").String() != id {
		t.Fatalf("reload=%s", data)
	}
	status, body = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","previous_response_id":"resp-kilo","input":"again"}`)
	if status != 200 {
		t.Fatalf("reload inference=%d %s", status, body)
	}
	t.Logf("management->Service->Manager->KiloExecutor->CONNECT->local TLS PASS; revision=3 persisted/reloaded; posts=%d connects=%d", f.posts.Load(), f.connects.Load())
}

func m3Wait(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("expected runtime state not observed")
}
func TestKiloM3DirectSharesAccountAdmission(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-finish; kiloSuccessStream(w, r) }, true)
	f.allowDirect = true
	id := m2Create(t, f, `,"concurrency_limit":1`)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":0,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, 200)
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"direct"}`)
		done <- status
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("direct did not reach local TLS")
	}
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":1,"mode":"pool","exclusive_proxy_ids":[],"primary_proxy_id":""}`, 409)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"wait"}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		response, err := f.server.Client().Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	m3Wait(t, func() bool {
		for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			if a.AuthID == id && a.Active == 1 && f.manager.CredentialConcurrencySnapshot()["pool_waiting"].(int) > 0 {
				return true
			}
		}
		return false
	})
	if f.posts.Load() != 1 || f.probes.Load() != 0 || f.connects.Load() != 0 {
		t.Fatalf("direct counters posts=%d probes=%d connects=%d", f.posts.Load(), f.probes.Load(), f.connects.Load())
	}
	cancel()
	<-waitDone
	close(finish)
	if status := <-done; status != 200 {
		t.Fatalf("direct=%d", status)
	}
	m3Wait(t, func() bool {
		for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			if a.Active != 0 {
				return false
			}
		}
		return true
	})
	m2Reload(t, f)
	view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(view, "accounts.0.mode").String() != "direct" {
		t.Fatalf("reload=%s", view)
	}
	t.Log("direct bypassed proxy probes/CONNECT while retaining AuthID active=1; second request waited and cancelled; mode reloaded")
}
func TestKiloM3InflightMutationLimitAndDisable(t *testing.T) {
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
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"held"}`)
		done <- status
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy request not started")
	}
	for _, body := range []string{fmt.Sprintf(`{"expected_revision":1,"url":%q}`, f.cfg.Kilo.Proxies[0].URL+"/changed"), fmt.Sprintf(`{"expected_revision":1,"account_id":%q}`, id)} {
		m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", body, 409)
	}
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":1,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, 409)
	view := m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":1,"proxy_limit":1}`, 200)
	if ojson.GetBytes(view, "runtime_revision").Uint() != 2 || ojson.GetBytes(view, "proxies.0.active").Int() != 1 {
		t.Fatalf("limit=%s", view)
	}
	view = m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", `{"expected_revision":2,"enabled":false}`, 200)
	if ojson.GetBytes(view, "proxies.0.healthy").Bool() || ojson.GetBytes(view, "proxies.0.active").Int() != 1 || ojson.GetBytes(view, "runtime_revision").Uint() != 3 {
		t.Fatalf("disable=%s", view)
	}
	close(finish)
	if status := <-done; status != 200 {
		t.Fatalf("active interrupted=%d", status)
	}
	f.assertReleased(t, 1)
	before := f.posts.Load()
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"disabled"}`)
	if status != 503 || f.posts.Load() != before {
		t.Fatalf("disabled admitted=%d", status)
	}
	t.Log("URL/ownership/mode mutation 409; limit/disable saved=runtime revision3 with active request preserved; new request rejected")
}
func TestKiloM3StorageFailureAndLoadFailure(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":0,"proxy_limit":1}`, 200)
	path := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":1,"proxy_limit":9}`, 503)
	view := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(view, "revision").Uint() != 1 || ojson.GetBytes(view, "proxy_limit").Int() != 1 {
		t.Fatalf("failed save published=%s", view)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("old disk state changed")
	}
	if err = os.WriteFile(path, []byte(`{"revision":"broken"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m2Reload(t, f)
	m2Request(t, f, "GET", kiloProxiesURL, "", 503)
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"fail closed"}`)
	if status != 503 || f.posts.Load() != 0 || f.probes.Load() != 0 {
		t.Fatalf("load failure downgraded=%d", status)
	}
}
func TestKiloM3ConfigSecretProjectionAndManagedGuard(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.cfg.Kilo.Proxies[0].URL = "http://private-user:private-pass@127.0.0.1:12345"
	raw, err := yaml.Marshal(map[string]any{"auth-dir": f.cfg.AuthDir, "kilo": f.cfg.Kilo})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v8/management/config", "/v8/management/config.yaml", "/v0/management/config", "/v0/management/config.yaml"} {
		data := m2Request(t, f, "GET", path, "", 200)
		if strings.Contains(string(data), "private-user") || strings.Contains(string(data), "private-pass") || strings.Contains(string(data), "12345") {
			t.Fatalf("config secret leaked via %s", path)
		}
	}
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":0,"proxy_limit":1}`, 200)
	m2Request(t, f, "PATCH", "/v8/management/config/upstream/kilo", `{"proxy-limit":77}`, 409)
	for _, path := range []string{"/v8/management/config.yaml", "/v0/management/config.yaml"} {
		data := m2Request(t, f, "GET", path, "", 200)
		m2Request(t, f, "PUT", path, string(data), 200)
		persisted, e := os.ReadFile(f.configPath)
		if e != nil || !strings.Contains(string(persisted), "private-pass") {
			t.Fatalf("redacted roundtrip lost secret via %s", path)
		}
	}
}

func TestKiloM3HealthyExtraPromotionThroughTransport(t *testing.T) {
	firstStarted, secondStarted := make(chan struct{}), make(chan struct{})
	failFirst, finishSecond := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-failFirst:
		default:
			close(failFirst)
		}
		select {
		case <-finishSecond:
		default:
			close(finishSecond)
		}
	}()
	var calls atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			close(firstStarted)
			<-failFirst
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		if n == 2 {
			close(secondStarted)
			<-finishSecond
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(kiloTerminal, "resp-kilo", fmt.Sprintf("resp-m3-%d", n)))
	}, true)
	id := m2Create(t, f, "")
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":0,"proxy_limit":1}`, 200)
	view := m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":1,"url":%q}`, f.cfg.Kilo.Proxies[0].URL), 201)
	extra := ojson.GetBytes(view, "proxies.1.id").String()
	view = m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":2,"url":%q}`, f.cfg.Kilo.Proxies[0].URL), 201)
	third := ojson.GetBytes(view, "proxies.2.id").String()
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/egress", `{"expected_revision":3,"mode":"pool","exclusive_proxy_ids":[],"primary_proxy_id":"local-proxy"}`, 200)
	first, second := make(chan int, 1), make(chan int, 1)
	go func() {
		s, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"one"}`)
		first <- s
	}()
	select {
	case <-firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first request not started")
	}
	go func() {
		s, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"two"}`)
		second <- s
	}()
	select {
	case <-secondStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("extra request not started")
	}
	view = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(view, "proxies.1.active").Int() != 1 || ojson.GetBytes(view, "accounts.0.primary_proxy_id").String() != "local-proxy" {
		t.Fatalf("extra before failure=%s", view)
	}
	close(failFirst)
	if s := <-first; s != 502 {
		t.Fatalf("failed POST=%d", s)
	}
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"three"}`)
	if status != 200 {
		t.Fatalf("third=%d %s", status, body)
	}
	view = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	if ojson.GetBytes(view, "accounts.0.primary_proxy_id").String() != extra || ojson.GetBytes(view, "proxies.1.active").Int() != 1 || ojson.GetBytes(view, "revision").Uint() != 8 {
		t.Fatalf("extra promotion=%s", view)
	}
	state, managed, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || !managed || state.Accounts[id].PrimaryProxyID != extra || state.Proxies[2].ID != third || state.Proxies[2].FirstUsed != 3 {
		t.Fatalf("persisted promotion=%+v %v", state, err)
	}
	close(finishSecond)
	if s := <-second; s != 200 {
		t.Fatalf("second=%d", s)
	}
	f.assertReleased(t, 3)
	if f.posts.Load() != 3 {
		t.Fatalf("failed POST replayed=%d", f.posts.Load())
	}
	t.Log("primary POST failed once; saturated healthy extra promoted and persisted revision7, third extra first-use revision8; all leases released")
}

func TestKiloM3ExclusiveReleaseAndWriteContract(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	a, b := m2Create(t, f, ""), m2Create(t, f, "")
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key"} {
		status, _ := f.request(t, "GET", kiloProxiesURL, key, "")
		if status != 401 {
			t.Fatalf("management auth=%d", status)
		}
	}
	for _, body := range []string{`{}`, `{"expected_revision":null}`, `{"expected_revision":0.5}`, `{"expected_revision":-1}`, `{"expected_revision":0,"id":"client-id","url":"http://127.0.0.1:1"}`, `{"expected_revision":0,"url":"direct"}`} {
		m2Request(t, f, "POST", kiloProxiesURL, body, 400)
	}
	m2Request(t, f, "PUT", kiloAccountsURL+"/missing/egress", `{"expected_revision":0,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, 400)
	view := m2Request(t, f, "PUT", kiloAccountsURL+"/"+a+"/egress", `{"expected_revision":0,"mode":"pool","exclusive_proxy_ids":["local-proxy"],"primary_proxy_id":"local-proxy"}`, 200)
	if ojson.GetBytes(view, "proxies.0.account_id").String() != a {
		t.Fatalf("exclusive=%s", view)
	}
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+b+"/egress", `{"expected_revision":1,"mode":"pool","exclusive_proxy_ids":["local-proxy"],"primary_proxy_id":"local-proxy"}`, 400)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+b+"/egress", `{"expected_revision":1,"mode":"pool","exclusive_proxy_ids":[],"primary_proxy_id":"local-proxy"}`, 400)
	view = m2Request(t, f, "PUT", kiloAccountsURL+"/"+a+"/egress", `{"expected_revision":1,"mode":"direct","exclusive_proxy_ids":[],"primary_proxy_id":""}`, 200)
	if ojson.GetBytes(view, "proxies.0.account_id").String() != "" {
		t.Fatalf("direct did not release exclusive=%s", view)
	}
	view = m2Request(t, f, "POST", kiloProxiesURL, `{"expected_revision":2,"url":"http://private-user:private-pass@127.0.0.1:7890","name":"secret"}`, 201)
	if strings.Contains(string(view), "private-user") || strings.Contains(string(view), "private-pass") || strings.Contains(string(view), `"url"`) {
		t.Fatalf("proxy secret leaked=%s", view)
	}
	if !ojson.GetBytes(view, "proxies.1.has_auth").Bool() || ojson.GetBytes(view, "proxies.1.host").String() != "127.0.0.1" {
		t.Fatalf("projection=%s", view)
	}
}
