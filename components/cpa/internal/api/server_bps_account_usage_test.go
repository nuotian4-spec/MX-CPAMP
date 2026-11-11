package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// 来源核实已脱敏的主窗口数值；其余额外字段用于验证白名单隔离。
const bpsUsageFixture = `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":17,"limit_window_seconds":2592000,"reset_after_seconds":0,"reset_at":1796977220},"secondary_window":null},"credits":{"balance":"PRIVATE_BALANCE"},"account_id":"PRIVATE_ACCOUNT","email":"PRIVATE_EMAIL","code_review_rate_limit":{"primary_window":{"used_percent":99}}}`

func callBPSAccountUsage(server *Server, ctx context.Context, query string, authorized bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v8/management/bps/credentials/usage"+query, nil).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:1234"
	if authorized {
		req.Header.Set("Authorization", "Bearer test-password")
	}
	w := httptest.NewRecorder()
	server.engine.ServeHTTP(w, req)
	return w
}

func TestBPSAccountUsagePublicHandlerReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantError, plan, used string
		status                            int
	}{
		{"source fixture", bpsUsageFixture, "", "pro", "17", 200},
		{"zero", `{"rate_limit":{"allowed":false,"limit_reached":false,"primary_window":{"used_percent":0,"reset_at":0,"reset_after_seconds":0}}}`, "", "", "0", 200},
		{"reset only", `{"plan_type":"UNKNOWN_PRIVATE_PLAN","rate_limit":{"primary_window":{"reset_at":1796977220}}}`, "", "", "null", 200},
		{"secondary only", `{"rate_limit":{"secondary_window":{"used_percent":100}}}`, "", "", "null", 200},
		{"missing", `{}`, "quota_unavailable", "", "", 200},
		{"null quota", `{"plan_type":"pro","rate_limit":null}`, "quota_unavailable", "", "", 200},
		{"empty windows", `{"rate_limit":{"allowed":true,"primary_window":{},"secondary_window":null}}`, "quota_unavailable", "", "", 200},
		{"period only", `{"rate_limit":{"primary_window":{"limit_window_seconds":2592000}}}`, "quota_unavailable", "", "", 200},
		{"extra quota only", `{"credits":{"balance":100},"code_review_rate_limit":{"primary_window":{"used_percent":50}}}`, "quota_unavailable", "", "", 200},
		{"string percent", `{"rate_limit":{"primary_window":{"used_percent":"0"}}}`, "invalid_response", "", "", 200},
		{"negative percent", `{"rate_limit":{"primary_window":{"used_percent":-1}}}`, "invalid_response", "", "", 200},
		{"large percent", `{"rate_limit":{"primary_window":{"used_percent":101}}}`, "invalid_response", "", "", 200},
		{"nonfinite", `{"rate_limit":{"primary_window":{"used_percent":1e999}}}`, "invalid_response", "", "", 200},
		{"zero period", `{"rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":0}}}`, "invalid_response", "", "", 200},
		{"negative reset", `{"rate_limit":{"primary_window":{"reset_at":-1}}}`, "invalid_response", "", "", 200},
		{"string reset", `{"rate_limit":{"primary_window":{"reset_at":"2026-11-11"}}}`, "invalid_response", "", "", 200},
		{"bad bool", `{"rate_limit":{"allowed":1,"primary_window":{"used_percent":0}}}`, "invalid_response", "", "", 200},
		{"bad window", `{"rate_limit":{"primary_window":[]}}`, "invalid_response", "", "", 200},
		{"bad plan", `{"plan_type":17,"rate_limit":{"primary_window":{"used_percent":0}}}`, "invalid_response", "", "", 200},
		{"bad root", `null`, "invalid_response", "", "", 200},
		{"unauthorized", "PRIVATE_UPSTREAM", "unauthorized", "", "", 401},
		{"forbidden", "PRIVATE_UPSTREAM", "forbidden", "", "", 403},
		{"rate limited", "PRIVATE_UPSTREAM", "rate_limited", "", "", 429},
		{"server error", "PRIVATE_UPSTREAM", "upstream_error", "", "", 503},
		{"redirect", "PRIVATE_UPSTREAM", "redirect_rejected", "", "", 302},
		{"oversize", strings.Repeat(" ", (2<<20)+1), "invalid_response", "", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, manager, selected, store := newBPSAccessServer(t)
			before := selected.Clone()
			calls := 0
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsAccessTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.String() != helps.BPSAccountUsageURL || r.Body != nil ||
					r.Header.Get("Authorization") != "Bearer TOKEN_A" || r.Header.Get("Chatgpt-Account-Id") != "ACCOUNT_A" ||
					r.Header.Get("User-Agent") != helps.BPSAccountUsageUserAgent || r.Header.Get("Content-Type") != "application/json" {
					t.Fatal("账号用量请求未使用精确 BPS 凭证或固定来源头")
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 {
					t.Fatal("缺少五秒预算")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://untrusted.invalid"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			w := callBPSAccountUsage(server, ctx, "?auth_index="+selected.EnsureIndex()+"&auth_id="+url.QueryEscape(selected.ID), true)
			root := ojson.ParseBytes(w.Body.Bytes())
			if w.Code != 200 || root.Get("provider").String() != "bps" || root.Get("source").String() != "chatgpt_account_usage" ||
				root.Get("auth_index").String() != selected.Index || root.Get("auth_id").String() != selected.ID ||
				root.Get("upstream_status").Int() != int64(tc.status) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.wantError != "" {
				if root.Get("error").String() != tc.wantError || root.Get("rate_limit").Raw != "null" || root.Get("plan_type").Raw != "null" {
					t.Fatal(w.Body.String())
				}
			} else {
				if root.Get("error").Raw != "null" || !root.Get("rate_limit").IsObject() || root.Get("plan_type").String() != tc.plan {
					t.Fatal(w.Body.String())
				}
				if tc.used != "null" && root.Get("rate_limit.primary_window.used_percent").Raw != tc.used {
					t.Fatal(w.Body.String())
				}
				if tc.name == "reset only" && root.Get("rate_limit.primary_window.used_percent").Raw != "null" ||
					tc.name == "zero" && (root.Get("rate_limit.primary_window.reset_at").Int() != 0 || root.Get("rate_limit.allowed").Raw != "false") {
					t.Fatal("未知值或合法零值被改写", w.Body.String())
				}
			}
			var keys map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil || len(keys) != 9 {
				t.Fatal("响应字段超出固定投影", w.Body.String())
			}
			if _, err := time.Parse(time.RFC3339, root.Get("queried_at").String()); err != nil {
				t.Fatal(err)
			}
			for _, marker := range []string{"PRIVATE", "TOKEN_", "ACCOUNT_", "credits", "code_review", "email", "UNKNOWN_"} {
				if strings.Contains(w.Body.String(), marker) {
					t.Fatal("用量响应泄露非白名单数据", marker)
				}
			}
			after, _ := manager.GetByID(selected.ID)
			if calls != 1 || store.writes.Load() != 0 || after.CredentialVersion != before.CredentialVersion ||
				after.LastRefreshedAt != before.LastRefreshedAt || after.Metadata["access_token"] != before.Metadata["access_token"] {
				t.Fatal("只读查询改变凭证或产生额外请求")
			}
		})
	}
}

func TestBPSAccountUsagePublicHandlerReferences(t *testing.T) {
	server, manager, selected, store := newBPSAccessServer(t)
	calls := 0
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsAccessTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("意外请求")
	}))
	codex, _ := manager.GetByID("codex.json")
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", 400}, {"?auth_index=", 400}, {"?auth_index=unknown", 404},
		{"?auth_index=" + codex.EnsureIndex(), 400},
		{"?auth_index=" + selected.Index + "&auth_id=bps-b.json", 409},
		{"?auth_index=" + selected.Index + "&auth_id=", 400},
		{"?auth_index=" + selected.Index + "&url=override", 400},
		{"?auth_index=" + selected.Index + "&auth_index=" + selected.Index, 400}, {"?auth_index=%zz", 400},
	} {
		if w := callBPSAccountUsage(server, ctx, tc.query, true); w.Code != tc.want {
			t.Fatal(tc.query, w.Code)
		}
	}
	if w := callBPSAccountUsage(server, ctx, "?auth_index="+selected.Index, false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := httptest.NewRecorder()
	server.engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v0/management/bps/credentials/usage", nil))
	if w.Code != 404 || calls != 0 || store.writes.Load() != 0 {
		t.Fatal("拒绝引用仍触发请求或写入")
	}
}

func TestBPSAccountUsageTransportAndSnapshot(t *testing.T) {
	for _, category := range []string{"timeout", "canceled", "transport_error", "credential_incomplete", "account_id", "access_token", "sub", "credential_proxy", "global_proxy"} {
		t.Run(category, func(t *testing.T) {
			server, manager, selected, store := newBPSAccessServer(t)
			if category == "credential_incomplete" {
				selected.Metadata["access_token"] = ""
				if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), selected); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsAccessTransport(func(*http.Request) (*http.Response, error) {
				calls++
				switch category {
				case "timeout":
					return nil, context.DeadlineExceeded
				case "canceled":
					return nil, context.Canceled
				case "transport_error":
					return nil, errors.New("PRIVATE_BODY")
				case "global_proxy":
					changed := server.cfg.CloneForRuntime()
					changed.ProxyURL = "http://127.0.0.1:29999"
					server.mgmt.SetConfig(changed)
				case "account_id", "access_token", "sub", "credential_proxy":
					changed := selected.Clone()
					if category == "credential_proxy" {
						changed.ProxyURL = "http://127.0.0.1:29998"
					} else {
						changed.Metadata[category] = "CHANGED"
					}
					if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), changed); err != nil {
						t.Fatal(err)
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(bpsUsageFixture))}, nil
			}))
			w := callBPSAccountUsage(server, ctx, "?auth_index="+selected.Index, true)
			root := ojson.ParseBytes(w.Body.Bytes())
			want := category
			if category == "account_id" || category == "access_token" || category == "sub" || category == "credential_proxy" || category == "global_proxy" {
				want = "credential_changed"
			}
			if w.Code != 200 || root.Get("error").String() != want || root.Get("rate_limit").Raw != "null" || root.Get("plan_type").Raw != "null" || store.writes.Load() != 0 || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal(w.Body.String())
			}
			if category == "credential_incomplete" && calls != 0 || category != "credential_incomplete" && calls != 1 {
				t.Fatal(calls)
			}
		})
	}
}

func TestBPSAccountUsageParentCancellation(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		t.Run(stage, func(t *testing.T) {
			server, _, selected, store := newBPSAccessServer(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			started, closed := make(chan struct{}), make(chan struct{})
			ctx := context.WithValue(parent, "cliproxy.roundtripper", bpsAccessTransport(func(r *http.Request) (*http.Response, error) {
				close(started)
				if stage == "headers" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &bpsAccessCancelBody{ctx: r.Context(), closed: closed}}, nil
			}))
			finished := make(chan *httptest.ResponseRecorder, 1)
			go func() { finished <- callBPSAccountUsage(server, ctx, "?auth_index="+selected.Index, true) }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("查询未启动")
			}
			cancel()
			select {
			case w := <-finished:
				if ojson.ParseBytes(w.Body.Bytes()).Get("error").String() != "canceled" || store.writes.Load() != 0 {
					t.Fatal(w.Body.String())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("父请求取消未停止查询")
			}
			if stage == "body" {
				select {
				case <-closed:
				default:
					t.Fatal("响应正文未关闭")
				}
			}
		})
	}
}

func TestBPSAccountUsageKeepsShorterDeadline(t *testing.T) {
	server, _, selected, _ := newBPSAccessServer(t)
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, _ := parent.Deadline()
	ctx := context.WithValue(parent, "cliproxy.roundtripper", bpsAccessTransport(func(r *http.Request) (*http.Response, error) {
		if got, _ := r.Context().Deadline(); !got.Equal(want) {
			t.Error("覆盖了更短的父预算")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(bpsUsageFixture))}, nil
	}))
	if w := callBPSAccountUsage(server, ctx, "?auth_index="+selected.Index, true); w.Code != 200 || ojson.ParseBytes(w.Body.Bytes()).Get("error").Raw != "null" {
		t.Fatal(w.Body.String())
	}
}
