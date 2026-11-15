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

const sourcePolicyFixture = `{"enabled":false,"interval_seconds":1800,"revision":0,"paused":false,"pause_generation":0,"scope":"","status":"disabled","observed_at":null,"next_query_at":null,"error_code":null,"persisted":true,"connection_ref":"UPSTREAM_REF","api_key":"SECRET"}`
const sourcePolicyPath = "/api/kilo/accounts/quota%201/source-policy"

func sourcePolicyFields(ref string) map[string]any {
	return map[string]any{"connection_ref": ref, "expected_policy_revision": 0, "enabled": true, "interval_seconds": 1800, "confirm_scope": true}
}
func sourcePolicyPrerequisite(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/v8/management/credentials/concurrency":
		io.WriteString(w, kiloRouterConcurrencyFixture)
		return true
	case "/v8/management/kilo/accounts":
		io.WriteString(w, `{"accounts":[]}`)
		return true
	}
	return false
}
func TestAE1SourcePolicyContract(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sourcePolicyPrerequisite(w, r) {
			return
		}
		calls.Add(1)
		if r.URL.EscapedPath() != "/v8/management/kilo/accounts/quota%201/source-policy" || r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("上游路径或认证错误")
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "connection_ref") {
			t.Error("连接引用被透传")
		}
		if r.Method == "GET" && len(raw) != 0 {
			t.Error("读取携带正文")
		}
		if r.Method == "PUT" {
			var fields map[string]any
			if json.Unmarshal(raw, &fields) != nil || fields["expected_policy_revision"] != float64(0) || fields["enabled"] != true || fields["interval_seconds"] != float64(1800) || fields["confirm_scope"] != true {
				t.Error("策略字段丢失")
			}
		}
		io.WriteString(w, sourcePolicyFixture)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloAccountsRef(t, h)
	for _, method := range []string{"GET", "PUT"} {
		if rec := quotaCall(h, method, sourcePolicyPath, "CLIENT_KEY", sourcePolicyFields(ref)); rec.Code != 401 {
			t.Fatal("管理鉴权失败")
		}
		rec := quotaCall(h, method, sourcePolicyPath, kiloRouterAdminKey, sourcePolicyFields(ref))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s=%d %s", method, rec.Code, rec.Body.String())
		}
		assertKiloRouterSanitized(t, rec)
		var policy model.KiloSourcePolicy
		if json.Unmarshal(rec.Body.Bytes(), &policy) != nil || policy.ConnectionRef != ref || policy.Enabled || policy.IntervalSeconds != 1800 || policy.ObservedAt != nil {
			t.Fatal("默认值或空值被修改")
		}
	}
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		if rec := quotaCall(h, method, sourcePolicyPath, kiloRouterAdminKey, sourcePolicyFields(ref)); rec.Code != 405 {
			t.Fatalf("方法未拦截:%d", rec.Code)
		}
	}
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"expected_policy_revision", nil}, {"expected_policy_revision", -1}, {"expected_policy_revision", 1.5}, {"expected_policy_revision", "0"}, {"expected_policy_revision", 9007199254740992.0},
		{"enabled", nil}, {"enabled", "true"}, {"interval_seconds", 59}, {"interval_seconds", 86401}, {"interval_seconds", 60.5}, {"interval_seconds", nil}, {"confirm_scope", nil}, {"confirm_scope", 1}, {"secret", "SECRET"},
	} {
		fields := sourcePolicyFields(ref)
		fields[tc.key] = tc.value
		if rec := kiloAccountsWrite(h, "PUT", sourcePolicyPath, kiloRouterAdminKey, fields); rec.Code != 400 {
			t.Fatalf("坏字段%s未拒绝:%d", tc.key, rec.Code)
		}
	}
	for _, key := range []string{"expected_policy_revision", "enabled", "interval_seconds"} {
		fields := sourcePolicyFields(ref)
		delete(fields, key)
		if rec := kiloAccountsWrite(h, "PUT", sourcePolicyPath, kiloRouterAdminKey, fields); rec.Code != 400 {
			t.Fatalf("缺失%s未拒绝", key)
		}
	}
	for _, tc := range []struct{ method, path, body string }{{"GET", sourcePolicyPath + "?secret=x", ""}, {"GET", sourcePolicyPath, "{}"}, {"PUT", sourcePolicyPath, "{} {}"}, {"PUT", sourcePolicyPath, "null"}} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("多余输入未拒绝:%d", rec.Code)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("非法请求触上游或重放:%d", calls.Load())
	}
}
func TestAE1SourcePolicyDriftErrorsCancellation(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		for _, scenario := range []string{"pre", "post", "cancel", "400", "401", "403", "404", "409", "502", "503"} {
			t.Run(method+scenario, func(t *testing.T) {
				resolver := &kiloRouterResolver{}
				var calls atomic.Int32
				var armed atomic.Bool
				reached := make(chan struct{})
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v8/management/credentials/concurrency" && armed.Load() && scenario == "pre" && method == "GET" {
						resolver.replace(store.Setup{})
					}
					if sourcePolicyPrerequisite(w, r) {
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
						io.WriteString(w, sourcePolicyFixture)
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
				if scenario == "pre" && method == "PUT" {
					resolver.replace(store.Setup{CPAUpstreamURL: up.URL, ManagementKey: "CHANGED"})
				}
				var body io.Reader
				if method == "PUT" {
					raw, _ := json.Marshal(sourcePolicyFields(ref))
					body = strings.NewReader(string(raw))
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req := httptest.NewRequest(method, sourcePolicyPath, body).WithContext(ctx)
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
				if rec.Code != want || calls.Load() != count {
					t.Fatalf("状态/次数=%d/%d，预期%d/%d", rec.Code, calls.Load(), want, count)
				}
				assertKiloRouterSanitized(t, rec)
			})
		}
	}
}
func TestAE1SourcePolicyProjection(t *testing.T) {
	var policyBody atomic.Value
	policyBody.Store(sourcePolicyFixture)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sourcePolicyPrerequisite(w, r) {
			return
		}
		io.WriteString(w, policyBody.Load().(string))
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	for _, status := range []string{"disabled", "pending", "ready", "paused", "source_pending", "query_failed", "waiting_query_egress", "storage_failed"} {
		policyBody.Store(strings.Replace(sourcePolicyFixture, `"status":"disabled"`, `"status":"`+status+`"`, 1))
		rec := kiloRouterRequest(h, "GET", sourcePolicyPath, kiloRouterAdminKey)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"`+status+`"`) {
			t.Fatalf("合法状态%s丢失:%d", status, rec.Code)
		}
		assertKiloRouterSanitized(t, rec)
	}
	for _, tc := range []struct {
		key   string
		value any
	}{{"enabled", nil}, {"paused", nil}, {"persisted", nil}, {"revision", -1}, {"revision", 9007199254740992.0}, {"pause_generation", 1.5}, {"interval_seconds", 59}, {"status", "ready_secret"}, {"scope", "unknown"}, {"observed_at", "bad-time"}, {"next_query_at", 123}, {"error_code", "SECRET"}} {
		var fields map[string]any
		json.Unmarshal([]byte(sourcePolicyFixture), &fields)
		fields[tc.key] = tc.value
		raw, _ := json.Marshal(fields)
		policyBody.Store(string(raw))
		rec := kiloRouterRequest(h, "GET", sourcePolicyPath, kiloRouterAdminKey)
		if rec.Code != 502 {
			t.Fatalf("坏回执%s未拒绝:%d", tc.key, rec.Code)
		}
		assertKiloRouterSanitized(t, rec)
	}
	var fields map[string]any
	json.Unmarshal([]byte(sourcePolicyFixture), &fields)
	for _, key := range []string{"enabled", "interval_seconds", "revision", "paused", "pause_generation", "scope", "status", "observed_at", "next_query_at", "error_code", "persisted"} {
		value := fields[key]
		delete(fields, key)
		raw, _ := json.Marshal(fields)
		policyBody.Store(string(raw))
		rec := kiloRouterRequest(h, "GET", sourcePolicyPath, kiloRouterAdminKey)
		if rec.Code != 502 {
			t.Fatalf("缺失回执字段%s未拒绝", key)
		}
		fields[key] = value
	}
	for _, code := range []string{"query_egress_unavailable", "storage_failed", "usage_network_error"} {
		fields["error_code"] = code
		fields["scope"] = "18000+604800"
		fields["observed_at"] = "2026-11-14T12:00:00+08:00"
		raw, _ := json.Marshal(fields)
		policyBody.Store(string(raw))
		rec := kiloRouterRequest(h, "GET", sourcePolicyPath, kiloRouterAdminKey)
		if rec.Code != 200 {
			t.Fatalf("合法回执被拒绝:%s", rec.Body.String())
		}
	}
}
func TestAE1UsedPercentProjection(t *testing.T) {
	for _, value := range []string{"99.999", "null", "0", "110", "-1"} {
		t.Run(value, func(t *testing.T) {
			body := strings.Replace(quotaViewFixture, `"remaining_percent":75`, `"remaining_percent":75,"used_percent":`+value, 1)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if sourcePolicyPrerequisite(w, r) {
					return
				}
				io.WriteString(w, body)
			}))
			defer up.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
			rec := kiloRouterRequest(h, "GET", "/api/kilo/accounts/quota%201/quota", kiloRouterAdminKey)
			if value == "-1" {
				if rec.Code != 502 {
					t.Fatal("负用量未拒绝")
				}
				return
			}
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"used_percent":`+value) {
				t.Fatalf("原始比例被改写:%s", rec.Body.String())
			}
		})
	}
}

func TestAE1QueryEgressUnavailableProjection(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sourcePolicyPrerequisite(w, r) {
			return
		}
		calls.Add(1)
		body := strings.Replace(quotaViewFixture, `"status":"success"`, `"status":"failed"`, 1)
		body = strings.Replace(body, `"error_code":null`, `"error_code":"query_egress_unavailable"`, 1)
		body = strings.Replace(body, `"stale":false`, `"stale":true`, 1)
		io.WriteString(w, body)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloAccountsRef(t, h)
	rec := kiloAccountsWrite(h, "POST", "/api/kilo/accounts/quota%201/quota/query", kiloRouterAdminKey, quotaFields("query", ref))
	var view model.KiloQuotaView
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &view) != nil || view.Query.Status != "failed" || view.Query.ErrorCode == nil || *view.Query.ErrorCode != "query_egress_unavailable" || view.ConnectionRef != ref || !view.Stale {
		t.Fatalf("查询出口缺失被改写:%d %s", rec.Code, rec.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatal("业务失败被重放")
	}
	assertKiloRouterSanitized(t, rec)
}
