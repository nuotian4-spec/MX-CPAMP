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

func TestKiloManagerUX2CapacityJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || controlURL == "" || key == "" {
		t.Skip("仅由Core UX2真实联合父桥执行")
	}
	for _, raw := range []string{cpaURL, controlURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("联合入口必须为loopback")
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
	client := &http.Client{Transport: transport, Timeout: 45 * time.Second}
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	var fixture struct {
		AccountID    string   `json:"account_id"`
		ProxyIDs     []string `json:"proxy_ids"`
		SpecifiedIPs []string `json:"specified_ips"`
	}
	if status != 200 || json.Unmarshal(raw, &fixture) != nil || fixture.AccountID == "" || len(fixture.SpecifiedIPs) < 5 {
		t.Fatalf("UX2父桥fixture无效: %d %s", status, raw)
	}
	control := func(path string, fields map[string]any) {
		t.Helper()
		body, _ := json.Marshal(fields)
		res, err := client.Post(controlURL+path, "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("父桥%s=%d:%s", path, res.StatusCode, data)
		}
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}, client)
	for _, invalid := range []string{"", "synthetic-kilo-key", key} {
		if rec := kiloRouterRequest(h, http.MethodGet, "/api/kilo/proxies", invalid); rec.Code != 401 {
			t.Fatalf("Manager管理门禁=%d", rec.Code)
		}
	}
	state := kiloProxySnapshot(t, h)
	reference := state.ConnectionRef
	read := func(phase string, wantActive, wantAssigned int, wantReason string) model.KiloProxyState {
		t.Helper()
		state := kiloProxySnapshot(t, h)
		if state.ConnectionRef != reference || state.IPConcurrencyLimit == nil || *state.IPConcurrencyLimit != 20 || state.IPGroups == nil {
			t.Fatalf("%s容量或连接字段缺失", phase)
		}
		var account *model.KiloAccountEgress
		for i := range state.Accounts {
			if state.Accounts[i].AccountID == fixture.AccountID {
				account = &state.Accounts[i]
			}
		}
		if account == nil || account.InferenceActive == nil || *account.InferenceActive != wantActive || account.ManagementActive == nil || *account.ManagementActive != 0 || len(account.AssignedIPs) != wantAssigned || account.ConfiguredCapacity == nil || account.CurrentCapacity == nil || account.EffectiveLimit == nil {
			t.Fatalf("%s账号投影不符: %+v", phase, account)
		}
		total, reasonFound := 0, wantReason == ""
		for _, group := range *state.IPGroups {
			if group.OwnerAccountID != fixture.AccountID {
				continue
			}
			if group.InferenceActive == nil || group.ManagementActive == nil || group.Capacity == nil {
				t.Fatalf("%s逐IP占用缺失", phase)
			}
			total += *group.InferenceActive
			if group.Reason == wantReason && group.Draining {
				reasonFound = true
			}
		}
		if total != wantActive || !reasonFound {
			t.Fatalf("%s逐IP总占用=%d/期望%d，排空原因%s存在=%v", phase, total, wantActive, wantReason, reasonFound)
		}
		code, direct := kiloJointGET(t, client, cpaURL+"/v8/management/kilo/proxies", key)
		var core map[string]any
		managerRaw, _ := json.Marshal(state)
		var manager map[string]any
		if code != 200 || json.Unmarshal(direct, &core) != nil || json.Unmarshal(managerRaw, &manager) != nil {
			t.Fatal("读取真实Core投影失败")
		}
		for _, name := range []string{"ip_concurrency_limit", "ip_entry_limit", "accounts", "ip_groups"} {
			if !reflect.DeepEqual(manager[name], core[name]) {
				t.Fatalf("%s Manager字段%s与真实Core不一致", phase, name)
			}
		}
		t.Logf("%s: Manager真实读回 active=%d assigned=%d configured=%d current=%d effective=%d reason=%s", phase, wantActive, wantAssigned, *account.ConfiguredCapacity, *account.CurrentCapacity, *account.EffectiveLimit, wantReason)
		return state
	}
	configure := func(capacity string, max int) {
		t.Helper()
		state := kiloProxySnapshot(t, h)
		fields := map[string]any{"mode": "auto", "capacity_mode": capacity, "allocation_mode": "auto", "max_ips": max, "specified_ips": []string{}, "candidate_proxy_ids": nil, "exclusive_proxy_ids": []string{}, "primary_proxy_id": "", "expected_revision": state.Revision, "connection_ref": reference}
		rec := kiloAccountsWrite(h, http.MethodPut, "/api/kilo/accounts/"+url.PathEscape(fixture.AccountID)+"/egress", kiloRouterAdminKey, fields)
		if rec.Code != 200 {
			t.Fatalf("容量配置经Manager写入=%d:%s", rec.Code, rec.Body.String())
		}
		assertKiloRouterSanitized(t, rec)
	}
	configure("ip", 5)
	control("/hold", map[string]any{"count": 41})
	read("三IP真实在途", 41, 3, "")
	configure("ip", 2)
	read("降至两IP等待排空", 41, 3, "max_ips_reduced")
	control("/release", map[string]any{})
	state = read("降额排空完成仍保留归属", 0, 2, "")
	control("/hold", map[string]any{"count": 21})
	read("两IP真实在途", 21, 2, "")
	configure("fixed", 1)
	read("切固定并发等待排空", 21, 2, "fixed_capacity")
	control("/release", map[string]any{})
	state = read("固定并发排空完成", 0, 1, "")
	stale := kiloAccountsWrite(h, http.MethodPut, "/api/kilo/accounts/"+url.PathEscape(fixture.AccountID)+"/egress", kiloRouterAdminKey, map[string]any{"mode": "auto", "capacity_mode": "ip", "allocation_mode": "auto", "max_ips": 5, "specified_ips": []string{}, "candidate_proxy_ids": nil, "exclusive_proxy_ids": []string{}, "primary_proxy_id": "", "expected_revision": state.Revision, "connection_ref": strings.Repeat("0", 32)})
	if stale.Code != 409 {
		t.Fatalf("旧连接写入=%d", stale.Code)
	}
	read("旧连接写入被拒后状态保持", 0, 1, "")
	t.Log("真实Core专用Key请求、生产CPA与Manager Router联合通过；未以DTO stub替代在途")
}
