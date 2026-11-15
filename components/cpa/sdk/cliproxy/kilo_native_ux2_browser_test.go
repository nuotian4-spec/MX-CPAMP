package cliproxy

import (
	"context"
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

func TestKiloNativeUX2BrowserHost(t *testing.T) {
	goBin, managerDir := os.Getenv("KILO_JOINT_GO_BIN"), os.Getenv("KILO_JOINT_MANAGER_DIR")
	if os.Getenv("KILO_BROWSER_RECEIPT") == "" || os.Getenv("KILO_BROWSER_PANEL") == "" || goBin == "" || managerDir == "" {
		t.Skip("UX2 browser host requires explicit isolated inputs")
	}
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	f.allowAutoFree = true
	var used, usageCalls, resetCalls, consumeCalls, deviceCalls, profileCalls atomic.Int32
	var usageStatus, resetStatus, credits atomic.Int32
	var approved, consumeUnknown atomic.Bool
	used.Store(25)
	credits.Store(3)
	usageStatus.Store(200)
	resetStatus.Store(200)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /backend-api/wham/usage":
			usageCalls.Add(1)
			if status := usageStatus.Load(); status != 200 {
				w.WriteHeader(int(status))
				_, _ = io.WriteString(w, `{"error":"合成额度查询失败"}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"plan_type":"plus","rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":%d,"reset_after_seconds":10800},"secondary_window":{"limit_window_seconds":604800,"used_percent":40,"reset_after_seconds":360000}}}`, used.Load())
		case "GET /backend-api/wham/rate-limit-reset-credits":
			resetCalls.Add(1)
			if status := resetStatus.Load(); status != 200 {
				w.WriteHeader(int(status))
				_, _ = io.WriteString(w, `{"error":"合成重置次数查询失败"}`)
				return
			}
			doc := ojson.NewDocString(`{"credits":[]}`)
			_ = doc.Set("available_count", credits.Load())
			for i := int32(0); i < credits.Load(); i++ {
				_ = doc.Set(fmt.Sprintf("credits.%d", i), map[string]any{"id": fmt.Sprintf("ux2-credit-%d", i+1), "reset_type": "codex_rate_limits", "status": "available", "granted_at": "2026-11-14T00:00:00Z", "expires_at": "2030-12-31T00:00:00Z"})
			}
			_, _ = w.Write(doc.Bytes())
		case "POST /backend-api/wham/rate-limit-reset-credits/consume":
			consumeCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if ojson.GetBytes(body, "redeem_request_id").String() == "" || credits.Load() <= 0 {
				w.WriteHeader(409)
				_, _ = io.WriteString(w, `{"error":"合成重置请求无可用次数"}`)
				return
			}
			credits.Add(-1)
			used.Store(0)
			if consumeUnknown.Swap(false) {
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"error":"合成重置已接收但回执未知"}`)
				return
			}
			_, _ = io.WriteString(w, `{"code":"reset"}`)
		default:
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":"未登记的本地替身路径"}`)
		}
	}
	f.profileHandler = func(w http.ResponseWriter, r *http.Request) {
		profileCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"organizations":[{"id":"ux2-org-a","name":"合成组织 A"}],"hasPersonalAccount":true}`)
	}
	f.deviceHandler = func(w http.ResponseWriter, r *http.Request) {
		deviceCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"code":"ux2-local-device","user_code":"UX2-LOCAL","verificationUrl":"https://app.kilo.ai/device","expiresIn":600}`)
		} else if approved.Load() {
			_, _ = io.WriteString(w, `{"token":"synthetic-kilo-token","userEmail":"ux2@example.invalid"}`)
		} else {
			w.WriteHeader(202)
			_, _ = io.WriteString(w, `{"status":"pending"}`)
		}
	}
	var gateMu sync.Mutex
	gate := make(chan struct{})
	close(gate)
	var posts, active, peak atomic.Int32
	ids := ux2NativeIPs(t, f, 6, func(_ int, w http.ResponseWriter, r *http.Request) {
		n := posts.Add(1)
		value := active.Add(1)
		for old := peak.Load(); value > old && !peak.CompareAndSwap(old, value); old = peak.Load() {
		}
		gateMu.Lock()
		wait := gate
		gateMu.Unlock()
		select {
		case <-wait:
		case <-r.Context().Done():
			active.Add(-1)
			return
		}
		active.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/api/v1/chat/completions" {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"UX2 合成 Auto Free 结果\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("ux2-browser-%d", n))
		_ = doc.Set("response.output.0.content.0.text", "UX2 合成模型结果；本地容量链路已完成。")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	autoID := m2Create(t, f, `,"name":"UX2 自动扩容 · 最多 5 IP"`)
	ux2NativePolicy(t, f, autoID, 5)
	specifiedID := m2Create(t, f, `,"name":"UX2 指定 IP · 203.0.113.6","enabled":false`)
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+specifiedID+"/egress", fmt.Sprintf(`{"expected_revision":%d,"mode":"auto","exclusive_proxy_ids":[],"primary_proxy_id":"","capacity_mode":"ip","allocation_mode":"specified","max_ips":1,"specified_ips":["203.0.113.6"]}`, ojson.GetBytes(state, "revision").Uint()), 200)
	manualID := m2Create(t, f, `,"name":"UX2 固定模式 · 人工停用","enabled":false`)
	m4oDirect(t, f, manualID)
	m5Codex(t, f, "source-ux2.json", "outer-ux2", "synthetic-outer-ux2", false, false)
	m5Bind(t, f, autoID, 0, `{"kind":"cpa_codex","credential_id":"source-ux2.json","chatgpt_account_id":"outer-ux2"}`)
	m5Query(t, f, autoID, 1)
	responseGate := &kiloBrowserResponseGate{requests: map[string]map[string]int{"a": {}, "b": {}}}
	startKiloBrowserServer(t, f, responseGate, "a")
	defer f.server.Close()
	peer := newKiloBrowserPeer(t, f.cfg)
	startKiloBrowserServer(t, peer, responseGate, "b")
	defer peer.server.Close()
	peerID := m2Create(t, peer, `,"name":"UX2 独立实例 B"`)
	stop := make(chan struct{})
	var stopOnce sync.Once
	var controlMu sync.Mutex
	holding := false
	count := 0
	done := make(chan ux2Reply, 100)
	release := func() error {
		if !holding {
			return nil
		}
		gateMu.Lock()
		close(gate)
		gateMu.Unlock()
		holding = false
		for i := 0; i < count; i++ {
			select {
			case result := <-done:
				if result.err != nil || result.status != 200 {
					return fmt.Errorf("推理状态 %d: %v", result.status, result.err)
				}
			case <-time.After(15 * time.Second):
				return fmt.Errorf("释放未完成")
			}
		}
		count = 0
		f.manager.SyncKiloAutoEgress(time.Now())
		return nil
	}
	defer func() { controlMu.Lock(); defer controlMu.Unlock(); _ = release() }()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method+" "+r.URL.Path == "GET /wait-stop" {
			select {
			case <-stop:
				_, _ = io.WriteString(w, `{"stopped":true}`)
			case <-r.Context().Done():
			}
			return
		}
		controlMu.Lock()
		defer controlMu.Unlock()
		doc := ojson.NewDocString(`{}`)
		switch r.Method + " " + r.URL.Path {
		case "GET /state", "GET /fixture":
			_ = doc.Set("fixture", "kilo-ux2-browser-fixture-01")
			_ = doc.Set("account_ids", map[string]string{"auto": autoID, "specified": specifiedID, "manual": manualID, "peer": peerID})
			_ = doc.Set("proxy_ids", ids)
			_ = doc.Set("model_posts", posts.Load())
			_ = doc.Set("active", active.Load())
			_ = doc.Set("peak", peak.Load())
			_ = doc.Set("usage_calls", usageCalls.Load())
			_ = doc.Set("reset_query_calls", resetCalls.Load())
			_ = doc.Set("consume_calls", consumeCalls.Load())
			_ = doc.Set("device_calls", deviceCalls.Load())
			_ = doc.Set("profile_calls", profileCalls.Load())
			_ = doc.Set("used_percent", used.Load())
			_ = doc.Set("available_credits", credits.Load())
			_ = doc.Set("concurrency", f.manager.CredentialConcurrencySnapshot())
			if view, err := f.executor.KiloProxyState(); err == nil {
				_ = doc.Set("pool", view)
			}
		case "POST /options":
			body, _ := io.ReadAll(r.Body)
			for key, target := range map[string]*atomic.Int32{"used_percent": &used, "usage_status": &usageStatus, "reset_status": &resetStatus, "available_credits": &credits} {
				if value := ojson.GetBytes(body, key); value.Exists() {
					target.Store(int32(value.Int()))
				}
			}
			if value := ojson.GetBytes(body, "device_approved"); value.Exists() {
				approved.Store(value.Bool())
			}
			if value := ojson.GetBytes(body, "consume_unknown_once"); value.Exists() {
				consumeUnknown.Store(value.Bool())
			}
			_ = doc.Set("ok", true)
		case "POST /hold":
			if holding {
				http.Error(w, "已有挂起请求", 409)
				return
			}
			body, _ := io.ReadAll(r.Body)
			count = int(ojson.GetBytes(body, "count").Int())
			if count < 1 || count > 100 {
				http.Error(w, "count 须为 1-100", 400)
				return
			}
			gateMu.Lock()
			gate = make(chan struct{})
			gateMu.Unlock()
			holding = true
			for i := 0; i < count; i++ {
				go func(n int) {
					done <- ux2Request(context.Background(), f, fmt.Sprintf("ux2-browser-hold-%d-%d", posts.Load(), n), `{"model":"gpt-6.1-sol","input":"本地容量验收"}`)
				}(i)
			}
			_ = doc.Set("started", count)
		case "POST /release":
			if err := release(); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_ = doc.Set("active", active.Load())
		case "POST /sync":
			f.manager.SyncKiloAutoEgress(time.Now())
			_ = doc.Set("ok", true)
		case "POST /stop":
			_ = release()
			stopOnce.Do(func() { close(stop) })
			_ = doc.Set("stopping", true)
		default:
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(doc.Bytes())
	}))
	defer control.Close()
	command := exec.Command(goBin, "test", "-v", "-count=1", "-timeout=24h", "-run", "^TestKiloManagerBrowserHost$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(os.Environ(), "KILO_JOINT_CPA_URL="+f.server.URL, "KILO_BROWSER_CPA_ALIAS="+peer.server.URL, "KILO_JOINT_CONTROL_URL="+control.URL, "KILO_JOINT_MANAGEMENT_KEY="+kiloManagementKey)
	output, err := command.CombinedOutput()
	t.Logf("UX2 浏览器 Manager:\n%s", output)
	if err != nil {
		t.Fatalf("UX2 浏览器宿主: %v", err)
	}
}
