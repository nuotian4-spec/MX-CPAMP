package kilo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type adminAuth struct{}

func (adminAuth) VerifyHeader(_ context.Context, value string) (bool, error) {
	return value == "Bearer ADMIN_FIXTURE", nil
}

type resolver struct{ value store.Setup }

func (r resolver) ResolveSetup(context.Context) (store.Setup, bool, error) {
	return r.value, r.value.CPAUpstreamURL != "", nil
}

func TestAdminGatePrecedesServiceAndClientInferenceKeyIsRejected(t *testing.T) {
	for _, authorization := range []string{"", "Bearer INFERENCE_FIXTURE"} {
		request := httptest.NewRequest(http.MethodGet, "/api/kilo/accounts", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		(&Handler{Auth: adminAuth{}}).Handle(response, request)
		if response.Code != 401 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("鉴权门禁错误: %d", response.Code)
		}
	}
}

func TestReadEndpointsWithoutReferenceReturnBoundWhitelist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer MANAGEMENT_FIXTURE" {
			t.Error("客户端认证被当作CPA管理认证")
		}
		switch r.URL.Path {
		case "/v8/management/credentials/concurrency":
			_, _ = w.Write([]byte(`{"mode":"local","default_limit":20,"pool_waiting":0,"credentials":[]}`))
		case "/v8/management/kilo/accounts":
			_, _ = w.Write([]byte(`{"accounts":[],"token":"SECRET","api_key":"SECRET","proxy_url":"SECRET"}`))
		case "/v8/management/kilo/status":
			_, _ = w.Write([]byte(`{"configured":false,"account_count":0,"healthy_proxy_count":0,"busy_proxy_count":0,"token":"SECRET"}`))
		case "/v8/management/kilo/capabilities":
			_, _ = w.Write([]byte(`{"channel":"kilo","public_protocols":["responses"],"tools_semantics":"source_parity","images_semantics":"source_parity","api_key":"SECRET"}`))
		default:
			t.Error("意外请求")
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	setup := resolver{store.Setup{CPAUpstreamURL: upstream.URL, ManagementKey: "MANAGEMENT_FIXTURE"}}
	handler := &Handler{Auth: adminAuth{}, Service: svc.New(setup, concurrency.New(setup, nil), upstream.Client())}
	ref := ""
	for _, resource := range []string{"accounts", "status", "capabilities"} {
		request := httptest.NewRequest(http.MethodGet, "/api/kilo/"+resource, nil)
		request.Header.Set("Authorization", "Bearer ADMIN_FIXTURE")
		response := httptest.NewRecorder()
		handler.Handle(response, request)
		var body map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		current, _ := body["connection_ref"].(string)
		if response.Code != 200 || len(current) != 32 || (ref != "" && current != ref) || strings.Contains(response.Body.String(), "SECRET") || strings.Contains(response.Body.String(), "FIXTURE") {
			t.Fatalf("只读回执错误: %d %s", response.Code, response.Body.String())
		}
		ref = current
		if resource == "status" && (body["auto_model_probe_enabled"] != false || body["auto_account_probe_enabled"] != false) {
			t.Fatal("默认探测未关闭")
		}
	}
}

func TestUnconfiguredAndUnsupportedRequests(t *testing.T) {
	h := &Handler{Auth: adminAuth{}, Service: svc.New(resolver{}, nil, nil)}
	for _, item := range []struct {
		method, path string
		want         int
	}{{"GET", "/api/kilo/status", 503}, {"POST", "/api/kilo/status", 405}, {"GET", "/api/kilo/unknown", 404}} {
		req := httptest.NewRequest(item.method, item.path, nil)
		req.Header.Set("Authorization", "Bearer ADMIN_FIXTURE")
		rec := httptest.NewRecorder()
		h.Handle(rec, req)
		if rec.Code != item.want {
			t.Fatalf("%s %s: %d", item.method, item.path, rec.Code)
		}
	}
}
