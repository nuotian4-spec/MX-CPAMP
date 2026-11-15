package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const kiloDeviceStartFixture = `{"id":"device 1","user_code":"PUBLIC-1234","verification_url":"https://app.kilo.ai/device?user_code=PUBLIC-1234","expires_in":600,"interval":3,"inference_egress_mode":"pool","code":"SECRET_PRIVATE_CODE","token":"SECRET_TOKEN","machine_id":"SECRET_MACHINE","connection_ref":"UPSTREAM_REF"}`
const kiloDeviceApprovedFixture = `{"status":"approved","email":"fixture@example.invalid","save_expires_in":300,"token":"SECRET_TOKEN","code":"SECRET_PRIVATE_CODE","account_id":"SECRET_EARLY_ACCOUNT"}`

var kiloDeviceSavedFixture = strings.TrimSuffix(kiloSavedFixture, "}") + `,"egress_mode":"pool","egress_status":"pending","egress_reason":"shared_proxy_not_configured"}`

var kiloDeviceRoutes = []struct {
	action, method, path, fixture string
	status                        int
}{
	{"start", "POST", "/api/kilo/device-auth", kiloDeviceStartFixture, 200},
	{"poll", "POST", "/api/kilo/device-auth/device%201/poll", kiloDeviceApprovedFixture, 200},
	{"save", "POST", "/api/kilo/device-auth/device%201/save", kiloDeviceSavedFixture, 201},
	{"cancel", "DELETE", "/api/kilo/device-auth/device%201", `{"status":"cancelled","token":"SECRET_TOKEN","email":"SECRET_EMAIL","save_expires_in":300,"account_id":"SECRET_ACCOUNT"}`, 200},
}

func kiloDeviceFields(action, ref string) map[string]any {
	fields := map[string]any{"connection_ref": ref}
	if action == "start" {
		fields["name"] = "设备账号"
		fields["priority"] = 2
		fields["enabled"] = true
		fields["concurrency_limit"] = nil
		fields["allow_direct_when_proxy_full"] = false
		fields["authorization_egress"] = map[string]any{"mode": "direct"}
	}
	return fields
}

func TestKiloDeviceStartErrorMeaning(t *testing.T) {
	for _, tc := range []struct {
		action, upstream, code, message string
	}{
		{"start", `{"error":"Kilo authorization egress is unavailable","token":"SECRET_TOKEN"}`, "device_egress_unavailable", "设备授权尚未启动"},
		{"start", `{"error":"SECRET_UNKNOWN"}`, "device_start_unconfirmed", "设备授权启动结果未确认"},
		{"save", `{"error":"Kilo authorization egress is unavailable"}`, "device_result_unconfirmed", "保存结果待核"},
	} {
		t.Run(tc.action+"/"+tc.code, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v8/management/credentials/concurrency":
					io.WriteString(w, kiloRouterConcurrencyFixture)
				case "/v8/management/kilo/proxies":
					io.WriteString(w, kiloProxyFixture)
				default:
					calls.Add(1)
					w.WriteHeader(503)
					io.WriteString(w, tc.upstream)
				}
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			ref := kiloProxySnapshot(t, h).ConnectionRef
			path := "/api/kilo/device-auth"
			if tc.action == "save" {
				path += "/session/save"
			}
			r := kiloAccountsWrite(h, "POST", path, kiloRouterAdminKey, kiloDeviceFields(tc.action, ref))
			if r.Code != 503 || !strings.Contains(r.Body.String(), tc.code) || !strings.Contains(r.Body.String(), tc.message) || calls.Load() != 1 {
				t.Fatalf("设备动作错误分类不符: %d %s calls=%d", r.Code, r.Body.String(), calls.Load())
			}
			if tc.action == "start" && strings.Contains(r.Body.String(), "保存结果待核") {
				t.Fatal("启动失败被标为保存待核")
			}
			assertKiloRouterSanitized(t, r)
		})
	}
}

func TestKiloDeviceRouterContract(t *testing.T) {
	var actions atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.URL.Path == "/v8/management/kilo/proxies" {
			io.WriteString(w, kiloProxyFixture)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("设备上游鉴权错误")
		}
		var fields map[string]any
		if json.NewDecoder(r.Body).Decode(&fields) != nil {
			t.Error("设备正文错误")
		}
		if _, ok := fields["connection_ref"]; ok {
			t.Error("连接引用透传到上游")
		}
		for _, route := range kiloDeviceRoutes {
			if r.Method == route.method && r.URL.EscapedPath() == strings.Replace(route.path, "/api/", "/v8/management/", 1) {
				if route.action != "start" && len(fields) != 0 {
					t.Error("动作正文未清空")
				}
				actions.Add(1)
				w.WriteHeader(route.status)
				io.WriteString(w, route.fixture)
				return
			}
		}
		t.Errorf("设备路径/编码错误:%s", r.URL.EscapedPath())
		w.WriteHeader(404)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloProxySnapshot(t, h).ConnectionRef
	for _, route := range kiloDeviceRoutes {
		fields := kiloDeviceFields(route.action, ref)
		if r := kiloAccountsWrite(h, route.method, route.path, "CLIENT", fields); r.Code != 401 {
			t.Fatal("四动作鉴权失效")
		}
		r := kiloAccountsWrite(h, route.method, route.path, kiloRouterAdminKey, fields)
		if r.Code != route.status {
			t.Fatalf("%s=%d %s", route.action, r.Code, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("设备回执未禁缓存")
		}
		if !strings.Contains(r.Body.String(), ref) {
			t.Fatal("未返回原连接引用")
		}
		if route.action == "save" && (strings.Contains(r.Body.String(), `"account":`) || !strings.Contains(r.Body.String(), `"egress_reason":"shared_proxy_not_configured"`)) {
			t.Fatal("save扁平账号投影错误")
		}
	}
	if actions.Load() != 4 {
		t.Fatal("四动作出现重放")
	}
	for _, route := range kiloDeviceRoutes {
		fields := kiloDeviceFields(route.action, ref)
		fields["token"] = "SECRET_INPUT"
		if r := kiloAccountsWrite(h, route.method, route.path, kiloRouterAdminKey, fields); r.Code != 400 {
			t.Fatal("未知字段通过")
		}
	}
	for _, bad := range []map[string]any{
		{"name": "", "authorization_egress": map[string]any{"mode": "pool"}},
		{"name": "有效", "authorization_egress": map[string]any{"mode": "pool", "url": "SECRET"}},
		{"name": "有效", "authorization_egress": map[string]any{"mode": "unknown"}},
		{"name": "有效", "priority": 1.5, "authorization_egress": map[string]any{"mode": "pool"}},
		{"name": "有效", "enabled": nil, "authorization_egress": map[string]any{"mode": "pool"}},
		{"name": "有效", "concurrency_limit": 0, "authorization_egress": map[string]any{"mode": "pool"}},
	} {
		bad["connection_ref"] = ref
		if r := kiloAccountsWrite(h, "POST", "/api/kilo/device-auth", kiloRouterAdminKey, bad); r.Code != 400 {
			t.Fatal("坏start字段通过")
		}
	}
	if actions.Load() != 4 {
		t.Fatal("无效输入触达Core")
	}
}

func TestKiloDeviceBodyMethodsAndResponseBounds(t *testing.T) {
	for _, scenario := range []string{"input", "malformed", "oversized", "redirect", "network"} {
		t.Run(scenario, func(t *testing.T) {
			var actions, redirected atomic.Int32
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirected.Add(1)
				io.WriteString(w, kiloDeviceStartFixture)
			}))
			defer redirect.Close()
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v8/management/credentials/concurrency":
					io.WriteString(w, kiloRouterConcurrencyFixture)
				case "/v8/management/kilo/proxies":
					io.WriteString(w, kiloProxyFixture)
				default:
					actions.Add(1)
					switch scenario {
					case "malformed":
						io.WriteString(w, "SECRET_NON_JSON")
					case "oversized":
						io.WriteString(w, strings.Repeat(" ", 128*1024)+kiloDeviceStartFixture)
					case "redirect":
						http.Redirect(w, r, redirect.URL, http.StatusTemporaryRedirect)
					default:
						io.WriteString(w, kiloDeviceStartFixture)
					}
				}
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			ref := kiloProxySnapshot(t, h).ConnectionRef
			if scenario == "input" {
				for _, body := range []string{"null", "[]", "{}{}", `{"connection_ref":"` + ref + `","name":"` + strings.Repeat("x", 128*1024) + `"}`, `{"connection_ref":null}`, `{"connection_ref":"bad"}`} {
					req := httptest.NewRequest("POST", "/api/kilo/device-auth", strings.NewReader(body))
					req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
					if rec.Code != 400 {
						t.Fatalf("非单对象/超限/坏引用未拒绝:%d", rec.Code)
					}
				}
				for _, path := range []string{"/api/kilo/device-auth", "/api/kilo/device-auth/id/poll", "/api/kilo/device-auth/id/save", "/api/kilo/device-auth/id"} {
					if r := kiloRouterRequest(h, "GET", path, kiloRouterAdminKey); r.Code != 405 {
						t.Fatalf("设备错误方法=%d", r.Code)
					}
				}
				if actions.Load() != 0 {
					t.Fatal("无效输入触达上游")
				}
				return
			}
			want := 502
			if scenario == "network" {
				up.Close()
				want = 503
			}
			r := kiloAccountsWrite(h, "POST", "/api/kilo/device-auth", kiloRouterAdminKey, kiloDeviceFields("start", ref))
			if r.Code != want || redirected.Load() != 0 {
				t.Fatalf("响应边界失败:%d redirected=%d", r.Code, redirected.Load())
			}
			assertKiloRouterSanitized(t, r)
			if scenario != "network" && actions.Load() != 1 {
				t.Fatal("失败请求被重放")
			}
		})
	}
}

func TestKiloDeviceFourActionsErrorsDriftAndCancel(t *testing.T) {
	for _, route := range kiloDeviceRoutes {
		for _, scenario := range []string{"400", "403", "404", "409", "429", "499", "502", "503", "pre", "post", "cancel"} {
			t.Run(route.action+scenario, func(t *testing.T) {
				var actions atomic.Int32
				resolver := &kiloRouterResolver{}
				reached := make(chan struct{})
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v8/management/credentials/concurrency" {
						io.WriteString(w, kiloRouterConcurrencyFixture)
						return
					}
					if r.URL.Path == "/v8/management/kilo/proxies" {
						io.WriteString(w, kiloProxyFixture)
						return
					}
					actions.Add(1)
					io.Copy(io.Discard, r.Body)
					if scenario == "cancel" {
						close(reached)
						<-r.Context().Done()
						return
					}
					if scenario == "post" {
						resolver.replace(store.Setup{})
						w.WriteHeader(route.status)
						io.WriteString(w, route.fixture)
						return
					}
					code, _ := strconv.Atoi(scenario)
					w.WriteHeader(code)
					io.WriteString(w, "SECRET_UPSTREAM_BODY")
				}))
				defer up.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
				h := newKiloFixtureRouter(t, resolver, up.Client())
				ref := kiloProxySnapshot(t, h).ConnectionRef
				if scenario == "pre" {
					resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: "CHANGED"})
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				raw, _ := json.Marshal(kiloDeviceFields(route.action, ref))
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(string(raw))).WithContext(ctx)
				req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
				r := httptest.NewRecorder()
				done := make(chan struct{})
				go func() { h.ServeHTTP(r, req); close(done) }()
				if scenario == "cancel" {
					select {
					case <-reached:
					case <-time.After(3 * time.Second):
						t.Fatal("请求未触达")
					}
					cancel()
				}
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("请求未收口")
				}
				want, _ := strconv.Atoi(scenario)
				count := int32(1)
				if scenario == "pre" || scenario == "post" {
					want = 409
				}
				if scenario == "pre" {
					count = 0
				}
				if scenario == "cancel" {
					want = 499
				}
				if r.Code != want || actions.Load() != count {
					t.Fatalf("状态/次数=%d/%d预期%d/%d", r.Code, actions.Load(), want, count)
				}
				assertKiloRouterSanitized(t, r)
			})
		}
	}
}

func TestKiloDeviceProjectionAndOfficialURL(t *testing.T) {
	for _, tc := range []struct {
		action, body string
		want         int
	}{
		{"start", strings.Replace(kiloDeviceStartFixture, "https://app.kilo.ai/device", "https://kilo.ai/device", 1), 200},
		{"start", strings.Replace(kiloDeviceStartFixture, "https://app.kilo.ai/device", "https://kilo.ai.attacker.invalid/device", 1), 502},
		{"start", strings.Replace(kiloDeviceStartFixture, "https://app.kilo.ai/device", "https://user:SECRET@kilo.ai/device", 1), 502},
		{"start", strings.Replace(kiloDeviceStartFixture, "https://app.kilo.ai/device", "http://kilo.ai/device", 1), 502},
		{"start", strings.Replace(kiloDeviceStartFixture, "https://app.kilo.ai/device", "https://kilo.ai:8443/device", 1), 502},
		{"poll", `{"status":"approved","save_expires_in":301}`, 502},
		{"poll", `{"status":"saved"}`, 502},
		{"poll", `{"status":"saved","account_id":"kilo-stable.json","token":"SECRET"}`, 200},
		{"cancel", `{"status":"saved","account_id":"kilo-stable.json","token":"SECRET"}`, 200},
		{"cancel", `{"status":"approved","save_expires_in":300}`, 502},
		{"save", strings.Replace(kiloDeviceSavedFixture, "shared_proxy_not_configured", "UNKNOWN", 1), 502},
	} {
		t.Run(tc.action+strconv.Itoa(tc.want)+tc.body[:15], func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				if r.URL.Path == "/v8/management/kilo/proxies" {
					io.WriteString(w, kiloProxyFixture)
					return
				}
				if tc.action == "save" {
					w.WriteHeader(201)
				}
				io.WriteString(w, tc.body)
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			ref := kiloProxySnapshot(t, h).ConnectionRef
			for _, route := range kiloDeviceRoutes {
				if route.action == tc.action {
					r := kiloAccountsWrite(h, route.method, route.path, kiloRouterAdminKey, kiloDeviceFields(tc.action, ref))
					if r.Code != tc.want {
						t.Fatalf("投影状态=%d %s", r.Code, r.Body.String())
					}
					assertKiloRouterSanitized(t, r)
				}
			}
		})
	}
}
