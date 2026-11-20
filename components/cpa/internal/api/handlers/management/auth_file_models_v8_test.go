package management

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestAuthFileModelsV8DiscoveryAndLegacyShape(t *testing.T) {
	m := coreauth.NewManager(nil, nil, nil)
	a, err := m.Register(t.Context(), &coreauth.Auth{ID: t.Name(), FileName: "new.json", Provider: "cpa-bps", Status: coreauth.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	m.RecordModelDiscovery(a, m.NextModelDiscoveryAttempt(), coreauth.ModelDiscovery{Status: "failed", PluginID: "cpa-bps", ErrorCode: "bps_model_discovery_rejected"})
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, m)
	for _, legacy := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/?name=new.json", nil)
		if legacy {
			h.GetAuthFileModels(c)
		} else {
			h.GetAuthFileModelsV8(c)
		}
		d := ojson.ParseBytes(w.Body.Bytes())
		if w.Code != 200 || !d.Get("models").IsArray() {
			t.Fatalf("response: %s", w.Body.String())
		}
		if legacy {
			if d.Get("discovery").Exists() {
				t.Fatal("legacy shape changed")
			}
		} else if d.Get("discovery.status").String() != "failed" || d.Get("discovery.upstream_status").Exists() {
			t.Fatalf("discovery: %s", w.Body.String())
		}
	}
}
