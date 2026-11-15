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

// TestKiloManagerAE1JointRuntime由Core的AE1真实CPA父桥驱动。
func TestKiloManagerAE1JointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || controlURL == "" || key == "" {
		t.Skip("仅由Core AE1联合父桥执行")
	}
	for _, raw := range []string{cpaURL, controlURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("联合入口必须是loopback")
		}
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("联合测试拒绝外部连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	var fixture struct {
		ProxyID         string `json:"proxy_id"`
		FixedIP         string `json:"fixed_ip"`
		SourceID        string `json:"source_id"`
		SourceAccountID string `json:"source_account_id"`
	}
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	if status != 200 || json.Unmarshal(raw, &fixture) != nil || fixture.ProxyID == "" || fixture.FixedIP == "" || fixture.SourceID == "" || fixture.SourceAccountID == "" {
		t.Fatal("AE1父桥fixture无效")
	}
	scenario := func(mode string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"mode": mode})
		res, err := client.Post(controlURL+"/scenario", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ = io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("父桥场景%s失败:%d %s", mode, res.StatusCode, body)
		}
	}
	resolver := &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}
	h := newKiloFixtureRouter(t, resolver, client)
	ref := kiloAccountsRef(t, h)
	request := func(method, path string, fields map[string]any, want int) []byte {
		t.Helper()
		var recStatus int
		var data []byte
		if method == "GET" {
			rec := kiloRouterRequest(h, method, path, kiloRouterAdminKey)
			recStatus, data = rec.Code, rec.Body.Bytes()
			assertKiloRouterSanitized(t, rec)
		} else {
			fields["connection_ref"] = ref
			rec := kiloAccountsWrite(h, method, path, kiloRouterAdminKey, fields)
			recStatus, data = rec.Code, rec.Body.Bytes()
			assertKiloRouterSanitized(t, rec)
		}
		if recStatus != want {
			t.Fatalf("Manager真实链%s %s=%d，预期%d:%s", method, path, recStatus, want, data)
		}
		return data
	}
	state := kiloProxySnapshot(t, h)
	if state.AutoAssignmentEnabled == nil || *state.AutoAssignmentEnabled || len(state.Proxies) != 1 {
		t.Fatal("联合初始代理池不符")
	}
	for _, tc := range []struct {
		method, path string
		fields       map[string]any
	}{
		{"PATCH", "/api/kilo/proxy-pool", map[string]any{"auto_assignment_enabled": true, "max_unique_accounts": 1}},
		{"POST", "/api/kilo/proxies/" + url.PathEscape(fixture.ProxyID) + "/recheck", map[string]any{}},
		{"PATCH", "/api/kilo/proxies/" + url.PathEscape(fixture.ProxyID), map[string]any{"fixed_exit_ip": fixture.FixedIP}},
	} {
		tc.fields["expected_revision"] = state.Revision
		raw = request(tc.method, tc.path, tc.fields, 200)
		if json.Unmarshal(raw, &state) != nil || state.ConnectionRef != ref {
			t.Fatal("池操作投影缺失")
		}
	}
	createAccount := func(name string) string {
		t.Helper()
		raw := request("POST", "/api/kilo/accounts", map[string]any{"api_key": "synthetic-ae1-" + name, "name": name, "enabled": true}, 201)
		var account model.KiloAccountSaved
		if json.Unmarshal(raw, &account) != nil || account.ID == "" || account.ConnectionRef != ref {
			t.Fatal("新账号保存投影无效")
		}
		return account.ID
	}
	a := createAccount("AE1-A")
	scenario("sync")
	b := createAccount("AE1-B")
	scenario("sync")
	state = kiloProxySnapshot(t, h)
	findGroup := func() model.KiloIPGroup {
		t.Helper()
		if state.IPGroups != nil {
			for _, group := range *state.IPGroups {
				if group.IP == fixture.FixedIP {
					return group
				}
			}
		}
		t.Fatal("固定IP组缺失")
		return model.KiloIPGroup{}
	}
	findEgress := func(id string) model.KiloAccountEgress {
		t.Helper()
		for _, egress := range state.Accounts {
			if egress.AccountID == id {
				return egress
			}
		}
		t.Fatal("账号出口缺失")
		return model.KiloAccountEgress{}
	}
	group := findGroup()
	if group.OwnerAccountID != a || group.UniqueAccounts != 1 || len(group.AccountIDs) != 1 || group.AccountIDs[0] != a || group.Retired {
		t.Fatal("首账号独占或历史计数不符")
	}
	if first, second := findEgress(a), findEgress(b); first.Mode != "auto" || first.AssignedIP != fixture.FixedIP || second.Mode != "auto" || second.AllocationState != "waiting" || second.AssignedIP != "" {
		t.Fatal("第二账号缺IP未保持等待")
	}
	base := "/api/kilo/accounts/" + url.PathEscape(a)
	var quota model.KiloQuotaView
	if json.Unmarshal(request("GET", base+"/quota", nil, 200), &quota) != nil {
		t.Fatal("额度读取失败")
	}
	raw = request("PUT", base+"/quota-source", map[string]any{"expected_binding_revision": quota.BindingRevision, "source": map[string]any{"kind": "cpa_codex", "credential_id": fixture.SourceID, "chatgpt_account_id": fixture.SourceAccountID}}, 200)
	if json.Unmarshal(raw, &quota) != nil || quota.Source.Kind != "cpa_codex" {
		t.Fatal("来源未真实绑定")
	}
	var policy model.KiloSourcePolicy
	if json.Unmarshal(request("GET", base+"/source-policy", nil, 200), &policy) != nil || policy.Enabled {
		t.Fatal("来源策略初始未默认关闭")
	}
	scenario("pause")
	if json.Unmarshal(request("PUT", base+"/source-policy", map[string]any{"expected_policy_revision": policy.Revision, "enabled": true, "interval_seconds": 1800}, 200), &policy) != nil || !policy.Enabled {
		t.Fatal("来源策略开启未确认")
	}
	query := func(used float64, paused bool) {
		t.Helper()
		raw := request("POST", base+"/quota/query", map[string]any{"expected_binding_revision": quota.BindingRevision}, 200)
		if json.Unmarshal(raw, &quota) != nil || quota.Query.Status != "success" || len(quota.Windows) == 0 {
			t.Fatalf("额度查询失败:%s", raw)
		}
		found := false
		for _, window := range quota.Windows {
			if window.Group == "rate_limit" && window.UsedPercent != nil && *window.UsedPercent == used {
				found = true
			}
		}
		if !found {
			t.Fatalf("原始用量%v未保留", used)
		}
		if json.Unmarshal(request("GET", base+"/source-policy", nil, 200), &policy) != nil || policy.Paused != paused || !policy.Persisted || policy.ConnectionRef != ref {
			t.Fatalf("来源策略状态不符:%+v", policy)
		}
		if paused && policy.Status != "paused" || !paused && policy.Status != "ready" {
			t.Fatalf("策略status=%s", policy.Status)
		}
		var listed model.KiloAccounts
		if json.Unmarshal(request("GET", "/api/kilo/accounts", nil, 200), &listed) != nil || len(listed.Accounts) != 2 {
			t.Fatal("两账号未保持保存")
		}
		found = false
		for _, account := range listed.Accounts {
			if account.ID == a {
				found = true
				if account.SourcePolicy == nil || account.SourcePolicy.Paused != paused {
					t.Fatal("账号summary未同步来源策略")
				}
				if account.AutoRestriction != nil && account.AutoRestriction.Reason != "none" {
					t.Fatal("来源策略被冒充实际Kilo限制")
				}
			}
		}
		if !found {
			t.Fatal("账号A丢失")
		}
	}
	query(100, true)
	scenario("sync")
	state = kiloProxySnapshot(t, h)
	group = findGroup()
	if !group.Retired || group.OwnerAccountID != "" || len(state.Recycle) != 1 || state.Recycle[0].FixedExitIP != fixture.FixedIP || group.UniqueAccounts != 1 {
		t.Fatal("来源暂停未真实退役或累计被清除")
	}
	scenario("recover")
	query(1, false)
	scenario("sync")
	state = kiloProxySnapshot(t, h)
	group = findGroup()
	if !group.Retired || group.OwnerAccountID != "" || group.UniqueAccounts != 1 || len(group.AccountIDs) != 1 || group.AccountIDs[0] != a {
		t.Fatal("恢复错误复活退休IP或改写历史")
	}
	for _, id := range []string{a, b} {
		egress := findEgress(id)
		if egress.AllocationState != "waiting" || egress.AssignedIP != "" {
			t.Fatalf("恢复后缺IP账号未等待:%+v", egress)
		}
	}
	t.Run("独立材料缺查询出口返回业务失败", func(t *testing.T) {
		raw := request("PUT", base+"/quota-source", map[string]any{"expected_binding_revision": quota.BindingRevision, "source": map[string]any{"kind": "imported", "access_token": "synthetic-ae1-independent-material", "chatgpt_account_id": fixture.SourceAccountID}}, 200)
		if json.Unmarshal(raw, &quota) != nil || quota.Source.Kind != "imported" {
			t.Fatal("独立材料绑定失败")
		}
		raw = request("POST", base+"/quota/query", map[string]any{"expected_binding_revision": quota.BindingRevision}, 200)
		if json.Unmarshal(raw, &quota) != nil || quota.Query.Status != "failed" || quota.Query.ErrorCode == nil || *quota.Query.ErrorCode != "query_egress_unavailable" {
			t.Fatalf("独立查询出口缺失被改写:%s", raw)
		}
	})
	t.Log("真实Manager→CPA完成：开启自动N1、健康重验、固定IP确认、双账号分配/等待、来源100暂停退役、独立查询恢复并继续等待；历史保留1，owner为空；独立材料缺出口返回业务失败。")
}
