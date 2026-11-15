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
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestKiloManagerQuotaJointRuntime仅由Core固定额度父桥驱动。
func TestKiloManagerQuotaJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if cpaURL == "" || controlURL == "" || key == "" {
		t.Skip("仅由Core额度联合fixture执行")
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
		AccountID           string `json:"account_id"`
		CredentialID        string `json:"credential_id"`
		ChatGPTAccountID    string `json:"chatgpt_account_id"`
		ImportedAccessToken string `json:"imported_access_token"`
		ImportedAccountID   string `json:"imported_account_id"`
	}
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	if status != 200 || json.Unmarshal(raw, &fixture) != nil || fixture.AccountID == "" || fixture.CredentialID == "" || fixture.ChatGPTAccountID == "" || fixture.ImportedAccessToken == "" || fixture.ImportedAccountID == "" {
		t.Fatal("额度联合fixture无效")
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}, client)
	ref := kiloAccountsRef(t, h)
	base := "/api/kilo/accounts/" + url.PathEscape(fixture.AccountID) + "/quota"
	verify := func(phase string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"phase": phase})
		r, err := client.Post(controlURL+"/verify", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 {
			t.Fatalf("父桥验证%s失败:%d %s", phase, r.StatusCode, raw)
		}
	}
	check := func(raw []byte) {
		t.Helper()
		for _, secret := range []string{fixture.ImportedAccessToken, `"access_token":`, `"refresh_token":`, `"cookie":`} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("额度响应泄露材料")
			}
		}
	}
	readSources := func() model.KiloQuotaSources {
		t.Helper()
		r := kiloRouterRequest(h, "GET", "/api/kilo/quota-sources", kiloRouterAdminKey)
		var v model.KiloQuotaSources
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &v) != nil || v.ConnectionRef != ref {
			t.Fatalf("来源列表失败:%d %s", r.Code, r.Body.String())
		}
		check(r.Body.Bytes())
		return v
	}
	sources := readSources()
	found := false
	for _, source := range sources.Sources {
		if source.CredentialID == fixture.CredentialID && source.ChatGPTAccountID != nil && *source.ChatGPTAccountID == fixture.ChatGPTAccountID {
			found = true
		}
	}
	if !found || len(sources.Sources) < 2 {
		t.Fatal("未提供精确身份的独立候选")
	}
	request := func(method, path string, fields map[string]any) model.KiloQuotaView {
		t.Helper()
		if fields != nil {
			fields["connection_ref"] = ref
		}
		r := quotaCall(h, method, path, kiloRouterAdminKey, fields)
		if r.Code != 200 {
			t.Fatalf("额度真实请求%s %s=%d %s", method, path, r.Code, r.Body.String())
		}
		var view model.KiloQuotaView
		if json.Unmarshal(r.Body.Bytes(), &view) != nil || view.AccountID != fixture.AccountID || view.ConnectionRef != ref {
			t.Fatal("额度视图身份/引用错误")
		}
		check(r.Body.Bytes())
		return view
	}
	checkAccountProjection := func(kind, credentialID string) {
		t.Helper()
		r := kiloRouterRequest(h, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
		var list model.KiloAccounts
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &list) != nil || list.ConnectionRef != ref {
			t.Fatalf("来源绑定后账号列表失败:%d %s", r.Code, r.Body.String())
		}
		found := false
		for _, account := range list.Accounts {
			if account.ID == fixture.AccountID {
				found = true
				if account.QuotaSource != kind || account.CodexCredentialID != credentialID {
					t.Fatalf("账号列表来源投影错误:%+v", account)
				}
			}
		}
		if !found {
			t.Fatal("绑定账号未出现在列表")
		}
		check(r.Body.Bytes())
		updated := kiloAccountsWrite(h, "PATCH", "/api/kilo/accounts/"+url.PathEscape(fixture.AccountID), kiloRouterAdminKey, map[string]any{"connection_ref": ref, "name": "联合额度账号"})
		var account model.KiloAccount
		if updated.Code != 200 || json.Unmarshal(updated.Body.Bytes(), &account) != nil || account.ID != fixture.AccountID || account.QuotaSource != kind || account.CodexCredentialID != credentialID {
			t.Fatalf("来源绑定后账号更新回执错误:%d %s", updated.Code, updated.Body.String())
		}
		check(updated.Body.Bytes())
	}
	initial := request("GET", base, nil)
	if initial.BindingRevision != 0 || initial.Source.Kind != "none" || initial.Query.Status != "never" {
		t.Fatal("初始来源状态不符")
	}
	reference := request("PUT", base+"-source", map[string]any{"expected_binding_revision": 0, "source": map[string]any{"kind": "cpa_codex", "credential_id": fixture.CredentialID, "chatgpt_account_id": fixture.ChatGPTAccountID}})
	if reference.BindingRevision != 1 || reference.Source.Kind != "cpa_codex" || reference.Query.Status != "never" || len(reference.Windows) != 0 {
		t.Fatal("引用绑定隐式查询或版本错误")
	}
	checkAccountProjection("codex_ref", fixture.CredentialID)
	reference = request("POST", base+"/query", map[string]any{"expected_binding_revision": 1})
	if reference.Query.Status != "success" || reference.Query.ObservedAt == nil || reference.Stale {
		t.Fatal("原生命周期额度查询未成功")
	}
	verify("reference")
	imported := request("PUT", base+"-source", map[string]any{"expected_binding_revision": 1, "source": map[string]any{"kind": "imported", "access_token": fixture.ImportedAccessToken, "chatgpt_account_id": fixture.ImportedAccountID}})
	if imported.BindingRevision != 2 || imported.Source.Kind != "imported" || imported.Query.Status != "never" || imported.Query.ObservedAt != nil || len(imported.Windows) != 0 {
		t.Fatal("独立来源更换未清旧观测")
	}
	checkAccountProjection("imported", "")
	imported = request("POST", base+"/query", map[string]any{"expected_binding_revision": 2})
	if imported.Query.Status != "success" || imported.Query.ObservedAt == nil {
		t.Fatal("独立材料查询失败")
	}
	verify("imported")
	failed := request("POST", base+"/query", map[string]any{"expected_binding_revision": 2})
	if failed.Query.Status != "failed" || failed.Source.Status != "needs_reimport" || !failed.Stale || failed.Query.ObservedAt == nil || *failed.Query.ObservedAt != *imported.Query.ObservedAt || !reflect.DeepEqual(failed.Windows, imported.Windows) || failed.Query.ErrorCode == nil {
		t.Fatal("usage失败未保旧值/成功时间或来源状态")
	}
	verify("failed")
	removed := request("DELETE", base+"-source", map[string]any{"expected_binding_revision": 2})
	if removed.BindingRevision != 3 || removed.Source.Kind != "none" || removed.Query.ObservedAt != nil || len(removed.Windows) != 0 {
		t.Fatal("解绑未清当前Kilo来源观测")
	}
	checkAccountProjection("none", "")
	final := request("GET", base, nil)
	if final.BindingRevision != 3 || final.Source.Kind != "none" {
		t.Fatal("解绑真实读取不一致")
	}
	remaining := readSources()
	found = false
	for _, source := range remaining.Sources {
		if source.CredentialID == fixture.CredentialID {
			found = true
		}
	}
	if !found {
		t.Fatal("解绑删除了Codex来源")
	}
	verify("unlinked")
	t.Log("真实额度五接口链PASS：精确Codex/outer身份→引用rev1原生命周期query成功→独立rev2清观测并成功query→usage失败200/needs_reimport保旧observed_at→解绑rev3保留Codex；模型请求0由父桥核实")
}
