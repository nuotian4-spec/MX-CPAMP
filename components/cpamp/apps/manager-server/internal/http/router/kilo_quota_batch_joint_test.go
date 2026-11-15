package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// TestKiloManagerQuotaBatchWebJointRuntime只启动合成Router和真实Web组件测试。
func TestKiloManagerQuotaBatchWebJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	node, web := os.Getenv("KILO_JOINT_NODE_BIN"), os.Getenv("KILO_JOINT_WEB_DIR")
	if cpaURL == "" || controlURL == "" || key == "" || node == "" || web == "" {
		t.Skip("仅由Core固定批量父桥执行")
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
	handler := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}, client)
	server := httptest.NewServer(handler)
	defer server.Close()
	report := filepath.Join(t.TempDir(), "web-result.json")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, filepath.Join(web, "..", "..", "node_modules", "vitest", "vitest.mjs"), "run", "src/features/kilo/KiloQuotaBatch.joint.test.tsx", "--reporter=json", "--outputFile="+report)
	command.Dir = web
	command.Env = append(os.Environ(), "KILO_BATCH_MANAGER_URL="+server.URL, "KILO_BATCH_MANAGER_KEY="+kiloRouterAdminKey, "KILO_BATCH_CONTROL_URL="+controlURL, "VERSION=kilo-m5q-joint", "NO_PROXY=127.0.0.1,localhost,::1", "no_proxy=127.0.0.1,localhost,::1")
	output, err := command.CombinedOutput()
	t.Logf("真实Web组件运行输出:\n%s", output)
	if err != nil {
		t.Fatalf("真实Web批量失败:%v", err)
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Success bool `json:"success"`
		Passed  int  `json:"numPassedTests"`
		Pending int  `json:"numPendingTests"`
		Results []struct {
			Assertions []struct {
				Title  string `json:"title"`
				Status string `json:"status"`
			} `json:"assertionResults"`
		} `json:"testResults"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success || result.Passed != 1 || result.Pending != 0 {
		t.Fatalf("真实Web测试未形成1PASS/0Skip:%s", raw)
	}
	found := false
	for _, file := range result.Results {
		for _, assertion := range file.Assertions {
			if strings.Contains(assertion.Title, "真实页面批量经Manager和Core完成四项额度查询") && assertion.Status == "passed" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("真实Web批量用例没有实际PASS")
	}
	t.Log("真实Web组件/API→Manager Router→Core Router：4个Kilo独立查询，Web1 PASS/0Skip；usage4/max3/model0由Core父桥核实")
}
