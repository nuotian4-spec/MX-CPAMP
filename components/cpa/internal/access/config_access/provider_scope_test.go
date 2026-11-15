package configaccess

import (
	"context"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"net/http/httptest"
	"testing"
)

func TestClientKeyScopeAuthenticationIgnoresChannelHeaders(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{APIKeys: []string{"synthetic", "legacy"}, ClientKeyScopes: []sdkconfig.ClientKeyScope{{APIKey: "synthetic", Channel: "kilo"}}}
	Register(cfg)
	defer Register(nil)
	providers := sdkaccess.RegisteredProviders()
	if len(providers) != 1 {
		t.Fatalf("providers=%d", len(providers))
	}
	for _, key := range cfg.APIKeys {
		req := httptest.NewRequest("POST", "/v1/responses", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-Channel", "codex")
		req.Header.Set("X-Provider", "bps")
		result, err := providers[0].Authenticate(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if key == "synthetic" {
			want = "kilo"
		}
		if result.Metadata["channel_scope"] != want {
			t.Fatalf("scope=%q", result.Metadata["channel_scope"])
		}
	}
	cfg.APIKeys = nil
	Register(cfg)
	providers = sdkaccess.RegisteredProviders()
	if len(providers) != 1 {
		t.Fatal("invalid scoped config removed authentication")
	}
	if _, err := providers[0].Authenticate(context.Background(), httptest.NewRequest("GET", "/v1/models", nil)); err == nil {
		t.Fatal("invalid scope accepted")
	}
}
