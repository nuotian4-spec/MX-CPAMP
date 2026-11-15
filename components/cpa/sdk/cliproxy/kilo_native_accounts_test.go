package cliproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const kiloAccountsURL = "/v8/management/kilo/accounts"
const kiloManagementKey = "synthetic-management-key"
const kiloInitialKey = "synthetic-kilo-token"

func m2Request(t *testing.T, f *kiloNativeFixture, method, path, body string, want int) []byte {
	t.Helper()
	status, data := f.request(t, method, path, kiloManagementKey, body)
	if status != want {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, status, want, data)
	}
	for _, secret := range []string{kiloInitialKey, "synthetic-replaced-token"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("secret returned by %s %s", method, path)
		}
	}
	if executor, ok := f.manager.Executor("kilo"); ok {
		f.executor, _ = executor.(*native.KiloExecutor)
	}
	return data
}
func m2Create(t *testing.T, f *kiloNativeFixture, fields string) string {
	t.Helper()
	body := `{"api_key":"` + kiloInitialKey + `"` + fields + `}`
	data := m2Request(t, f, "POST", kiloAccountsURL, body, 201)
	id := ojson.GetBytes(data, "id").String()
	if !strings.HasPrefix(id, "kilo-") || !strings.HasSuffix(id, ".json") {
		t.Fatalf("server id=%q", id)
	}
	executor, ok := f.manager.Executor("kilo")
	if !ok {
		a, _ := f.manager.GetByID(id)
		if a == nil || !a.Disabled {
			t.Fatal("Service did not register executor")
		}
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
		return id
	}
	f.executor, ok = executor.(*native.KiloExecutor)
	if !ok {
		t.Fatalf("executor=%T", executor)
	}
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	return id
}
func m2FileStore(f *kiloNativeFixture) *sdkauth.FileTokenStore {
	s := sdkauth.NewFileTokenStore()
	s.SetBaseDir(f.cfg.AuthDir)
	return s
}
func m2Reload(t *testing.T, f *kiloNativeFixture) {
	t.Helper()
	manager := coreauth.NewManager(m2FileStore(f), nil, f.hook)
	manager.SetConfig(f.cfg)
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	service := &Service{cfg: f.cfg, coreManager: manager}
	for _, a := range manager.List() {
		service.registerExecutorForAuth(a, false)
		service.registerModelsForAuth(context.Background(), a)
	}
	f.manager = manager
	if executor, ok := manager.Executor("kilo"); ok {
		f.executor = executor.(*native.KiloExecutor)
	}
	f.server.Close()
	server := api.NewServer(f.cfg, manager, access.NewManager(), filepath.Join(t.TempDir(), "config.yaml"), api.WithLocalManagementPassword(kiloManagementKey), api.WithPostAuthPersistHook(service.runtimeAuthSyncHook()))
	f.server = httptest.NewServer(server.Handler())
	f.server.Client().Transport = &http.Transport{Proxy: nil}
	t.Cleanup(f.server.Close)
}
func TestKiloM2AccountLifecycleThroughRealRouter(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key", "synthetic-bps-key", "synthetic-legacy-key"} {
		status, _ := f.request(t, "POST", kiloAccountsURL, key, `{"api_key":"synthetic-kilo-token"}`)
		if status != 401 {
			t.Fatalf("inference key granted management: %d", status)
		}
	}
	if len(f.manager.List()) != 0 {
		t.Fatal("unauthorized import changed manager")
	}
	for _, body := range []string{`{}`, `{"api_key":""}`, `{"api_key":null}`, `{"api_key":"x","organization_id":"org"}`, `{"api_key":"x","proxy_url":"http://localhost"}`, `{"api_key":"x","concurrency_limit":0}`, `{"api_key":"x","concurrency_limit":1.5}`} {
		m2Request(t, f, "POST", kiloAccountsURL, body, 400)
	}
	id := m2Create(t, f, `,"name":"first","enabled":false,"priority":3,"concurrency_limit":2,"allow_direct_when_proxy_full":true`)
	path := filepath.Join(f.cfg.AuthDir, id)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("disabled import did not persist: %v", err)
	}
	a, _ := f.manager.GetByID(id)
	if !a.Disabled {
		t.Fatal("disabled import is active")
	}
	m2Reload(t, f)
	a, _ = f.manager.GetByID(id)
	if a == nil || !a.Disabled || a.Label != "first" || a.Attributes["priority"] != "3" || a.Metadata["allow_direct_when_proxy_full"] != true {
		t.Fatalf("reload policy=%+v", a)
	}
	data := m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":true,"name":"renamed","allow_direct_when_proxy_full":false}`, 200)
	if ojson.GetBytes(data, "id").String() != id || ojson.GetBytes(data, "effective_limit").Int() != 2 {
		t.Fatalf("patch DTO=%s", data)
	}
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"first"}`)
	if status != 200 || !bytes.Contains(body, []byte("kilo-ok")) {
		t.Fatalf("imported inference=%d %s", status, body)
	}
	f.assertReleased(t, 1)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":""}`, 400)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"concurrency_limit":null}`, 200)
	a, _ = f.manager.GetByID(id)
	if a.Metadata["api_key"] != kiloInitialKey {
		t.Fatal("omitted key was changed")
	}
	m2Reload(t, f)
	data = m2Request(t, f, "GET", kiloAccountsURL, "", 200)
	if ojson.GetBytes(data, "accounts.0.effective_limit").Int() != 20 || ojson.GetBytes(data, "accounts.0.id").String() != id {
		t.Fatalf("stable reload/default=%s", data)
	}
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 204)
	if _, ok := f.manager.GetByID(id); ok {
		t.Fatal("deleted auth still registered")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("deleted file=%v", err)
	}
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 404)
}
func TestKiloM2IdentityRevisionKeepsOrdinaryContinuation(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"first","prompt_cache_key":"m2-session"}`)
	if status != 200 {
		t.Fatalf("first=%d", status)
	}
	f.assertReleased(t, 1)
	a, _ := f.manager.GetByID(id)
	identity := coreauth.KiloIdentity(a)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"name":"ordinary","priority":2,"concurrency_limit":3,"allow_direct_when_proxy_full":false}`, 200)
	a, _ = f.manager.GetByID(id)
	if coreauth.KiloIdentity(a) != identity {
		t.Fatal("ordinary fields changed identity")
	}
	continuation := `{"model":"gpt-6.1-sol","previous_response_id":"resp-kilo","input":"next"}`
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", continuation)
	if status != 200 {
		t.Fatalf("ordinary continuation=%d", status)
	}
	f.assertReleased(t, 2)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 200)
	a, _ = f.manager.GetByID(id)
	if coreauth.KiloIdentity(a).Revision != identity.Revision+1 {
		t.Fatal("manual replacement revision missing")
	}
	before := f.posts.Load()
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", continuation)
	if status != 409 || f.posts.Load() != before {
		t.Fatalf("changed identity continuation=%d POST delta=%d", status, f.posts.Load()-before)
	}
	m2Reload(t, f)
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", continuation)
	if status != 409 || f.posts.Load() != before {
		t.Fatalf("reloaded identity continuation=%d POST delta=%d", status, f.posts.Load()-before)
	}
}
func TestKiloM2InFlightMutationAndDisable(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; kiloSuccessStream(w, r) }, true)
	id := m2Create(t, f, `,"concurrency_limit":2`)
	done := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"held"}`)
		done <- status
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream never entered")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 409)
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 409)
	data := m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false,"concurrency_limit":1}`, 200)
	if ojson.GetBytes(data, "active").Int() != 1 {
		t.Fatalf("active lease reset: %s", data)
	}
	select {
	case status := <-done:
		t.Fatalf("disable interrupted request: %d", status)
	default:
	}
	release <- struct{}{}
	select {
	case status := <-done:
		if status != 200 {
			t.Fatalf("held request=%d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("held request failed to complete")
	}
	f.assertReleased(t, 1)
	if f.posts.Load() != 1 {
		t.Fatalf("unexpected replay POST=%d", f.posts.Load())
	}
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 204)
}

type m2FailStore struct {
	coreauth.Store
	save, remove bool
}

func (s m2FailStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	if s.save {
		return "", errors.New("synthetic-replaced-token")
	}
	return s.Store.Save(ctx, a)
}
func (s m2FailStore) Delete(ctx context.Context, id string) error {
	if s.remove {
		return errors.New("synthetic-replaced-token")
	}
	return s.Store.Delete(ctx, id)
}
func TestKiloM2PersistenceFailureKeepsDiskAndRuntime(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	path := filepath.Join(f.cfg.AuthDir, id)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := f.manager.GetByID(id)
	identity := coreauth.KiloIdentity(a)
	f.manager.SetStore(m2FailStore{Store: m2FileStore(f), save: true, remove: true})
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token","name":"lost"}`, 503)
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 503)
	m2Request(t, f, "POST", kiloAccountsURL, `{"api_key":"synthetic-replaced-token"}`, 503)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed mutation changed disk")
	}
	a, _ = f.manager.GetByID(id)
	if coreauth.KiloIdentity(a) != identity || a.Label == "lost" || len(f.manager.List()) != 1 {
		t.Fatal("failed mutation published runtime")
	}
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"old usable"}`)
	if status != 200 {
		t.Fatalf("old runtime not usable: %d", status)
	}
	f.assertReleased(t, 1)
}
func TestKiloM2LegacySecretAndMutationRoutes(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	other := "ordinary.json"
	otherPath := filepath.Join(f.cfg.AuthDir, other)
	original := []byte(`{"type":"claude","label":"ordinary"}`)
	if err := os.WriteFile(otherPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/v8/management/credentials", "/v0/management/auth-files"} {
		m2Request(t, f, "GET", prefix+"/download?name="+id, "", 409)
		m2Request(t, f, "POST", prefix+"?name=new.json", `{"type":"kilo","api_key":"synthetic-kilo-token"}`, 409)
		m2Request(t, f, "POST", prefix+"?name="+id, string(original), 409)
		m2Request(t, f, "PATCH", prefix+"/fields", `{"name":"`+id+`","api_key":"synthetic-replaced-token"}`, 409)
		m2Request(t, f, "PATCH", prefix+"/status", `{"name":"`+id+`","disabled":true}`, 409)
		m2Request(t, f, "DELETE", prefix+"?name="+id, "", 409)
		body := ojson.NewDocString("{}")
		if err := body.Set("names", []string{other, id}); err != nil {
			t.Fatal(err)
		}
		m2Request(t, f, "DELETE", prefix, body.String(), 409)
		m2Request(t, f, "DELETE", prefix+"?all=true", "", 409)
		after, err := os.ReadFile(otherPath)
		if err != nil || !bytes.Equal(after, original) {
			t.Fatal("batch/all deletion partially changed ordinary file")
		}
		var payload bytes.Buffer
		writer := multipart.NewWriter(&payload)
		for _, part := range []struct{ name, data string }{{other, `{"type":"claude","label":"changed"}`}, {"new-kilo.json", `{"type":"kilo","api_key":"synthetic-kilo-token"}`}} {
			dest, err := writer.CreateFormFile("files", part.name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(dest, part.data)
		}
		_ = writer.Close()
		req, _ := http.NewRequest("POST", f.server.URL+prefix, &payload)
		req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 409 {
			t.Fatalf("batch upload=%d", resp.StatusCode)
		}
		after, err = os.ReadFile(otherPath)
		if err != nil || !bytes.Equal(after, original) {
			t.Fatal("batch upload partially changed ordinary file")
		}
	}

	// Existing files may have a legacy api_key attribute or free-form diagnostics.
	legacy, _ := f.manager.GetByID(id)
	legacy.Attributes["api_key"] = kiloInitialKey
	legacy.StatusMessage = kiloInitialKey
	if _, err := f.manager.Update(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	m2Request(t, f, "GET", kiloAccountsURL, "", 200)
	m2Request(t, f, "GET", "/v8/management/credentials", "", 200)
	m2Request(t, f, "GET", "/v0/management/auth-files", "", 200)

	otherAuth := &coreauth.Auth{ID: "non-kilo", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := f.manager.Register(coreauth.WithSkipPersist(context.Background()), otherAuth); err != nil {
		t.Fatal(err)
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/non-kilo", `{"name":"wrong"}`, 404)
	m2Request(t, f, "DELETE", kiloAccountsURL+"/non-kilo", "", 404)
	m2Request(t, f, "GET", "/v8/management/credentials/download?name="+other, "", 200)
	if f.posts.Load() != 0 {
		t.Fatal("management unexpectedly called upstream")
	}
}

func TestKiloM2DelayedPersistCallbackCannotResurrectDeletedAccount(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	entered := make(chan *coreauth.Auth, 1)
	resume := make(chan struct{})
	f.postPersistGate = func(_ context.Context, a *coreauth.Auth) { entered <- a.Clone(); <-resume }
	created := make(chan int, 1)
	go func() {
		status, _ := f.request(t, "POST", kiloAccountsURL, kiloManagementKey, `{"api_key":"synthetic-kilo-token"}`)
		created <- status
	}()
	var saved *coreauth.Auth
	select {
	case saved = <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("post-persist callback not reached")
	}
	if _, ok := f.manager.GetByID(saved.ID); !ok {
		t.Fatal("save was not committed before callback")
	}
	m2Request(t, f, "DELETE", kiloAccountsURL+"/"+saved.ID, "", 204)
	close(resume)
	select {
	case status := <-created:
		if status != 201 {
			t.Fatalf("create completion=%d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not finish")
	}
	if _, ok := f.manager.GetByID(saved.ID); ok {
		t.Fatal("delayed callback resurrected runtime account")
	}
	if len(registry.GetGlobalRegistry().GetModelsForClient(saved.ID)) != 0 {
		t.Fatal("delayed callback resurrected models")
	}
	if _, err := os.Stat(filepath.Join(f.cfg.AuthDir, saved.ID)); !os.IsNotExist(err) {
		t.Fatal("delayed callback recreated file")
	}
}

func TestKiloM2ManualReplacementClearsRejectedKeyState(t *testing.T) {
	calls := 0
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "synthetic denial", 401)
			return
		}
		kiloSuccessStream(w, r)
	}, true)
	id := m2Create(t, f, "")
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"rejected"}`)
	if status != 401 {
		t.Fatalf("first status=%d", status)
	}
	f.assertReleased(t, 1)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 200)
	f.expectedToken.Store("synthetic-replaced-token")
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"new identity"}`)
	if status != 200 || !bytes.Contains(body, []byte("kilo-ok")) {
		t.Fatalf("replacement inference=%d %s", status, body)
	}
	f.assertReleased(t, 2)
	if f.posts.Load() != 2 {
		t.Fatalf("POST=%d", f.posts.Load())
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
	data := m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-kilo-token"}`, 200)
	current, _ := f.manager.GetByID(id)
	if ojson.GetBytes(data, "enabled").Bool() || !current.Disabled {
		t.Fatal("replacement overrode manual disable")
	}
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"disabled identity"}`)
	if status == 200 || f.posts.Load() != 2 {
		t.Fatalf("disabled replacement admitted request status=%d POST=%d", status, f.posts.Load())
	}
}

func TestKiloM2StrictJSONAccountContract(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, `,"name":"original","concurrency_limit":2`)
	path := filepath.Join(f.cfg.AuthDir, id)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, body string }{
		{"empty", ""}, {"null", `null`}, {"array", `[]`}, {"scalar", `"text"`},
		{"trailing-object", `{} {}`}, {"trailing-null", `{} null`}, {"truncated", `{"enabled":`},
		{"key-null", `{"api_key":null}`}, {"key-number", `{"api_key":1}`}, {"key-empty", `{"api_key":" "}`},
		{"name-bool", `{"name":true}`}, {"enabled-string", `{"enabled":"true"}`}, {"direct-number", `{"allow_direct_when_proxy_full":1}`},
		{"priority-null", `{"priority":null}`}, {"priority-string", `{"priority":"1"}`}, {"priority-fraction", `{"priority":1.0}`}, {"priority-exponent", `{"priority":1e0}`}, {"priority-overflow", `{"priority":9223372036854775808}`},
		{"limit-string", `{"concurrency_limit":"2"}`}, {"limit-fraction", `{"concurrency_limit":2.0}`}, {"limit-exponent", `{"concurrency_limit":2e0}`}, {"limit-zero", `{"concurrency_limit":0}`}, {"limit-overflow", `{"concurrency_limit":2147483648}`},
		{"unknown", `{"unsupported_field":"unsupported"}`}, {"duplicate-last-invalid", `{"enabled":true,"enabled":"false"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, tc.body, 400) })
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected JSON changed persisted credential")
	}
	data := m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":"ignored","enabled":false,"name":"renamed\u0020account","priority":-7,"concurrency_limit":2147483647,"allow_direct_when_proxy_full":true}`, 200)
	if ojson.GetBytes(data, "enabled").Bool() || ojson.GetBytes(data, "priority").Int() != -7 || ojson.GetBytes(data, "effective_limit").Int() != 2147483647 || ojson.GetBytes(data, "name").String() != "renamed account" || !ojson.GetBytes(data, "allow_direct_when_proxy_full").Bool() {
		t.Fatalf("typed fields=%s", data)
	}
	data = m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":true,"concurrency_limit":null}`, 200)
	if ojson.GetBytes(data, "concurrency_limit").Type != ojson.Null || ojson.GetBytes(data, "effective_limit").Int() != 20 {
		t.Fatalf("null limit=%s", data)
	}
	m2Reload(t, f)
	a, _ := f.manager.GetByID(id)
	if coreauth.KiloIdentity(a).Revision != 1 || a.Metadata["api_key"] != kiloInitialKey || a.Label != "renamed account" || a.Disabled {
		t.Fatal("typed JSON fields did not survive reload")
	}
}
