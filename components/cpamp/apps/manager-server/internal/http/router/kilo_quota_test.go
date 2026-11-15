package router

import (
	"context"
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const quotaSourcesFixture = `{"sources":[{"credential_id":"codex-first.json","label":"同名","chatgpt_account_id":"outer-first","status":"refresh_required","has_query_material":false,"access_token":"SECRET"},{"credential_id":"codex-second.json","label":"同名","chatgpt_account_id":null,"status":"identity_unknown","has_query_material":false}],"token":"SECRET"}`
const quotaViewFixture = `{"account_id":"quota 1","binding_revision":3,"source":{"kind":"cpa_codex","credential_id":"codex-first.json","status":"ready","access_token":"SECRET"},"query":{"status":"success","attempted_at":"2026-11-13T00:00:00Z","observed_at":"2026-11-13T00:00:00Z","error_code":null,"token":"SECRET"},"plan":"plus","windows":[{"group":"rate_limit","slot":"primary_window","duration_seconds":18000,"remaining_percent":75,"reset_at":null,"source":"api_query","secret":"SECRET"}],"credits":{"remaining":null,"source_field":null,"unit":null},"stale":false,"connection_ref":"UPSTREAM_REF","access_token":"SECRET"}`

var quotaRoutes = []struct{ action, method, path string }{{"sources", "GET", "/api/kilo/quota-sources"}, {"get", "GET", "/api/kilo/accounts/quota%201/quota"}, {"put", "PUT", "/api/kilo/accounts/quota%201/quota-source"}, {"delete", "DELETE", "/api/kilo/accounts/quota%201/quota-source"}, {"query", "POST", "/api/kilo/accounts/quota%201/quota/query"}}

func quotaFields(action, ref string) map[string]any {
	fields := map[string]any{"connection_ref": ref, "expected_binding_revision": 3}
	if action == "put" {
		fields["source"] = map[string]any{"kind": "cpa_codex", "credential_id": "codex-first.json", "chatgpt_account_id": "outer-first"}
	}
	return fields
}
func quotaCall(h http.Handler, method, path, key string, fields map[string]any) *httptest.ResponseRecorder {
	if method == "GET" {
		return kiloRouterRequest(h, method, path, key)
	}
	return kiloAccountsWrite(h, method, path, key, fields)
}

func TestKiloQuotaFiveActionsContract(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.URL.Path == "/v8/management/kilo/accounts" {
			io.WriteString(w, `{"accounts":[]}`)
			return
		}
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("管理认证错误")
		}
		for _, route := range quotaRoutes {
			if r.Method == route.method && r.URL.EscapedPath() == strings.Replace(route.path, "/api/", "/v8/management/", 1) {
				raw, _ := io.ReadAll(r.Body)
				if strings.Contains(string(raw), "connection_ref") {
					t.Error("引用透传")
				}
				if r.Method == "GET" && len(raw) > 0 {
					t.Error("只读带正文")
				}
				if r.Method != "GET" {
					var fields map[string]any
					if json.Unmarshal(raw, &fields) != nil || fields["expected_binding_revision"] != float64(3) {
						t.Error("业务revision丢失")
					}
				}
				if route.action == "sources" {
					io.WriteString(w, quotaSourcesFixture)
				} else {
					io.WriteString(w, quotaViewFixture)
				}
				return
			}
		}
		t.Error("错误固定路径")
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloAccountsRef(t, h)
	for _, route := range quotaRoutes {
		if r := quotaCall(h, route.method, route.path, "CLIENT_KEY", quotaFields(route.action, ref)); r.Code != 401 {
			t.Fatal("五动作鉴权失败")
		}
		r := quotaCall(h, route.method, route.path, kiloRouterAdminKey, quotaFields(route.action, ref))
		if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s=%d %s", route.action, r.Code, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
		if !strings.Contains(r.Body.String(), ref) {
			t.Fatal("当前ref缺失")
		}
		if route.action == "sources" {
			var sources model.KiloQuotaSources
			json.Unmarshal(r.Body.Bytes(), &sources)
			if len(sources.Sources) != 2 || sources.Sources[0].CredentialID == sources.Sources[1].CredentialID || sources.Sources[1].ChatGPTAccountID != nil {
				t.Fatal("同名来源或未知身份被改写")
			}
		}
	}
	if calls.Load() != 5 {
		t.Fatalf("动作重放:%d", calls.Load())
	}
	for _, input := range []map[string]any{{"source": map[string]any{"kind": "imported", "access_token": "SECRET"}}, {"expected_binding_revision": nil}, {"expected_binding_revision": -1}, {"expected_binding_revision": 1.5}, {"expected_binding_revision": "3"}, {"expected_binding_revision": 3, "source": map[string]any{"kind": "cpa_codex", "credential_id": "codex-first.json"}}, {"expected_binding_revision": 3, "source": map[string]any{"kind": "cpa_codex", "credential_id": "codex-first.json", "chatgpt_account_id": nil}}, {"expected_binding_revision": 3, "source": map[string]any{"kind": "imported", "access_token": "SECRET", "refresh_token": "SECRET"}}, {"expected_binding_revision": 3, "source": map[string]any{"kind": "imported", "access_token": " "}}, {"expected_binding_revision": 3, "source": map[string]any{"kind": "imported", "access_token": "SECRET", "url": "https://invalid"}}} {
		input["connection_ref"] = ref
		if r := kiloAccountsWrite(h, "PUT", quotaRoutes[2].path, kiloRouterAdminKey, input); r.Code != 400 {
			t.Fatalf("坏字段未拒绝:%d %s", r.Code, r.Body.String())
		}
	}
	if calls.Load() != 5 {
		t.Fatal("无效请求触上游")
	}
}

func TestKiloQuotaErrorsDriftAndCancellation(t *testing.T) {
	for _, route := range quotaRoutes {
		for _, scenario := range []string{"400", "401", "403", "404", "409", "502", "503", "pre", "post", "cancel"} {
			t.Run(route.action+scenario, func(t *testing.T) {
				resolver := &kiloRouterResolver{}
				var calls atomic.Int32
				var armed atomic.Bool
				reached := make(chan struct{})
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v8/management/credentials/concurrency" {
						if armed.Load() && scenario == "pre" && route.method == "GET" {
							resolver.replace(store.Setup{})
						}
						io.WriteString(w, kiloRouterConcurrencyFixture)
						return
					}
					if r.URL.Path == "/v8/management/kilo/accounts" {
						io.WriteString(w, `{"accounts":[]}`)
						return
					}
					calls.Add(1)
					io.Copy(io.Discard, r.Body)
					if scenario == "cancel" {
						close(reached)
						<-r.Context().Done()
						return
					}
					if scenario == "post" {
						resolver.replace(store.Setup{})
						io.WriteString(w, quotaViewFixture)
						return
					}
					status, _ := strconv.Atoi(scenario)
					w.WriteHeader(status)
					io.WriteString(w, "SECRET_FAILURE")
				}))
				defer up.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
				h := newKiloFixtureRouter(t, resolver, up.Client())
				ref := kiloAccountsRef(t, h)
				armed.Store(true)
				if scenario == "pre" && route.method != "GET" {
					resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: "CHANGED"})
				}
				var body io.Reader
				if route.method != "GET" {
					raw, _ := json.Marshal(quotaFields(route.action, ref))
					body = strings.NewReader(string(raw))
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req := httptest.NewRequest(route.method, route.path, body).WithContext(ctx)
				req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
				rec := httptest.NewRecorder()
				done := make(chan struct{})
				go func() { h.ServeHTTP(rec, req); close(done) }()
				if scenario == "cancel" {
					select {
					case <-reached:
					case <-time.After(3 * time.Second):
						t.Fatal("未触上游")
					}
					cancel()
				}
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("未收口")
				}
				want, _ := strconv.Atoi(scenario)
				count := int32(1)
				if scenario == "pre" {
					want = 409
					count = 0
				}
				if scenario == "post" {
					want = 409
				}
				if scenario == "cancel" {
					want = 499
				}
				if rec.Code != want || calls.Load() != count {
					t.Fatalf("状态/次数=%d/%d预期%d/%d", rec.Code, calls.Load(), want, count)
				}
				assertKiloRouterSanitized(t, rec)
			})
		}
	}
}

func TestKiloQuotaFailedAndSafeCancelledProjection(t *testing.T) {
	for _, status := range []int{200, 499} {
		for _, drift := range []bool{false, true} {
			t.Run(strconv.Itoa(status)+strconv.FormatBool(drift), func(t *testing.T) {
				resolver := &kiloRouterResolver{}
				body := strings.Replace(quotaViewFixture, `"status":"success"`, `"status":"failed"`, 1)
				body = strings.Replace(body, `"error_code":null`, `"error_code":"usage_unauthorized"`, 1)
				body = strings.Replace(body, `"stale":false`, `"stale":true`, 1)
				if status == 499 {
					body = strings.Replace(body, `"status":"failed"`, `"status":"cancelled"`, 1)
					body = strings.Replace(body, "usage_unauthorized", "usage_cancelled", 1)
				}
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v8/management/credentials/concurrency" {
						io.WriteString(w, kiloRouterConcurrencyFixture)
						return
					}
					if r.URL.Path == "/v8/management/kilo/accounts" {
						io.WriteString(w, `{"accounts":[]}`)
						return
					}
					if drift {
						resolver.replace(store.Setup{})
					}
					w.WriteHeader(status)
					io.WriteString(w, body)
				}))
				defer up.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
				h := newKiloFixtureRouter(t, resolver, up.Client())
				ref := kiloAccountsRef(t, h)
				r := kiloAccountsWrite(h, "POST", quotaRoutes[4].path, kiloRouterAdminKey, quotaFields("query", ref))
				want := status
				if drift {
					want = 409
				}
				if r.Code != want {
					t.Fatalf("状态=%d %s", r.Code, r.Body.String())
				}
				assertKiloRouterSanitized(t, r)
				if drift {
					if strings.Contains(r.Body.String(), "binding_revision") {
						t.Fatal("漂移发布了旧观测")
					}
					return
				}
				var result model.KiloQuotaView
				if json.Unmarshal(r.Body.Bytes(), &result) != nil || result.ConnectionRef != ref || !result.Stale || result.Query.ObservedAt == nil || result.Windows[0].RemainingPercent == nil || *result.Windows[0].RemainingPercent != 75 {
					t.Fatal("失败或取消丢失安全旧值")
				}
				if status == 200 && result.Query.Status != "failed" {
					t.Fatal("usage401冒充查询成功")
				}
				if status == 499 && result.Query.Status != "cancelled" {
					t.Fatal("取消语义丢失")
				}
			})
		}
	}
}

func TestKiloQuotaBoundsMethodsAndImportedInput(t *testing.T) {
	for _, scenario := range []string{"input", "imported", "oversized", "malformed", "shape", "redirect", "network"} {
		t.Run(scenario, func(t *testing.T) {
			var calls, redirects atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1); io.WriteString(w, quotaViewFixture) }))
			defer target.Close()
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				if r.URL.Path == "/v8/management/kilo/accounts" {
					io.WriteString(w, `{"accounts":[]}`)
					return
				}
				calls.Add(1)
				switch scenario {
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				case "network":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				case "oversized":
					io.WriteString(w, strings.Repeat(" ", 128*1024)+quotaViewFixture)
				case "malformed":
					io.WriteString(w, "SECRET_NOT_JSON")
				case "shape":
					io.WriteString(w, strings.Replace(quotaViewFixture, `"error_code":null`, `"error_code":"SECRET_UNKNOWN"`, 1))
				case "imported":
					var fields map[string]any
					if json.NewDecoder(r.Body).Decode(&fields) != nil || len(fields) != 2 {
						t.Error("imported正文字段异常")
					}
					source, _ := fields["source"].(map[string]any)
					if len(source) != 2 || source["kind"] != "imported" || source["access_token"] != "SECRET_ACCESS" {
						t.Error("独立导入材料不符")
					}
					io.WriteString(w, quotaViewFixture)
				default:
					io.WriteString(w, quotaViewFixture)
				}
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			ref := kiloAccountsRef(t, h)
			if scenario == "input" {
				for _, body := range []string{"null", "[]", "{}{}", `{"connection_ref":"` + ref + `","expected_binding_revision":3,"source":{"kind":"imported","access_token":"` + strings.Repeat("X", 128*1024) + `"}}`} {
					req := httptest.NewRequest("PUT", quotaRoutes[2].path, strings.NewReader(body))
					req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
					if rec.Code != 400 {
						t.Fatal("无效/超限正文通过")
					}
				}
				for _, route := range quotaRoutes {
					method := "PATCH"
					if rec := kiloRouterRequest(h, method, route.path, kiloRouterAdminKey); rec.Code != 405 {
						t.Fatal("方法限制失败")
					}
				}
				if rec := kiloRouterRequest(h, "GET", quotaRoutes[0].path+"?url=SECRET", kiloRouterAdminKey); rec.Code != 400 {
					t.Fatal("任意查询参数通过")
				}
				if calls.Load() != 0 {
					t.Fatal("坏输入触上游")
				}
				return
			}
			var rec *httptest.ResponseRecorder
			want := 502
			if scenario == "imported" {
				want = 200
				rec = kiloAccountsWrite(h, "PUT", quotaRoutes[2].path, kiloRouterAdminKey, map[string]any{"connection_ref": ref, "expected_binding_revision": 3, "source": map[string]any{"kind": "imported", "access_token": "SECRET_ACCESS"}})
			} else {
				rec = kiloRouterRequest(h, "GET", quotaRoutes[1].path, kiloRouterAdminKey)
				if scenario == "network" {
					want = 503
				}
			}
			if rec.Code != want || calls.Load() != 1 || redirects.Load() != 0 {
				t.Fatalf("边界状态/次数/重定向=%d/%d/%d", rec.Code, calls.Load(), redirects.Load())
			}
			assertKiloRouterSanitized(t, rec)
		})
	}
}
