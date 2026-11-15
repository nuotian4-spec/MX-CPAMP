package management

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientKeyScopeManagementMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, method, path, body, etag string
		want                           int
	}{
		{"legacy unchanged", "PATCH", "/config", `{"server":{"port":8318}}`, "", 200},
		{"legacy same list", "PUT", "/config/access/api-keys", `["old","scoped"]`, "", 200},
		{"new missing scope", "PUT", "/config/access/api-keys", `["old","scoped","new"]`, "", 422},
		{"rotate legacy missing scope", "PUT", "/config/access/api-keys", `["new","scoped"]`, "", 422},
		{"remove scope only", "DELETE", "/config/access/client-key-scopes", "", "", 422},
		{"raw YAML bypass", "PUT", "/config.yaml", `config-version: 8
access: {api-keys: [old, added]}
`, "", 422},
		{"delete key and scope", "PATCH", "/config", `{"access":{"api-keys":["old"],"client-key-scopes":[]}}`, "", 200},
		{"last key empty", "PATCH", "/config", `{"access":{"api-keys":[],"client-key-scopes":[]}}`, "", 200},
		{"stale CAS", "PATCH", "/config", `{"server":{"port":8318}}`, "stale", 412},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte("config-version: 8\nserver: {port: 8317}\naccess: {api-keys: [old, scoped], client-key-scopes: [{api-key: scoped, channel: bps}]}\n")
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			h := &Handler{cfg: cfg, configFilePath: path}
			router := gin.New()
			for _, method := range []string{"PUT", "PATCH", "DELETE"} {
				router.Handle(method, "/config", h.ConfigV8)
				router.Handle(method, "/config.yaml", h.ConfigV8)
				router.Handle(method, "/config/*path", h.ConfigV8)
			}
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.etag != "" {
				req.Header.Set("If-Match", tc.etag)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want != 200 {
				got, _ := os.ReadFile(path)
				if string(got) != string(raw) {
					t.Fatal("rejected write changed disk")
				}
				if h.cfg != cfg {
					t.Fatal("rejected write published config")
				}
			}
		})
	}
	for _, channel := range []string{"codex", "bps", "kilo"} {
		t.Run("atomic publish "+channel, func(t *testing.T) {
			raw := []byte("config-version: 8\naccess: {api-keys: [old]}\n")
			path := filepath.Join(t.TempDir(), "config.yaml")
			_ = os.WriteFile(path, raw, 0600)
			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			h := &Handler{cfg: cfg, configFilePath: path}
			published := make(chan *config.Config, 1)
			h.SetConfigReloadHook(func(_ context.Context, c *config.Config) { published <- c })
			router := gin.New()
			router.PATCH("/config", h.ConfigV8)
			req := httptest.NewRequest("PATCH", "/config", strings.NewReader(`{"access":{"api-keys":["old","new"],"client-key-scopes":[{"api-key":"new","channel":"`+channel+`"}]}}`))
			req.Header.Set("If-Match", configV8ETag(config.ComputeSourceHash(raw)))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			saved, err := config.LoadConfig(path)
			if err != nil || len(saved.ClientKeyScopes) != 1 || saved.ClientKeyScopes[0].Channel != channel {
				t.Fatal(saved, err)
			}
			select {
			case snapshot := <-published:
				if len(snapshot.ClientKeyScopes) != 1 || snapshot.ClientKeyScopes[0].Channel != channel {
					t.Fatal("runtime snapshot lost scope")
				}
			case <-time.After(time.Second):
				t.Fatal("runtime publication missing")
			}
		})
	}
}
