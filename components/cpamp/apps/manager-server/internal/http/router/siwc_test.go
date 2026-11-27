package router

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	sqliterepo "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/sqlite"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/security"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/adminauth"
	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	siwcsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/siwc"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const siwcAdminKey = "cpamp_SYNTHETIC_SIWC_ADMIN"
const siwcManagementKey = "SYNTHETIC_SIWC_MANAGEMENT"

type siwcFixture struct {
	t                 *testing.T
	mu                sync.Mutex
	resolver          *kiloRouterResolver
	handler           http.Handler
	credential        map[string]any
	mode              string
	status            string
	cancelled         bool
	responseStatus    map[string]int
	calls             map[string]int
	callback          map[string]string
	callbackSubmitted chan struct{}
	callbackRelease   chan struct{}
}

func newSIWCFixture(t *testing.T) *siwcFixture {
	t.Helper()
	f := &siwcFixture{t: t, status: "wait", mode: "local", cancelled: true, responseStatus: map[string]int{}, calls: map[string]int{},
		credential: map[string]any{"id": "fixture.json", "auth_index": "fixture-index", "provider": "codex", "source": "file", "runtime_only": false,
			"siwc_bind_supported": true, "use_siwc": false, "siwc_status": "off", "siwc_bound": false,
			"access_token": "SECRET_ACCESS", "siwc_refresh_token": "SECRET_REFRESH", "metadata": map[string]string{"client_id": "SECRET_CLIENT"}}}
	upstream := httptest.NewServer(http.HandlerFunc(f.serveCPA))
	t.Cleanup(upstream.Close)
	f.resolver = &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: siwcManagementKey}}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := sqliterepo.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	admin, err := security.NewAdminCredential(siwcAdminKey, "siwc-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAdminCredential(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{CORSOrigins: []string{"http://fixture.invalid"}}
	connections := concurrency.New(f.resolver, nil)
	f.handler = New(&app.Context{Config: cfg, Store: st, AdminAuthService: adminauth.New(cfg, st),
		CredentialConcurrencyService: connections, SIWCService: siwcsvc.New(f.resolver, connections, upstream.Client())})
	return f
}

func (f *siwcFixture) serveCPA(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+siwcManagementKey {
		f.t.Error("上游管理认证未按绑定连接发送")
	}
	f.calls[r.Method+" "+r.URL.Path]++
	if status := f.responseStatus[r.URL.Path]; status != 0 {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"SECRET_UPSTREAM_ERROR"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method + " " + r.URL.Path {
	case "GET /v8/management/credentials/concurrency":
		_ = json.NewEncoder(w).Encode(map[string]any{"mode": f.mode, "credentials": []any{}, "default_limit": 20, "pool_waiting": 0})
	case "GET /v8/management/credentials":
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{f.credential}, "token": "SECRET_TOPLEVEL"})
	case "POST /v8/management/oauth/siwc/bind":
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input) != 1 || input["name"] != "fixture.json" {
			f.t.Error("未按精确凭证id绑定")
		}
		state := fmt.Sprintf("synthetic-state-%d", f.calls[r.Method+" "+r.URL.Path])
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "flow": "siwc_bind", "url": "https://auth.invalid/authorize?client_id=dynamic_agent_client&state=" + state, "state": state, "expires_in": 300, "secret": "SECRET_BIND"})
	case "POST /v8/management/oauth/callback":
		if err := json.NewDecoder(r.Body).Decode(&f.callback); err != nil {
			f.t.Error(err)
		}
		if f.callbackSubmitted != nil {
			close(f.callbackSubmitted)
			<-f.callbackRelease
		}
		_, _ = w.Write([]byte(`{"status":"ok","secret":"SECRET_CALLBACK"}`))
	case "GET /v8/management/oauth/status":
		if r.URL.Query().Get("state") == "" {
			f.t.Error("空state触达上游")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": f.status, "error": "SECRET_STATUS_ERROR"})
	case "DELETE /v8/management/oauth/session":
		if r.URL.Query().Get("state") == "" {
			f.t.Error("空state取消触达上游")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "cancelled": f.cancelled, "secret": "SECRET_CANCEL"})
	default:
		f.t.Errorf("意外上游路径: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
	}
}

func (f *siwcFixture) request(method, path, key string, input any) *httptest.ResponseRecorder {
	var body *bytes.Reader
	if raw, ok := input.(string); ok {
		body = bytes.NewReader([]byte(raw))
	} else if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			f.t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Origin", "http://fixture.invalid")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func assertSIWCResponse(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("状态=%d，期望=%d: %s", rec.Code, status, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("SIWC回执缺少no-store")
	}
	for _, secret := range []string{"SECRET_", siwcAdminKey, siwcManagementKey, "access_token", "refresh_token", "SYNTHETIC_AUTH_CODE"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("SIWC回执暴露秘密: %s", secret)
		}
	}
}

func (f *siwcFixture) identity() model.SIWCIdentity {
	f.t.Helper()
	rec := f.request("GET", "/api/siwc/credential?auth_id=fixture.json&auth_index=fixture-index", siwcAdminKey, nil)
	assertSIWCResponse(f.t, rec, 200)
	var credential model.SIWCCredential
	if err := json.Unmarshal(rec.Body.Bytes(), &credential); err != nil || len(credential.ConnectionRef) != 32 {
		f.t.Fatalf("连接引用无效: %s", rec.Body.String())
	}
	return credential.SIWCIdentity
}

func (f *siwcFixture) bind() model.SIWCSessionInput {
	f.t.Helper()
	identity := f.identity()
	rec := f.request("POST", "/api/siwc/bind", siwcAdminKey, identity)
	assertSIWCResponse(f.t, rec, 200)
	var result model.SIWCBindResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Status != "wait" || result.Flow != "siwc_bind" || result.ExpiresIn != 300 {
		f.t.Fatalf("链接生成被当作绑定完成: %s", rec.Body.String())
	}
	return model.SIWCSessionInput{SIWCIdentity: identity, State: result.State}
}

func siwcSessionPath(resource string, input model.SIWCSessionInput) string {
	return "/api/siwc/" + resource + "?" + url.Values{"connection_ref": {input.ConnectionRef}, "auth_id": {input.AuthID}, "auth_index": {input.AuthIndex}, "state": {input.State}}.Encode()
}

func siwcCallback(input model.SIWCSessionInput) model.SIWCCallbackInput {
	return model.SIWCCallbackInput{SIWCSessionInput: input,
		RedirectURL: "http://127.0.0.1:1455/auth/callback?state=" + input.State + "&code=SYNTHETIC_AUTH_CODE&client_id=issued-client%2Bvalue"}
}

func TestSIWCRouterAdminBoundary(t *testing.T) {
	f := newSIWCFixture(t)
	for _, route := range []struct{ method, path string }{{"GET", "credential"}, {"POST", "bind"}, {"POST", "callback"}, {"GET", "status"}, {"DELETE", "session"}} {
		for _, key := range []string{"", "SYNTHETIC_CLIENT_KEY", siwcManagementKey} {
			rec := f.request(route.method, "/api/siwc/"+route.path, key, nil)
			assertSIWCResponse(t, rec, 401)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("非管理员请求触达CPA")
	}
}

func TestSIWCRouterCapabilityAndIdentity(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*siwcFixture)
		query  string
		status int
		code   string
	}{
		{"supported", func(f *siwcFixture) { f.credential["siwc_expires_at"] = "2026-12-01T00:00:00Z" }, "", 200, `"siwc_bind_supported":true`},
		{"runtime", func(f *siwcFixture) { f.credential["runtime_only"] = true }, "", 200, "credential_ineligible"},
		{"non_file", func(f *siwcFixture) { f.credential["source"] = "memory" }, "", 200, "credential_ineligible"},
		{"home_file", func(f *siwcFixture) { f.mode = "home" }, "", 200, "home_managed"},
		{"not_codex", func(f *siwcFixture) { f.credential["provider"] = "bps" }, "", 400, "credential_not_codex"},
		{"old_fields", func(f *siwcFixture) { delete(f.credential, "siwc_bind_supported") }, "", 501, "siwc_unsupported"},
		{"old_route", func(f *siwcFixture) { f.responseStatus["/v8/management/credentials"] = 404 }, "", 501, "siwc_unsupported"},
		{"wrong_id", func(*siwcFixture) {}, "auth_id=other&auth_index=fixture-index", 409, "credential_identity_changed"},
		{"wrong_index", func(*siwcFixture) {}, "auth_id=fixture.json&auth_index=other", 409, "credential_identity_changed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newSIWCFixture(t)
			scenario.change(f)
			query := scenario.query
			if query == "" {
				query = "auth_id=fixture.json&auth_index=fixture-index"
			}
			rec := f.request("GET", "/api/siwc/credential?"+query, siwcAdminKey, nil)
			assertSIWCResponse(t, rec, scenario.status)
			if !strings.Contains(rec.Body.String(), scenario.code) {
				t.Fatalf("能力分类错误: %s", rec.Body.String())
			}
			if scenario.code == "credential_ineligible" || scenario.code == "home_managed" {
				var credential model.SIWCCredential
				_ = json.Unmarshal(rec.Body.Bytes(), &credential)
				assertSIWCResponse(t, f.request("POST", "/api/siwc/bind", siwcAdminKey, credential.SIWCIdentity), 409)
				if f.calls["POST /v8/management/oauth/siwc/bind"] != 0 {
					t.Fatal("只读凭证发起绑定")
				}
			}
		})
	}
}

func TestSIWCRouterBindDuplicateAndInvalidReference(t *testing.T) {
	f := newSIWCFixture(t)
	input := f.bind()
	assertSIWCResponse(t, f.request("POST", "/api/siwc/bind", siwcAdminKey, input.SIWCIdentity), 409)
	invalid := input.SIWCIdentity
	invalid.ConnectionRef = "invalid-reference"
	assertSIWCResponse(t, f.request("POST", "/api/siwc/bind", siwcAdminKey, invalid), 409)
	if f.calls["POST /v8/management/oauth/siwc/bind"] != 1 {
		t.Fatal("重复点击生成多个上游会话")
	}
}

func TestSIWCRouterCallbackAndCompletion(t *testing.T) {
	for _, errorOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("error_only_%t", errorOnly), func(t *testing.T) {
			f := newSIWCFixture(t)
			input := f.bind()
			callback := siwcCallback(input)
			if errorOnly {
				callback.RedirectURL = "http://127.0.0.1:1455/auth/callback?state=" + input.State + "&error=access_denied&error_description=user%20cancelled"
			}
			var logs bytes.Buffer
			previousLog := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(previousLog)
			rec := f.request("POST", "/api/siwc/callback", siwcAdminKey, callback)
			assertSIWCResponse(t, rec, 200)
			if !strings.Contains(rec.Body.String(), `"status":"wait"`) || f.callback["redirect_url"] != callback.RedirectURL || f.callback["provider"] != "codex" || len(f.callback) != 2 {
				t.Fatal("完整回调被改写或提交被当作完成")
			}
			assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, callback), 409)
			rec = f.request("GET", siwcSessionPath("status", input), siwcAdminKey, nil)
			assertSIWCResponse(t, rec, 200)
			if !strings.Contains(rec.Body.String(), `"status":"wait"`) || f.calls["GET /v8/management/credentials"] != 2 {
				t.Fatal("等待阶段错误回读凭证并当作完成")
			}
			if errorOnly {
				f.status = "error"
			} else {
				f.status = "ok"
				f.credential["siwc_bound"] = true
				f.credential["use_siwc"] = true
				f.credential["siwc_status"] = "valid"
			}
			rec = f.request("GET", siwcSessionPath("status", input), siwcAdminKey, nil)
			assertSIWCResponse(t, rec, 200)
			if errorOnly {
				if !strings.Contains(rec.Body.String(), "siwc_session_failed") || strings.Contains(rec.Body.String(), `"credential"`) {
					t.Fatal("授权拒绝状态未明确保留")
				}
			} else if !strings.Contains(rec.Body.String(), `"siwc_bound":true`) || f.calls["GET /v8/management/credentials"] != 3 {
				t.Fatal("完成后缺少精确凭证回读")
			}
			for _, secret := range []string{"SYNTHETIC_AUTH_CODE", "issued-client", input.State, siwcAdminKey, siwcManagementKey, "access_denied"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("请求日志泄露回调数据: %s", secret)
				}
			}
		})
	}
}

func TestSIWCRouterRejectsInvalidCallback(t *testing.T) {
	f := newSIWCFixture(t)
	input := f.bind()
	for _, redirect := range []string{
		"http://127.0.0.1:1455/auth/callback?state=wrong&code=x&client_id=y",
		"http://127.0.0.1:1455/auth/callback?state=" + input.State + "&code=x",
		"http://127.0.0.1:1455/auth/callback?state=" + input.State + "&state=other&error=denied",
		"http://127.0.0.1:1455/auth/callback?state=" + input.State + "&code=x&client_id=y&client_id=z",
		"http://other.invalid/auth/callback?state=" + input.State + "&error=denied",
		"not-a-callback",
	} {
		callback := model.SIWCCallbackInput{SIWCSessionInput: input, RedirectURL: redirect}
		assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, callback), 400)
	}
	wrong := siwcCallback(input)
	wrong.AuthIndex = "another-index"
	assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, wrong), 409)
	if f.calls["POST /v8/management/oauth/callback"] != 0 {
		t.Fatal("无效回调触达上游")
	}
}

func TestSIWCRouterCancelledSessionRejectsLateCallback(t *testing.T) {
	f := newSIWCFixture(t)
	input := f.bind()
	for range 2 {
		rec := f.request("DELETE", siwcSessionPath("session", input), siwcAdminKey, nil)
		assertSIWCResponse(t, rec, 200)
		if !strings.Contains(rec.Body.String(), `"cancelled":true`) || !strings.Contains(rec.Body.String(), `"status":"cancelled"`) {
			t.Fatalf("取消状态错误: %s", rec.Body.String())
		}
	}
	assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, siwcCallback(input)), 409)
	if f.calls["POST /v8/management/oauth/callback"] != 0 || f.calls["DELETE /v8/management/oauth/session"] != 1 {
		t.Fatal("取消后仍转发回调或重复取消")
	}
	// 已结束会话释放凭证，允许用户重新生成链接。
	assertSIWCResponse(t, f.request("POST", "/api/siwc/bind", siwcAdminKey, input.SIWCIdentity), 200)
}

func TestSIWCRouterCancelFalseChecksSameState(t *testing.T) {
	for _, status := range []string{"wait", "error", "ok"} {
		t.Run(status, func(t *testing.T) {
			f := newSIWCFixture(t)
			f.credential["siwc_bound"] = true
			input := f.bind()
			f.cancelled, f.status = false, status
			rec := f.request("DELETE", siwcSessionPath("session", input), siwcAdminKey, nil)
			assertSIWCResponse(t, rec, 200)
			var result model.SIWCCancelResult
			_ = json.Unmarshal(rec.Body.Bytes(), &result)
			if result.Cancelled || result.Status != status || f.calls["GET /v8/management/oauth/status"] != 1 {
				t.Fatalf("cancelled=false被误判: %s", rec.Body.String())
			}
			if (result.Credential != nil) != (status == "ok") {
				t.Fatal("旧bound摘要冒充本次重绑完成")
			}
			if status == "error" && result.Code != "siwc_session_failed" {
				t.Fatal("缺少会话失败分类")
			}
		})
	}
}

func TestSIWCRouterConnectionChangeNeverForwardsOldState(t *testing.T) {
	f := newSIWCFixture(t)
	input := f.bind()
	newTargetCalls := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newTargetCalls++; w.WriteHeader(500) }))
	defer other.Close()
	f.resolver.replace(store.Setup{CPAUpstreamURL: other.URL, ManagementKey: "OTHER_MANAGEMENT"})
	assertSIWCResponse(t, f.request("GET", siwcSessionPath("status", input), siwcAdminKey, nil), 409)
	assertSIWCResponse(t, f.request("DELETE", siwcSessionPath("session", input), siwcAdminKey, nil), 409)
	assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, siwcCallback(input)), 409)
	if newTargetCalls != 0 || f.calls["GET /v8/management/oauth/status"] != 0 || f.calls["DELETE /v8/management/oauth/session"] != 0 || f.calls["POST /v8/management/oauth/callback"] != 0 {
		t.Fatal("连接切换后仍向CPA转发旧会话")
	}
}

func TestSIWCRouterOldBindRouteAndUnknownState(t *testing.T) {
	f := newSIWCFixture(t)
	identity := f.identity()
	f.responseStatus["/v8/management/oauth/siwc/bind"] = 404
	rec := f.request("POST", "/api/siwc/bind", siwcAdminKey, identity)
	assertSIWCResponse(t, rec, 501)
	if !strings.Contains(rec.Body.String(), "siwc_unsupported") {
		t.Fatal("旧内核未明确提示不支持")
	}
	input := model.SIWCSessionInput{SIWCIdentity: identity, State: "unknown-state"}
	assertSIWCResponse(t, f.request("GET", siwcSessionPath("status", input), siwcAdminKey, nil), 410)
	input.State = ""
	assertSIWCResponse(t, f.request("GET", siwcSessionPath("status", input), siwcAdminKey, nil), 400)
	if f.calls["GET /v8/management/oauth/status"] != 0 {
		t.Fatal("未登记state触达上游")
	}
}

func TestSIWCRouterCallbackFailureDoesNotResubmit(t *testing.T) {
	f := newSIWCFixture(t)
	input := f.bind()
	f.responseStatus["/v8/management/oauth/callback"] = 503
	callback := siwcCallback(input)
	assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, callback), 502)
	assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, callback), 409)
	assertSIWCResponse(t, f.request("GET", siwcSessionPath("status", input), siwcAdminKey, nil), 200)
	if f.calls["POST /v8/management/oauth/callback"] != 1 {
		t.Fatal("不确定回执导致授权码重复提交")
	}
}

func TestSIWCRouterCallbackInputRejectionAllowsCorrection(t *testing.T) {
	f := newSIWCFixture(t)
	input := f.bind()
	f.responseStatus["/v8/management/oauth/callback"] = 400
	callback := siwcCallback(input)
	rec := f.request("POST", "/api/siwc/callback", siwcAdminKey, callback)
	assertSIWCResponse(t, rec, 400)
	if !strings.Contains(rec.Body.String(), "siwc_callback_invalid") {
		t.Fatal("未接收的回调未提示修正")
	}
	delete(f.responseStatus, "/v8/management/oauth/callback")
	callback.RedirectURL += "&updated=true"
	assertSIWCResponse(t, f.request("POST", "/api/siwc/callback", siwcAdminKey, callback), 200)
	if f.calls["POST /v8/management/oauth/callback"] != 2 || f.callback["redirect_url"] != callback.RedirectURL {
		t.Fatal("明确输入拒绝后未允许提交修正回调")
	}
}

func TestSIWCRouterRestartExpiresSessionAndAllowsNewBind(t *testing.T) {
	for _, operation := range []struct{ method, resource, upstream string }{
		{"POST", "callback", "/v8/management/oauth/callback"},
		{"GET", "status", "/v8/management/oauth/status"},
		{"DELETE", "session", "/v8/management/oauth/session"},
	} {
		t.Run(operation.resource, func(t *testing.T) {
			f := newSIWCFixture(t)
			input := f.bind()
			f.responseStatus[operation.upstream] = 404
			path := siwcSessionPath(operation.resource, input)
			var body any
			if operation.resource == "callback" {
				path, body = "/api/siwc/callback", siwcCallback(input)
			}
			rec := f.request(operation.method, path, siwcAdminKey, body)
			assertSIWCResponse(t, rec, 410)
			if !strings.Contains(rec.Body.String(), "siwc_session_expired") {
				t.Fatal("CPA重启会话丢失被误判为能力缺失")
			}
			assertSIWCResponse(t, f.request("POST", "/api/siwc/bind", siwcAdminKey, input.SIWCIdentity), 200)
		})
	}
}

func TestSIWCRouterConcurrentBind(t *testing.T) {
	f := newSIWCFixture(t)
	identity := f.identity()
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { <-start; results <- f.request("POST", "/api/siwc/bind", siwcAdminKey, identity) }()
	}
	close(start)
	statuses := map[int]int{}
	for range 2 {
		rec := <-results
		statuses[rec.Code]++
	}
	if statuses[200] != 1 || statuses[409] != 1 || f.calls["POST /v8/management/oauth/siwc/bind"] != 1 {
		t.Fatalf("并发生成重复会话: %v", statuses)
	}
}

func TestSIWCRouterConcurrentCallback(t *testing.T) {
	f := newSIWCFixture(t)
	input := f.bind()
	f.callbackSubmitted, f.callbackRelease = make(chan struct{}), make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	go func() { results <- f.request("POST", "/api/siwc/callback", siwcAdminKey, siwcCallback(input)) }()
	<-f.callbackSubmitted
	go func() { results <- f.request("POST", "/api/siwc/callback", siwcAdminKey, siwcCallback(input)) }()
	close(f.callbackRelease)
	statuses := map[int]int{}
	for range 2 {
		statuses[(<-results).Code]++
	}
	if statuses[200] != 1 || statuses[409] != 1 || f.calls["POST /v8/management/oauth/callback"] != 1 {
		t.Fatalf("并发提交重复转发授权码: %v", statuses)
	}
}

func TestSIWCRouterInputAndRouteBoundaries(t *testing.T) {
	f := newSIWCFixture(t)
	for _, input := range []string{`{"auth_id":"fixture.json","auth_index":"fixture-index","extra":true}`, `{}` + `{}`, strings.Repeat(" ", 17<<10) + `{}`} {
		assertSIWCResponse(t, f.request("POST", "/api/siwc/bind", siwcAdminKey, input), 400)
	}
	for _, resource := range []string{"credential", "bind", "callback", "status", "session"} {
		assertSIWCResponse(t, f.request("PATCH", "/api/siwc/"+resource, siwcAdminKey, nil), 405)
	}
	rec := f.request("POST", "/v8/management/oauth/siwc/bind", siwcAdminKey, nil)
	if rec.Code != 404 || len(f.calls) != 0 {
		t.Fatal("SIWC实现扩大了通用v8代理入口")
	}
}
