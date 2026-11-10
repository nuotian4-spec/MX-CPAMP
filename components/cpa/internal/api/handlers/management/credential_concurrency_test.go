package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	fileauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestCredentialConcurrencyFieldsPersistAndRejectInvalid(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "account.json")
	store := fileauth.NewFileTokenStore()
	store.SetBaseDir(dir)
	m := coreauth.NewManager(store, nil, nil)
	_, err := m.Register(context.Background(), &coreauth.Auth{ID: "account.json", FileName: "account.json", Provider: "codex", Attributes: map[string]string{"path": filePath}, Metadata: map[string]any{"type": "codex", "access_token": "secret-test-token"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: dir}, m)
	patch := func(value string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/v8/management/credentials/fields", strings.NewReader(`{"name":"account.json","concurrency_limit":`+value+`}`))
		h.PatchAuthFileFields(ctx)
		return rec
	}
	for _, value := range []string{"0", "-1", "1.5", "2147483648", `"2"`, "true", "{}"} {
		if rec := patch(value); rec.Code != 400 {
			t.Fatalf("value=%s status=%d body=%s", value, rec.Code, rec.Body.String())
		}
	}
	if rec := patch("3"); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["concurrency_limit"] != float64(3) {
		t.Fatalf("saved limit=%v", body["concurrency_limit"])
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	h.GetCredentialConcurrency(ctx)
	if strings.Contains(rec.Body.String(), "secret-test-token") {
		t.Fatal("snapshot exposed credential material")
	}
	if !strings.Contains(rec.Body.String(), `"effective_limit":3`) {
		t.Fatal(rec.Body.String())
	}
	if rec = patch("null"); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	a, _ := m.GetByID("account.json")
	if _, exists := a.Metadata[coreauth.AttributeConcurrencyLimit]; exists {
		t.Fatal("override was not removed")
	}
	m.SetConfig(&config.Config{Home: config.HomeConfig{Enabled: true}})
	if rec = patch("8"); rec.Code != 409 {
		t.Fatalf("Home update status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(rec)
	h.GetCredentialConcurrency(ctx)
	if strings.Contains(rec.Body.String(), "active") || strings.Contains(rec.Body.String(), "default_limit") || !strings.Contains(rec.Body.String(), `"mode":"home"`) {
		t.Fatal(rec.Body.String())
	}
}

func TestCredentialConcurrencyConfigV8RoundTripAndHome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := []byte("config-version: 8\napi-keys:\n  codex:\n    - name: pool\n      base-url: https://example.invalid\n      keys:\n        - api-key: fake-key\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	m := coreauth.NewManager(nil, nil, nil)
	h := &Handler{cfg: cfg, configFilePath: path, authManager: m}
	router := gin.New()
	router.GET("/v8/management/config/*path", h.ConfigV8)
	router.PUT("/v8/management/config/*path", h.ConfigV8)
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v8/management/config/api-keys", nil))
	if get.Code != 200 {
		t.Fatal(get.Body.String())
	}
	put := func(value, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/v8/management/config/api-keys/codex", strings.NewReader(`[{"name":"pool","base-url":"https://example.invalid","keys":[{"api-key":"fake-key","concurrency-limit":`+value+`}]}]`))
		req.Header.Set("If-Match", etag)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := put("4", `"stale"`); rec.Code != 412 {
		t.Fatalf("stale write=%d", rec.Code)
	}
	rec := put("4", get.Header().Get("ETag"))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	parsed, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.CodexKey) != 1 {
		t.Fatalf("config lost Codex key: %#v", parsed.CodexKey)
	}
	if parsed.CodexKey[0].ConcurrencyLimit == nil || *parsed.CodexKey[0].ConcurrencyLimit != 4 {
		t.Fatal("config override missing")
	}
	m.SetConfig(&config.Config{Home: config.HomeConfig{Enabled: true}})
	if rejected := put("8", rec.Header().Get("ETag")); rejected.Code != 409 {
		t.Fatalf("Home config edit=%d body=%s", rejected.Code, rejected.Body.String())
	}
}
