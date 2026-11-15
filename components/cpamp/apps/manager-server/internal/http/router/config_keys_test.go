package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
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

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
	sqliterepo "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/sqlite"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/security"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/adminauth"
	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/managerconfig"
	proxysvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/proxy"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func newConfigKeysRouter(t *testing.T, address, managementKey string) http.Handler {
	t.Helper()
	// 复用既有真实AdminAuth的内存测试fixture，不新增业务表或依赖。
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err = sqliterepo.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	credential, err := security.NewAdminCredential(kiloRouterAdminKey, "config-keys-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SaveAdminCredential(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{CPAUpstreamURL: address, ManagementKey: managementKey, CORSOrigins: []string{"http://fixture.invalid"}}
	manager := managerconfig.New(cfg, st, nil)
	return New(&app.Context{Config: cfg, Store: st, AdminAuthService: adminauth.New(cfg, st), ManagerConfigService: manager, ProxyService: proxysvc.New(manager), CredentialConcurrencyService: concurrency.New(manager, nil)})
}

func TestConfigKeysRouterAdminAndV8Alias(t *testing.T) {
	calls, puts := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer synthetic-management" {
			t.Error("错误管理凭据")
		}
		if r.URL.Path == "/v8/management/credentials/concurrency" {
			_, _ = io.WriteString(w, `{"mode":"local","default_limit":20,"pool_waiting":0,"credentials":[]}`)
			return
		}
		if r.URL.Path != "/v8/management/config.yaml" {
			t.Errorf("配置目标=%s", r.URL.Path)
		}
		if r.Method == http.MethodPut {
			puts++
			if r.Header.Get("If-Match") != `"v1"` {
				w.WriteHeader(412)
				return
			}
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, "config-version: 8\naccess: {api-keys: [synthetic-client]}\n")
	}))
	defer upstream.Close()
	h := newConfigKeysRouter(t, upstream.URL, "synthetic-management")
	request := func(method, key, ref, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/v0/management/config.yaml", strings.NewReader("config-version: 8\n"))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set(proxysvc.ConfigConnectionHeader, ref)
		r.Header.Set("If-Match", etag)
		r.Header.Set("Origin", "http://fixture.invalid")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, key := range []string{"synthetic-client", "synthetic-management", ""} {
		if w := request("GET", key, "required", ""); w.Code != 401 {
			t.Fatalf("推理或上游Key获得管理权限:%d", w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("未授权请求到达Core")
	}
	get := request("GET", kiloRouterAdminKey, "required", "")
	ref := get.Header().Get(proxysvc.ConfigConnectionHeader)
	if get.Code != 200 || len(ref) != 32 || !strings.Contains(get.Header().Get("Access-Control-Expose-Headers"), "ETag") {
		t.Fatalf("配置引用/CORS错误:%d", get.Code)
	}
	if w := request("PUT", kiloRouterAdminKey, ref, `"stale"`); w.Code != 412 {
		t.Fatalf("ETag漂移=%d", w.Code)
	}
	if w := request("PUT", kiloRouterAdminKey, ref, `"v1"`); w.Code != 200 || w.Header().Get(proxysvc.ConfigConnectionHeader) != ref {
		t.Fatalf("受保护配置保存=%d", w.Code)
	}
	if puts != 2 {
		t.Fatalf("实际PUT=%d，发生重放", puts)
	}
	legacy := request("GET", kiloRouterAdminKey, "", "")
	if legacy.Code != 200 || legacy.Header().Get(proxysvc.ConfigConnectionHeader) != "" {
		t.Fatal("旧无ref调用兼容语义改变")
	}
	preflight := request("OPTIONS", "", "", "")
	if preflight.Code != 204 || !strings.Contains(preflight.Header().Get("Access-Control-Allow-Headers"), "If-Match") || !strings.Contains(preflight.Header().Get("Access-Control-Allow-Headers"), proxysvc.ConfigConnectionHeader) {
		t.Fatal("配置事务预检头缺失")
	}
}

func TestKiloManagerChannelKeysWebJointRuntime(t *testing.T) {
	cpaURL, controlURL, key := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_JOINT_CONTROL_URL"), os.Getenv("KILO_JOINT_MANAGEMENT_KEY")
	node, web := os.Getenv("KILO_JOINT_NODE_BIN"), os.Getenv("KILO_JOINT_WEB_DIR")
	if cpaURL == "" || controlURL == "" || key == "" || node == "" || web == "" {
		t.Skip("仅由Core固定三通道Key父桥执行")
	}
	for _, raw := range []string{cpaURL, controlURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("联合入口必须为loopback")
		}
	}
	h := newConfigKeysRouter(t, cpaURL, key)
	server := httptest.NewServer(h)
	defer server.Close()
	report := filepath.Join(t.TempDir(), "web-result.json")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, filepath.Join(web, "..", "..", "node_modules", "vitest", "vitest.mjs"), "run", "src/features/config/ConfigPage.persistence.test.tsx", "-t", "真实组件经Manager和Core保存三通道Key", "--reporter=json", "--outputFile="+report)
	command.Dir = web
	command.Env = append(os.Environ(), "KILO_KEYS_MANAGER_URL="+server.URL, "KILO_KEYS_MANAGER_KEY="+kiloRouterAdminKey, "KILO_KEYS_CONTROL_URL="+controlURL, "NO_PROXY=127.0.0.1,localhost,::1", "no_proxy=127.0.0.1,localhost,::1")
	output, err := command.CombinedOutput()
	t.Logf("真实配置组件输出:\n%s", output)
	raw, reportErr := os.ReadFile(report)
	if err != nil {
		if reportErr == nil {
			t.Logf("真实配置组件报告:\n%s", raw)
		} else {
			t.Logf("真实配置组件报告读取失败:%v", reportErr)
		}
		t.Fatalf("真实配置组件失败:%v", err)
	}
	if reportErr != nil {
		t.Fatal(reportErr)
	}
	var result struct {
		Success bool `json:"success"`
		Passed  int  `json:"numPassedTests"`
		Failed  int  `json:"numFailedTests"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success || result.Passed != 1 || result.Failed != 0 {
		t.Fatalf("真实组件未形成单项PASS:%s", raw)
	}
	t.Log("真实ConfigPage/Key编辑器/API→Manager Router→Core v8配置：6PUT；三通道生效及计数由Core父桥/verify核实")
}
