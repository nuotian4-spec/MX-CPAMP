package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBPSSettingsAndImageReadRoutes(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "test-management-key")
	server := newTestServer(t)
	for _, authorized := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, "/v8/management/bps/settings", nil)
		request.RemoteAddr = "127.0.0.1:1234"
		if authorized {
			request.Header.Set("Authorization", "Bearer test-management-key")
		}
		w := httptest.NewRecorder()
		server.engine.ServeHTTP(w, request)
		want := 401
		if authorized {
			want = 200
		}
		if w.Code != want {
			t.Fatal(authorized, w.Code, w.Body.String())
		}
	}
	methods := map[string]bool{}
	for _, route := range server.engine.Routes() {
		if route.Path == "/api/bps-images/:token" {
			methods[route.Method] = true
		}
	}
	if len(methods) != 2 || !methods[http.MethodGet] || !methods[http.MethodHead] {
		t.Fatal("图片入口超出只读契约", methods)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		server.engine.ServeHTTP(w, httptest.NewRequest(method, "/api/bps-images/not-a-capability", nil))
		if w.Code != 404 {
			t.Fatal("未知能力链接未返回404", method, w.Code)
		}
	}
}
