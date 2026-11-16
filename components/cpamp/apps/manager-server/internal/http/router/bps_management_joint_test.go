package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

func bpsLoopbackTransport(t *testing.T, addresses ...string) *http.Transport {
	t.Helper()
	for _, address := range addresses {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("BPS 测试仅接受明确的 loopback HTTP 入口")
		}
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("BPS 测试拒绝非 loopback 连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	return transport
}

func TestBPSManagerManagementJointRuntime(t *testing.T) {
	cpaURL, missingURL, managementKey := os.Getenv("BPS_JOINT_CPA_URL"), os.Getenv("BPS_JOINT_MISSING_URL"), os.Getenv("BPS_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || missingURL == "" || managementKey == "" {
		t.Skip("仅由 CPA 正式路由联合 fixture 启动")
	}
	bpsLoopbackTransport(t, cpaURL, missingURL)
	handler := newConfigKeysRouter(t, cpaURL, managementKey)
	request := func(h http.Handler, method, path, key, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Fatalf("%s %s 状态=%d 预期=%d 正文=%s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	const licensePath = "/v0/management/cpa-bps/license"
	const configPath = "/v0/management/plugins/cpa-bps/config"
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, licensePath, ""},
		{http.MethodPost, licensePath + "/activate", `{"license_key":"synthetic-bps-license-once"}`},
		{http.MethodPost, licensePath + "/refresh", `{}`},
		{http.MethodPatch, configPath, `{"session_proxy_idle_minutes":999}`},
	} {
		request(handler, http.MethodGet, licensePath, kiloRouterAdminKey, "", http.StatusOK)
		for _, key := range []string{"", "synthetic-inference-key"} {
			request(handler, tc.method, tc.path, key, tc.body, http.StatusUnauthorized)
		}
	}
	request(handler, http.MethodGet, licensePath, kiloRouterAdminKey, "", http.StatusOK)
	request(handler, http.MethodGet, licensePath+"/activate", kiloRouterAdminKey, "", http.StatusNotFound)
	request(handler, http.MethodGet, licensePath+"/refresh", kiloRouterAdminKey, "", http.StatusNotFound)
	request(handler, http.MethodPost, licensePath, kiloRouterAdminKey, `{}`, http.StatusNotFound)
	activated := request(handler, http.MethodPost, licensePath+"/activate", kiloRouterAdminKey, `{"license_key":"synthetic-bps-license-once"}`, http.StatusOK)
	var status map[string]any
	if err := json.Unmarshal(activated.Body.Bytes(), &status); err != nil || status["active"] != true || status["required"] != true {
		t.Fatal("合成许可激活状态往返失败")
	}
	for _, field := range []string{"required", "active", "code", "key_hint", "bound_ip", "license_expires_at", "expires_at", "last_error"} {
		if _, ok := status[field]; !ok {
			t.Fatalf("许可字段丢失：%s", field)
		}
	}
	request(handler, http.MethodPost, licensePath+"/refresh", kiloRouterAdminKey, `{}`, http.StatusOK)
	request(handler, http.MethodGet, licensePath, kiloRouterAdminKey, "", http.StatusOK)
	missing := newConfigKeysRouter(t, missingURL, managementKey)
	request(missing, http.MethodGet, licensePath, kiloRouterAdminKey, "", http.StatusNotFound)
	request(missing, http.MethodPost, licensePath+"/activate", kiloRouterAdminKey, `{"license_key":"synthetic-bps-license-once"}`, http.StatusNotFound)

	patch := `{"session_proxy_pool_enabled":true,"session_proxy_pool":"socks5://127.0.0.1:31001\nhttp://127.0.0.1:31002","session_proxy_idle_minutes":23,"object_field":{"fresh":"replacement"},"remove_me":null}`
	request(handler, http.MethodPatch, configPath, kiloRouterAdminKey, patch, http.StatusOK)
	response := request(handler, http.MethodGet, configPath, kiloRouterAdminKey, "", http.StatusOK)
	var actual map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{
		"enabled": true, "session_proxy_pool_enabled": true,
		"session_proxy_pool": "socks5://127.0.0.1:31001\nhttp://127.0.0.1:31002", "session_proxy_idle_minutes": float64(23),
		"unknown_top": map[string]any{"retained": true}, "object_field": map[string]any{"fresh": "replacement"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("正式配置 PATCH/GET 往返内容错误：%v", actual)
	}
	t.Log("Manager 与 CPA 双管理鉴权；许可三接口方法、正文、状态字段；未注册404；正式 PATCH 字符串/对象/null 语义通过")
}
