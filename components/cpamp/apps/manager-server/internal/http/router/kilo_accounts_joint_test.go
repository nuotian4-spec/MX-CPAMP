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

func TestKiloManagerAccountsJointRuntime(t *testing.T) {
	cpaURL, controlURL := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL")
	if cpaURL == "" || controlURL == "" {
		t.Skip("仅由CPA账号联合fixture执行")
	}
	for _, address := range []string{cpaURL, controlURL} {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
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
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	handler := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: "synthetic-management-key"}}, client)
	ref := kiloAccountsRef(t, handler)
	write := func(method, id string, fields map[string]any, want int) model.KiloAccountSaved {
		t.Helper()
		fields["connection_ref"] = ref
		path := "/api/kilo/accounts"
		if id != "" {
			path += "/" + url.PathEscape(id)
		}
		rec := kiloAccountsWrite(handler, method, path, kiloRouterAdminKey, fields)
		if rec.Code != want {
			t.Fatalf("真实账号写链 %s 状态=%d，预期%d", method, rec.Code, want)
		}
		for _, secret := range []string{"synthetic-kilo-token", "synthetic-replaced-token", "api_key", "token", "proxy_url"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatal("账号写回执泄露敏感数据")
			}
		}
		var result model.KiloAccountSaved
		if want == 201 || want == 200 {
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ConnectionRef != ref || result.ID == "" {
				t.Fatal("账号写回执缺少绑定")
			}
		}
		if want == 204 && rec.Body.Len() != 0 {
			t.Fatal("删除204含正文")
		}
		return result
	}
	state := func(id string) map[string]any {
		t.Helper()
		status, raw := kiloJointGET(t, client, controlURL+"/state?auth_id="+url.QueryEscape(id), "")
		if status != 200 {
			t.Fatalf("fixture状态读取=%d", status)
		}
		var result map[string]any
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal("fixture状态格式无效")
		}
		return result
	}
	account := write("POST", "", map[string]any{"api_key": "synthetic-kilo-token", "name": "联合账号", "enabled": true, "concurrency_limit": nil}, 201)
	id := account.ID
	if account.EffectiveLimit != 20 {
		t.Fatal("新账号默认并发不是20")
	}
	defer func() {
		if id != "" {
			kiloAccountsWrite(handler, "DELETE", "/api/kilo/accounts/"+url.PathEscape(id), kiloRouterAdminKey, map[string]any{"connection_ref": ref})
		}
	}()
	created := state(id)
	if created["auth_exists"] != true || created["file_exists"] != true || created["model_count"].(float64) <= 0 {
		t.Fatal("创建未进入真实持久化/注册模型")
	}
	account = write("PATCH", id, map[string]any{"name": "联合修改", "priority": 3, "concurrency_limit": 2, "allow_direct_when_proxy_full": false, "api_key": "synthetic-replaced-token"}, 200)
	if account.Name != "联合修改" || account.Priority != 3 || account.EffectiveLimit != 2 {
		t.Fatal("普通字段或换Key后回执错误")
	}
	write("PATCH", id, map[string]any{"enabled": false}, 200)
	write("PATCH", id, map[string]any{"enabled": true, "concurrency_limit": nil}, 200)
	rec := kiloRouterRequest(handler, http.MethodGet, "/api/kilo/accounts", kiloRouterAdminKey)
	var listed model.KiloAccounts
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &listed) != nil || len(listed.Accounts) != 1 || listed.Accounts[0].ID != id || listed.ConnectionRef != ref {
		t.Fatal("真实列表与写入不一致")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", cpaURL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"synthetic","stream":false}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	type outcome struct {
		status int
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := client.Do(req)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		_, err = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		done <- outcome{status: res.StatusCode, err: err}
	}()
	if status, _ := kiloJointGET(t, client, controlURL+"/started", ""); status != 200 {
		t.Fatal("实际推理未进入生产执行器")
	}
	if state(id)["active"].(float64) != 1 {
		t.Fatal("在途账号未占用槽")
	}
	write("PATCH", id, map[string]any{"api_key": "synthetic-kilo-token"}, 409)
	write("DELETE", id, map[string]any{}, 409)
	release, _ := http.NewRequest("POST", controlURL+"/release", nil)
	response, err := client.Do(release)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("释放合成上游失败")
	}
	select {
	case result := <-done:
		if result.err != nil || result.status != 200 {
			t.Fatalf("真实生产执行器请求未成功: %d", result.status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("推理未完成")
	}
	deadline := time.Now().Add(3 * time.Second)
	for state(id)["active"].(float64) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("账号槽未释放")
		}
		time.Sleep(time.Millisecond)
	}
	write("DELETE", id, map[string]any{}, 204)
	removed := state(id)
	if removed["auth_exists"] != false || removed["file_exists"] != false || removed["model_count"].(float64) != 0 || removed["active"].(float64) != 0 {
		t.Fatal("删除后持久化/注册/模型/槽位残留")
	}
	write("PATCH", id, map[string]any{"name": "旧账号"}, 404)
	id = ""
	posts := removed["posts"]
	oldReq, _ := http.NewRequest("POST", cpaURL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"synthetic"}`))
	oldReq.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	oldReq.Header.Set("Content-Type", "application/json")
	oldRes, err := client.Do(oldReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, oldRes.Body)
	_ = oldRes.Body.Close()
	if oldRes.StatusCode < 400 || state("")["posts"] != posts {
		t.Fatal("已删除账号仍进入执行器")
	}
	final := kiloRouterRequest(handler, "GET", "/api/kilo/accounts", kiloRouterAdminKey)
	if json.Unmarshal(final.Body.Bytes(), &listed) != nil || len(listed.Accounts) != 0 {
		t.Fatal("删除后列表未归空")
	}
	t.Log("真实Router账号链：201创建/持久化/注册，200普通修改/换Key/启停，生产执行器使用，在途换Key与删除409，204删除后文件/模型/槽位归零且旧账号失效")
}
