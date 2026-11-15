package cliproxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const kiloDeviceURL = "/v8/management/kilo/device-auth"
const kiloDevicePrivateCode = "private/device ?#"
const kiloDeviceToken = "synthetic-device-secret"

func m4DeviceUpstream(t *testing.T, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-KILOCODE-MACHINEID") != "" {
			t.Error("device protocol header/body mismatch")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/api/device-auth/codes" {
			_, _ = io.WriteString(w, `{"code":"private/device ?#","user_code":"USER-LOCAL","verificationUrl":"https://app.kilo.ai/device","expiresIn":600}`)
			return
		}
		if r.Method != "GET" || r.URL.EscapedPath() != "/api/device-auth/codes/private%2Fdevice%20%3F%23" {
			t.Errorf("poll route=%s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"token":"synthetic-device-secret","userEmail":"local@example.invalid"}`)
	}
}
func m4Request(t *testing.T, f *kiloNativeFixture, method, path, body string, want int) []byte {
	t.Helper()
	data := m2Request(t, f, method, path, body, want)
	for _, secret := range []string{kiloDevicePrivateCode, kiloDeviceToken, "machine_id", "auth_method"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("private field leaked: %q", secret)
		}
	}
	return data
}
func m4Start(t *testing.T, f *kiloNativeFixture, mode string) string {
	t.Helper()
	data := m4Request(t, f, "POST", kiloDeviceURL, `{"name":" device account ","authorization_egress":{"mode":"`+mode+`"},"priority":7,"concurrency_limit":2,"enabled":true}`, 200)
	if ojson.GetBytes(data, "interval").Int() != 3 || ojson.GetBytes(data, "inference_egress_mode").String() != "pool" {
		t.Fatalf("start=%s", data)
	}
	return ojson.GetBytes(data, "id").String()
}
func TestKiloDeviceNativeRouterDirectAndPool(t *testing.T) {
	for _, mode := range []string{"direct", "pool"} {
		t.Run(mode, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.allowDirect = true
			var calls atomic.Int32
			f.deviceHandler = m4DeviceUpstream(t, &calls)
			if mode == "pool" {
				m4Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":0}`, 200)
			}
			id := m4Start(t, f, mode)
			poll := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "{}", 200)
			if ojson.GetBytes(poll, "status").String() != "approved" || ojson.GetBytes(poll, "email").String() != "local@example.invalid" {
				t.Fatalf("poll=%s", poll)
			}
			m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
			beforeProbes := f.probes.Load()
			data := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "{}", 201)
			accountID := ojson.GetBytes(data, "id").String()
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(accountID) })
			if accountID == "" || ojson.GetBytes(data, "egress_mode").String() != "pool" || f.probes.Load() != beforeProbes {
				t.Fatalf("save=%s", data)
			}
			want := "pending"
			if mode == "pool" {
				want = "available"
			}
			if ojson.GetBytes(data, "egress_status").String() != want {
				t.Fatalf("egress=%s", data)
			}
			again := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "", 201)
			if ojson.GetBytes(again, "id").String() != accountID || len(f.manager.List()) != 1 {
				t.Fatal("duplicate save created another account")
			}
			cancelled := m4Request(t, f, "DELETE", kiloDeviceURL+"/"+id, "", 200)
			if ojson.GetBytes(cancelled, "status").String() != "saved" || ojson.GetBytes(cancelled, "account_id").String() != accountID {
				t.Fatal("cancel removed committed account")
			}
			raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, accountID))
			if err != nil {
				t.Fatal(err)
			}
			account, _ := f.manager.GetByID(accountID)
			machine := coreauth.KiloMachineID(account)
			if machine == "" || ojson.GetBytes(raw, "machine_id").String() != machine || ojson.GetBytes(raw, "api_key").String() != kiloDeviceToken || ojson.GetBytes(raw, "auth_method").String() != "device" || account.Label != "device account" {
				t.Fatal("single credential commit incomplete")
			}
			if len(registry.GetGlobalRegistry().GetModelsForClient(accountID)) == 0 {
				t.Fatal("Service did not publish models")
			}
			for _, p := range f.executor.KiloProxySnapshot().Proxies {
				if p.Active != 0 || p.Owner != "" {
					t.Fatal("device lease retained")
				}
			}
			state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{Limit: 2, Accounts: map[string]kilo.AccountEgress{}})
			if err != nil || len(state.Accounts) != 1 || state.Accounts[accountID].Mode != "pool" || len(state.PendingDevices) != 0 {
				t.Fatalf("account policy persistence mismatch: %v", err)
			}
			if mode == "pool" && (len(state.Proxies) != 1 || state.Proxies[0].FirstUsed != 1) {
				t.Fatal("proxy first_used not recorded independently")
			}
			m2Reload(t, f)
			reloaded, _ := f.manager.GetByID(accountID)
			if coreauth.KiloMachineID(reloaded) != machine || len(f.manager.List()) != 1 {
				t.Fatal("reload identity differs")
			}
			if calls.Load() != 2 || f.posts.Load() != 0 || (mode == "pool" && (f.deviceDirect.Load() != 0 || f.deviceConnects.Load() != 2)) || (mode == "direct" && (f.deviceDirect.Load() != 2 || f.deviceConnects.Load() != 0)) {
				t.Fatalf("calls=%d model=%d direct=%d CONNECT=%d", calls.Load(), f.posts.Load(), f.deviceDirect.Load(), f.deviceConnects.Load())
			}
			t.Logf("real Router/device client/Transport/atomic save/register/reload PASS: codes=%d model=0 direct=%d CONNECT=%d accounts=1", calls.Load(), f.deviceDirect.Load(), f.deviceConnects.Load())
		})
	}
}
func TestKiloDeviceNativeValidationAndSaveRetry(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowDirect = true
	var calls atomic.Int32
	f.deviceHandler = m4DeviceUpstream(t, &calls)
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key", "synthetic-bps-key"} {
		status, _ := f.request(t, "POST", kiloDeviceURL, key, `{"name":"x","authorization_egress":{"mode":"direct"}}`)
		if status != 401 {
			t.Fatalf("management auth=%d", status)
		}
	}
	for _, body := range []string{`{}`, `{"name":"x"}`, `{"name":"x","authorization_egress":{"mode":"auto"}}`, `{"name":"x","api_key":"secret","authorization_egress":{"mode":"direct"}}`, `{"name":"x","machine_id":"x","authorization_egress":{"mode":"direct"}}`, `{"name":"x","authorization_egress":{"mode":"direct","url":"https://kilo.ai"}}`} {
		m4Request(t, f, "POST", kiloDeviceURL, body, 400)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid start reached upstream")
	}
	id := m4Start(t, f, "direct")
	m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "", 409)
	m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", `{"token":"x"}`, 400)
	m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
	f.manager.SetStore(m2FailStore{Store: m2FileStore(f), save: true})
	m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "", 503)
	if len(f.manager.List()) != 0 {
		t.Fatal("failed save half-registered")
	}
	poll := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/poll", "", 200)
	if ojson.GetBytes(poll, "status").String() != "approved" {
		t.Fatal("failed save consumed approval")
	}
	f.manager.SetStore(m2FileStore(f))
	data := m4Request(t, f, "POST", kiloDeviceURL+"/"+id+"/save", "", 201)
	accountID := ojson.GetBytes(data, "id").String()
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(accountID) })
	files, _ := os.ReadDir(f.cfg.AuthDir)
	if len(files) != 1 || f.posts.Load() != 0 || calls.Load() != 2 {
		t.Fatal("save retry created extra file/network")
	}
}

func TestKiloDeviceNativePoolNoImplicitDirect(t *testing.T) {
	for _, kind := range []string{"empty", "unhealthy", "full", "exclusive"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream, true)
			f.allowDirect = true
			var calls atomic.Int32
			f.deviceHandler = m4DeviceUpstream(t, &calls)
			switch kind {
			case "empty":
				m4Request(t, f, "DELETE", kiloProxiesURL+"/local-proxy", `{"expected_revision":0}`, 200)
			case "unhealthy":
				f.probeHandler = func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad", 502) }
				m4Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":0}`, 502)
			case "full", "exclusive":
				accountID := m2Create(t, f, "")
				if kind == "exclusive" {
					m4Request(t, f, "PUT", kiloAccountsURL+"/"+accountID+"/egress", `{"expected_revision":0,"mode":"pool","exclusive_proxy_ids":["local-proxy"],"primary_proxy_id":"local-proxy"}`, 200)
				}
				view := m4Request(t, f, "GET", kiloProxiesURL, "", 200)
				m4Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(view, "revision").Uint()), 200)
				if kind == "full" {
					a, _ := f.manager.GetByID(accountID)
					_, release, wait, err := f.executor.TryAcquireEgress(a)
					if err != nil || wait {
						t.Fatalf("reserve %v %v", wait, err)
					}
					defer release()
				}
			}
			m4Request(t, f, "POST", kiloDeviceURL, `{"name":"x","authorization_egress":{"mode":"pool"}}`, 503)
			if calls.Load() != 0 || f.deviceDirect.Load() != 0 || f.posts.Load() != 0 {
				t.Fatal("unavailable pool fell back to direct")
			}
		})
	}
}

func TestKiloDeviceNativeFirstUsePersistenceFailure(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowDirect = true
	var calls atomic.Int32
	f.deviceHandler = m4DeviceUpstream(t, &calls)
	m4Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", `{"expected_revision":0,"proxy_limit":2}`, 200)
	m4Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", `{"expected_revision":1}`, 200)
	path := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	m4Request(t, f, "POST", kiloDeviceURL, `{"name":"x","authorization_egress":{"mode":"pool"}}`, 503)
	for _, p := range f.executor.KiloProxySnapshot().Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatal("failed first-use retained lease")
		}
	}
	if calls.Load() != 0 || f.deviceDirect.Load() != 0 || f.deviceConnects.Load() != 0 || f.posts.Load() != 0 {
		t.Fatal("failed first-use sent traffic")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed allocation changed durable state")
	}
}

func TestKiloDeviceNativeSlowPollDoesNotBlockAccountAndCancelReleases(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	// Two pool entries allow distinct owners while one authorization HTTP is held.
	f.cfg.Kilo.Proxies = append(f.cfg.Kilo.Proxies, config.KiloProxy{ID: "other-proxy", URL: f.cfg.Kilo.Proxies[0].URL})
	id := m2Create(t, f, "")
	view := m4Request(t, f, "GET", kiloProxiesURL, "", 200)
	for _, proxyID := range []string{"local-proxy", "other-proxy"} {
		view = m4Request(t, f, "POST", kiloProxiesURL+"/"+proxyID+"/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(view, "revision").Uint()), 200)
	}
	var calls atomic.Int32
	base := m4DeviceUpstream(t, &calls)
	entered, closed := make(chan struct{}), make(chan struct{})
	f.deviceHandler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			base(w, r)
			return
		}
		close(entered)
		<-r.Context().Done()
		close(closed)
	}
	session := m4Start(t, f, "pool")
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", kiloDeviceURL+"/"+session+"/poll", kiloManagementKey, "{}")
		done <- status
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow poll not entered")
	}
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"independent account"}`)
	if status != 200 {
		t.Fatalf("slow authorization blocked account: %d %s", status, body)
	}
	data := m4Request(t, f, "DELETE", kiloDeviceURL+"/"+session, "", 200)
	if ojson.GetBytes(data, "status").String() != "cancelled" {
		t.Fatal("cancel status")
	}
	select {
	case status = <-done:
		if status != 200 {
			t.Fatalf("late poll=%d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel failed to finish poll")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream socket retained after cancel")
	}
	for _, p := range f.executor.KiloProxySnapshot().Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatal("cancel retained pool owner")
		}
	}
	state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
	if err != nil || len(state.Accounts) != 1 || state.Accounts[id].Mode != "pool" {
		t.Fatal("temporary authorization bound in persisted state")
	}
	// A canceled authorization leaves the same pool available for later account work.
	a, _ := f.manager.GetByID(id)
	_, release, wait, err := f.executor.TryAcquireEgress(a)
	if err != nil || wait {
		t.Fatal("pool did not recover")
	}
	release()
}
