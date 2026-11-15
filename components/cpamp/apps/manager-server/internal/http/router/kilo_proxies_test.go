package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const kiloProxyFixture = `{"revision":7,"runtime_revision":7,"managed":true,"proxy_limit":10,"proxies":[{"id":"p1","name":"代理一","order":0,"enabled":true,"account_id":"","scheme":"http","host":"127.0.0.1","port":"8080","has_auth":true,"healthy":false,"active":1,"owner":"a1","url":"http://SECRET_USER:SECRET_PASS@127.0.0.1:8080","password":"SECRET_PASS"}],"accounts":[{"account_id":"a1","mode":"pool","primary_proxy_id":"p1","token":"SECRET_TOKEN"}],"connection_ref":"UPSTREAM_REF"}`

func kiloProxySnapshot(t *testing.T, h http.Handler) model.KiloProxyState {
	t.Helper()
	r := kiloRouterRequest(h, "GET", "/api/kilo/proxies", kiloRouterAdminKey)
	var result model.KiloProxyState
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || len(result.ConnectionRef) != 32 {
		t.Fatalf("读取代理投影失败: %d %s", r.Code, r.Body.String())
	}
	assertKiloRouterSanitized(t, r)
	return result
}

func TestKiloProxiesRouterContract(t *testing.T) {
	var writes atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("上游管理认证错误")
		}
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.Method != "GET" {
			writes.Add(1)
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if strings.Contains(string(raw), "connection_ref") || body["expected_revision"] != float64(7) {
				t.Error("连接引用未剔除或版本缺失")
			}
			if r.Method == "POST" {
				if body["url"] != "http://USER:SECRET_INPUT@127.0.0.1:8080" {
					t.Error("写入URL未完整传递")
				}
				w.WriteHeader(201)
			}
		}
		io.WriteString(w, kiloProxyFixture)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	if r := kiloRouterRequest(h, "GET", "/api/kilo/proxies", "CLIENT_KEY"); r.Code != 401 {
		t.Fatal("读取未鉴权")
	}
	snapshot := kiloProxySnapshot(t, h)
	if snapshot.Revision != 7 || snapshot.RuntimeRevision != 7 || snapshot.Proxies[0].Healthy || snapshot.Proxies[0].Active != 1 || snapshot.Proxies[0].Owner != "a1" {
		t.Fatal("运行投影不一致")
	}
	for _, tc := range []struct {
		method, path string
		fields       map[string]any
		status       int
	}{
		{"POST", "/api/kilo/proxies", map[string]any{"url": "http://USER:SECRET_INPUT@127.0.0.1:8080", "name": "新代理", "enabled": true, "account_id": ""}, 201},
		{"PATCH", "/api/kilo/proxies/p1", map[string]any{"order": 2, "name": "改名", "enabled": false, "account_id": "a1"}, 200},
		{"PATCH", "/api/kilo/proxy-pool", map[string]any{"proxy_limit": 9}, 200},
		{"PUT", "/api/kilo/accounts/a1/egress", map[string]any{"mode": "direct", "exclusive_proxy_ids": []string{}, "primary_proxy_id": ""}, 200},
	} {
		tc.fields["expected_revision"] = 7
		tc.fields["connection_ref"] = snapshot.ConnectionRef
		if denied := kiloAccountsWrite(h, tc.method, tc.path, "CLIENT_KEY", tc.fields); denied.Code != 401 {
			t.Fatal("写鉴权失效")
		}
		r := kiloAccountsWrite(h, tc.method, tc.path, kiloRouterAdminKey, tc.fields)
		if r.Code != tc.status {
			t.Fatalf("%s %s=%d %s", tc.method, tc.path, r.Code, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
	}
	if writes.Load() != 4 {
		t.Fatal("写次数错误")
	}
	if r := kiloAccountsWrite(h, "PUT", "/api/kilo/proxies/p1", kiloRouterAdminKey, map[string]any{}); r.Code != 405 {
		t.Fatal("意外支持整条替换")
	}
}

func TestKiloProxiesValidationConflictAndDrift(t *testing.T) {
	for _, change := range []string{"none", "url", "key", "removed", "conflict"} {
		t.Run(change, func(t *testing.T) {
			var writes atomic.Int32
			resolver := &kiloRouterResolver{}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				if r.Method != "GET" {
					writes.Add(1)
					setup, _, _ := resolver.ResolveSetup(r.Context())
					switch change {
					case "url":
						setup.CPAUpstreamURL = "http://never-contact.invalid"
					case "key":
						setup.ManagementKey = "CHANGED"
					case "removed":
						setup = store.Setup{}
					case "conflict":
						w.WriteHeader(409)
						io.WriteString(w, "SECRET_SERVER")
						return
					}
					resolver.replace(setup)
				}
				io.WriteString(w, kiloProxyFixture)
			}))
			defer up.Close()
			resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
			h := newKiloFixtureRouter(t, resolver, up.Client())
			ref := kiloProxySnapshot(t, h).ConnectionRef
			for _, fields := range []map[string]any{{"proxy_limit": 1}, {"expected_revision": nil, "proxy_limit": 1}, {"expected_revision": -1, "proxy_limit": 1}, {"expected_revision": 1.5, "proxy_limit": 1}, {"expected_revision": 7, "proxy_limit": 101}, {"expected_revision": 7, "url": "SECRET_INPUT"}} {
				fields["connection_ref"] = ref
				r := kiloAccountsWrite(h, "PATCH", "/api/kilo/proxy-pool", kiloRouterAdminKey, fields)
				if r.Code != 400 {
					t.Fatalf("无效输入未拒绝:%d", r.Code)
				}
			}
			if writes.Load() != 0 {
				t.Fatal("无效输入到达上游")
			}
			for _, reference := range []string{"", strings.Repeat("f", 32)} {
				r := kiloAccountsWrite(h, "PATCH", "/api/kilo/proxy-pool", kiloRouterAdminKey, map[string]any{"connection_ref": reference, "expected_revision": 7, "proxy_limit": 8})
				if r.Code != 400 && r.Code != 409 {
					t.Fatal("无效引用通过")
				}
			}
			r := kiloAccountsWrite(h, "PATCH", "/api/kilo/proxy-pool", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "expected_revision": 7, "proxy_limit": 8})
			want := 409
			if change == "none" {
				want = 200
			}
			if r.Code != want || writes.Load() != 1 {
				t.Fatalf("漂移/冲突错误:%d writes=%d", r.Code, writes.Load())
			}
			assertKiloRouterSanitized(t, r)
			if change != "none" && change != "conflict" && !strings.Contains(r.Body.String(), "result_unconfirmed") {
				t.Fatal("后漂移结果未待核")
			}
		})
	}
}

func TestKiloProxiesEveryWriteErrorAndConnectionBoundary(t *testing.T) {
	for _, route := range []struct {
		method, path string
		fields       map[string]any
	}{
		{"POST", "/api/kilo/proxies", map[string]any{"url": "http://u:SECRET_INPUT@127.0.0.1:8080"}},
		{"PATCH", "/api/kilo/proxies/p1", map[string]any{"name": "新名"}},
		{"PATCH", "/api/kilo/proxy-pool", map[string]any{"proxy_limit": 8}},
		{"PUT", "/api/kilo/accounts/a1/egress", map[string]any{"mode": "pool", "exclusive_proxy_ids": []string{"p1"}, "primary_proxy_id": "p1"}},
	} {
		for _, scenario := range []string{"pre-drift", "post-url", "post-key", "post-removed", "400", "404", "409", "503", "502", "redirect"} {
			t.Run(route.method+route.path+scenario, func(t *testing.T) {
				var writes atomic.Int32
				resolver := &kiloRouterResolver{}
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
					setup, _, _ := resolver.ResolveSetup(r.Context())
					switch scenario {
					case "post-url":
						setup.CPAUpstreamURL = "http://never-contact.invalid"
					case "post-key":
						setup.ManagementKey = "CHANGED"
					case "post-removed":
						setup = store.Setup{}
					case "400":
						w.WriteHeader(400)
					case "404":
						w.WriteHeader(404)
					case "409":
						w.WriteHeader(409)
					case "503":
						w.WriteHeader(503)
					case "502":
						w.WriteHeader(500)
					case "redirect":
						w.Header().Set("Location", "/should-not-follow")
						w.WriteHeader(302)
					}
					resolver.replace(setup)
					if strings.HasPrefix(scenario, "post-") {
						if route.method == "POST" {
							w.WriteHeader(201)
						}
						io.WriteString(w, kiloProxyFixture)
					} else {
						io.WriteString(w, "SECRET_SERVER")
					}
				}))
				defer up.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
				h := newKiloFixtureRouter(t, resolver, up.Client())
				state := kiloProxySnapshot(t, h)
				if scenario == "pre-drift" {
					resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: "CHANGED"})
				}
				fields := map[string]any{"expected_revision": 7, "connection_ref": state.ConnectionRef}
				for k, v := range route.fields {
					fields[k] = v
				}
				r := kiloAccountsWrite(h, route.method, route.path, kiloRouterAdminKey, fields)
				want := 409
				switch scenario {
				case "400":
					want = 400
				case "404":
					want = 404
				case "503":
					want = 503
				case "502", "redirect":
					want = 502
				}
				count := int32(1)
				if scenario == "pre-drift" {
					count = 0
				}
				if r.Code != want || writes.Load() != count {
					t.Fatalf("状态/次数=%d/%d，预期%d/%d", r.Code, writes.Load(), want, count)
				}
				assertKiloRouterSanitized(t, r)
			})
		}
	}
}

func TestKiloProxiesEgressRequiresFullReplacement(t *testing.T) {
	var writes atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.Method != "GET" {
			writes.Add(1)
		}
		io.WriteString(w, kiloProxyFixture)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloProxySnapshot(t, h).ConnectionRef
	for _, fields := range []map[string]any{
		{"mode": "pool"}, {"mode": "pool", "exclusive_proxy_ids": []string{}},
		{"mode": "direct", "exclusive_proxy_ids": []string{"p1"}, "primary_proxy_id": ""},
		{"mode": "direct", "exclusive_proxy_ids": []string{}, "primary_proxy_id": "p1"},
		{"mode": "direct", "exclusive_proxy_ids": nil, "primary_proxy_id": ""},
		{"mode": "unknown", "exclusive_proxy_ids": []string{}, "primary_proxy_id": ""},
	} {
		fields["expected_revision"] = 0
		fields["connection_ref"] = ref
		r := kiloAccountsWrite(h, "PUT", "/api/kilo/accounts/a1/egress", kiloRouterAdminKey, fields)
		if r.Code != 400 {
			t.Fatalf("不完整出口未拒绝:%d", r.Code)
		}
	}
	if writes.Load() != 0 {
		t.Fatal("无效出口调用上游")
	}
	r := kiloAccountsWrite(h, "PUT", "/api/kilo/accounts/a1/egress", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "expected_revision": 0, "mode": "direct", "exclusive_proxy_ids": []string{}, "primary_proxy_id": ""})
	if r.Code != 200 || writes.Load() != 1 {
		t.Fatal("合法零版本/direct未通过")
	}
}

func TestKiloProxiesReadErrorsAndDrift(t *testing.T) {
	for _, scenario := range []string{"503", "409", "drift", "malformed-host"} {
		t.Run(scenario, func(t *testing.T) {
			resolver := &kiloRouterResolver{}
			var reads atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				reads.Add(1)
				switch scenario {
				case "503":
					w.WriteHeader(503)
					io.WriteString(w, "SECRET_SERVER")
				case "409":
					w.WriteHeader(409)
					io.WriteString(w, "SECRET_SERVER")
				case "drift":
					resolver.replace(store.Setup{})
					io.WriteString(w, kiloProxyFixture)
				case "malformed-host":
					io.WriteString(w, strings.Replace(kiloProxyFixture, `"host":"127.0.0.1"`, `"host":"SECRET_USER@127.0.0.1"`, 1))
				}
			}))
			defer up.Close()
			resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
			h := newKiloFixtureRouter(t, resolver, up.Client())
			r := kiloRouterRequest(h, "GET", "/api/kilo/proxies", kiloRouterAdminKey)
			want := 409
			if scenario == "503" {
				want = 503
			}
			if scenario == "malformed-host" {
				want = 502
			}
			if r.Code != want || reads.Load() != 1 {
				t.Fatalf("读取状态=%d次数=%d", r.Code, reads.Load())
			}
			assertKiloRouterSanitized(t, r)
		})
	}
}
