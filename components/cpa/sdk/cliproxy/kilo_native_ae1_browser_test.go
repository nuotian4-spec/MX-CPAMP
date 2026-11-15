package cliproxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

// TestKiloNativeAE1BrowserHost uses independent temporary stores and random loopback ports.
func TestKiloNativeAE1BrowserHost(t *testing.T) {
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if os.Getenv("KILO_BROWSER_RECEIPT") == "" || os.Getenv("KILO_BROWSER_PANEL") == "" || goBin == "" || managerDir == "" {
		t.Skip("AE1 browser host requires explicit isolated inputs")
	}
	var used, usageCalls, deviceCalls atomic.Int32
	used.Store(25)
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "203.0.113.10") }
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		usageCalls.Add(1)
		_, _ = fmt.Fprintf(w, `{"plan_type":"plus","rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":%d,"reset_after_seconds":1800},"secondary_window":{"limit_window_seconds":604800,"used_percent":30,"reset_after_seconds":3600}}}`, used.Load())
	}
	f.deviceHandler = func(w http.ResponseWriter, r *http.Request) {
		deviceCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"code":"ae1-private-device","user_code":"AE1-LOCAL","verificationUrl":"https://app.kilo.ai/device","expiresIn":600}`)
		} else {
			_, _ = io.WriteString(w, `{"token":"synthetic-kilo-token","userEmail":"ae1@example.invalid"}`)
		}
	}
	manual := m2Create(t, f, `,"name":"既有手工账号 · 保持直连","enabled":false`)
	m4oDirect(t, f, manual)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	rev := ojson.GetBytes(state, "revision").Uint()
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"auto_assignment_enabled":true,"max_unique_accounts":2}`, rev), 200)
	state = m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	rev = ojson.GetBytes(state, "revision").Uint()
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, rev), 200)
	m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", fmt.Sprintf(`{"expected_revision":%d,"fixed_exit_ip":"203.0.113.10","name":"自动独占 · 固定公网IP"}`, rev), 200)
	active := m2Create(t, f, `,"name":"自动 A · 已分配固定IP"`)
	waiting := m2Create(t, f, `,"name":"自动 B · 等待空闲IP"`)
	m5Codex(t, f, "source-ae1.json", "outer-ae1", "synthetic-outer-ae1", false, false)
	m5Bind(t, f, active, 0, `{"kind":"cpa_codex","credential_id":"source-ae1.json","chatgpt_account_id":"outer-ae1"}`)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+active+"/source-policy", `{"expected_policy_revision":0,"enabled":true,"interval_seconds":1800}`, 200)
	m5Query(t, f, active, 1)
	gate := &kiloBrowserResponseGate{requests: map[string]map[string]int{"a": {}, "b": {}}}
	startKiloBrowserServer(t, f, gate, "a")
	defer f.server.Close()
	peer := newKiloBrowserPeer(t, f.cfg)
	startKiloBrowserServer(t, peer, gate, "b")
	defer peer.server.Close()
	peerID := m2Create(t, peer, `,"name":"实例 B · 自动默认关闭"`)
	f.manager.StartKiloRecovery(t.Context())
	defer f.manager.StopKiloRecovery()
	peer.manager.StartKiloRecovery(t.Context())
	defer peer.manager.StopKiloRecovery()
	stop := make(chan struct{})
	var once sync.Once
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /state":
			doc := ojson.NewDocString(`{}`)
			_ = doc.Set("account_ids", map[string]string{"active": active, "waiting": waiting, "manual": manual, "peer": peerID})
			_ = doc.Set("model_posts", f.posts.Load())
			_ = doc.Set("usage_calls", usageCalls.Load())
			_ = doc.Set("usage_percent", used.Load())
			_ = doc.Set("device_calls", deviceCalls.Load())
			_ = doc.Set("concurrency", f.manager.CredentialConcurrencySnapshot())
			view, err := f.executor.KiloProxyState()
			if err == nil {
				_ = doc.Set("pool", view)
			}
			_, _ = w.Write(doc.Bytes())
		case "POST /options":
			body, _ := io.ReadAll(r.Body)
			if value := ojson.GetBytes(body, "used_percent"); value.Exists() {
				used.Store(int32(value.Int()))
			}
			f.manager.SyncKiloAutoEgress(time.Now())
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "POST /sync":
			f.manager.SyncKiloAutoEgress(time.Now())
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "POST /stop":
			once.Do(func() { close(stop) })
			_, _ = io.WriteString(w, `{"stopping":true}`)
		case "GET /wait-stop":
			select {
			case <-stop:
				_, _ = io.WriteString(w, `{"stopped":true}`)
			case <-r.Context().Done():
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer control.Close()
	command := exec.Command(goBin, "test", "-v", "-count=1", "-timeout=2h", "-run", "^TestKiloManagerBrowserHost$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(os.Environ(), "KILO_JOINT_CPA_URL="+f.server.URL, "KILO_BROWSER_CPA_ALIAS="+peer.server.URL, "KILO_JOINT_CONTROL_URL="+control.URL, "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("AE1 browser Manager:\n%s", output)
	if err != nil {
		t.Fatalf("AE1 browser host: %v", err)
	}
}
