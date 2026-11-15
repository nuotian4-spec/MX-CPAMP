package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestUIV1AlphaSearchAuthenticatedChannelMatrix(t *testing.T) {
	cases := []struct {
		name, key, model string
		mode             any
		status           int
		codex, live      int
	}{
		{"兼容Key直达Codex", "legacy-key", "alpha-codex-search", "cached", 200, 1, 0},
		{"Codex专用Key同通道", "codex-key", "alpha-codex-search", "cached", 200, 1, 0},
		{"BPS专用Key拒绝Codex", "bps-key", "alpha-codex-search", "cached", 403, 0, 0},
		{"Kilo专用Key拒绝Codex", "kilo-key", "alpha-codex-search", "cached", 403, 0, 0},
		{"兼容Key保留BPS_live", "legacy-key", "alpha-bps-model", true, 200, 0, 1},
		{"BPS专用Key拒绝外部live", "bps-key", "alpha-bps-model", true, 0, 0, 0},
		{"BPS专用Key拒绝嵌套cached", "bps-key", "alpha-bps-model", "cached", 0, 0, 0},
		{"BPS专用Key拒绝嵌套indexed", "bps-key", "alpha-bps-model", "indexed", 0, 0, 0},
		{"Codex专用Key拒绝BPS入口", "codex-key", "alpha-bps-model", true, 403, 0, 0},
		{"Kilo专用Key拒绝BPS入口", "kilo-key", "alpha-bps-model", true, 403, 0, 0},
	}
	for _, route := range []string{"/v1/alpha/search", "/backend-api/codex/alpha/search"} {
		for _, tc := range cases {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				server, cfg, bps, live := newAlphaFixture(t, "bps")
				codex := registerAlphaCodex(t, server, "alpha-codex-search")
				accessCfg := *cfg
				accessCfg.APIKeys = []string{"legacy-key", "codex-key", "bps-key", "kilo-key"}
				accessCfg.ClientKeyScopes = []sdkconfig.ClientKeyScope{{APIKey: "codex-key", Channel: "codex"}, {APIKey: "bps-key", Channel: "bps"}, {APIKey: "kilo-key", Channel: "kilo"}}
				if !server.applyAccessConfig(nil, &accessCfg) {
					t.Fatal("合成Key鉴权配置未生效")
				}
				body := alphaBody()
				_ = body.Set("model", tc.model)
				_ = body.Set("settings.external_web_access", tc.mode)
				request := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body.String())).WithContext(context.Background())
				request.Header.Set("Authorization", "Bearer "+tc.key)
				request.Header.Set("X-Channel", "codex")
				request.Header.Set("X-Provider", "codex")
				response := httptest.NewRecorder()
				server.engine.ServeHTTP(response, request)
				if (tc.status != 0 && response.Code != tc.status) || (tc.status == 0 && response.Code < 400) {
					t.Fatalf("通道结果异常: status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
				}
				if codex.httpCalls != tc.codex || live.calls != tc.live || bps.calls != 0 {
					t.Fatalf("通道调用越界: Codex=%d/%d live=%d/%d BPS推理=%d", codex.httpCalls, tc.codex, live.calls, tc.live, bps.calls)
				}
				t.Logf("真实Key鉴权 status=%d Codex=%d live=%d BPS推理=%d", response.Code, codex.httpCalls, live.calls, bps.calls)
			})
		}
	}
}
