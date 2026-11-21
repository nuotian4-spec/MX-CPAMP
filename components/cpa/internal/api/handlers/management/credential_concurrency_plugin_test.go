package management

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/json"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/synthesizer"
	fileauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type concurrencyPluginParser struct {
	stale bool
}

func (p concurrencyPluginParser) ParseAuth(_ context.Context, req pluginapi.AuthParseRequest) (*coreauth.Auth, bool, error) {
	data := pluginapi.AuthData{
		ID: req.FileName, FileName: req.FileName, Provider: "cpa-bps",
		Metadata: map[string]any{"type": "cpa-bps", "access_token": "synthetic-token"}, StorageJSON: req.RawJSON,
	}
	if p.stale {
		data.Attributes = map[string]string{"concurrency_limit": "20"}
		data.Metadata["concurrency_limit"] = 20
	}
	return (*pluginhost.Host)(nil).AuthDataToCoreAuth(data, req.Path, req.FileName), true, nil
}

type concurrencyPluginStore struct {
	*fileauth.FileTokenStore
	fail bool
}

func (s *concurrencyPluginStore) Save(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if s.fail {
		// Exercise the metadata injection that precedes a storage write failure.
		if setter, ok := auth.Storage.(interface{ SetMetadata(map[string]any) }); ok {
			setter.SetMetadata(auth.Metadata)
		}
		return "", errors.New("synthetic storage write failure")
	}
	return s.FileTokenStore.Save(ctx, auth)
}

func concurrencyPluginFixture(t *testing.T, raw string, stale bool) (*coreauth.Manager, *Handler, *concurrencyPluginStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "account.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := concurrencyPluginParser{stale: stale}
	auth, _, err := parser.ParseAuth(t.Context(), pluginapi.AuthParseRequest{Path: path, FileName: "account.json", RawJSON: []byte(raw)})
	if err != nil {
		t.Fatal(err)
	}
	store := &concurrencyPluginStore{FileTokenStore: fileauth.NewFileTokenStore()}
	store.SetBaseDir(dir)
	m := coreauth.NewManager(store, nil, nil)
	if _, err := m.Register(coreauth.WithSkipPersist(t.Context()), auth); err != nil {
		t.Fatal(err)
	}
	return m, NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: dir}, m), store, path
}

func patchConcurrency(t *testing.T, h *Handler, value string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPatch, "/v8/management/credentials/fields", strings.NewReader(`{"name":"account.json","concurrency_limit":`+value+`}`))
	h.PatchAuthFileFields(c)
	return rec
}

func assertPluginConcurrency(t *testing.T, m *coreauth.Manager, path string, want int) {
	t.Helper()
	var row coreauth.CredentialConcurrencyEntry
	for _, entry := range m.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
		if entry.AuthID == "account.json" {
			row = entry
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if want == 0 {
		if _, exists := disk["concurrency_limit"]; exists || row.ConcurrencyLimit != nil || row.EffectiveLimit != 20 || row.LimitSource != "default" {
			t.Fatalf("expected inherited 20: disk=%v runtime=%+v", disk["concurrency_limit"], row)
		}
	} else if disk["concurrency_limit"] != float64(want) || row.ConcurrencyLimit == nil || *row.ConcurrencyLimit != want || row.EffectiveLimit != want || row.LimitSource != "override" {
		t.Fatalf("expected override %d: disk=%v runtime=%+v", want, disk["concurrency_limit"], row)
	}
}

func TestCredentialConcurrencyPluginLifecycle(t *testing.T) {
	m, h, store, path := concurrencyPluginFixture(t, `{"type":"cpa-bps","access_token":"synthetic-token"}`, false)
	fileauth.RegisterPluginAuthParser(concurrencyPluginParser{})
	t.Cleanup(func() { fileauth.RegisterPluginAuthParser(nil) })
	if _, err := m.Register(t.Context(), &coreauth.Auth{ID: "untouched.json", FileName: "untouched.json", Provider: "cpa-bps", Metadata: map[string]any{"type": "cpa-bps"}}); err != nil {
		t.Fatal(err)
	}
	if rec := patchConcurrency(t, h, "30"); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	assertPluginConcurrency(t, m, path, 30)
	base, _ := m.GetByID("account.json")
	refreshed, _, _ := (concurrencyPluginParser{}).ParseAuth(t.Context(), pluginapi.AuthParseRequest{Path: path, FileName: "account.json", RawJSON: []byte(`{"type":"cpa-bps","access_token":"refreshed-token"}`)})
	if _, err := m.UpdateRefreshedAuth(t.Context(), base, refreshed); err != nil {
		t.Fatal(err)
	}
	assertPluginConcurrency(t, m, path, 30)

	reload := func(want int) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		auths, err := synthesizer.SynthesizeAuthFile(&synthesizer.SynthesisContext{AuthDir: filepath.Dir(path), Now: time.Now(), PluginAuthParser: concurrencyPluginParser{}, StrictFileParsing: true}, path, raw)
		if err != nil || len(auths) != 1 {
			t.Fatalf("synthesize: count=%d err=%v", len(auths), err)
		}
		if _, err := m.Update(coreauth.WithSkipPersist(t.Context()), auths[0]); err != nil {
			t.Fatal(err)
		}
		assertPluginConcurrency(t, m, path, want)
		cold, err := store.List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		fresh := coreauth.NewManager(nil, nil, nil)
		for _, a := range cold {
			if _, err := fresh.Register(coreauth.WithSkipPersist(t.Context()), a); err != nil {
				t.Fatal(err)
			}
		}
		assertPluginConcurrency(t, fresh, path, want)
	}
	reload(30)
	staleBase, _ := m.GetByID("account.json")
	if rec := patchConcurrency(t, h, "null"); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	assertPluginConcurrency(t, m, path, 0)
	if _, err := m.UpdateRefreshedAuth(t.Context(), staleBase, staleBase.Clone()); err != nil {
		t.Fatal(err)
	}
	assertPluginConcurrency(t, m, path, 0)
	reload(0)
	for _, row := range m.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
		if row.AuthID == "untouched.json" && (row.ConcurrencyLimit != nil || row.EffectiveLimit != 20) {
			t.Fatalf("unrelated credential changed: %+v", row)
		}
	}
}

func TestCredentialConcurrencyPluginWriteFailure(t *testing.T) {
	m, h, store, path := concurrencyPluginFixture(t, `{"type":"cpa-bps"}`, false)
	store.fail = true
	called := false
	h.postAuthPersistHook = func(context.Context, *coreauth.Auth) error { called = true; return nil }
	rec := patchConcurrency(t, h, "30")
	if rec.Code != http.StatusInternalServerError || called {
		t.Fatalf("write failure must return 500 without runtime hook: status=%d hook=%v", rec.Code, called)
	}
	assertPluginConcurrency(t, m, path, 0)
	auth, _ := m.GetByID("account.json")
	var stored map[string]any
	if err := json.Unmarshal(auth.Storage.(interface{ RawJSON() []byte }).RawJSON(), &stored); err != nil {
		t.Fatal(err)
	}
	if _, exists := stored["concurrency_limit"]; exists {
		t.Fatal("failed edit changed the published storage snapshot")
	}
}

func TestCredentialConcurrencyPluginStaleAttribute(t *testing.T) {
	m, h, _, path := concurrencyPluginFixture(t, `{"type":"cpa-bps","concurrency_limit":20}`, true)
	for _, tc := range []struct {
		value string
		want  int
	}{{"30", 30}, {"null", 0}} {
		if rec := patchConcurrency(t, h, tc.value); rec.Code != 200 {
			t.Fatal(rec.Body.String())
		}
		assertPluginConcurrency(t, m, path, tc.want)
	}
}

func TestCredentialConcurrencyPluginFileProjection(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      int
	}{
		{"override", `{"type":"cpa-bps","concurrency_limit":30}`, 30},
		{"inherit", `{"type":"cpa-bps"}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, path := concurrencyPluginFixture(t, tc.raw, false)
			parser := concurrencyPluginParser{stale: true}
			fileauth.RegisterPluginAuthParser(parser)
			t.Cleanup(func() { fileauth.RegisterPluginAuthParser(nil) })
			cold, err := store.List(t.Context())
			if err != nil || len(cold) != 1 {
				t.Fatalf("load: count=%d err=%v", len(cold), err)
			}
			reloaded, err := synthesizer.SynthesizeAuthFile(&synthesizer.SynthesisContext{AuthDir: filepath.Dir(path), Now: time.Now(), PluginAuthParser: parser, StrictFileParsing: true}, path, []byte(tc.raw))
			if err != nil || len(reloaded) != 1 {
				t.Fatalf("reload: count=%d err=%v", len(reloaded), err)
			}
			for _, auth := range []*coreauth.Auth{cold[0], reloaded[0]} {
				m := coreauth.NewManager(nil, nil, nil)
				if _, err := m.Register(coreauth.WithSkipPersist(t.Context()), auth); err != nil {
					t.Fatal(err)
				}
				assertPluginConcurrency(t, m, path, tc.want)
			}
		})
	}
}
