package management

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBPSJointManagementFixtures(t *testing.T) {
	dir := os.Getenv("BPS_JOINT_DIR")
	if dir == "" {
		t.Skip("需要RPC上一步共享输出")
	}
	gin.SetMode(gin.TestMode)
	for _, name := range []string{"missing", "null", "zero"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name+".rpc-output.json"))
			if err != nil {
				t.Fatal(err)
			}
			var decoded pluginapi.QuotaFetchResponse
			if err = json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			manager := coreauth.NewManager(nil, nil, nil)
			a := &coreauth.Auth{ID: "synthetic-auth", Index: "synthetic-index", Provider: "cpa-bps"}
			if _, err = manager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
				t.Fatal(err)
			}
			fetches := 0
			host := pluginhost.New()
			host.RegisterPluginForTest("cpa-bps", pluginapi.Plugin{Capabilities: pluginapi.Capabilities{QuotaProvider: &mockQuotaProvider{identifier: "cpa-bps", fetchFn: func(_ context.Context, q pluginapi.QuotaFetchRequest) (pluginapi.QuotaFetchResponse, error) {
				fetches++
				if q.AuthID != a.ID || q.AuthIndex != a.Index {
					t.Error("管理GET身份不匹配")
				}
				return decoded, nil
			}, resetFn: func(context.Context, pluginapi.QuotaResetRequest) (pluginapi.QuotaResetResponse, error) {
				t.Fatal("消费接口被触发")
				return pluginapi.QuotaResetResponse{}, nil
			}}}})
			h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
			h.SetPluginHost(host)
			router := gin.New()
			router.GET("/v8/management/plugins/:id/quota", h.GetPluginQuota)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", "/v8/management/plugins/cpa-bps/quota?auth_index=synthetic-index&auth_id=synthetic-auth", nil))
			if rec.Code != 200 || fetches != 1 {
				t.Fatal(rec.Code, rec.Body.String(), fetches)
			}
			if err = os.WriteFile(filepath.Join(dir, name+".management-output.json"), rec.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			t.Log("消费同名RPC输出文件，真实GetPluginQuota GET序列化=1；consume_POST=0")
		})
	}
}
