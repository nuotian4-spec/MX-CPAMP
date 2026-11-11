package oauthlogin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	login "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/oauthlogin"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type fixedSetup struct{ setup store.Setup }

func (s fixedSetup) ResolveSetup(context.Context) (store.Setup, bool, error) {
	return s.setup, true, nil
}

func TestCallbackValidationThroughServiceAndHandler(t *testing.T) {
	tests := []struct {
		name, body, code string
		status           int
	}{
		{"url", `{"error":"invalid_callback_url","detail":"RAW_TOKEN_CANARY"}`, "invalid_callback_url", 400},
		{"query", `{"code":"invalid_callback_query"}`, "invalid_callback_query", 400},
		{"conflict", `{"error":"conflicting_callback_parameters"}`, "conflicting_callback_parameters", 400},
		{"state_mismatch", `{"error":"callback_state_mismatch"}`, "callback_state_mismatch", 400},
		{"code_required", `{"error":"callback_code_required"}`, "callback_code_required", 400},
		{"provider_mismatch", `{"error":"oauth_provider_mismatch"}`, "oauth_provider_mismatch", 400},
		{"body", `{"error":"invalid_body"}`, "invalid_body", 400},
		{"state", `{"error":"invalid_state"}`, "invalid_state", 400},
		{"provider", `{"error":"unsupported_provider"}`, "unsupported_provider", 400},
		{"unknown_400", `{"error":"RAW_TOKEN_CANARY"}`, "", 400},
		{"invalid_json", `RAW_TOKEN_CANARY`, "", 400},
		{"partial_decode_failure", `{"code":"invalid_callback_url","error":{}}`, "", 400},
		{"trailing_json_failure", `{"error":"invalid_callback_url"}RAW_TOKEN_CANARY`, "", 400},
		{"trailing_second_object", `{"error":"invalid_callback_url"}{}`, "", 400},
		{"truncated_json", `{"error":"invalid_callback_url","detail":"`, "", 400},
		{"oversized_body", `{"error":"invalid_callback_url"}` + strings.Repeat(" ", 4096), "", 400},
		{"upstream_5xx", `{"error":"invalid_callback_url","detail":"RAW_TOKEN_CANARY"}`, "", 500},
		{"decode_failure", `RAW_TOKEN_CANARY`, "", 200},
		{"transport_failure", "", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var callbacks atomic.Int32
			cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/auth-url"):
					_, _ = w.Write([]byte(`{"url":"https://auth.invalid/authorize","state":"state"}`))
				case strings.HasSuffix(r.URL.Path, "/callback") && callbacks.Add(1) == 1:
					if tc.status == 0 {
						connection, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = connection.Close()
						return
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				default:
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				}
			}))
			defer cpa.Close()
			mutations := cpaauthfiles.NewMutationCoordinator()
			service := login.New(fixedSetup{store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "SYNTHETIC_KEY"}}, mutations, "", "")
			h := &Handler{Service: service, Auth: auth(true)}
			call := func(action, body string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				h.Handle(w, httptest.NewRequest(http.MethodPost, "/api/oauth-login/oauth/"+action, strings.NewReader(body)))
				return w
			}
			if w := call("auth-url", `{"provider":"bps"}`); w.Code != 200 {
				t.Fatalf("创建合成会话失败：%d", w.Code)
			}
			callback := `{"provider":"bps","state":"state","redirect_url":"https://callback.invalid/?state=state&code=CALLBACK_CODE_CANARY"}`
			w := call("callback", callback)
			wantStatus := http.StatusBadGateway
			if tc.code != "" {
				wantStatus = http.StatusBadRequest
			} else if tc.status == 0 || tc.status == 200 {
				wantStatus = http.StatusServiceUnavailable
			}
			if w.Code != wantStatus {
				t.Errorf("回调分类错误：得到%d，预期%d", w.Code, wantStatus)
			}
			var result map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["code"] != tc.code {
				t.Errorf("稳定诊断码错误：%q", result["code"])
			}
			for _, forbidden := range []string{"RAW_TOKEN_CANARY", "CALLBACK_CODE_CANARY", "SYNTHETIC_KEY", "callback.invalid", cpa.URL} {
				if strings.Contains(w.Body.String(), forbidden) {
					t.Errorf("响应泄露输入或上游正文：%s", forbidden)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			release, err := mutations.AcquireAll(ctx)
			if tc.code != "" {
				if err != nil {
					t.Fatal("明确拒绝后未释放提交锁")
				}
				release()
				if retry := call("callback", callback); retry.Code != 200 {
					t.Errorf("明确拒绝后修正提交未获接受：%d", retry.Code)
				}
			} else {
				if err == nil {
					release()
					t.Error("保存结果未知时释放了提交锁")
				}
				if retry := call("callback", callback); retry.Code != 409 || callbacks.Load() != 1 {
					t.Error("保存结果未知时重复转发了授权码")
				}
			}
			_, _ = service.OAuth(context.Background(), http.MethodGet, "status", map[string]string{"state": "state"})
		})
	}
}

func TestPasswordLoginCapabilitiesAreAuthenticatedAndConfigurationOnly(t *testing.T) {
	for _, tc := range []struct {
		name, workerURL, secret string
		configured              bool
	}{
		{"missing", "", "", false},
		{"missing_secret", "https://worker.invalid", "", false},
		{"missing_url", "", "SECRET_CANARY", false},
		{"configured", "https://worker.invalid", "SECRET_CANARY", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{Service: login.New(nil, nil, tc.workerURL, tc.secret), Auth: auth(true)}
			w := httptest.NewRecorder()
			h.Handle(w, httptest.NewRequest(http.MethodGet, "/api/oauth-login/capabilities", nil))
			var result map[string]bool
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result) != 1 || result["password_login_configured"] != tc.configured {
				t.Fatalf("配置契约错误：%d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Error("配置结果未禁缓存")
			}
			w = httptest.NewRecorder()
			h.Handle(w, httptest.NewRequest(http.MethodPost, "/api/oauth-login/capabilities", nil))
			if w.Code != 405 {
				t.Error("配置查询接受了写方法")
			}
		})
	}
	h := &Handler{Auth: auth(false)}
	w := httptest.NewRecorder()
	h.Handle(w, httptest.NewRequest(http.MethodGet, "/api/oauth-login/capabilities", nil))
	if w.Code != 401 {
		t.Error("配置查询未鉴权")
	}
}
