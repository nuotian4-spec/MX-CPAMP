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

var kiloRecycleFixture = strings.Replace(kiloProxyFixture, `"accounts":`, `"recycle":[{"id":"r1","name":"已归档","order":3,"enabled":false,"original_account_id":"a1","scheme":"http","host":"127.0.0.1","port":"8081","has_auth":true,"archived_at":"2026-11-13T01:02:03.123456789Z","url":"http://SECRET_USER:SECRET_PASS@127.0.0.1:8081","password":"SECRET_PASS"}],"accounts":`, 1)

var kiloRecycleRoutes = []struct{ method, path string }{
	{"DELETE", "/api/kilo/proxies/p1"},
	{"POST", "/api/kilo/proxy-recycle/r1/restore"},
	{"DELETE", "/api/kilo/proxy-recycle"},
	{"POST", "/api/kilo/proxies/p1/recheck"},
}

func TestKiloRecycleRouterContractAndValidation(t *testing.T) {
	var writes atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("上游管理鉴权错误")
		}
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.Method != "GET" {
			writes.Add(1)
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["expected_revision"] != float64(7) {
				t.Error("版本未转发")
			}
			if _, ok := body["connection_ref"]; ok {
				t.Error("连接引用泄露到Core")
			}
			found := false
			for _, route := range kiloRecycleRoutes {
				if route.method == r.Method && strings.Replace(route.path, "/api/kilo", "/v8/management/kilo", 1) == r.URL.Path {
					found = true
				}
			}
			if !found {
				t.Errorf("资源路径错误:%s", r.URL.Path)
			}
			if r.URL.Path == "/v8/management/kilo/proxy-recycle" && body["confirm"] != true {
				t.Error("清空确认未转发")
			}
		}
		io.WriteString(w, kiloRecycleFixture)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	for _, path := range []string{"/api/kilo/proxies", "/api/kilo/proxy-recycle"} {
		if denied := kiloRouterRequest(h, "GET", path, "CLIENT"); denied.Code != 401 {
			t.Fatal("回收站读取未鉴权")
		}
		r := kiloRouterRequest(h, "GET", path, kiloRouterAdminKey)
		var state model.KiloProxyState
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &state) != nil || len(state.Recycle) != 1 || state.Recycle[0].OriginalAccountID != "a1" || state.Recycle[0].Port != "8081" || state.Recycle[0].ArchivedAt != "2026-11-13T01:02:03.123456789Z" {
			t.Fatalf("归档白名单投影错误:%s", r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
	}
	state := kiloProxySnapshot(t, h)
	for _, route := range kiloRecycleRoutes {
		fields := map[string]any{"expected_revision": 7, "connection_ref": state.ConnectionRef}
		if route.path == "/api/kilo/proxy-recycle" {
			fields["confirm"] = true
		}
		if denied := kiloAccountsWrite(h, route.method, route.path, "CLIENT", fields); denied.Code != 401 {
			t.Fatal("四写未鉴权")
		}
		before := writes.Load()
		for _, bad := range []map[string]any{
			{"connection_ref": state.ConnectionRef},
			{"expected_revision": nil, "connection_ref": state.ConnectionRef},
			{"expected_revision": -1, "connection_ref": state.ConnectionRef},
			{"expected_revision": 7, "connection_ref": state.ConnectionRef, "url": "SECRET_INPUT"},
			{"expected_revision": 7, "connection_ref": state.ConnectionRef, "enabled": true},
			{"expected_revision": 7, "connection_ref": "invalid"},
		} {
			if r := kiloAccountsWrite(h, route.method, route.path, kiloRouterAdminKey, bad); r.Code != 400 {
				t.Fatalf("坏输入未阻断:%d %s", r.Code, route.path)
			}
		}
		if writes.Load() != before {
			t.Fatal("坏输入触达Core")
		}
		r := kiloAccountsWrite(h, route.method, route.path, kiloRouterAdminKey, fields)
		if r.Code != 200 || writes.Load() != before+1 {
			t.Fatalf("四写结果错误:%d %s", r.Code, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
	}
	for _, value := range []any{nil, false, "true", 1} {
		r := kiloAccountsWrite(h, "DELETE", "/api/kilo/proxy-recycle", kiloRouterAdminKey, map[string]any{"connection_ref": state.ConnectionRef, "expected_revision": 7, "confirm": value})
		if r.Code != 400 {
			t.Fatal("清空未要求显式布尔确认")
		}
	}
	if writes.Load() != 4 {
		t.Fatal("四写发生重放")
	}
}

func TestKiloRecycleEveryWriteErrorsAndDrift(t *testing.T) {
	for _, route := range kiloRecycleRoutes {
		for _, scenario := range []string{"400", "404", "409", "499", "502", "503", "pre-drift", "post-url", "post-key", "post-removed"} {
			t.Run(route.path+route.method+scenario, func(t *testing.T) {
				var writes atomic.Int32
				resolver := &kiloRouterResolver{}
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v8/management/credentials/concurrency" {
						io.WriteString(w, kiloRouterConcurrencyFixture)
						return
					}
					if r.Method == "GET" {
						io.WriteString(w, kiloRecycleFixture)
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
					}
					resolver.replace(setup)
					if status, err := strconv.Atoi(scenario); err == nil {
						w.WriteHeader(status)
						io.WriteString(w, "SECRET_ERROR")
						return
					}
					io.WriteString(w, kiloRecycleFixture)
				}))
				defer up.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey})
				h := newKiloFixtureRouter(t, resolver, up.Client())
				state := kiloProxySnapshot(t, h)
				if scenario == "pre-drift" {
					resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: "CHANGED"})
				}
				fields := map[string]any{"expected_revision": 7, "connection_ref": state.ConnectionRef}
				if route.path == "/api/kilo/proxy-recycle" {
					fields["confirm"] = true
				}
				r := kiloAccountsWrite(h, route.method, route.path, kiloRouterAdminKey, fields)
				want, err := strconv.Atoi(scenario)
				if err != nil {
					want = 409
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

func TestKiloRecycleClientCancellationNotConnectionDrift(t *testing.T) {
	for _, route := range kiloRecycleRoutes {
		t.Run(route.method+route.path, func(t *testing.T) {
			reached := make(chan struct{})
			var writes atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				if r.Method == "GET" {
					io.WriteString(w, kiloRecycleFixture)
					return
				}
				io.Copy(io.Discard, r.Body)
				writes.Add(1)
				close(reached)
				<-r.Context().Done()
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			state := kiloProxySnapshot(t, h)
			fields := map[string]any{"expected_revision": 7, "connection_ref": state.ConnectionRef}
			if route.path == "/api/kilo/proxy-recycle" {
				fields["confirm"] = true
			}
			raw, _ := json.Marshal(fields)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(string(raw))).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
			rec := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { h.ServeHTTP(rec, req); close(done) }()
			select {
			case <-reached:
			case <-time.After(3 * time.Second):
				t.Fatal("请求未到达上游")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("取消未结束请求")
			}
			if rec.Code != 499 || writes.Load() != 1 || !strings.Contains(rec.Body.String(), "proxy_operation_cancelled") {
				t.Fatalf("取消误报或重放:%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestKiloRecycleReadRejectsMalformedProjection(t *testing.T) {
	for _, scenario := range []string{"missing", "host", "port", "time", "timezone"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := kiloRecycleFixture
			switch scenario {
			case "missing":
				fixture = kiloProxyFixture
			case "host":
				fixture = strings.Replace(fixture, `"host":"127.0.0.1","port":"8081"`, `"host":"SECRET_USER@127.0.0.1","port":"8081"`, 1)
			case "port":
				fixture = strings.Replace(fixture, `"port":"8081"`, `"port":"65536"`, 1)
			case "time":
				fixture = strings.Replace(fixture, "2026-11-13T01:02:03.123456789Z", "not-a-time", 1)
			case "timezone":
				fixture = strings.Replace(fixture, "2026-11-13T01:02:03.123456789Z", "2026-11-13T01:02:03+08:00", 1)
			}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				if r.URL.Path != "/v8/management/kilo/proxy-recycle" {
					t.Error("回收站读取资源错误")
				}
				io.WriteString(w, fixture)
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			r := kiloRouterRequest(h, "GET", "/api/kilo/proxy-recycle", kiloRouterAdminKey)
			want := 502
			if scenario == "missing" {
				want = 200
				if !strings.Contains(r.Body.String(), `"recycle":[]`) {
					t.Fatal("缺失回收站未规范为空数组")
				}
			}
			if r.Code != want {
				t.Fatalf("回收站投影校验错误:%d %s", r.Code, r.Body.String())
			}
			assertKiloRouterSanitized(t, r)
		})
	}
}
