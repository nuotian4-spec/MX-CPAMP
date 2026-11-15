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
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// TestKiloManagerProxiesJointRuntime由Core父测试提供实际Router和本地外部替身。
func TestKiloManagerProxiesJointRuntime(t *testing.T) {
	cpaURL, controlURL := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL")
	if cpaURL == "" || controlURL == "" {
		t.Skip("仅由Core代理联合fixture执行")
	}
	loopback := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && u.Scheme == "http" && u.User == nil && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
	}
	if !loopback(cpaURL) || !loopback(controlURL) {
		t.Fatal("联合入口必须为loopback")
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
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	var fixture struct {
		AccountID string `json:"account_id"`
		ProxyURL  string `json:"proxy_url"`
	}
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	if status != 200 || json.Unmarshal(raw, &fixture) != nil || fixture.AccountID == "" {
		t.Fatal("代理联合fixture无效")
	}
	proxyURL, err := url.Parse(fixture.ProxyURL)
	if err != nil || proxyURL.Scheme != "http" || net.ParseIP(proxyURL.Hostname()) == nil || !net.ParseIP(proxyURL.Hostname()).IsLoopback() {
		t.Fatal("测试代理必须为loopback HTTP代理")
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: "synthetic-management-key"}}, client)
	state := kiloProxySnapshot(t, h)
	ref := state.ConnectionRef
	read := func() model.KiloProxyState {
		t.Helper()
		result := kiloProxySnapshot(t, h)
		if result.ConnectionRef != ref || result.Revision != result.RuntimeRevision {
			t.Fatal("真实池投影引用或运行版本不一致")
		}
		return result
	}
	write := func(method, path string, fields map[string]any, want int) model.KiloProxyState {
		t.Helper()
		fields["expected_revision"] = state.Revision
		fields["connection_ref"] = ref
		rec := kiloAccountsWrite(h, method, path, kiloRouterAdminKey, fields)
		if rec.Code != want {
			t.Fatalf("真实池写入%s %s状态=%d正文=%s", method, path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), fixture.ProxyURL) {
			t.Fatal("代理URL被回显")
		}
		assertKiloRouterSanitized(t, rec)
		var result model.KiloProxyState
		if json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.ConnectionRef != ref || result.Revision != result.RuntimeRevision || !result.Managed {
			t.Fatal("写响应非真实已发布投影")
		}
		return result
	}
	accountsRec := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	var accounts model.KiloAccounts
	if accountsRec.Code != 200 || json.Unmarshal(accountsRec.Body.Bytes(), &accounts) != nil || len(accounts.Accounts) != 1 || accounts.Accounts[0].ID != fixture.AccountID {
		t.Fatal("联合账号不唯一或不匹配")
	}
	known := map[string]bool{}
	for _, p := range state.Proxies {
		known[p.ID] = true
	}
	oldRevision := state.Revision
	state = write("POST", "/api/kilo/proxies", map[string]any{"url": fixture.ProxyURL, "name": "联合代理", "enabled": true}, 201)
	proxyID := ""
	for _, p := range state.Proxies {
		if !known[p.ID] {
			if proxyID != "" {
				t.Fatal("新增代理数量错误")
			}
			proxyID = p.ID
		}
	}
	if proxyID == "" {
		t.Fatal("未返回新增代理")
	}
	stale := kiloAccountsWrite(h, "PATCH", "/api/kilo/proxies/"+url.PathEscape(proxyID), kiloRouterAdminKey, map[string]any{"expected_revision": oldRevision, "connection_ref": ref, "name": "不应保存"})
	if stale.Code != 409 {
		t.Fatalf("旧版本应409，实际%d", stale.Code)
	}
	if current := read(); current.Revision != state.Revision {
		t.Fatal("版本冲突改变了状态")
	}
	state = write("PATCH", "/api/kilo/proxies/"+url.PathEscape(proxyID), map[string]any{"name": "联合出口", "order": 0, "enabled": true}, 200)
	state = write("PATCH", "/api/kilo/proxy-pool", map[string]any{"proxy_limit": 2}, 200)
	state = write("PUT", "/api/kilo/accounts/"+url.PathEscape(fixture.AccountID)+"/egress", map[string]any{"mode": "pool", "exclusive_proxy_ids": []string{proxyID}, "primary_proxy_id": proxyID}, 200)
	infer := func(label string) {
		t.Helper()
		req, err := http.NewRequest("POST", cpaURL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"`+label+`","stream":false}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		var response struct {
			ID string `json:"id"`
		}
		if err != nil || res.StatusCode != 200 || json.Unmarshal(body, &response) != nil || response.ID == "" {
			t.Fatalf("正式执行器响应未完成: status=%d err=%v body=%s", res.StatusCode, err, string(body))
		}
	}
	counts := func(wantProxy, wantDirect int) {
		t.Helper()
		status, raw := kiloJointGET(t, client, controlURL+"/state", "")
		var result struct {
			Proxy  *int `json:"proxy_requests"`
			Direct *int `json:"direct_requests"`
		}
		if status != 200 || json.Unmarshal(raw, &result) != nil || result.Proxy == nil || result.Direct == nil || *result.Proxy != wantProxy || *result.Direct != wantDirect {
			t.Fatalf("真实出站计数不匹配: %s", string(raw))
		}
	}
	counts(0, 0)
	infer("joint-proxy")
	counts(1, 0)
	state = read()
	state = write("PUT", "/api/kilo/accounts/"+url.PathEscape(fixture.AccountID)+"/egress", map[string]any{"mode": "direct", "exclusive_proxy_ids": []string{}, "primary_proxy_id": ""}, 200)
	for _, p := range state.Proxies {
		if p.AccountID == fixture.AccountID {
			t.Fatal("direct未释放账号专属")
		}
	}
	found := false
	for _, a := range state.Accounts {
		if a.AccountID == fixture.AccountID {
			found = a.Mode == "direct" && a.PrimaryProxyID == ""
		}
	}
	if !found {
		t.Fatal("direct/主偏好状态未落地")
	}
	infer("joint-direct")
	counts(1, 1)
	state = read()
	for _, p := range state.Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatal("正式推理完成后代理占用未释放")
		}
	}
	accountsRec = kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	if accountsRec.Code != 200 || json.Unmarshal(accountsRec.Body.Bytes(), &accounts) != nil || len(accounts.Accounts) != 1 || accounts.Accounts[0].ID != fixture.AccountID || accounts.Accounts[0].Active != 0 || accounts.Accounts[0].Waiting != 0 {
		t.Fatal("账号身份或租约释放错误")
	}
	t.Logf("真实双Router代理链：创建201、代理/容量/全量出口200、旧版本409；正式出站proxy=1/direct=1，配置运行revision=%d，占用归零，账号身份保持", state.Revision)
}
