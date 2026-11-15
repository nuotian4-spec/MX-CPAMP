package router

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKiloManagerUX1DeviceJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || controlURL == "" || key == "" {
		t.Skip("仅由Core UX1设备联合fixture执行")
	}
	for _, raw := range []string{cpaURL, controlURL} {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("联合入口必须是loopback")
		}
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("联合测试拒绝非loopback连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	old := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = old; transport.CloseIdleConnections() })
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	response, err := client.Post(controlURL+"/scenario", "application/json", strings.NewReader(`{"mode":"ux1"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("UX1场景失败:%d", response.StatusCode)
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}, client)
	state := kiloProxySnapshot(t, h)
	if len(state.Proxies) < 2 {
		t.Fatal("UX1要求两个合成共享出口")
	}
	ref := state.ConnectionRef
	first, second := state.Proxies[0].ID, state.Proxies[1].ID
	call := func(method, path string, fields map[string]any, want int, destination any) {
		t.Helper()
		fields["connection_ref"] = ref
		rec := kiloAccountsWrite(h, method, path, kiloRouterAdminKey, fields)
		if rec.Code != want {
			t.Fatalf("UX1 %s %s=%d期望%d:%s", method, path, rec.Code, want, rec.Body.String())
		}
		assertKiloRouterSanitized(t, rec)
		if json.Unmarshal(rec.Body.Bytes(), destination) != nil {
			t.Fatal("UX1回执无效")
		}
	}
	var started model.KiloDeviceStarted
	call("POST", "/api/kilo/device-auth", map[string]any{"name": "UX1双出口账号", "authorization_egress": map[string]any{"mode": "pool", "proxy_id": second, "expected_revision": state.Revision}, "inference_egress": map[string]any{"mode": "pool", "candidate_proxy_ids": []string{first, second}, "exclusive_proxy_ids": []string{}, "primary_proxy_id": second, "expected_revision": state.Revision}, "enabled": true, "concurrency_limit": nil, "priority": 0, "allow_direct_when_proxy_full": false}, 200, &started)
	if started.AuthorizationEgress == nil || started.AuthorizationEgress.ProxyID != second || started.InferenceEgress == nil || len(started.InferenceEgress.CandidateProxyIDs) != 2 {
		t.Fatal("start未返回固定代理与完整草稿")
	}
	for _, p := range kiloProxySnapshot(t, h).Proxies {
		if p.Active != 0 {
			t.Fatal("等待授权保留了请求租约")
		}
	}
	base := "/api/kilo/device-auth/" + url.PathEscape(started.ID)
	var approved model.KiloDeviceState
	call("POST", base+"/poll", map[string]any{}, 200, &approved)
	if approved.Status != "approved" || approved.AuthorizationEgress == nil || approved.AuthorizationEgress.ProxyID != second || approved.InferenceEgress == nil || approved.InferenceEgress.PrimaryProxyID != second {
		t.Fatal("poll固定代理或草稿改变")
	}
	var saved model.KiloDeviceSaved
	call("POST", base+"/save", map[string]any{}, 201, &saved)
	if saved.ID == "" || saved.InferenceEgress == nil || len(saved.InferenceEgress.CandidateProxyIDs) != 2 || saved.EgressMode != "pool" {
		t.Fatal("账号和出口保存回执不完整")
	}
	id := saved.ID
	call("POST", base+"/save", map[string]any{}, 201, &saved)
	if saved.ID != id {
		t.Fatal("重复核实创建了不同账号")
	}
	final := kiloProxySnapshot(t, h)
	found := false
	for _, policy := range final.Accounts {
		if policy.AccountID == id {
			found = true
			if len(policy.CandidateProxyIDs) != 2 || policy.PrimaryProxyID != second {
				t.Fatal("最终候选或主偏好未共同保存")
			}
		}
	}
	if !found {
		t.Fatal("保存后未读回账号出口")
	}
	for _, p := range final.Proxies {
		if p.Active != 0 || p.AccountID != "" || p.Owner != "" {
			t.Fatal("共享候选被隐式独占或授权租约残留")
		}
	}
	rec := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	var list model.KiloAccounts
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil {
		t.Fatal("保存后账号列表读取失败")
	}
	assertKiloRouterSanitized(t, rec)
	for _, account := range list.Accounts {
		if account.ID == id {
			if account.QuotaSourceSummary == nil || account.QuotaSourceSummary.Kind != "none" || account.QuotaSnapshot == nil || account.QuotaSnapshot.Query.Status != "never" || account.QuotaSnapshot.ConnectionRef != ref {
				t.Fatal("本地额度来源/快照未正确透传")
			}
		}
	}
	t.Logf("UX1真实Manager/Core:固定第二代理=%s;双候选保存读回;共享归属不变;重复save同ID;等待租约释放;列表额度无隐式查询", second)
}
