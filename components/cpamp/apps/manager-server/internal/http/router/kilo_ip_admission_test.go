package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func TestKiloIPAdmissionProjection(t *testing.T) {
	for _, status := range []string{"legacy", "pending", "admitted"} {
		t.Run(status, func(t *testing.T) {
			var fixture map[string]any
			_ = json.Unmarshal([]byte(kiloProxyFixture), &fixture)
			proxy := fixture["proxies"].([]any)[0].(map[string]any)
			proxy["admission_status"], proxy["admission_reason"], proxy["admitted_ip"], proxy["admitted_at"] = status, "", "", nil
			if status == "pending" {
				proxy["admission_reason"] = "ip_changed"
			}
			if status != "legacy" {
				proxy["admitted_ip"], proxy["admitted_at"] = "203.0.113.10", "2026-11-15T00:00:00Z"
			}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				_ = json.NewEncoder(w).Encode(fixture)
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			state := kiloProxySnapshot(t, h)
			if state.Proxies[0].AdmissionStatus != status || state.Proxies[0].AdmissionReason != proxy["admission_reason"] || state.Proxies[0].AdmittedIP != proxy["admitted_ip"] {
				t.Fatal("接纳字段丢失")
			}
			if status != "legacy" && (state.Proxies[0].AdmittedAt == nil || *state.Proxies[0].AdmittedAt != "2026-11-15T00:00:00Z") {
				t.Fatal("接纳时间丢失")
			}
		})
	}
}

func TestKiloIPAdmissionPreviewRouter(t *testing.T) {
	var previews atomic.Int32
	response := `{"status":"ready","reason":"","observed_exit_ip":"203.0.113.10","observed_at":"2026-11-15T00:00:00Z","url":"SECRET_URL","password":"SECRET_PASS"}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			io.WriteString(w, kiloRouterConcurrencyFixture)
		case "/v8/management/kilo/proxies/preview":
			previews.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
				t.Error("预览方法或管理认证错误")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body) != 1 || body["url"] != "http://USER:SECRET_INPUT@127.0.0.1:8080" {
				t.Errorf("预览传递了非URL字段: %v", len(body))
			}
			io.WriteString(w, response)
		default:
			io.WriteString(w, kiloProxyFixture)
		}
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	state := kiloProxySnapshot(t, h)
	fields := map[string]any{"connection_ref": state.ConnectionRef, "url": "http://USER:SECRET_INPUT@127.0.0.1:8080"}
	denied := kiloAccountsWrite(h, "POST", "/api/kilo/proxies/preview", "CLIENT_KEY", fields)
	if denied.Code != 401 || previews.Load() != 0 {
		t.Fatal("推理Key触达预览")
	}
	out := kiloAccountsWrite(h, "POST", "/api/kilo/proxies/preview", kiloRouterAdminKey, fields)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"status":"ready"`) || !strings.Contains(out.Body.String(), `"observed_exit_ip":"203.0.113.10"`) {
		t.Fatalf("预览失败 %d %s", out.Code, out.Body)
	}
	assertKiloRouterSanitized(t, out)
	for _, key := range []string{"expected_revision", "admission_status", "admitted_ip", "unknown"} {
		fields[key] = "admitted"
		out = kiloAccountsWrite(h, "POST", "/api/kilo/proxies/preview", kiloRouterAdminKey, fields)
		if out.Code != 400 {
			t.Fatalf("预览未知字段未拒绝: %s %d", key, out.Code)
		}
		delete(fields, key)
	}
	if previews.Load() != 1 || kiloProxySnapshot(t, h).Revision != state.Revision {
		t.Fatal("无效字段触达上游或预览改变版本")
	}
	for _, raw := range []string{
		`{"status":"pending","reason":"SECRET","observed_exit_ip":null,"observed_at":null}`,
		`{"status":"ready","reason":"","observed_exit_ip":"127.0.0.1","observed_at":"2026-11-15T00:00:00Z"}`,
		`{"status":"duplicate","reason":"duplicate_ip","observed_exit_ip":"203.0.113.10","observed_at":"2026-11-15T00:00:00Z","registered_proxy_id":"http://SECRET"}`,
		`{"status":"ready","reason":null,"observed_exit_ip":"203.0.113.10","observed_at":"2026-11-15T00:00:00Z"}`,
	} {
		response = raw
		out = kiloAccountsWrite(h, "POST", "/api/kilo/proxies/preview", kiloRouterAdminKey, fields)
		if out.Code != 502 || strings.Contains(out.Body.String(), "SECRET") {
			t.Fatalf("非法预览投影通过: %d %s", out.Code, out.Body)
		}
	}
}

func TestKiloIPAdmissionDuplicateAndUnknownConflict(t *testing.T) {
	for _, scenario := range []string{"duplicate", "unsafe-details", "other-409", "code-only", "drift", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			resolver := &kiloRouterResolver{}
			var cancel context.CancelFunc
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				if r.Method == "GET" {
					io.WriteString(w, kiloProxyFixture)
					return
				}
				writes.Add(1)
				if scenario == "drift" {
					setup, _, _ := resolver.ResolveSetup(r.Context())
					setup.ManagementKey = "CHANGED"
					resolver.replace(setup)
				}
				if scenario == "cancel" {
					cancel()
				}
				w.WriteHeader(409)
				switch scenario {
				case "other-409":
					io.WriteString(w, `{"error":"SECRET_OTHER"}`)
				case "code-only":
					io.WriteString(w, `{"code":"proxy_ip_already_registered","error":"SECRET_OTHER"}`)
				case "unsafe-details":
					io.WriteString(w, `{"error":"proxy_ip_already_registered","ip":"SECRET_IP","proxy_id":"http://SECRET"}`)
				default:
					io.WriteString(w, `{"error":"proxy_ip_already_registered","ip":"203.0.113.10","proxy_id":"kilo-proxy-original","password":"SECRET"}`)
				}
			}))
			defer up.Close()
			resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
			h := newKiloFixtureRouter(t, resolver, up.Client())
			state := kiloProxySnapshot(t, h)
			raw, _ := json.Marshal(map[string]any{"url": "http://127.0.0.1:8080", "expected_revision": state.Revision, "connection_ref": state.ConnectionRef})
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			cancel = stop
			req := httptest.NewRequest("POST", "/api/kilo/proxies", strings.NewReader(string(raw))).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
			req.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			h.ServeHTTP(out, req)
			if writes.Load() != 1 || strings.Contains(out.Body.String(), "SECRET") {
				t.Fatalf("错误泄露或请求重放: %d %s", writes.Load(), out.Body)
			}
			duplicate := strings.Contains(out.Body.String(), `"error":"proxy_ip_already_registered"`)
			if duplicate != (scenario == "duplicate" || scenario == "unsafe-details") {
				t.Fatalf("重复与未知错误混淆: %s %d %s", scenario, out.Code, out.Body)
			}
			if scenario == "duplicate" && (!strings.Contains(out.Body.String(), `"ip":"203.0.113.10"`) || !strings.Contains(out.Body.String(), `"proxy_id":"kilo-proxy-original"`)) {
				t.Fatal("安全定位字段丢失")
			}
			if scenario == "unsafe-details" && (strings.Contains(out.Body.String(), `"ip":`) || strings.Contains(out.Body.String(), `"proxy_id":`)) {
				t.Fatal("不安全定位字段未丢弃")
			}
		})
	}
}

func TestKiloIPAdmissionPreviewConnectionBoundary(t *testing.T) {
	for _, scenario := range []string{"drift", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			resolver := &kiloRouterResolver{}
			var calls atomic.Int32
			var cancel context.CancelFunc
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				if r.URL.Path != "/v8/management/kilo/proxies/preview" {
					io.WriteString(w, kiloProxyFixture)
					return
				}
				calls.Add(1)
				if scenario == "drift" {
					setup, _, _ := resolver.ResolveSetup(r.Context())
					setup.ManagementKey = "CHANGED"
					resolver.replace(setup)
				} else {
					cancel()
				}
				io.WriteString(w, `{"status":"ready","reason":"","observed_exit_ip":"203.0.113.10","observed_at":"2026-11-15T00:00:00Z"}`)
			}))
			defer up.Close()
			resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
			h := newKiloFixtureRouter(t, resolver, up.Client())
			state := kiloProxySnapshot(t, h)
			raw, _ := json.Marshal(map[string]any{"url": "http://127.0.0.1:8080", "connection_ref": state.ConnectionRef})
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			cancel = stop
			req := httptest.NewRequest("POST", "/api/kilo/proxies/preview", strings.NewReader(string(raw))).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
			out := httptest.NewRecorder()
			h.ServeHTTP(out, req)
			want := 409
			if scenario == "cancel" {
				want = 499
			}
			if out.Code != want || calls.Load() != 1 || strings.Contains(out.Body.String(), `"status":"ready"`) {
				t.Fatalf("旧预览未废弃 %d %s", out.Code, out.Body)
			}
		})
	}
}
