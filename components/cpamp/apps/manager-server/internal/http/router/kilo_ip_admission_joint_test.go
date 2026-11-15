package router

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// TestKiloManagerIPAdmissionJointRuntime 仅由 Core 父测试提供真实 Router 与本地观测。
func TestKiloManagerIPAdmissionJointRuntime(t *testing.T) {
	cpaURL, controlURL := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL")
	if cpaURL == "" || controlURL == "" {
		t.Skip("仅由 Core 公网 IP 准入联合 fixture 执行")
	}
	loopback := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && u.Scheme == "http" && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
	}
	if !loopback(cpaURL) || !loopback(controlURL) {
		t.Fatal("联合入口必须为 loopback")
	}
	managementKey := os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	if managementKey == "" {
		t.Fatal("缺少合成管理认证")
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("联合测试拒绝非 loopback 连接")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	var fixture struct {
		ProxyURL        string `json:"proxy_url"`
		PendingProxyURL string `json:"pending_proxy_url"`
	}
	status, raw := kiloJointGET(t, client, controlURL+"/fixture", "")
	if status != http.StatusOK || json.Unmarshal(raw, &fixture) != nil || !loopback(fixture.ProxyURL) || !loopback(fixture.PendingProxyURL) {
		t.Fatal("公网 IP 准入 fixture 无效")
	}
	if expected := os.Getenv("KILO_JOINT_PROXY_URL"); expected == "" || expected != fixture.ProxyURL {
		t.Fatal("父桥代理 URL 与 fixture 不一致")
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: managementKey}}, client)
	initial := kiloProxySnapshot(t, h)
	if len(initial.Proxies) != 0 || len(initial.Recycle) != 0 || len(initial.Accounts) != 0 {
		t.Fatal("准入联合初始状态必须没有代理或账号")
	}
	ref := initial.ConnectionRef
	read := func() model.KiloProxyState {
		t.Helper()
		state := kiloProxySnapshot(t, h)
		if state.ConnectionRef != ref || state.Revision != state.RuntimeRevision {
			t.Fatal("真实运行版本或连接引用不一致")
		}
		return state
	}
	assertSanitized := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		assertKiloRouterSanitized(t, rec)
		if strings.Contains(rec.Body.String(), fixture.ProxyURL) || strings.Contains(rec.Body.String(), fixture.PendingProxyURL) || strings.Contains(rec.Body.String(), managementKey) {
			t.Fatal("联合回执暴露代理原文或管理认证")
		}
	}
	preview := func(proxyURL string) model.KiloProxyPreview {
		t.Helper()
		rec := kiloAccountsWrite(h, "POST", "/api/kilo/proxies/preview", kiloRouterAdminKey, map[string]any{"url": proxyURL, "connection_ref": ref})
		assertSanitized(rec)
		var result model.KiloProxyPreview
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil {
			t.Fatalf("真实预览失败: %d %s", rec.Code, rec.Body)
		}
		return result
	}
	create := func(proxyURL string, revision uint64) *httptest.ResponseRecorder {
		return kiloAccountsWrite(h, "POST", "/api/kilo/proxies", kiloRouterAdminKey, map[string]any{"url": proxyURL, "enabled": true, "expected_revision": revision, "connection_ref": ref})
	}
	created := func(rec *httptest.ResponseRecorder) model.KiloProxyState {
		t.Helper()
		assertSanitized(rec)
		var state model.KiloProxyState
		if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &state) != nil || state.ConnectionRef != ref || state.Revision != state.RuntimeRevision {
			t.Fatalf("真实创建失败: %d %s", rec.Code, rec.Body)
		}
		return state
	}
	assertDuplicate := func(rec *httptest.ResponseRecorder, ip, id string) {
		t.Helper()
		assertSanitized(rec)
		var detail map[string]any
		if rec.Code != 409 || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || detail["error"] != "proxy_ip_already_registered" || detail["ip"] != ip || detail["proxy_id"] != id {
			t.Fatalf("确定重复未保留业务分类: %d %s", rec.Code, rec.Body)
		}
	}
	denied := kiloAccountsWrite(h, "POST", "/api/kilo/proxies/preview", "synthetic-kilo-key", map[string]any{"url": fixture.ProxyURL, "connection_ref": ref})
	if denied.Code != 401 {
		t.Fatal("推理 Key 进入管理预览")
	}
	ready := preview(fixture.ProxyURL)
	if ready.Status != "ready" || ready.Reason != "" || ready.ObservedExitIP == nil || *ready.ObservedExitIP != "203.0.113.10" || ready.ObservedAt == nil || *ready.ObservedAt == "" {
		t.Fatal("预览没有实际观测到首个公网 IP")
	}
	if current := read(); !reflect.DeepEqual(initial, current) {
		t.Fatal("只读预览登记了代理或改变了版本")
	}
	state := created(create(fixture.ProxyURL, initial.Revision))
	if state.Revision != initial.Revision+1 || len(state.Proxies) != 1 {
		t.Fatal("正式创建未仅增加一个代理和一个版本")
	}
	original := state.Proxies[0]
	if original.AdmissionStatus != "admitted" || original.AdmissionReason != "" || original.AdmittedIP != "203.0.113.10" || original.AdmittedAt == nil || *original.AdmittedAt == "" {
		t.Fatal("创建接纳四字段不完整")
	}
	duplicatePreview := preview(fixture.ProxyURL)
	if duplicatePreview.Status != "duplicate" || duplicatePreview.Reason != "duplicate_ip" || duplicatePreview.RegisteredProxyID != original.ID {
		t.Fatal("重复预览没有定位原记录")
	}
	assertDuplicate(create(fixture.ProxyURL, state.Revision), "203.0.113.10", original.ID)
	if current := read(); current.Revision != state.Revision || len(current.Proxies) != len(state.Proxies) {
		t.Fatal("重复创建修改了版本或条目数")
	}
	stale := create(fixture.ProxyURL, initial.Revision)
	assertSanitized(stale)
	if stale.Code != 409 || strings.Contains(stale.Body.String(), "proxy_ip_already_registered") {
		t.Fatal("旧版本冲突被误识别为确定重复")
	}
	pendingPreview := preview(fixture.PendingProxyURL)
	if pendingPreview.Status != "pending" || pendingPreview.Reason != "probe_failed" {
		t.Fatal("失败观测没有返回待核预览")
	}
	state = created(create(fixture.PendingProxyURL, state.Revision))
	pending := model.KiloProxy{}
	for _, proxy := range state.Proxies {
		if proxy.ID != original.ID {
			pending = proxy
		}
	}
	if len(state.Proxies) != 2 || pending.AdmissionStatus != "pending" || pending.AdmissionReason != "probe_failed" || pending.AdmittedIP != "" || pending.AdmittedAt != nil {
		t.Fatal("失败观测保存被误报成已接纳")
	}
	req, err := http.NewRequest("POST", controlURL+"/probe-ip", strings.NewReader(`{"ip":"203.0.113.11"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t.Fatal("合成观测切换失败")
	}
	results := make(chan *httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- create(fixture.ProxyURL, state.Revision)
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	accepted, conflicted := 0, 0
	for rec := range results {
		assertSanitized(rec)
		switch rec.Code {
		case 201:
			accepted++
		case 409:
			conflicted++
		default:
			t.Fatalf("并发创建状态错误: %d %s", rec.Code, rec.Body)
		}
	}
	current := read()
	if accepted != 1 || conflicted != 1 || len(current.Proxies) != len(state.Proxies)+1 || current.Revision != state.Revision+1 {
		t.Fatalf("并发同 IP 创建不原子: accepted=%d conflicted=%d count=%d revision=%d", accepted, conflicted, len(current.Proxies), current.Revision)
	}
	admittedID, admittedCount := "", 0
	for _, proxy := range current.Proxies {
		if proxy.AdmissionStatus == "admitted" && proxy.AdmittedIP == "203.0.113.11" {
			admittedID, admittedCount = proxy.ID, admittedCount+1
		}
	}
	if admittedCount != 1 {
		t.Fatal("相同公网 IP 出现多个登记记录")
	}
	assertDuplicate(create(fixture.ProxyURL, current.Revision), "203.0.113.11", admittedID)
	if final := read(); final.Revision != current.Revision || len(final.Proxies) != len(current.Proxies) {
		t.Fatal("并发后确定重复改变了状态")
	}
	t.Logf("真实双 Router 准入链通过：预览无副作用、推理 Key 隔离、admitted/pending 四字段、重复定位、旧版本隔离、并发仅一条；最终 revision=%d，代理=%d，无账号或模型调用", current.Revision, len(current.Proxies))
}
