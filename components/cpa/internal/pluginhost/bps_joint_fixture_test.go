package pluginhost

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBPSJointRPCFixtures(t *testing.T) {
	dir := os.Getenv("BPS_JOINT_DIR")
	if dir == "" {
		t.Skip("需要共享联合输出目录")
	}
	for _, tc := range []struct{ name, summary string }{{"missing", `[{"key":"reset_cards_available"}]`}, {"null", `[{"key":"reset_cards_available","value":null}]`}, {"zero", `[{"key":"reset_cards_available","value":0}]`}} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"ok":true,"result":{"subscription":{"plan":"pro","tierName":"Exact registered plan"},"summary":` + tc.summary + `}}`
			client := &quotaPresenceClient{raw: raw}
			provider := rpcQuotaProvider{rpcPluginAdapter: &rpcPluginAdapter{id: "cpa-bps", client: client}, identifier: "cpa-bps"}
			host := newHostWithRecords(capabilityRecord{id: "cpa-bps", plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{QuotaProvider: provider}}})
			provider.rpcPluginAdapter.host = host
			client.host = host
			response, handled, err := host.FetchQuotaByPlugin(context.Background(), "cpa-bps", pluginapi.QuotaFetchRequest{AuthID: "synthetic-auth", AuthIndex: "synthetic-index", Provider: "cpa-bps"})
			if err != nil || !handled || client.calls != 1 {
				t.Fatal(err, handled, client.calls)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.JSON(200, response)
			if err = os.WriteFile(filepath.Join(dir, tc.name+".rpc-input.json"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, tc.name+".rpc-output.json"), rec.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("原始RPC及真实解码输出已保存；quota.fetch=1 consume_POST=0")
		})
	}
}
