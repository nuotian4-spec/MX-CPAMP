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

func manualRequest(h http.Handler, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/kilo/accounts/manual.json/test", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestKiloManualRouterContract(t *testing.T) {
	var posts atomic.Int32
	status, outcome := 200, "completed"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+kiloRouterManagementKey {
			t.Error("管理凭据未固定")
		}
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			io.WriteString(w, kiloRouterConcurrencyFixture)
		case "/v8/management/kilo/accounts":
			io.WriteString(w, `{"accounts":[{"id":"manual.json","enabled":false,"effective_limit":20,"quota_source":"none"}]}`)
		case "/v8/management/routing/model-definitions/kilo":
			io.WriteString(w, `{"channel":"kilo","models":[{"id":"gpt-6.1-sol","token":"SECRET"}]}`)
		case "/v8/management/kilo/accounts/manual.json/test":
			posts.Add(1)
			var fields map[string]string
			json.NewDecoder(r.Body).Decode(&fields)
			if len(fields) != 2 || fields["model"] != "gpt-6.1-sol" || fields["prompt"] != "  保留原提示  " {
				t.Errorf("上游字段=%v", fields)
			}
			w.WriteHeader(status)
			if status != 200 {
				io.WriteString(w, `{"error":"SECRET_UPSTREAM"}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"model": "gpt-6.1-sol", "status": outcome, "started_at": "2026-11-13T00:00:00Z", "completed_at": "2026-11-13T00:00:01Z", "duration_ms": 1000, "text": "<b>合成文本</b>", "error_code": "SECRET_UPSTREAM", "token": "SECRET"})
		default:
			t.Errorf("未知路径:%s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}, upstream.Client())
	ref := kiloAccountsRef(t, h)
	body := `{"model":"gpt-6.1-sol","prompt":"  保留原提示  ","connection_ref":"` + ref + `"}`
	for _, key := range []string{"", "CLIENT_KEY", kiloRouterManagementKey} {
		if w := manualRequest(h, key, body); w.Code != 401 {
			t.Fatalf("推理或上游管理Key=%d", w.Code)
		}
	}
	if posts.Load() != 0 {
		t.Fatal("未授权请求进入模型")
	}
	models := kiloRouterRequest(h, "GET", "/api/kilo/test-models", kiloRouterAdminKey)
	if models.Code != 200 || strings.Contains(models.Body.String(), "SECRET") || !strings.Contains(models.Body.String(), "gpt-6.1-sol") {
		t.Fatalf("静态目录:%d %s", models.Code, models.Body.String())
	}
	for _, extra := range []string{`,"endpoint":"http://forbidden.invalid"`, `,"mode":"auto"`, `,"token":"SECRET"`} {
		bad := strings.TrimSuffix(body, "}") + extra + "}"
		if w := manualRequest(h, kiloRouterAdminKey, bad); w.Code != 400 {
			t.Fatalf("字段注入=%d", w.Code)
		}
	}
	if posts.Load() != 0 {
		t.Fatal("非法字段进入Core")
	}
	for _, invalid := range []string{
		`{"model":"","prompt":"x"`,
		`{"model":"gpt-6.1-sol","prompt":"   "`,
		`{"model":[],"prompt":"x"`,
		`{"model":"gpt-6.1-sol","prompt":null`,
	} {
		if w := manualRequest(h, kiloRouterAdminKey, invalid+`,"connection_ref":"`+ref+`"}`); w.Code != 400 {
			t.Fatalf("非法模型或提示词=%d", w.Code)
		}
	}
	if w := manualRequest(h, kiloRouterAdminKey, strings.Replace(body, ref, strings.Repeat("b", 32), 1)); w.Code != 409 {
		t.Fatalf("旧连接引用=%d", w.Code)
	}
	if posts.Load() != 0 {
		t.Fatal("非法输入或旧引用进入Core")
	}
	for _, outcome = range []string{"completed", "incomplete", "failed", "canceled"} {
		w := manualRequest(h, kiloRouterAdminKey, body)
		if w.Code != 200 {
			t.Fatalf("结果%s:%d %s", outcome, w.Code, w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["connection_ref"] != ref || result["account_id"] != "manual.json" || result["status"] != outcome {
			t.Fatal("回执绑定或状态错误")
		}
		if (outcome == "completed" || outcome == "incomplete") && result["text"] != "<b>合成文本</b>" {
			t.Fatal("合法结果文本丢失")
		}
		if (outcome == "failed" || outcome == "canceled") && result["text"] != "" {
			t.Fatal("失败/取消泄漏文本")
		}
		if strings.Contains(w.Body.String(), "SECRET") {
			t.Fatal("回执泄漏原始字段")
		}
	}
	for _, status = range []int{400, 401, 404, 409, 429, 503} {
		w := manualRequest(h, kiloRouterAdminKey, body)
		want := status
		if status == 401 {
			want = 502
		}
		if w.Code != want || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("HTTP分类:%d=>%d", status, w.Code)
		}
	}
	if posts.Load() != 10 {
		t.Fatalf("实际POST=%d，存在重复", posts.Load())
	}
}

func TestKiloManualRouterCancellationAndTargetChange(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "目标变化", true: "请求取消"}[cancelRequest], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var posts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v8/management/credentials/concurrency":
					io.WriteString(w, kiloRouterConcurrencyFixture)
				case "/v8/management/kilo/accounts":
					io.WriteString(w, `{"accounts":[{"id":"manual.json","effective_limit":20,"quota_source":"none"}]}`)
				default:
					_, _ = io.Copy(io.Discard, r.Body)
					posts.Add(1)
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					io.WriteString(w, `{"model":"gpt-6.1-sol","status":"completed","started_at":"2026-11-13T00:00:00Z","completed_at":"2026-11-13T00:00:01Z","duration_ms":1000,"text":"迟到文本","error_code":null}`)
				}
			}))
			defer upstream.Close()
			resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: kiloRouterManagementKey}}
			h := newKiloFixtureRouter(t, resolver, upstream.Client())
			ref := kiloAccountsRef(t, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := httptest.NewRequest("POST", "/api/kilo/accounts/manual.json/test", strings.NewReader(`{"model":"gpt-6.1-sol","prompt":"x","connection_ref":"`+ref+`"}`)).WithContext(ctx)
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
			if w.Code != want || posts.Load() != 1 || strings.Contains(w.Body.String(), "迟到文本") {
				t.Fatalf("漂移/取消:%d POST%d %s", w.Code, posts.Load(), w.Body.String())
			}
		})
	}
}
