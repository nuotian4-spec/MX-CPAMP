package management

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestUploadAuthFileV8RejectsRestrictedParsedIdentityBeforeWrite(t *testing.T) {
	for _, provider := range []string{"kilo", "bps"} {
		for _, existing := range []bool{false, true} {
			name := provider + "/new"
			if existing {
				name = provider + "/replace"
			}
			t.Run(name, func(t *testing.T) {
				h, manager := bpsTestHandler(t)
				enableBPSReplacement(h)
				path := filepath.Join(h.cfg.AuthDir, "vendor.json")
				const original = `{"type":"vendor","label":"original"}`
				if existing {
					if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
						t.Fatal(err)
					}
					if _, err := manager.Register(context.Background(), &coreauth.Auth{ID: "existing", Provider: "vendor", Label: "original", FileName: "vendor.json"}); err != nil {
						t.Fatal(err)
					}
				}
				host := pluginhost.New()
				host.RegisterPluginForTest("vendor-plugin", pluginapi.Plugin{Capabilities: pluginapi.Capabilities{
					AuthProvider: &v8UploadTestAuthProvider{
						testAuthProvider: testAuthProvider{identifier: "vendor", identifiers: []string{"vendor"}},
						parseFn: func(_ context.Context, req pluginapi.AuthParseRequest) (pluginapi.AuthParseResponse, error) {
							return pluginapi.AuthParseResponse{Handled: true, Auths: []pluginapi.AuthData{
								{ID: "allowed", Provider: "vendor", StorageJSON: req.RawJSON},
								{ID: "restricted", Provider: provider, StorageJSON: req.RawJSON},
							}}, nil
						},
					},
				}})
				h.SetPluginHost(host)
				hookCalled := false
				h.SetPostAuthPersistHook(func(context.Context, *coreauth.Auth) error { hookCalled = true; return nil })
				rec := performUploadRequest(t, newV8AndV0AuthRouter(h), "/v8/management/credentials", false,
					map[string]string{"vendor.json": `{"type":"vendor"}`}, []string{"vendor.json"})
				if rec.Code != http.StatusConflict {
					t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
				}
				data, err := os.ReadFile(path)
				if existing {
					if err != nil || string(data) != original {
						t.Fatalf("existing file changed: %q, %v", data, err)
					}
					if got := managerAuthIDs(manager); len(got) != 1 || got[0] != "existing" {
						t.Fatalf("existing identities changed: %v", got)
					}
				} else if !os.IsNotExist(err) || len(manager.List()) != 0 {
					t.Fatalf("rejected upload persisted state: file error = %v, IDs = %v", err, managerAuthIDs(manager))
				}
				if hookCalled {
					t.Fatal("rejected upload invoked persistence hook")
				}
			})
		}
	}
}
