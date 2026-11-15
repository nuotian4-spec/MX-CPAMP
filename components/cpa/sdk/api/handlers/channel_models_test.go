package handlers

import (
	"context"
	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
)

func TestChannelScopeModelListAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []string{"kilo", "codex", "bps"} {
		id := t.Name() + provider
		registry.GetGlobalRegistry().RegisterClient(id, provider, []*registry.ModelInfo{{ID: "shared-scope-test"}, {ID: "only-" + provider}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	body := []byte("{\"data\":[{\"id\":\"shared-scope-test\"},{\"id\":\"only-kilo\"},{\"id\":\"only-codex\"},{\"id\":\"only-bps\"}]}")
	for _, scope := range []string{"", "kilo", "codex", "bps"} {
		t.Run(scope, func(t *testing.T) {
			ctx := channelscope.With(context.Background(), scope)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil).WithContext(ctx)
			h := &BaseAPIHandler{Cfg: &config.SDKConfig{}}
			h.WriteModelListResponse(c, "openai", body)
			got := []string{}
			for _, item := range ojson.GetBytes(rec.Body.Bytes(), "data").Array() {
				got = append(got, item.Get("id").String())
			}
			want := []string{"shared-scope-test", "only-" + scope}
			if scope == "" {
				want = []string{"shared-scope-test", "only-codex", "only-bps"}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("models=%v want %v", got, want)
			}
			inherited, cancel := h.GetContextWithCancel(nil, c, context.Background())
			defer cancel()
			if channelscope.From(inherited) != scope {
				t.Fatalf("scope lost: %q", channelscope.From(inherited))
			}
		})
	}
}
func TestChannelScopeBlocksPluginDirectPaths(t *testing.T) {
	h := &BaseAPIHandler{}
	for _, scope := range []string{"kilo", "codex", "bps"} {
		ctx := channelscope.With(context.Background(), scope)
		_, _, err := h.executeWithPluginExecutor(ctx, "openai", "openai", "model", "model", nil, "", "plugin", modelExecutionOptions{})
		if err == nil || err.StatusCode != 403 {
			t.Fatalf("execute=%v", err)
		}
		_, _, err = h.countWithPluginExecutor(ctx, "claude", "model", "model", nil, "", "plugin", modelExecutionOptions{})
		if err == nil || err.StatusCode != 403 {
			t.Fatalf("count=%v", err)
		}
		_, _, errs := h.streamWithPluginExecutor(ctx, "openai", "openai", "model", "model", nil, "", "plugin", modelExecutionOptions{})
		if err = <-errs; err == nil || err.StatusCode != 403 {
			t.Fatalf("stream=%v", err)
		}
	}
}
