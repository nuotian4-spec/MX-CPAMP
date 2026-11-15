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

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const kiloOrganizationsFixture = `{"organizations":[{"id":"org_first","name":"第一组织","token":"SECRET"}],"has_personal_account":false,"profile":{"token":"SECRET"},"connection_ref":"UPSTREAM_REF"}`

func TestKiloOrganizationsRouterContract(t *testing.T) {
	var reads, writes atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("鉴权不符")
		}
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			io.WriteString(w, kiloRouterConcurrencyFixture)
		case "/v8/management/kilo/accounts":
			io.WriteString(w, `{"accounts":[]}`)
		case "/v8/management/kilo/accounts/account 1/organizations":
			reads.Add(1)
			body, _ := io.ReadAll(r.Body)
			if r.Method != "GET" || len(body) != 0 || r.URL.RawQuery != "" || r.URL.EscapedPath() != "/v8/management/kilo/accounts/account%201/organizations" {
				t.Error("查询路径/正文不符")
			}
			io.WriteString(w, kiloOrganizationsFixture)
		default:
			writes.Add(1)
			var fields map[string]any
			json.NewDecoder(r.Body).Decode(&fields)
			if r.Method != "PATCH" || len(fields) != 1 || fields["organization_id"] != "org_first" {
				t.Errorf("字段/引用剔除错误:%v", fields)
			}
			io.WriteString(w, strings.Replace(kiloSavedFixture, `"organization_id":""`, `"organization_id":"org_first"`, 1))
		}
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloAccountsRef(t, h)
	path := "/api/kilo/accounts/account%201/organizations"
	for _, key := range []string{"", "SYNTHETIC_CLIENT_KEY", kiloRouterManagementKey} {
		if r := kiloRouterRequest(h, "GET", path, key); r.Code != 401 {
			t.Fatal("组织查询绕过管理鉴权")
		}
	}
	r := kiloRouterRequest(h, "GET", path, kiloRouterAdminKey)
	var result model.KiloOrganizations
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.ConnectionRef != ref || result.HasPersonalAccount || len(result.Organizations) != 1 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("查询合同错误:%d %s", r.Code, r.Body.String())
	}
	assertKiloRouterSanitized(t, r)
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		if r := kiloRouterRequest(h, method, path, kiloRouterAdminKey); r.Code != 405 {
			t.Fatal("查询错误方法通过")
		}
	}
	for _, request := range []*http.Request{httptest.NewRequest("GET", path+"?api_key=SECRET", nil), httptest.NewRequest("GET", path, strings.NewReader(`{"api_key":"SECRET"}`))} {
		request.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request)
		if rec.Code != 400 {
			t.Fatal("查询接受业务输入")
		}
	}
	r = kiloAccountsWrite(h, "PATCH", "/api/kilo/accounts/account%201", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "organization_id": "org_first"})
	var saved model.KiloAccountSaved
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &saved) != nil || saved.OrganizationID == nil || *saved.OrganizationID != "org_first" || saved.ConnectionRef != ref {
		t.Fatal("组织保存DTO不符")
	}
	assertKiloRouterSanitized(t, r)
	for _, organization := range []any{nil, 3, " bad", "org/x", strings.Repeat("a", 129)} {
		if r := kiloAccountsWrite(h, "PATCH", "/api/kilo/accounts/account%201", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "organization_id": organization}); r.Code != 400 {
			t.Fatal("非法组织字段通过")
		}
	}
	if r := kiloAccountsWrite(h, "POST", "/api/kilo/accounts", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "api_key": "SECRET", "organization_id": ""}); r.Code != 400 {
		t.Fatal("创建接受组织字段")
	}
	start := kiloDeviceFields("start", ref)
	start["organization_id"] = "org_first"
	if r := kiloAccountsWrite(h, "POST", "/api/kilo/device-auth", kiloRouterAdminKey, start); r.Code != 400 {
		t.Fatal("设备start接受组织字段")
	}
	if reads.Load() != 1 || writes.Load() != 1 {
		t.Fatalf("重复或无效请求触上游:%d/%d", reads.Load(), writes.Load())
	}
}

func TestKiloOrganizationsErrorsDriftAndCancel(t *testing.T) {
	for _, method := range []string{"GET", "PATCH"} {
		for _, scenario := range []string{"403", "404", "409", "499", "502", "503", "pre", "post", "cancel"} {
			t.Run(method+scenario, func(t *testing.T) {
				resolver := &kiloRouterResolver{}
				var actions atomic.Int32
				var armed atomic.Bool
				reached := make(chan struct{})
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v8/management/credentials/concurrency" {
						if armed.Load() && scenario == "pre" && method == "GET" {
							resolver.replace(store.Setup{})
						}
						io.WriteString(w, kiloRouterConcurrencyFixture)
						return
					}
					if r.URL.Path == "/v8/management/kilo/accounts" {
						io.WriteString(w, `{"accounts":[]}`)
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
						if method == "GET" {
							io.WriteString(w, kiloOrganizationsFixture)
						} else {
							io.WriteString(w, kiloSavedFixture)
						}
						return
					}
					code, _ := strconv.Atoi(scenario)
					w.WriteHeader(code)
					io.WriteString(w, "SECRET_UPSTREAM")
				}))
				defer up.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
				h := newKiloFixtureRouter(t, resolver, up.Client())
				ref := kiloAccountsRef(t, h)
				armed.Store(true)
				if scenario == "pre" && method == "PATCH" {
					resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: "CHANGED"})
				}
				path := "/api/kilo/accounts/fixture-account"
				var body io.Reader
				if method == "GET" {
					path += "/organizations"
				} else {
					body = strings.NewReader(`{"connection_ref":"` + ref + `","organization_id":"org_first"}`)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req := httptest.NewRequest(method, path, body).WithContext(ctx)
				req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
				rec := httptest.NewRecorder()
				done := make(chan struct{})
				go func() { h.ServeHTTP(rec, req); close(done) }()
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
				if rec.Code != want || actions.Load() != count {
					t.Fatalf("状态/次数=%d/%d预期%d/%d", rec.Code, actions.Load(), want, count)
				}
				assertKiloRouterSanitized(t, rec)
			})
		}
	}
}

func TestKiloOrganizationsResponseBoundsAndUnknownAccountValue(t *testing.T) {
	for _, body := range []string{"SECRET_NON_JSON", "null", `{"organizations":{},"has_personal_account":true}`, `{"organizations":[],"has_personal_account":null}`, `{"organizations":[{"id":"bad/id","name":"x"}],"has_personal_account":true}`, strings.Repeat(" ", 128*1024) + kiloOrganizationsFixture} {
		t.Run(strconv.Itoa(len(body)), func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				io.WriteString(w, body)
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			r := kiloRouterRequest(h, "GET", "/api/kilo/accounts/id/organizations", kiloRouterAdminKey)
			if r.Code != 502 {
				t.Fatalf("无效组织响应通过:%d", r.Code)
			}
			assertKiloRouterSanitized(t, r)
		})
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		io.WriteString(w, `{"accounts":[{"id":"unknown-org","effective_limit":20,"quota_source":"none"}]}`)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	r := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	if r.Code != 200 || strings.Contains(r.Body.String(), "organization_id") {
		t.Fatal("组织缺失值被冒充个人空间")
	}
}

func TestKiloOrganizationsRedirectNetworkAndPersonal(t *testing.T) {
	for _, scenario := range []string{"redirect", "network", "personal"} {
		t.Run(scenario, func(t *testing.T) {
			var actions, redirects atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirects.Add(1)
				io.WriteString(w, kiloOrganizationsFixture)
			}))
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
				actions.Add(1)
				if scenario == "redirect" {
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
					return
				}
				if scenario == "network" {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					connection.Close()
					return
				}
				var fields map[string]any
				if json.NewDecoder(r.Body).Decode(&fields) != nil || len(fields) != 1 || fields["organization_id"] != "" {
					t.Error("个人空值或引用剔除错误")
				}
				io.WriteString(w, kiloSavedFixture)
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			ref := kiloAccountsRef(t, h)
			var response *httptest.ResponseRecorder
			want := 502
			if scenario == "personal" {
				want = 200
				response = kiloAccountsWrite(h, "PATCH", "/api/kilo/accounts/fixture-account", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "organization_id": ""})
				var account model.KiloAccountSaved
				if json.Unmarshal(response.Body.Bytes(), &account) != nil || account.OrganizationID == nil || *account.OrganizationID != "" {
					t.Fatal("个人空值被省略")
				}
			} else {
				if scenario == "network" {
					want = 503
				}
				response = kiloRouterRequest(h, "GET", "/api/kilo/accounts/fixture-account/organizations", kiloRouterAdminKey)
			}
			if response.Code != want || actions.Load() != 1 || redirects.Load() != 0 {
				t.Fatalf("单发/状态/跳转边界错误:%d/%d/%d", response.Code, actions.Load(), redirects.Load())
			}
			assertKiloRouterSanitized(t, response)
		})
	}
}
