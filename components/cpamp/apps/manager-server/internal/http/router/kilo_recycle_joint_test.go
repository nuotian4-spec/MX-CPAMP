package router

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// TestKiloManagerRecycleJointRuntime只由Core冻结副本的联合父测试驱动。
func TestKiloManagerRecycleJointRuntime(t *testing.T) {
	cpaURL, controlURL := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL")
	if cpaURL == "" || controlURL == "" {
		t.Skip("仅由Core回收站联合fixture执行")
	}
	loopback := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && u.Scheme == "http" && u.User == nil && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
	}
	if !loopback(cpaURL) || !loopback(controlURL) {
		t.Fatal("联合入口必须是loopback")
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("联合测试拒绝非loopback连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	var fixture struct {
		AccountID string `json:"account_id"`
		ProxyURL  string `json:"proxy_url"`
	}
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	if status != 200 || json.Unmarshal(raw, &fixture) != nil || fixture.AccountID == "" || !loopback(fixture.ProxyURL) {
		t.Fatal("回收站联合fixture无效")
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: "synthetic-management-key"}}, client)
	state := kiloProxySnapshot(t, h)
	if len(state.Proxies) != 0 || len(state.Recycle) != 0 {
		t.Fatal("回收站联合要求空的隔离代理池")
	}
	ref := state.ConnectionRef
	read := func() model.KiloProxyState {
		t.Helper()
		r := kiloRouterRequest(h, "GET", "/api/kilo/proxy-recycle", kiloRouterAdminKey)
		var result model.KiloProxyState
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.ConnectionRef != ref || result.Revision != result.RuntimeRevision {
			t.Fatalf("真实回收站投影无效:%d %s", r.Code, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
		return result
	}
	write := func(method, path string, fields map[string]any, want int) {
		t.Helper()
		fields["expected_revision"], fields["connection_ref"] = state.Revision, ref
		r := kiloAccountsWrite(h, method, path, kiloRouterAdminKey, fields)
		if r.Code != want {
			t.Fatalf("真实回收站写%s %s=%d，预期%d:%s", method, path, r.Code, want, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
		if strings.Contains(r.Body.String(), fixture.ProxyURL) {
			t.Fatal("代理URL被回显")
		}
		if want >= 400 {
			return
		}
		if json.Unmarshal(r.Body.Bytes(), &state) != nil || state.ConnectionRef != ref || state.Revision != state.RuntimeRevision || !state.Managed {
			t.Fatal("写响应未返回完整已发布视图")
		}
	}
	type observed struct {
		Health int `json:"health_requests"`
		Proxy  int `json:"proxy_requests"`
		Direct int `json:"direct_requests"`
	}
	observe := func() observed {
		t.Helper()
		status, raw := kiloJointGET(t, client, controlURL+"/state", "")
		var result observed
		if status != 200 || json.Unmarshal(raw, &result) != nil {
			t.Fatal("读取联合观察失败")
		}
		return result
	}
	healthMode := func(healthy bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]bool{"healthy": healthy})
		r, err := client.Post(controlURL+"/health-mode", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		io.Copy(io.Discard, r.Body)
		if r.StatusCode != 200 {
			t.Fatal("外部IP目标切换失败")
		}
	}
	infer := func(label string) {
		t.Helper()
		req, _ := http.NewRequest("POST", cpaURL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"`+label+`","stream":false}`))
		req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
		req.Header.Set("Content-Type", "application/json")
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		var result struct {
			ID string `json:"id"`
		}
		if err != nil || r.StatusCode != 200 || json.Unmarshal(body, &result) != nil || result.ID == "" {
			t.Fatalf("正式执行器未完成:%d %s %v", r.StatusCode, body, err)
		}
	}
	healthMode(true)
	write("POST", "/api/kilo/proxies", map[string]any{"url": fixture.ProxyURL, "name": "回收站联合代理", "enabled": true}, 201)
	if len(state.Proxies) != 1 {
		t.Fatal("新增代理数量错误")
	}
	p1 := state.Proxies[0]
	write("PUT", "/api/kilo/accounts/"+url.PathEscape(fixture.AccountID)+"/egress", map[string]any{"mode": "pool", "exclusive_proxy_ids": []string{p1.ID}, "primary_proxy_id": p1.ID}, 200)
	write("POST", "/api/kilo/proxies/"+url.PathEscape(p1.ID)+"/recheck", map[string]any{}, 200)
	infer("joint-before-archive")
	if count := observe(); count.Proxy != 1 || count.Direct != 0 {
		t.Fatalf("归档前真实代理链未命中:%+v", count)
	}
	state = read()
	write("DELETE", "/api/kilo/proxies/"+url.PathEscape(p1.ID), map[string]any{}, 200)
	if len(state.Proxies) != 0 || len(state.Recycle) != 1 || state.Recycle[0].ID != p1.ID || state.Recycle[0].OriginalAccountID != fixture.AccountID || state.Recycle[0].Name != p1.Name || state.Recycle[0].Order != p1.Order {
		t.Fatal("归档未保留稳定字段与原归属")
	}
	for _, a := range state.Accounts {
		if a.AccountID == fixture.AccountID && a.PrimaryProxyID != "" {
			t.Fatal("归档未清主偏好")
		}
	}
	archived := state
	healthMode(false)
	before := observe().Health
	write("POST", "/api/kilo/proxy-recycle/"+url.PathEscape(p1.ID)+"/restore", map[string]any{}, 502)
	state = read()
	if state.Revision != archived.Revision || !reflect.DeepEqual(state.Recycle, archived.Recycle) || len(state.Proxies) != 0 || observe().Health <= before {
		t.Fatal("失败恢复未强制探测或改变了归档状态")
	}
	healthMode(true)
	before = observe().Health
	write("POST", "/api/kilo/proxy-recycle/"+url.PathEscape(p1.ID)+"/restore", map[string]any{}, 200)
	if len(state.Recycle) != 0 || len(state.Proxies) != 1 || observe().Health <= before {
		t.Fatal("成功恢复未走真实健康目标")
	}
	restored := state.Proxies[0]
	if restored.ID != p1.ID || restored.Name != p1.Name || restored.Order != p1.Order || !restored.Enabled || !restored.Healthy || restored.AccountID != "" || restored.Owner != "" || restored.Active != 0 {
		t.Fatal("恢复未保持稳定字段或恢复了旧归属/占用")
	}
	for _, a := range state.Accounts {
		if a.AccountID == fixture.AccountID && a.PrimaryProxyID != "" {
			t.Fatal("恢复复活了旧主偏好")
		}
	}
	infer("joint-after-restore")
	if count := observe(); count.Proxy != 2 || count.Direct != 0 {
		t.Fatalf("恢复后正式执行器未复用代理:%+v", count)
	}
	state = read()
	write("PATCH", "/api/kilo/proxies/"+url.PathEscape(p1.ID), map[string]any{"enabled": false}, 200)
	revision := state.Revision
	before = observe().Health
	write("POST", "/api/kilo/proxies/"+url.PathEscape(p1.ID)+"/recheck", map[string]any{}, 200)
	if state.Revision != revision || state.Proxies[0].Enabled || state.Proxies[0].Healthy || observe().Health <= before {
		t.Fatal("disabled重验改变配置或复用缓存")
	}
	write("PATCH", "/api/kilo/proxies/"+url.PathEscape(p1.ID), map[string]any{"enabled": true}, 200)
	write("POST", "/api/kilo/proxies", map[string]any{"url": fixture.ProxyURL, "name": "待清空代理", "enabled": true}, 201)
	p2 := ""
	for _, p := range state.Proxies {
		if p.ID != p1.ID {
			p2 = p.ID
		}
	}
	if p2 == "" {
		t.Fatal("未创建独立第二代理")
	}
	write("DELETE", "/api/kilo/proxies/"+url.PathEscape(p2), map[string]any{}, 200)
	active := append([]model.KiloProxy(nil), state.Proxies...)
	write("DELETE", "/api/kilo/proxy-recycle", map[string]any{"confirm": true}, 200)
	state = read()
	if len(state.Recycle) != 0 || !reflect.DeepEqual(active, state.Proxies) || len(state.Proxies) != 1 || state.Proxies[0].ID != p1.ID || state.Proxies[0].Active != 0 || state.Proxies[0].Owner != "" {
		t.Fatal("清空影响活跃池或保留占用")
	}
	accountsRec := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	var accounts model.KiloAccounts
	if accountsRec.Code != 200 || json.Unmarshal(accountsRec.Body.Bytes(), &accounts) != nil || len(accounts.Accounts) != 1 || accounts.Accounts[0].ID != fixture.AccountID || accounts.Accounts[0].Active != 0 || accounts.Accounts[0].Waiting != 0 {
		t.Fatal("账号身份或租约释放异常")
	}
	t.Logf("真实双Router回收站：归档200→失败恢复502留站→真实IP健康恢复200共享→正式Executor前后proxy=2/direct=0→disabled重验不改revision→清空仅站；最终revision=%d health_requests=%d", state.Revision, observe().Health)
}
