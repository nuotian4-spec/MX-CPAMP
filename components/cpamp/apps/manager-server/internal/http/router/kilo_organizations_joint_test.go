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

// TestKiloManagerOrganizationsJointRuntime仅由Core固定组织父桥驱动。
func TestKiloManagerOrganizationsJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || controlURL == "" || key == "" {
		t.Skip("仅由Core组织联合fixture执行")
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
		AccountID      string `json:"account_id"`
		ReplacementKey string `json:"replacement_key"`
	}
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	if status != 200 || json.Unmarshal(raw, &fixture) != nil || fixture.AccountID == "" || fixture.ReplacementKey == "" {
		t.Fatal("组织联合fixture无效")
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}, client)
	ref := kiloAccountsRef(t, h)
	accountPath := "/api/kilo/accounts/" + url.PathEscape(fixture.AccountID)
	list := func(organization string) {
		t.Helper()
		r := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
		var result model.KiloAccounts
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || len(result.Accounts) != 1 || result.ConnectionRef != ref || result.Accounts[0].ID != fixture.AccountID || result.Accounts[0].OrganizationID == nil || *result.Accounts[0].OrganizationID != organization {
			t.Fatalf("真实账号组织不一致:%d %s", r.Code, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
	}
	organizations := kiloRouterRequest(h, "GET", accountPath+"/organizations", kiloRouterAdminKey)
	var choices model.KiloOrganizations
	if organizations.Code != 200 || json.Unmarshal(organizations.Body.Bytes(), &choices) != nil || choices.ConnectionRef != ref || !choices.HasPersonalAccount || len(choices.Organizations) != 2 || choices.Organizations[0].ID != "org_first" || choices.Organizations[1].ID != "org_second" {
		t.Fatalf("正式组织profile列表不符:%d %s", organizations.Code, organizations.Body.String())
	}
	assertKiloRouterSanitized(t, organizations)
	patch := func(fields map[string]any, want int, organization string) {
		t.Helper()
		fields["connection_ref"] = ref
		r := kiloAccountsWrite(h, "PATCH", accountPath, kiloRouterAdminKey, fields)
		if r.Code != want {
			t.Fatalf("真实组织PATCH=%d预期%d:%s", r.Code, want, r.Body.String())
		}
		assertKiloRouterSanitized(t, r)
		if strings.Contains(r.Body.String(), fixture.ReplacementKey) {
			t.Fatal("换Key回执泄密")
		}
		if want == 200 {
			var saved model.KiloAccountSaved
			if json.Unmarshal(r.Body.Bytes(), &saved) != nil || saved.ID != fixture.AccountID || saved.ConnectionRef != ref || saved.OrganizationID == nil || *saved.OrganizationID != organization {
				t.Fatal("组织保存回执不符")
			}
		}
		list(organization)
	}
	verify := func(phase string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"phase": phase})
		r, err := client.Post(controlURL+"/verify", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 {
			t.Fatalf("父桥组织头/旧续轮验证失败:%d %s", r.StatusCode, body)
		}
	}
	patch(map[string]any{"organization_id": "org_first"}, 200, "org_first")
	verify("first")
	patch(map[string]any{"organization_id": "org_second"}, 200, "org_second")
	verify("second")
	patch(map[string]any{"api_key": fixture.ReplacementKey}, 403, "org_second")
	patch(map[string]any{"organization_id": ""}, 200, "")
	verify("personal")
	list("")
	t.Log("真实Manager组织链：当前Key组织GET200→org_first PATCH200→org_second PATCH200→新Key保留组织复验403且旧组织未变→个人PATCH200；同AuthID/1账号；模型请求仅由Core父桥显式验证")
}
