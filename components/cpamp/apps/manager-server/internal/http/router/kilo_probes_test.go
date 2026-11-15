package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func probeProjection() map[string]any {
	state := func(model string) map[string]any {
		return map[string]any{"enabled": false, "model": model, "interval_seconds": 300, "status": "never", "next_run_at": nil, "last_started_at": nil, "last_completed_at": nil, "error_code": nil, "model_calls": 0, "skipped_count": 0, "token": "SECRET"}
	}
	return map[string]any{"account_id": "probe.json", "revision": 0, "runtime_started_at": "2026-11-13T00:00:00Z", "manual_model_calls": 2, "health": state("kilo-auto/free"), "model_probe": state(""), "endpoint": "SECRET"}
}

func probesRequest(h http.Handler, method, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/kilo/accounts/probe.json/probes", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestKiloProbesRouterContract(t *testing.T) {
	var gets, puts atomic.Int32
	status := 200
	view := probeProjection()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("未使用保存的管理凭据")
		}
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			io.WriteString(w, kiloRouterConcurrencyFixture)
		case "/v8/management/kilo/accounts":
			io.WriteString(w, `{"accounts":[{"id":"probe.json","enabled":true,"effective_limit":20,"quota_source":"none"}]}`)
		case "/v8/management/kilo/accounts/probe.json/probes":
			if r.Method == "PUT" {
				puts.Add(1)
				var fields map[string]json.RawMessage
				json.NewDecoder(r.Body).Decode(&fields)
				if len(fields) != 3 || fields["expected_revision"] == nil || fields["health"] == nil || fields["model_probe"] == nil {
					t.Errorf("完整配置未保留或透传了额外字段:%s", fields)
				}
			} else if r.Method == "GET" {
				gets.Add(1)
			} else {
				t.Error("错误方法")
			}
			w.WriteHeader(status)
			if status != 200 {
				io.WriteString(w, `{"error":"SECRET"}`)
				return
			}
			json.NewEncoder(w).Encode(view)
		default:
			t.Errorf("意外模型或其它路径:%s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}, upstream.Client())
	ref := kiloAccountsRef(t, h)
	body := `{"expected_revision":0,"health":{"enabled":false},"model_probe":{"enabled":false,"model":"","interval_seconds":300},"connection_ref":"` + ref + `"}`
	for _, key := range []string{"", "CLIENT_KEY", kiloRouterManagementKey} {
		for _, method := range []string{"GET", "PUT"} {
			if w := probesRequest(h, method, key, map[string]string{"PUT": body}[method]); w.Code != 401 {
				t.Fatalf("未授权=%d", w.Code)
			}
		}
	}
	if gets.Load() != 0 || puts.Load() != 0 {
		t.Fatal("未授权请求进入Core")
	}
	w := probesRequest(h, "GET", kiloRouterAdminKey, "")
	if w.Code != 200 {
		t.Fatalf("probes GET=%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "SECRET") || !strings.Contains(w.Body.String(), ref) {
		t.Fatal("投影脱敏或引用错误")
	}
	for _, bad := range []string{
		strings.Replace(body, `"expected_revision":0`, `"expected_revision":null`, 1),
		strings.Replace(body, `"expected_revision":0`, `"expected_revision":-1`, 1),
		strings.Replace(body, `"expected_revision":0`, `"expected_revision":0.5`, 1),
		strings.Replace(body, `"health":{"enabled":false}`, `"health":{"enabled":null}`, 1),
		strings.Replace(body, `"health":{"enabled":false}`, `"health":{"enabled":false,"model":"injected"}`, 1),
		strings.Replace(body, `"model":""`, `"model":null`, 1),
		strings.Replace(body, `"interval_seconds":300`, `"interval_seconds":59`, 1),
		strings.Replace(body, `"interval_seconds":300`, `"interval_seconds":86401`, 1),
		strings.Replace(body, `"interval_seconds":300`, `"interval_seconds":60.5`, 1),
		strings.Replace(body, `"model_probe":{"enabled":false`, `"model_probe":{"enabled":true`, 1),
		strings.TrimSuffix(body, "}") + `,"endpoint":"http://forbidden.invalid"}`,
		strings.TrimSuffix(body, "}") + `,"manual":true}`,
		strings.Replace(body, `"health":{"enabled":false},`, "", 1),
	} {
		if w := probesRequest(h, "PUT", kiloRouterAdminKey, bad); w.Code != 400 {
			t.Fatalf("非法配置=%d %s", w.Code, bad)
		}
	}
	if w := probesRequest(h, "GET", kiloRouterAdminKey, `{}`); w.Code != 400 {
		t.Fatalf("GET正文=%d", w.Code)
	}
	if w := probesRequest(h, "PUT", kiloRouterAdminKey, strings.Replace(body, ref, strings.Repeat("b", 32), 1)); w.Code != 409 {
		t.Fatalf("旧ref=%d", w.Code)
	}
	if puts.Load() != 0 {
		t.Fatal("非法配置进入Core")
	}
	if w := probesRequest(h, "PUT", kiloRouterAdminKey, body); w.Code != 200 {
		t.Fatalf("PUT=%d %s", w.Code, w.Body.String())
	}
	for _, state := range []string{"scheduled", "running", "completed", "incomplete", "failed", "canceled", "skipped"} {
		view["health"].(map[string]any)["status"] = state
		w := probesRequest(h, "GET", kiloRouterAdminKey, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), state) {
			t.Fatalf("状态%s=%d", state, w.Code)
		}
	}
	view["health"].(map[string]any)["error_code"] = "SECRET"
	if w := probesRequest(h, "GET", kiloRouterAdminKey, ""); w.Code != 502 || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatal("未知错误码未拒绝")
	}
	view = probeProjection()
	for _, status = range []int{400, 401, 403, 404, 409, 503} {
		want := status
		if status == 401 || status == 403 {
			want = 502
		}
		w := probesRequest(h, "PUT", kiloRouterAdminKey, body)
		if w.Code != want || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("错误分类%d=>%d", status, w.Code)
		}
	}
	if puts.Load() != 7 {
		t.Fatalf("PUT重复:%d", puts.Load())
	}
}

func TestKiloProbesRouterCancellationAndDrift(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "目标变化", true: "取消保存"}[cancelRequest], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var puts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v8/management/credentials/concurrency":
					io.WriteString(w, kiloRouterConcurrencyFixture)
				case "/v8/management/kilo/accounts":
					io.WriteString(w, `{"accounts":[{"id":"probe.json","effective_limit":20,"quota_source":"none"}]}`)
				default:
					io.Copy(io.Discard, r.Body)
					puts.Add(1)
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					json.NewEncoder(w).Encode(probeProjection())
				}
			}))
			defer upstream.Close()
			resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}
			h := newKiloFixtureRouter(t, resolver, upstream.Client())
			ref := kiloAccountsRef(t, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := httptest.NewRequest("PUT", "/api/kilo/accounts/probe.json/probes", strings.NewReader(`{"expected_revision":0,"health":{"enabled":false},"model_probe":{"enabled":false,"model":"","interval_seconds":300},"connection_ref":"`+ref+`"}`)).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { h.ServeHTTP(w, r); close(done) }()
			<-started
			if cancelRequest {
				cancel()
			} else {
				resolver.replace(store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: "OTHER"})
				close(release)
			}
			<-done
			want := 409
			if cancelRequest {
				want = 499
			}
			if w.Code != want || puts.Load() != 1 || strings.Contains(w.Body.String(), "runtime_started_at") {
				t.Fatalf("取消/漂移:%d PUT%d", w.Code, puts.Load())
			}
		})
	}
}
