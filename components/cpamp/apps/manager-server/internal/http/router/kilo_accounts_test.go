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

const kiloSavedFixture = `{"id":"fixture-account","name":"合成账号","enabled":true,"priority":1,"organization_id":"","allow_direct_when_proxy_full":false,"concurrency_limit":null,"effective_limit":20,"active":0,"waiting":0,"quota_source":"none","codex_credential_id":"","api_key":"SECRET_OUTPUT","token":"SECRET_TOKEN","proxy_url":"SECRET_PROXY","connection_ref":"UPSTREAM_REF"}`

func kiloAccountsWrite(handler http.Handler, method, path, key string, fields map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(fields)
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func kiloAccountsRef(t *testing.T, handler http.Handler) string {
	t.Helper()
	rec := kiloRouterRequest(handler, http.MethodGet, "/api/kilo/accounts", kiloRouterAdminKey)
	var result struct {
		Ref string `json:"connection_ref"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result.Ref) != 32 {
		t.Fatalf("读取引用失败: %d", rec.Code)
	}
	return result.Ref
}

func TestKiloAccountsWriteRouterContract(t *testing.T) {
	var writes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("CPA管理认证错误")
		}
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			_, _ = io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"accounts":[]}`)
			return
		}
		writes.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "connection_ref") {
			t.Error("BFF引用被传给CPA")
		}
		if r.Method == http.MethodDelete {
			if len(raw) != 0 {
				t.Error("CPA删除应无正文")
			}
			w.WriteHeader(204)
			return
		}
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil {
			t.Error("账号正文错误")
		}
		if r.Method == http.MethodPost {
			if body["api_key"] != "SECRET_INPUT" {
				t.Error("导入key未传递")
			}
			w.WriteHeader(201)
		}
		if r.Method == http.MethodPatch {
			if _, present := body["api_key"]; present {
				t.Error("省略key被修改")
			}
		}
		_, _ = io.WriteString(w, kiloSavedFixture)
	}))
	defer upstream.Close()
	resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}
	handler := newKiloFixtureRouter(t, resolver, upstream.Client())
	ref := kiloAccountsRef(t, handler)
	for _, tc := range []struct {
		method, path string
		fields       map[string]any
		status       int
	}{
		{"POST", "/api/kilo/accounts", map[string]any{"api_key": "SECRET_INPUT", "concurrency_limit": nil}, 201},
		{"PATCH", "/api/kilo/accounts/fixture-account", map[string]any{"name": "修改", "enabled": false, "priority": 2, "concurrency_limit": 4, "allow_direct_when_proxy_full": true}, 200},
		{"DELETE", "/api/kilo/accounts/fixture-account", map[string]any{}, 204},
	} {
		tc.fields["connection_ref"] = ref
		denied := kiloAccountsWrite(handler, tc.method, tc.path, "SYNTHETIC_CLIENT_KEY", tc.fields)
		if denied.Code != 401 {
			t.Fatalf("写鉴权失效: %d", denied.Code)
		}
		rec := kiloAccountsWrite(handler, tc.method, tc.path, kiloRouterAdminKey, tc.fields)
		if rec.Code != tc.status || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("写入状态=%d，预期%d", rec.Code, tc.status)
		}
		assertKiloRouterSanitized(t, rec)
		if tc.status == 204 {
			if rec.Body.Len() != 0 {
				t.Fatal("204含正文")
			}
			continue
		}
		var result map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &result)
		if result["connection_ref"] != ref || result["id"] != "fixture-account" || len(result) != 13 {
			t.Fatal("写回执不是白名单账号与当前引用")
		}
	}
	if writes.Load() != 3 {
		t.Fatalf("写请求被重复或鉴权穿透: %d", writes.Load())
	}
}

func TestKiloAccountsWriteRejectsInvalidFieldsAndReferences(t *testing.T) {
	var writes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			w.WriteHeader(500)
			return
		}
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			_, _ = io.WriteString(w, kiloRouterConcurrencyFixture)
			return
		}
		_, _ = io.WriteString(w, `{"accounts":[]}`)
	}))
	defer upstream.Close()
	resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}
	handler := newKiloFixtureRouter(t, resolver, upstream.Client())
	ref := kiloAccountsRef(t, handler)
	for _, fields := range []map[string]any{
		{"api_key": "SECRET_INPUT"}, {"api_key": "SECRET_INPUT", "connection_ref": ""},
		{"api_key": "SECRET_INPUT", "connection_ref": strings.Repeat("f", 32)},
		{"connection_ref": ref}, {"connection_ref": ref, "api_key": ""}, {"connection_ref": ref, "api_key": nil},
		{"connection_ref": ref, "api_key": "SECRET_INPUT", "organization_id": "x"},
		{"connection_ref": ref, "api_key": "SECRET_INPUT", "proxy_url": "SECRET_PROXY"},
		{"connection_ref": ref, "api_key": "SECRET_INPUT", "profile": true},
		{"connection_ref": ref, "api_key": "SECRET_INPUT", "enabled": nil},
		{"connection_ref": ref, "api_key": "SECRET_INPUT", "concurrency_limit": 0},
	} {
		rec := kiloAccountsWrite(handler, "POST", "/api/kilo/accounts", kiloRouterAdminKey, fields)
		if rec.Code != 400 && rec.Code != 409 {
			t.Fatalf("无效字段未拒绝: %d", rec.Code)
		}
		assertKiloRouterSanitized(t, rec)
	}
	for _, key := range []any{"", nil} {
		rec := kiloAccountsWrite(handler, "PATCH", "/api/kilo/accounts/fixture", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "api_key": key})
		if rec.Code != 400 {
			t.Fatalf("修改key空/null未拒绝: %d", rec.Code)
		}
	}
	resolver.replace(store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: "CHANGED"})
	rec := kiloAccountsWrite(handler, "POST", "/api/kilo/accounts", kiloRouterAdminKey, map[string]any{"connection_ref": ref, "api_key": "SECRET_INPUT"})
	if rec.Code != 409 || writes.Load() != 0 {
		t.Fatalf("旧连接发生写入: %d writes=%d", rec.Code, writes.Load())
	}
}

func TestKiloAccountsWritePostDriftNeverReplays(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		for _, change := range []string{"url", "key", "removed"} {
			t.Run(method+"_"+change, func(t *testing.T) {
				var writes atomic.Int32
				resolver := &kiloRouterResolver{}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" {
						if r.URL.Path == "/v8/management/credentials/concurrency" {
							_, _ = io.WriteString(w, kiloRouterConcurrencyFixture)
						} else {
							_, _ = io.WriteString(w, `{"accounts":[]}`)
						}
						return
					}
					writes.Add(1)
					setup, _, _ := resolver.ResolveSetup(r.Context())
					switch change {
					case "url":
						setup.CPAUpstreamURL = "http://never-contact.invalid"
					case "key":
						setup.ManagementKey = "CHANGED"
					case "removed":
						setup = store.Setup{}
					}
					resolver.replace(setup)
					if method == "DELETE" {
						w.WriteHeader(204)
						return
					}
					if method == "POST" {
						w.WriteHeader(201)
					}
					_, _ = io.WriteString(w, kiloSavedFixture)
				}))
				defer upstream.Close()
				resolver.replace(store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey})
				handler := newKiloFixtureRouter(t, resolver, upstream.Client())
				ref := kiloAccountsRef(t, handler)
				path := "/api/kilo/accounts"
				fields := map[string]any{"connection_ref": ref}
				if method == "POST" {
					fields["api_key"] = "SECRET_INPUT"
				} else {
					path += "/fixture"
					if method == "PATCH" {
						fields["name"] = "修改"
					}
				}
				rec := kiloAccountsWrite(handler, method, path, kiloRouterAdminKey, fields)
				if rec.Code != 409 || !strings.Contains(rec.Body.String(), "result_unconfirmed") || !strings.Contains(rec.Body.String(), "结果待核") || writes.Load() != 1 {
					t.Fatalf("后漂移未准确收口: %d writes=%d", rec.Code, writes.Load())
				}
				assertKiloRouterSanitized(t, rec)
			})
		}
	}
}

func TestKiloAccountsWriteErrorStatusSanitized(t *testing.T) {
	for _, status := range []int{400, 404, 409, 503, 500, 401, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var writes atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if r.URL.Path == "/v8/management/credentials/concurrency" {
						_, _ = io.WriteString(w, kiloRouterConcurrencyFixture)
					} else {
						_, _ = io.WriteString(w, `{"accounts":[]}`)
					}
					return
				}
				writes.Add(1)
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "SECRET_INPUT SECRET_TOKEN")
			}))
			defer upstream.Close()
			h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}, upstream.Client())
			rec := kiloAccountsWrite(h, "POST", "/api/kilo/accounts", kiloRouterAdminKey, map[string]any{"connection_ref": kiloAccountsRef(t, h), "api_key": "SECRET_INPUT"})
			want := status
			if want == 500 || want == 401 || want == 302 {
				want = 502
			}
			if rec.Code != want || writes.Load() != 1 {
				t.Fatalf("错误分类或请求次数错误: %d/%d", rec.Code, writes.Load())
			}
			assertKiloRouterSanitized(t, rec)
		})
	}
}
