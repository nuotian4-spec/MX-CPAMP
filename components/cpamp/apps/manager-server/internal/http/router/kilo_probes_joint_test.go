package router

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func TestKiloManagerProbesWebJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	node, web := os.Getenv("KILO_JOINT_NODE_BIN"), os.Getenv("KILO_JOINT_WEB_DIR")
	if cpaURL == "" || controlURL == "" || key == "" || node == "" || web == "" {
		t.Skip("仅由Core固定自动生成配置父桥执行")
	}
	for _, raw := range []string{cpaURL, controlURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("联合入口必须为loopback")
		}
	}
	h := newKiloFixtureRouter(t, &kiloRouterResolver{setup: store.Setup{CPAUpstreamURL: cpaURL, ManagementKey: key}}, nil)
	server := httptest.NewServer(h)
	defer server.Close()
	report := filepath.Join(t.TempDir(), "probes-web-result.json")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, filepath.Join(web, "..", "..", "node_modules", "vitest", "vitest.mjs"), "run", "src/features/kilo/KiloProbesPanel.test.tsx", "-t", "真实账号页经Manager与Core保存双类自动配置并读取两轮结果", "--reporter=json", "--outputFile="+report)
	command.Dir = web
	command.Env = append(os.Environ(), "KILO_PROBES_MANAGER_URL="+server.URL, "KILO_PROBES_MANAGER_KEY="+kiloRouterAdminKey, "KILO_PROBES_CONTROL_URL="+controlURL, "NO_PROXY=127.0.0.1,localhost,::1", "no_proxy=127.0.0.1,localhost,::1")
	output, err := command.CombinedOutput()
	t.Logf("真实自动生成配置组件输出:\n%s", output)
	raw, readErr := os.ReadFile(report)
	if err != nil {
		t.Logf("真实组件报告:%s，读取错误:%v", raw, readErr)
		t.Fatalf("自动生成配置联合失败:%v", err)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	var result struct {
		Success bool `json:"success"`
		Passed  int  `json:"numPassedTests"`
		Failed  int  `json:"numFailedTests"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success || result.Passed != 1 || result.Failed != 0 {
		t.Fatalf("联合未形成单项PASS:%s", raw)
	}
	t.Log("真实账号页→Manager Router/AdminAuth/连接引用→Core：两类配置及两个周期的实际生成结果；调用与槽位由父桥核实")
}
