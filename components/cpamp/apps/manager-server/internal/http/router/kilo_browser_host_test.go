package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/collector"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
	sqliterepo "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/sqlite"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/security"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

func TestKiloManagerBrowserHost(t *testing.T) {
	receipt, panel := os.Getenv("KILO_BROWSER_RECEIPT"), os.Getenv("KILO_BROWSER_PANEL")
	cpaURL, alias, control := os.Getenv("KILO_JOINT_CPA_URL"), os.Getenv("KILO_BROWSER_CPA_ALIAS"), os.Getenv("KILO_JOINT_CONTROL_URL")
	if receipt == "" || panel == "" || cpaURL == "" || alias == "" || control == "" {
		t.Skip("仅由Core浏览器测试宿主显式启动")
	}
	for _, address := range []string{cpaURL, alias, control} {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "http" || u.User != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			t.Fatal("宿主入口必须为loopback")
		}
	}
	if data, err := os.ReadFile(panel); err != nil || len(data) < 100000 {
		t.Fatal("最终Web产物缺失")
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("浏览器宿主拒绝非loopback目标")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previous; transport.CloseIdleConnections() }()
	t.Setenv("CPAMP_UPDATE_CHECK_ENABLED", "false")
	addresses := make([]string, 0, 2)
	for _, upstream := range []string{cpaURL, alias} {
		dataDir := t.TempDir()
		db, err := sql.Open("sqlite", filepath.Join(dataDir, "browser.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		if err := sqliterepo.Migrate(db); err != nil {
			t.Fatal(err)
		}
		st := store.New(db)
		credential, err := security.NewAdminCredential(kiloRouterAdminKey, "m10-browser")
		if err != nil || st.SaveAdminCredential(t.Context(), credential) != nil {
			t.Fatal("合成管理员初始化失败")
		}
		if err := st.SaveSetup(t.Context(), store.Setup{CPAUpstreamURL: upstream, ManagementKey: os.Getenv("KILO_JOINT_MANAGEMENT_KEY")}); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(os.Getenv("KILO_BROWSER_REVISION"), "kilo-ux2-local-") && upstream == cpaURL {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, upstream+"/v0/management/auth-files?name=source-ux2.json", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+os.Getenv("KILO_JOINT_MANAGEMENT_KEY"))
			response, err := (&http.Client{Transport: transport, Timeout: 10 * time.Second}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var sources struct {
				Files []struct {
					ID        string `json:"id"`
					AuthIndex string `json:"auth_index"`
				} `json:"files"`
			}
			err = json.NewDecoder(response.Body).Decode(&sources)
			_ = response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK || len(sources.Files) != 1 || sources.Files[0].ID != "source-ux2.json" || sources.Files[0].AuthIndex == "" {
				t.Fatal("UX2用量来源身份读取失败")
			}
			if err := st.SaveModelPrices(t.Context(), map[string]store.ModelPrice{
				"gpt-5.6-sol": {Prompt: 2, PromptConfigured: true, Completion: 8, CompletionConfigured: true},
			}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			events := make([]usage.Event, 0, 3)
			for index, age := range []time.Duration{90 * time.Minute, 30 * time.Minute, 24 * time.Hour} {
				at := now.Add(-age)
				events = append(events, usage.Event{
					EventHash: fmt.Sprintf("%064x", index+1), TimestampMS: at.UnixMilli(), Timestamp: at.Format(time.RFC3339Nano),
					Provider: "codex", ExecutorType: "codex", Model: "gpt-5.6-sol", ResolvedModel: "gpt-5.6-sol",
					AuthIndex: sources.Files[0].AuthIndex, Source: "source-ux2.json", AuthFileSnapshot: "source-ux2.json",
					AuthProviderSnapshot: "codex", AuthAccountIDSnapshot: "outer-ux2", AuthLabelSnapshot: "same-name",
					AuthSnapshotAtMS: at.UnixMilli(), InputTokens: 100000, OutputTokens: 25000, TotalTokens: 125000, CreatedAtMS: now.UnixMilli(),
				})
			}
			if result, err := st.InsertEvents(t.Context(), events); err != nil || result.Inserted != len(events) {
				t.Fatalf("UX2本地历史写入失败：result=%+v err=%v", result, err)
			}
			t.Logf("UX2真实SQLite历史已种入：来源source-ux2.json，索引%s，3条成功请求；5h成本0.80，7d成本1.20，最近请求早于额度观测", sources.Files[0].AuthIndex)
		}
		cfg := config.Config{PanelPath: panel, DataDir: dataDir}
		appCtx := app.FromExisting(cfg, st, collector.NewManager(cfg, st), time.Now().UnixMilli(), nil, nil, nil, "cpa-manager-plus")
		server := httptest.NewServer(New(appCtx))
		defer server.Close()
		addresses = append(addresses, server.URL)
	}
	revision := os.Getenv("KILO_BROWSER_REVISION")
	if revision == "" {
		revision = "kilo-ux1-local-03"
	}
	data, err := json.MarshalIndent(map[string]any{"revision": revision, "fixture": "kilo-ux1-browser-fixture-02", "manager_urls": addresses, "admin_key": kiloRouterAdminKey, "control_url": control, "cpa_urls": []string{cpaURL, alias}, "panel_path": panel, "runtime": "independent Core A/B; real DB-backed Manager Router; final Web; loopback synthetic upstream only"}, "", "  ")
	if err != nil || os.WriteFile(receipt, data, 0600) != nil {
		t.Fatal("宿主回执写入失败")
	}
	t.Logf("宿主已就绪：%s", data)
	response, err := (&http.Client{Transport: transport}).Get(control + "/wait-stop")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("宿主停止回执异常")
	}
}
