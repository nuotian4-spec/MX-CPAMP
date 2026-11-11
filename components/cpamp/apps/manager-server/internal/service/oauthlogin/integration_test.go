package oauthlogin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealWorkerPipeToCPA(t *testing.T) {
	node, component := os.Getenv("OAUTH_LOGIN_TEST_NODE"), os.Getenv("OAUTH_LOGIN_TEST_COMPONENT")
	if node == "" || component == "" {
		t.Skip("本专项由固定Node与组件路径显式运行")
	}
	secret := "integration-service-secret-0000000000000000"
	var stderr bytes.Buffer
	cmd := exec.Command(node, filepath.Join(component, "test", "integration-server.mjs"))
	cmd.Stdin = strings.NewReader(`{"secret":"` + secret + `","timeout_ms":1500}`)
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if strings.Contains(stderr.String(), "PASSWORD_CANARY") {
			t.Error("Worker输出泄露密码")
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("Worker启动未返回内网地址")
	}
	workerURL := scanner.Text()
	var imports atomic.Int32
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c credentials
		_ = json.NewDecoder(r.Body).Decode(&c)
		if c.AccessToken != "fake-access" || c.RefreshToken != "fake-refresh" || c.LoginMethod != "2fa" {
			t.Error("真实pipe授权未传入CPA")
		}
		imports.Add(1)
		jsonReply(w, map[string]any{"status": "ok", "provider": r.URL.Query().Get("provider"), "auth_id": "fake-account.json", "saved": true, "registered": true})
	}))
	defer cpa.Close()
	s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "fake-management-key"}}, nil, workerURL, secret)
	s.poll = 10 * time.Millisecond
	for _, test := range []struct{ profile, email, want string }{{"codex", "ok@example.test", "succeeded"}, {"bps", "ok@example.test", "succeeded"}, {"bps", "challenge@example.test", "needs_user_action"}, {"codex", "bad@example.test", "failed"}, {"bps", "hold@example.test", "cancelled"}} {
		task, err := s.Create(Input{Profile: test.profile, Email: test.email, Password: "PASSWORD_CANARY", TOTPSecret: "JBSWY3DPEHPK3PXP"})
		if err != nil {
			t.Fatal(err)
		}
		if test.want == "cancelled" {
			waitTask(t, s, task.ID, func(v Task) bool { return v.Status == "running" })
			_, _ = s.Cancel(task.ID)
		}
		got := waitTask(t, s, task.ID, func(v Task) bool { return terminal(v.Status) })
		if got.Status != test.want {
			t.Fatalf("%s: %+v", test.email, got)
		}
		raw, _ := json.Marshal(got)
		if strings.Contains(string(raw), "CANARY") || strings.Contains(string(raw), "fake-access") {
			t.Fatal("Manager结果泄露")
		}
		// Worker终态清理通过真实HTTP完成，再启动下一任务。
		time.Sleep(20 * time.Millisecond)
	}
	if imports.Load() != 2 {
		t.Fatalf("意外导入次数=%d", imports.Load())
	}
}
