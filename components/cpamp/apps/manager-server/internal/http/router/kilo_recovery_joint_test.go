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

func TestKiloManagerRecoveryJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || controlURL == "" || key == "" {
		t.Skip("仅由Core固定恢复父桥驱动")
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
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	var fixture struct {
		AccountID           string `json:"account_id"`
		RestrictedAccountID string `json:"restricted_account_id"`
	}
	if status != 200 || json.Unmarshal(raw, &fixture) != nil || fixture.AccountID == "" || fixture.RestrictedAccountID == "" {
		t.Fatal("恢复fixture无效")
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}, client)
	list := func() model.KiloAccounts {
		t.Helper()
		r := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
		var snapshot model.KiloAccounts
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &snapshot) != nil {
			t.Fatalf("真实恢复列表:%d %s", r.Code, r.Body.String())
		}
		if strings.Contains(r.Body.String(), "access_token") || strings.Contains(r.Body.String(), "synthetic-") {
			t.Fatal("自动状态泄露材料")
		}
		return snapshot
	}
	find := func(snapshot model.KiloAccounts, id string) model.KiloAccount {
		t.Helper()
		for _, a := range snapshot.Accounts {
			if a.ID == id {
				return a
			}
		}
		t.Fatal("fixture账号未返回")
		return model.KiloAccount{}
	}
	verify := func(phase string) {
		t.Helper()
		r, err := client.Post(controlURL+"/verify", "application/json", strings.NewReader(`{"phase":"`+phase+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 {
			t.Fatalf("父桥%s核验:%d %s", phase, r.StatusCode, body)
		}
	}
	before := list()
	quota, restricted := find(before, fixture.AccountID), find(before, fixture.RestrictedAccountID)
	if !quota.Enabled || !restricted.Enabled || quota.AutoRestriction == nil || restricted.AutoRestriction == nil || quota.AutoRestriction.Reason != "quota_exhausted" || restricted.AutoRestriction.Reason != "account_restricted" {
		t.Fatal("人工/自动限制状态混淆")
	}
	verify("before")
	r := kiloAccountsWrite(h, "PATCH", "/api/kilo/accounts/"+url.PathEscape(fixture.AccountID), kiloRouterAdminKey, map[string]any{"connection_ref": before.ConnectionRef, "enabled": true})
	var restored model.KiloAccountSaved
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &restored) != nil || restored.ID != fixture.AccountID || restored.ConnectionRef != before.ConnectionRef || !restored.Enabled || restored.AutoRestriction == nil || restored.AutoRestriction.Reason != "none" || !restored.AutoRestriction.Persisted || restored.AutoRestriction.Generation <= quota.AutoRestriction.Generation {
		t.Fatalf("显式同值true恢复不成立:%d %s", r.Code, r.Body.String())
	}
	after := list()
	other := find(after, fixture.RestrictedAccountID)
	if other.AutoRestriction == nil || other.AutoRestriction.Reason != "account_restricted" || other.AutoRestriction.Generation != restricted.AutoRestriction.Generation || !other.Enabled {
		t.Fatal("人工恢复错误清理另账号限制")
	}
	verify("restored")
	t.Log("真实Manager/Core恢复链PASS：quota/restricted与人工true分别投影；首账号显式同值enabled=true清自身/代次增/已保存，另账号restricted保持；模型总2为父合成造限制，人工恢复自身model0由父核实")
}
