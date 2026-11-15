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

const kiloRecoveryFixture = `{"reason":"quota_exhausted","generation":3,"identity_revision":2,"source_revision":1,"recover_at":"2026-11-13T15:00:00Z","query_status":"success","error_code":null,"persisted":true,"access_token":"SECRET_RECOVERY","upstream_message":"SECRET_MESSAGE"}`

func recoveryAccount(raw string) string {
	return strings.TrimSuffix(kiloSavedFixture, "}") + `,"auto_restriction":` + raw + `}`
}

func TestKiloRecoveryProjectionAndExplicitEnable(t *testing.T) {
	var writes atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.Method == "GET" {
			io.WriteString(w, `{"accounts":[`+recoveryAccount(kiloRecoveryFixture)+`]}`)
			return
		}
		writes.Add(1)
		var fields map[string]any
		json.NewDecoder(r.Body).Decode(&fields)
		if _, ok := fields["connection_ref"]; ok {
			t.Error("引用被透传")
		}
		switch {
		case strings.Contains(r.URL.Path, "device-auth"):
			if len(fields) != 0 {
				t.Error("设备保存多余字段")
			}
			w.WriteHeader(201)
			io.WriteString(w, strings.TrimSuffix(recoveryAccount(kiloRecoveryFixture), "}")+`,"egress_mode":"pool","egress_status":"pending","egress_reason":"shared_proxy_not_configured"}`)
		case r.Method == "POST":
			w.WriteHeader(201)
			io.WriteString(w, recoveryAccount(kiloRecoveryFixture))
		default:
			if len(fields) != 1 || fields["enabled"] != true {
				t.Error("显式恢复没有单独同值true")
			}
			io.WriteString(w, recoveryAccount(strings.Replace(kiloRecoveryFixture, `"quota_exhausted"`, `"none"`, 1)))
		}
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	ref := kiloAccountsRef(t, h)
	for _, tc := range []struct {
		method, path string
		fields       map[string]any
		code         int
	}{{"GET", "/api/kilo/accounts", nil, 200}, {"POST", "/api/kilo/accounts", map[string]any{"api_key": "SECRET_INPUT", "connection_ref": ref}, 201}, {"PATCH", "/api/kilo/accounts/fixture-account", map[string]any{"enabled": true, "connection_ref": ref}, 200}, {"POST", "/api/kilo/device-auth/device/save", map[string]any{"connection_ref": ref}, 201}} {
		denied := quotaCall(h, tc.method, tc.path, "CLIENT", tc.fields)
		if denied.Code != 401 {
			t.Fatal("恢复投影鉴权失效")
		}
		r := quotaCall(h, tc.method, tc.path, kiloRouterAdminKey, tc.fields)
		if r.Code != tc.code {
			t.Fatalf("投影%s失败:%d %s", tc.path, r.Code, r.Body.String())
		}
		if !strings.Contains(r.Body.String(), `"auto_restriction":`) || !strings.Contains(r.Body.String(), `"identity_revision":2`) || !strings.Contains(r.Body.String(), ref) {
			t.Fatal("状态字段或引用丢失")
		}
		if strings.Contains(r.Body.String(), "SECRET") || strings.Contains(r.Body.String(), "upstream_message") {
			t.Fatal("自动状态材料泄露")
		}
	}
	if writes.Load() != 3 {
		t.Fatal("恢复重复发送")
	}
	r := kiloAccountsWrite(h, "PATCH", "/api/kilo/accounts/fixture-account", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "auto_restriction": map[string]any{"reason": "none"}})
	if r.Code != 400 || writes.Load() != 3 {
		t.Fatal("客户端直接改自动状态通过")
	}
}

func TestKiloRecoveryRejectsInvalidProjectionAndKeepsUnknown(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(kiloRecoveryFixture, `"quota_exhausted"`, `"SECRET_REASON"`, 1), strings.Replace(kiloRecoveryFixture, `"generation":3`, `"generation":-1`, 1), strings.Replace(kiloRecoveryFixture, `"identity_revision":2`, `"identity_revision":1.5`, 1), strings.Replace(kiloRecoveryFixture, `"source_revision":1`, `"source_revision":9007199254740992`, 1), strings.Replace(kiloRecoveryFixture, `"persisted":true`, `"persisted":null`, 1), strings.Replace(kiloRecoveryFixture, `"persisted":true`, `"persisted":"true"`, 1), strings.Replace(kiloRecoveryFixture, `"query_status":"success"`, `"query_status":"SECRET_QUERY"`, 1), strings.Replace(kiloRecoveryFixture, `"error_code":null`, `"error_code":"SECRET_ERROR"`, 1), strings.Replace(kiloRecoveryFixture, `"recover_at":"2026-11-13T15:00:00Z"`, `"recover_at":"2026-11-13T15:00:00+08:00"`, 1), strings.Replace(kiloRecoveryFixture, `"source_revision":1,`, "", 1), `[]`,
	} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v8/management/credentials/concurrency" {
				io.WriteString(w, kiloRouterConcurrencyFixture)
				return
			}
			if r.URL.Path == "/v8/management/kilo/proxies" {
				io.WriteString(w, kiloProxyFixture)
				return
			}
			if r.Method == "GET" {
				io.WriteString(w, `{"accounts":[`+recoveryAccount(raw)+`]}`)
				return
			}
			if r.Method == "POST" {
				w.WriteHeader(201)
			}
			body := recoveryAccount(raw)
			if strings.Contains(r.URL.Path, "device-auth") {
				body = strings.TrimSuffix(body, "}") + `,"egress_mode":"pool","egress_status":"pending","egress_reason":"shared_proxy_not_configured"}`
			}
			io.WriteString(w, body)
		}))
		h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
		if r := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey); r.Code != 502 || strings.Contains(r.Body.String(), "SECRET") {
			t.Fatalf("非法列表未脱敏拒绝:%d", r.Code)
		}
		ref := kiloProxySnapshot(t, h).ConnectionRef
		for _, tc := range []struct {
			method, path string
			fields       map[string]any
		}{{"PATCH", "/api/kilo/accounts/id", map[string]any{"enabled": true, "connection_ref": ref}}, {"POST", "/api/kilo/device-auth/device/save", map[string]any{"connection_ref": ref}}} {
			if r := kiloAccountsWrite(h, tc.method, tc.path, kiloRouterAdminKey, tc.fields); r.Code != 502 || strings.Contains(r.Body.String(), "SECRET") {
				t.Fatal("非法写/设备状态未拒绝")
			}
		}
		up.Close()
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		io.WriteString(w, `{"accounts":[`+kiloSavedFixture+`]}`)
	}))
	defer up.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: up.URL, ManagementKey: kiloRouterManagementKey}}, up.Client())
	r := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	if r.Code != 200 || strings.Contains(r.Body.String(), "auto_restriction") {
		t.Fatal("缺字段被虚构为none")
	}
}
