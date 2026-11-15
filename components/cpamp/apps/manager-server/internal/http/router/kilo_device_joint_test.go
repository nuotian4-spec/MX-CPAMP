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

// TestKiloManagerDeviceJointRuntime由Core冻结副本的设备授权父桥驱动。
func TestKiloManagerDeviceJointRuntime(t *testing.T) {
	cpaURL, controlURL, managementKey := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || controlURL == "" || managementKey == "" {
		t.Skip("仅由Core设备授权联合fixture执行")
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
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: managementKey}}, client)
	ref := kiloAccountsRef(t, h)
	list := func() model.KiloAccounts {
		t.Helper()
		r := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
		var result model.KiloAccounts
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.ConnectionRef != ref {
			t.Fatalf("联合账号列表无效:%d %s", r.Code, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
		return result
	}
	if len(list().Accounts) != 0 {
		t.Fatal("联合授权要求空隔离账号集")
	}
	ids := map[string]bool{}
	for _, mode := range []string{"direct", "pool"} {
		body, _ := json.Marshal(map[string]string{"mode": mode})
		res, err := client.Post(controlURL+"/scenario", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("场景切换失败:%d", res.StatusCode)
		}
		call := func(method, path string, fields map[string]any, want int, destination any) {
			t.Helper()
			fields["connection_ref"] = ref
			r := kiloAccountsWrite(h, method, path, kiloRouterAdminKey, fields)
			if r.Code != want {
				t.Fatalf("真实设备链%s %s=%d预期%d:%s", method, path, r.Code, want, r.Body.String())
			}
			assertKiloRouterSanitized(t, r)
			for _, secret := range []string{`"code":`, `"token":`, `"api_key":`, `"machine_id":`} {
				if strings.Contains(r.Body.String(), secret) {
					t.Fatal("设备联合回执含私密字段")
				}
			}
			if json.Unmarshal(r.Body.Bytes(), destination) != nil {
				t.Fatal("联合回执非JSON")
			}
		}
		var started model.KiloDeviceStarted
		call("POST", "/api/kilo/device-auth", map[string]any{"name": "设备联合-" + mode, "authorization_egress": map[string]string{"mode": mode}, "priority": 2, "enabled": true, "concurrency_limit": nil, "allow_direct_when_proxy_full": false}, 200, &started)
		if started.ID == "" || started.UserCode == "" || started.VerificationURL == "" || started.ConnectionRef != ref || started.Interval != 3 || started.InferenceEgressMode != "pool" {
			t.Fatal("start缺少有效白名单字段")
		}
		sessionPath := "/api/kilo/device-auth/" + url.PathEscape(started.ID)
		if len(list().Accounts) != len(ids) {
			t.Fatal("仅start就产生正式账号")
		}
		var approved model.KiloDeviceState
		call("POST", sessionPath+"/poll", map[string]any{}, 200, &approved)
		if approved.Status != "approved" || approved.SaveExpiresIn == nil || *approved.SaveExpiresIn < 1 || *approved.SaveExpiresIn > 300 || approved.AccountID != "" || approved.ConnectionRef != ref {
			t.Fatal("approved语义或保存窗口错误")
		}
		window := *approved.SaveExpiresIn
		call("POST", sessionPath+"/poll", map[string]any{}, 200, &approved)
		if approved.Status != "approved" || approved.SaveExpiresIn == nil || *approved.SaveExpiresIn > window || len(list().Accounts) != len(ids) {
			t.Fatal("重复approved延长窗口或产生账号")
		}
		var account model.KiloDeviceSaved
		call("POST", sessionPath+"/save", map[string]any{}, 201, &account)
		if account.ID == "" || ids[account.ID] || account.ConnectionRef != ref || account.EgressMode != "pool" || account.EffectiveLimit != 20 || !account.Enabled {
			t.Fatal("save未返回稳定正式账号或默认共享池")
		}
		if mode == "direct" && (account.EgressStatus != "pending" || account.EgressReason != "shared_proxy_not_configured") {
			t.Fatalf("direct授权不应套成推理直连:%+v", account)
		}
		if mode == "pool" && (account.EgressStatus != "available" || account.EgressReason != "") {
			t.Fatalf("pool正式出口快照错误:%+v", account)
		}
		id := account.ID
		ids[id] = true
		call("POST", sessionPath+"/save", map[string]any{}, 201, &account)
		if account.ID != id {
			t.Fatal("重复save另建账号")
		}
		var final model.KiloDeviceState
		call("POST", sessionPath+"/poll", map[string]any{}, 200, &final)
		if final.Status != "saved" || final.AccountID != id || final.ConnectionRef != ref {
			t.Fatal("poll未确认稳定saved/AuthID")
		}
		call("DELETE", sessionPath, map[string]any{}, 200, &final)
		if final.Status != "saved" || final.AccountID != id {
			t.Fatal("cancel错误撤销已保存账号")
		}
		accounts := list()
		if len(accounts.Accounts) != len(ids) {
			t.Fatal("重复save/cancel改变账号数量")
		}
		for _, a := range accounts.Accounts {
			if !ids[a.ID] || a.Active != 0 || a.Waiting != 0 {
				t.Fatal("账号身份或槽位异常")
			}
		}
		t.Logf("真实设备链 mode=%s start200→approved未保存→重复poll窗口不续→save201→重复save同ID→poll/cancel=saved；account_id=%s egress=%s/%s", mode, id, account.EgressStatus, account.EgressReason)
	}
	pool := kiloProxySnapshot(t, h)
	if len(list().Accounts) != 2 || len(pool.Proxies) != 1 || len(pool.Accounts) != 2 {
		t.Fatalf("最终共享池或账号出口数量异常:%+v", pool)
	}
	for _, policy := range pool.Accounts {
		if policy.Mode != "pool" || policy.PrimaryProxyID != "" || policy.CandidateProxyIDs != nil {
			t.Fatal("默认pool策略改变")
		}
	}
	for _, p := range pool.Proxies {
		if p.AccountID != "" || p.Owner != "" || p.Active != 0 {
			t.Fatal("授权结束残留临时归属或占用")
		}
	}
	// 未显式配置出口的账号仍使用pool；save共同保存凭据与默认出口。
	t.Log("真实Manager设备授权四动作完成：2正式账号；授权direct/pool独立；保存前无账号；保存后默认pool；无临时归属与租约残留；本子测未发送模型请求")
}
