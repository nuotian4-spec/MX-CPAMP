package router

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/collector"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
	sqliterepo "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/sqlite"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/security"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func TestBPSManagerBrowserPreview(t *testing.T) {
	receipt, panel := os.Getenv("BPS_PREVIEW_RECEIPT"), os.Getenv("BPS_PREVIEW_PANEL")
	cpaURL, missingURL := os.Getenv("BPS_JOINT_CPA_URL"), os.Getenv("BPS_JOINT_MISSING_URL")
	control, managementKey := os.Getenv("BPS_PREVIEW_CONTROL_URL"), os.Getenv("BPS_JOINT_MANAGEMENT_KEY")
	if receipt == "" || panel == "" || cpaURL == "" || missingURL == "" || control == "" || managementKey == "" {
		t.Skip("仅由 BPS 正式 CPA fixture 显式启动")
	}
	data, err := os.ReadFile(panel)
	if err != nil || len(data) < 100000 || !bytes.Contains(data, []byte("<html")) {
		t.Fatal("最终 Web 单文件产物缺失；禁止源码 stub")
	}
	transport := bpsLoopbackTransport(t, cpaURL, missingURL, control)
	client := &http.Client{Transport: transport}
	t.Setenv("CPAMP_UPDATE_CHECK_ENABLED", "false")
	addresses := make([]string, 0, 2)
	for _, upstream := range []string{cpaURL, missingURL} {
		dataDir := t.TempDir()
		db, err := sql.Open("sqlite", filepath.Join(dataDir, "preview.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		db.SetMaxOpenConns(1)
		if err := sqliterepo.Migrate(db); err != nil {
			t.Fatal(err)
		}
		st := store.New(db)
		credential, err := security.NewAdminCredential(kiloRouterAdminKey, "bps-final-panel-preview")
		if err != nil || st.SaveAdminCredential(t.Context(), credential) != nil {
			t.Fatal("合成管理员初始化失败")
		}
		if err := st.SaveSetup(t.Context(), store.Setup{CPAUpstreamURL: upstream, ManagementKey: managementKey}); err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{PanelPath: panel, DataDir: dataDir}
		appCtx := app.FromExisting(cfg, st, collector.NewManager(cfg, st), time.Now().UnixMilli(), nil, nil, nil, "cpa-manager-plus")
		server := httptest.NewServer(New(appCtx))
		t.Cleanup(server.Close)
		addresses = append(addresses, server.URL)
		response, err := client.Get(server.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		served, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(served, data) {
			t.Fatal("Manager 实际提供内容与最终 Web 产物不一致")
		}
		probe, err := http.NewRequest(http.MethodGet, server.URL+"/v0/management/plugins", nil)
		if err != nil {
			t.Fatal(err)
		}
		probe.Header.Set("Authorization", "Bearer "+kiloRouterAdminKey)
		capability, err := client.Do(probe)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, capability.Body)
		_ = capability.Body.Close()
		if capability.StatusCode != http.StatusOK || capability.Header.Get("X-CPA-SUPPORT-PLUGIN") != "1" {
			t.Fatal("预览平台能力头未到达真实 Manager 响应")
		}
	}
	output, err := json.MarshalIndent(map[string]any{
		"fixture": "bps-formal-router-final-panel", "manager_urls": addresses,
		"registered_manager_url": addresses[0], "unregistered_manager_url": addresses[1],
		"admin_key": kiloRouterAdminKey, "synthetic_license_key": "synthetic-bps-license-once",
		"control_url": control, "panel_path": panel, "panel_bytes": len(data),
		"plugin_support_header": "1", "core_version": "v0.0.0-bps-local-preview",
		"platform_fixture": "Windows CGO=0；仅预览 HTTP 包装层将已有 X-CPA-SUPPORT-PLUGIN 由0覆盖为1，以模拟目标Linux能力；不证明真实加载或ABI兼容",
		"network_boundary": "CPA与Manager出站HTTP仅允许loopback",
		"scenarios":        []string{"inactive", "active", "not_required", "unknown", "expired", "http404", "http500"},
		"runtime":          "真实最终 Web、真实 SQLite Manager、正式 CPA Router；仅合成插件，无供应商二进制及真实授权请求",
	}, "", "  ")
	if err != nil || os.WriteFile(receipt, output, 0600) != nil {
		t.Fatal("本地预览回执写入失败")
	}
	t.Logf("真实产物预览已就绪：%s", output)
	if os.Getenv("BPS_PREVIEW_SMOKE") == "1" {
		response, err := client.Post(control+"/stop", "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatal("预览冒烟收口失败")
		}
		return
	}
	response, err := client.Get(control + "/wait-stop")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("预览停止回执异常")
	}
}
