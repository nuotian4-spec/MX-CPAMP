package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const autoGroupFixture = `{"ip":"203.0.113.10","owner_account_id":"current","account_ids":["historic","current"],"draining":false,"retired":false,"reason":"","unique_accounts":2,"max_unique_accounts":3,"state":"assigned","proxy_ids":["p1","p2"],"secret":"SECRET"}`

func autoProxyFixture() string {
	result := strings.Replace(kiloProxyFixture, `"proxy_limit":10`, `"proxy_limit":10,"auto_assignment_enabled":true,"max_unique_accounts":3,"auto_error":null,"ip_groups":[`+autoGroupFixture+`]`, 1)
	result = strings.Replace(result, `"id":"p1"`, `"id":"p1","fixed_exit_ip":"203.0.113.10"`, 1)
	return strings.Replace(result, `"mode":"pool"`, `"mode":"auto","assigned_ip":"203.0.113.10","allocation_state":"assigned","allocation_reason":""`, 1)
}
func TestAE1AutoEgressRouterContract(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.Method != "GET" {
			calls.Add(1)
			raw, _ := io.ReadAll(r.Body)
			var fields map[string]any
			json.Unmarshal(raw, &fields)
			if strings.Contains(string(raw), "connection_ref") || fields["expected_revision"] != float64(7) {
				t.Error("引用或版本透传错误")
			}
		}
		io.WriteString(w, autoProxyFixture())
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	state := kiloProxySnapshot(t, h)
	if state.IPGroups == nil || len(*state.IPGroups) != 1 || (*state.IPGroups)[0].OwnerAccountID != "current" || (*state.IPGroups)[0].AccountIDs[0] != "historic" || state.Accounts[0].Mode != "auto" {
		t.Fatal("当前归属与历史混淆")
	}
	for _, tc := range []struct {
		method, path string
		fields       map[string]any
	}{
		{"PATCH", "/api/kilo/proxy-pool", map[string]any{"auto_assignment_enabled": true, "max_unique_accounts": 3}},
		{"PATCH", "/api/kilo/proxy-pool", map[string]any{"auto_assignment_enabled": false}},
		{"PATCH", "/api/kilo/proxies/p1", map[string]any{"fixed_exit_ip": "203.0.113.10"}},
		{"PUT", "/api/kilo/accounts/a1/egress", map[string]any{"mode": "auto", "exclusive_proxy_ids": []string{}, "primary_proxy_id": "", "candidate_proxy_ids": nil}},
	} {
		tc.fields["connection_ref"] = state.ConnectionRef
		tc.fields["expected_revision"] = 7
		if rec := kiloAccountsWrite(h, tc.method, tc.path, "CLIENT_KEY", tc.fields); rec.Code != 401 {
			t.Fatal("鉴权失败")
		}
		rec := kiloAccountsWrite(h, tc.method, tc.path, kiloRouterAdminKey, tc.fields)
		if rec.Code != 200 {
			t.Fatalf("新动作拒绝:%d %s", rec.Code, rec.Body.String())
		}
		assertKiloRouterSanitized(t, rec)
	}
	for _, tc := range []struct {
		path   string
		fields map[string]any
	}{
		{"/api/kilo/proxy-pool", map[string]any{"max_unique_accounts": 0}}, {"/api/kilo/proxy-pool", map[string]any{"max_unique_accounts": 1.5}}, {"/api/kilo/proxy-pool", map[string]any{"max_unique_accounts": nil}}, {"/api/kilo/proxy-pool", map[string]any{"auto_assignment_enabled": "true"}}, {"/api/kilo/proxies/p1", map[string]any{"fixed_exit_ip": "not-ip"}}, {"/api/kilo/proxies/p1", map[string]any{"fixed_exit_ip": nil}},
	} {
		tc.fields["connection_ref"] = state.ConnectionRef
		tc.fields["expected_revision"] = 7
		if rec := kiloAccountsWrite(h, "PATCH", tc.path, kiloRouterAdminKey, tc.fields); rec.Code != 400 {
			t.Fatalf("坏字段未拒绝:%d", rec.Code)
		}
	}
	for _, field := range []string{"exclusive_proxy_ids", "candidate_proxy_ids", "primary_proxy_id"} {
		fields := map[string]any{"mode": "auto", "exclusive_proxy_ids": []string{}, "primary_proxy_id": "", "connection_ref": state.ConnectionRef, "expected_revision": 7}
		if field == "primary_proxy_id" {
			fields[field] = "p1"
		} else {
			fields[field] = []string{"p1"}
		}
		if rec := kiloAccountsWrite(h, "PUT", "/api/kilo/accounts/a1/egress", kiloRouterAdminKey, fields); rec.Code != 400 {
			t.Fatal("auto接受手工绑定")
		}
	}
	if rec := kiloAccountsWrite(h, "POST", "/api/kilo/proxies", kiloRouterAdminKey, map[string]any{"url": "http://127.0.0.1:8080", "fixed_exit_ip": "203.0.113.10", "connection_ref": state.ConnectionRef, "expected_revision": 7}); rec.Code != 400 {
		t.Fatal("创建伪造固定IP")
	}
	if calls.Load() != 4 {
		t.Fatalf("非法字段触上游或重放:%d", calls.Load())
	}
}
func TestAE1AutoEgressProjection(t *testing.T) {
	for _, tc := range []struct {
		old, next string
		want      int
	}{
		{`"state":"assigned"`, `"state":"retired"`, 200},
		{`"state":"assigned"`, `"state":"SECRET"`, 502},
		{`"account_ids":["historic","current"]`, `"account_ids":null`, 502},
		{`"unique_accounts":2,`, ``, 502},
		{`"auto_error":null`, `"auto_error":"storage_failed"`, 200},
		{`"auto_error":null`, `"auto_error":"SECRET"`, 502},
		{`"max_unique_accounts":3`, `"max_unique_accounts":0`, 502},
		{`"allocation_reason":""`, `"allocation_reason":"storage_failed"`, 200},
		{`"allocation_reason":""`, `"allocation_reason":"SECRET"`, 502},
		{`"allocation_state":"assigned"`, `"allocation_state":"waiting"`, 200},
		{`"allocation_state":"assigned"`, `"allocation_state":"SECRET"`, 502},
	} {
		t.Run(tc.next, func(t *testing.T) {
			body := strings.Replace(autoProxyFixture(), tc.old, tc.next, 1)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					io.WriteString(w, kiloRouterConcurrencyFixture)
					return
				}
				io.WriteString(w, body)
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			rec := kiloRouterRequest(h, "GET", "/api/kilo/proxies", kiloRouterAdminKey)
			if rec.Code != tc.want {
				t.Fatalf("状态%d，预期%d:%s", rec.Code, tc.want, rec.Body.String())
			}
			assertKiloRouterSanitized(t, rec)
		})
	}
}

func TestAE1AutoDeviceContract(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.URL.Path == "/v8/management/kilo/proxies" {
			io.WriteString(w, autoProxyFixture())
			return
		}
		calls.Add(1)
		if r.URL.Path == "/v8/management/kilo/device-auth" {
			var fields map[string]json.RawMessage
			json.NewDecoder(r.Body).Decode(&fields)
			var draft map[string]any
			json.Unmarshal(fields["inference_egress"], &draft)
			if draft["mode"] != "auto" || draft["expected_revision"] != float64(7) {
				t.Error("设备auto草稿丢失")
			}
			if _, ok := fields["connection_ref"]; ok {
				t.Error("设备连接ref透传")
			}
			io.WriteString(w, strings.Replace(kiloDeviceStartFixture, `"inference_egress_mode":"pool"`, `"inference_egress_mode":"auto"`, 1))
			return
		}
		w.WriteHeader(201)
		body := strings.Replace(kiloDeviceSavedFixture, `"egress_mode":"pool"`, `"egress_mode":"auto"`, 1)
		io.WriteString(w, strings.Replace(body, "shared_proxy_not_configured", "auto_egress_waiting", 1))
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	state := kiloProxySnapshot(t, h)
	fields := kiloDeviceFields("start", state.ConnectionRef)
	fields["inference_egress"] = map[string]any{"mode": "auto", "exclusive_proxy_ids": []string{}, "primary_proxy_id": "", "expected_revision": 7}
	start := kiloAccountsWrite(h, "POST", "/api/kilo/device-auth", kiloRouterAdminKey, fields)
	if start.Code != 200 || !strings.Contains(start.Body.String(), `"inference_egress_mode":"auto"`) {
		t.Fatalf("设备auto启动失败:%s", start.Body.String())
	}
	assertKiloRouterSanitized(t, start)
	saved := kiloAccountsWrite(h, "POST", "/api/kilo/device-auth/device%201/save", kiloRouterAdminKey, kiloDeviceFields("save", state.ConnectionRef))
	if saved.Code != 201 || !strings.Contains(saved.Body.String(), `"egress_reason":"auto_egress_waiting"`) {
		t.Fatalf("设备缺IP保存被改写:%s", saved.Body.String())
	}
	assertKiloRouterSanitized(t, saved)
	if calls.Load() != 2 {
		t.Fatal("设备动作重放")
	}
}
