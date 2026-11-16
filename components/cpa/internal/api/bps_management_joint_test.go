package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

const bpsJointManagementKey = "synthetic-bps-management-key"
const bpsJointLicenseKey = "synthetic-bps-license-once"

type bpsManagementFixture struct {
	mu        sync.Mutex
	reads     int
	activates int
	refreshes int
	active    bool
	required  bool
	code      string
	status    int
	reloads   int
	reloaded  chan *proxyconfig.Config
}

func (f *bpsManagementFixture) RegisterManagement(_ context.Context, _ pluginapi.ManagementRegistrationRequest) (pluginapi.ManagementRegistrationResponse, error) {
	return pluginapi.ManagementRegistrationResponse{Routes: []pluginapi.ManagementRoute{
		{Method: http.MethodGet, Path: "/v0/management/cpa-bps/license", Handler: f},
		{Method: http.MethodPost, Path: "/v0/management/cpa-bps/license/activate", Handler: f},
		{Method: http.MethodPost, Path: "/v0/management/cpa-bps/license/refresh", Handler: f},
	}}, nil
}

func (f *bpsManagementFixture) HandleManagement(_ context.Context, request pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(request.Query) != 0 || request.Headers.Get("Authorization") != "Bearer "+bpsJointManagementKey {
		return pluginapi.ManagementResponse{}, fmt.Errorf("插件请求携带查询参数或错误管理凭据")
	}
	switch request.Path {
	case "/v0/management/cpa-bps/license":
		if request.Method != http.MethodGet || len(request.Body) != 0 {
			return pluginapi.ManagementResponse{}, fmt.Errorf("读取许可的方法或正文错误")
		}
		f.reads++
	case "/v0/management/cpa-bps/license/activate":
		body := ojson.ParseBytes(request.Body)
		if !ojson.ValidBytes(request.Body) || !body.IsObject() || len(body.Map()) != 1 || body.Get("license_key").String() != bpsJointLicenseKey {
			return pluginapi.ManagementResponse{}, fmt.Errorf("合成激活正文不符合契约")
		}
		f.activates++
		f.active, f.code = true, "active"
	case "/v0/management/cpa-bps/license/refresh":
		if string(request.Body) != "{}" {
			return pluginapi.ManagementResponse{}, fmt.Errorf("刷新许可正文不等于空对象")
		}
		f.refreshes++
	default:
		return pluginapi.ManagementResponse{}, fmt.Errorf("未登记的合成许可路径")
	}
	code := f.code
	if code == "" {
		code = "not_activated"
	}
	body := ojson.NewDoc([]byte(`{}`))
	for name, value := range map[string]any{
		"required": f.required, "active": f.active, "code": code,
		"key_hint": "fixture-only", "bound_ip": "192.0.2.20",
		"license_expires_at": "2030-01-01T00:00:00Z", "expires_at": "2030-01-01T00:00:00Z", "last_error": "",
	} {
		if err := body.Set(name, value); err != nil {
			return pluginapi.ManagementResponse{}, err
		}
	}
	return pluginapi.ManagementResponse{StatusCode: f.status, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: body.Bytes()}, nil
}

func newBPSManagementRouter(t *testing.T, registered bool) (*Server, *httptest.Server, *bpsManagementFixture) {
	t.Helper()
	t.Setenv("MANAGEMENT_PASSWORD", "")
	updateRoot := t.TempDir()
	t.Setenv("MX_UPDATE_LOCK_PATH", filepath.Join(updateRoot, ".mx-update.lock"))
	t.Setenv("MX_UPDATE_STATE_DIR", filepath.Join(updateRoot, "state"))
	t.Setenv("MX_UPDATE_CAPABILITY_EVIDENCE", "")
	host := pluginhost.New()
	t.Cleanup(host.ShutdownAll)
	fixture := &bpsManagementFixture{required: true, reloaded: make(chan *proxyconfig.Config, 1)}
	if registered {
		host.RegisterPluginForTest("cpa-bps", pluginapi.Plugin{
			SchemaVersion: 6,
			Metadata: pluginapi.Metadata{Name: "BPS synthetic fixture", Version: "0.0.0-fixture", Author: "local-test", ConfigFields: []pluginapi.ConfigField{
				{Name: "session_proxy_pool_enabled", Type: pluginapi.ConfigFieldTypeBoolean, Default: false, Description: "合成布尔配置"},
				{Name: "session_proxy_pool", Type: pluginapi.ConfigFieldTypeString, Default: "", Description: "合成多行字符串配置"},
				{Name: "session_proxy_idle_minutes", Type: pluginapi.ConfigFieldTypeInteger, Default: 15, Description: "合成整数配置"},
			}},
			Capabilities: pluginapi.Capabilities{ManagementAPI: fixture},
		})
	}
	var initial proxyconfig.PluginInstanceConfig
	if err := yaml.Unmarshal([]byte("enabled: true\nsession_proxy_pool_enabled: false\nsession_proxy_pool: initial\nsession_proxy_idle_minutes: 15\nunknown_top:\n  retained: true\nobject_field:\n  old: old-value\n  keep: replaced-with-parent\nremove_me: present\n"), &initial); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(bpsJointManagementKey), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(t.TempDir(), "defaults.yaml")
	if err := os.WriteFile(defaultPath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := proxyconfig.LoadConfig(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RemoteManagement.SecretKey = string(hash)
	cfg.RemoteManagement.DisableControlPanel = true
	cfg.Plugins.Enabled = true
	cfg.Plugins.Dir = t.TempDir()
	cfg.Plugins.Configs = map[string]proxyconfig.PluginInstanceConfig{"cpa-bps": initial}
	cfg.APIKeys = []string{"synthetic-inference-key"}
	server := newTestServerWithConfig(t, cfg, WithPluginHost(host), WithConfigReloadHook(func(_ context.Context, next *proxyconfig.Config) {
		fixture.mu.Lock()
		fixture.reloads++
		fixture.mu.Unlock()
		select {
		case fixture.reloaded <- next:
		default:
		}
	}))
	raw, err := yaml.Marshal(server.getConfig())
	if err != nil || os.WriteFile(server.configFilePath, raw, 0600) != nil {
		t.Fatal("合成配置文件写入失败")
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return server, httpServer, fixture
}

func bpsRunManagerTest(t *testing.T, cpaURL, missingURL, testName string, extra ...string) {
	t.Helper()
	goBin, managerDir := os.Getenv("BPS_JOINT_GO_BIN"), os.Getenv("BPS_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Fatal("联合测试缺少明确的 Go 或 Manager 路径")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-count=1", "-v", "-timeout", "25m", "-run", "^"+testName+"$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(os.Environ(), "BPS_JOINT_CPA_URL="+cpaURL, "BPS_JOINT_MISSING_URL="+missingURL,
		"BPS_JOINT_MANAGEMENT_KEY="+bpsJointManagementKey, "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOFLAGS=-mod=readonly")
	command.Env = append(command.Env, extra...)
	output, err := command.CombinedOutput()
	t.Logf("正式 Manager 联合测试输出：\n%s", output)
	if err != nil {
		t.Fatalf("正式 Manager 联合测试失败：%v", err)
	}
}

func TestBPSManagementManagerJointBridge(t *testing.T) {
	if os.Getenv("BPS_JOINT_GO_BIN") == "" || os.Getenv("BPS_JOINT_MANAGER_DIR") == "" {
		t.Skip("仅显式启用本地双路由联合测试")
	}
	server, upstream, fixture := newBPSManagementRouter(t, true)
	_, missing, missingFixture := newBPSManagementRouter(t, false)
	bpsRunManagerTest(t, upstream.URL, missing.URL, "TestBPSManagerManagementJointRuntime")
	select {
	case next := <-fixture.reloaded:
		if _, ok := next.Plugins.Configs["cpa-bps"]; !ok {
			t.Fatal("正式配置重载回调缺少 BPS 配置")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("正式配置保存未触发重载回调")
	}
	fixture.mu.Lock()
	reads, activates, refreshes := fixture.reads, fixture.activates, fixture.refreshes
	fixture.mu.Unlock()
	if reads != 6 || activates != 1 || refreshes != 1 {
		t.Fatalf("真实插件派发次数不符：GET=%d 激活=%d 刷新=%d", reads, activates, refreshes)
	}
	if missingFixture.reads+missingFixture.activates+missingFixture.refreshes != 0 {
		t.Fatal("未注册插件产生了调用")
	}
	raw, err := os.ReadFile(server.configFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), bpsJointLicenseKey) {
		t.Fatal("激活正文出现在持久配置中")
	}
	var saved map[string]any
	if err := yaml.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	plugins := saved["plugins"].(map[string]any)
	configs := plugins["configs"].(map[string]any)
	actual := configs["cpa-bps"].(map[string]any)
	expected := map[string]any{
		"enabled":                    true,
		"session_proxy_pool_enabled": true,
		"session_proxy_pool":         "socks5://127.0.0.1:31001\nhttp://127.0.0.1:31002",
		"session_proxy_idle_minutes": 23,
		"unknown_top":                map[string]any{"retained": true},
		"object_field":               map[string]any{"fresh": "replacement"},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("正式 PATCH 持久化内容不符：%v", actual)
	}
	if _, err := proxyconfig.LoadConfig(filepath.Clean(server.configFilePath)); err != nil {
		t.Fatalf("保存后配置重新加载失败：%v", err)
	}
	t.Logf("正式 Manager→CPA→插件派发通过；激活=1 刷新=1；PATCH 多行字符串、未知顶层保全、对象替换、null 删除及磁盘重载通过")
}
