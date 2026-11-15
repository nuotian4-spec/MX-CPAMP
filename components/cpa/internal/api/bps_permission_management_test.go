package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestBPSPermissionSuspensionManagementV8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8317\nremote-management: {secret-key: synthetic-admin}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AuthDir = t.TempDir()
	m := coreauth.NewManager(nil, nil, nil)
	m.SetCooldownStateStore(coreauth.NewFileCooldownStateStore(t.TempDir()))
	auth, err := m.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: "permission-admin", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"email": "shared@example.invalid"}, Attributes: map[string]string{"runtime_only": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"model-a", "model-b"} {
		m.MarkResult(context.Background(), coreauth.Result{AuthID: auth.ID, Provider: "bps", Model: model, Error: &coreauth.Error{HTTPStatus: 403, Message: "bounded"}, PermissionDenial: &coreauth.PermissionDenial{Source: "http", UpstreamHTTPStatus: 403, SemanticStatus: 403, ReasonCode: "unknown"}})
	}
	h := management.NewHandler(cfg, path, m)
	h.SetLocalPassword("synthetic-admin")
	s := &Server{cfg: cfg, engine: gin.New(), mgmt: h}
	s.managementRoutesEnabled.Store(true)
	s.registerManagementRoutes()
	request := func(method, url, body string, authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Content-Type", "application/json")
		if authorized {
			req.Header.Set("Authorization", "Bearer synthetic-admin")
		}
		out := httptest.NewRecorder()
		s.engine.ServeHTTP(out, req)
		return out
	}
	route := "/v8/management/credentials/permission-suspensions/release"
	if denied := request(http.MethodPost, route, "{}", false); denied.Code != 401 {
		t.Fatalf("unprotected release: %d", denied.Code)
	}
	list := request(http.MethodGet, "/v8/management/credentials", "", true)
	item := ojson.GetBytes(list.Body.Bytes(), "files.0")
	if list.Code != 200 || item.Get("auth_id").String() != auth.ID || item.Get("permission_suspensions.#").Int() != 2 || item.Get("generation").Type != ojson.String {
		t.Fatalf("snapshot: %d %s", list.Code, list.Body.String())
	}
	current, _ := m.GetByID(auth.ID)
	body := ojson.NewDocString(`{"provider":"bps","scopes":[{"scope":"model","model_key":"model-a"}]}`)
	_ = body.Set("auth_id", auth.ID)
	_ = body.Set("expected_registration_epoch", strconv.FormatUint(current.RegistrationEpoch, 10))
	_ = body.Set("expected_credential_version", strconv.FormatUint(current.CredentialVersion, 10))
	_ = body.Set("expected_generation", strconv.FormatUint(current.Generation, 10))
	result := request(http.MethodPost, route, body.String(), true)
	if result.Code != 200 || ojson.GetBytes(result.Body.Bytes(), "permission_suspensions.#").Int() != 1 {
		t.Fatalf("release: %d %s", result.Code, result.Body.String())
	}
	if stale := request(http.MethodPost, route, body.String(), true); stale.Code != 409 {
		t.Fatalf("stale release: %d", stale.Code)
	}
	list = request(http.MethodGet, "/v8/management/credentials?page=1&page_size=10", "", true)
	if ojson.GetBytes(list.Body.Bytes(), "files.0.permission_suspensions.#").Int() != 1 {
		t.Fatalf("partial readback: %s", list.Body.String())
	}
	if wrong := request(http.MethodPost, "/v0/management/credentials/permission-suspensions/release", body.String(), true); wrong.Code != 404 {
		t.Fatalf("unexpected v0 release: %d", wrong.Code)
	}
}
