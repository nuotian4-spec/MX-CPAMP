package management

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/bps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func bpsTestHandler(t *testing.T) (*Handler, *coreauth.Manager) {
	t.Helper()
	dir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	h := NewHandler(&config.Config{AuthDir: dir}, "", manager)
	store := sdkAuth.NewFileTokenStore()
	store.SetBaseDir(dir)
	h.tokenStore = store
	return h, manager
}

func bpsTestJSON(t *testing.T, fields map[string]any) string {
	t.Helper()
	doc := ojson.NewDoc([]byte(`{}`))
	for k, v := range fields {
		k = strings.NewReplacer("\\", "\\\\", ".", "\\.", ":", "\\:").Replace(k)
		if err := doc.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	return doc.String()
}

func bpsTestBody(provider, email, access string) map[string]any {
	profile, client := "excel", bps.ClientID
	if provider == "codex" {
		profile, client = "codex", codex.ClientID
	}
	return map[string]any{"profile": profile, "client_id": client, "access_token": access, "refresh_token": "refresh-credential", "account_id": "shared-workspace", "email": email, "expired": time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "login_method": "2fa"}
}

func bpsTestRequest(t *testing.T, handler gin.HandlerFunc, method, path, body string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	c.Request = req
	handler(c)
	return w
}

func bpsTestImport(t *testing.T, h *Handler, provider string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return bpsTestRequest(t, h.ImportOAuthV8, "POST", "/v8/management/oauth/import?provider="+provider, bpsTestJSON(t, body), nil)
}

func TestBPSImportDurableRegisteredAndChannelsIndependent(t *testing.T) {
	h, m := bpsTestHandler(t)
	for _, provider := range []string{"codex", "bps"} {
		w := bpsTestImport(t, h, provider, bpsTestBody(provider, "user@example.com", "access-secret"))
		if w.Code != 200 {
			t.Fatalf("import %s: %d %s", provider, w.Code, w.Body.String())
		}
		doc := ojson.NewDoc(w.Body.Bytes())
		if !doc.Get("saved").Bool() || !doc.Get("registered").Bool() || strings.Contains(w.Body.String(), "access-secret") || strings.Contains(w.Body.String(), "refresh-credential") {
			t.Fatal(w.Body.String())
		}
		auth, exists := m.GetByID(doc.Get("auth_id").String())
		if !exists || auth.Provider != provider {
			t.Fatal("not registered")
		}
		raw, err := os.ReadFile(filepath.Join(h.cfg.AuthDir, auth.FileName))
		if err != nil {
			t.Fatal(err)
		}
		if ojson.NewDoc(raw).Get("refresh_token").String() != "refresh-credential" {
			t.Fatal("refresh token not durable")
		}
	}
	if len(m.List()) != 2 {
		t.Fatal("channel overwritten")
	}
}

func TestBPSImportReloginPreservesManagementAndEpoch(t *testing.T) {
	h, m := bpsTestHandler(t)
	body := bpsTestBody("bps", "user@example.com", "first")
	w := bpsTestImport(t, h, "bps", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := ojson.NewDoc(w.Body.Bytes()).Get("auth_id").String()
	_, _, err := m.Modify(coreauth.WithSkipPersist(t.Context()), id, 0, func(a *coreauth.Auth) error {
		a.Metadata["note"] = "keep"
		a.Metadata["priority"] = float64(7)
		a.Metadata["custom.key"] = "literal"
		a.Disabled = true
		a.Status = coreauth.StatusDisabled
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := m.GetByID(id)
	body["access_token"] = "replacement"
	body["refresh_token"] = "rotated"
	body["login_method"] = "oauth"
	w = bpsTestImport(t, h, "bps", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	after, _ := m.GetByID(id)
	if after.Metadata["note"] != "keep" || after.Metadata["priority"] != float64(7) || !after.Disabled || after.Status != coreauth.StatusDisabled || after.RegistrationEpoch <= before.RegistrationEpoch || after.Metadata["refresh_token"] != "rotated" || after.Metadata["login_method"] != "oauth" {
		t.Fatalf("relogin lost management/epoch: %#v", after)
	}
	raw, err := os.ReadFile(filepath.Join(h.cfg.AuthDir, after.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if ojson.NewDoc(raw).Get("custom\\.key").String() != "literal" {
		t.Fatal(string(raw))
	}
}

func TestBPSImportSharedWorkspaceKeepsDifferentUsers(t *testing.T) {
	h, m := bpsTestHandler(t)
	for _, email := range []string{"first@example.com", "second@example.com"} {
		w := bpsTestImport(t, h, "bps", bpsTestBody("bps", email, "access"))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if len(m.List()) != 2 {
		t.Fatal("workspace users collided")
	}
}

func TestBPSImportReloginRestoresRejectedCredential(t *testing.T) {
	h, m := bpsTestHandler(t)
	body := bpsTestBody("bps", "user@example.com", "old-access")
	w := bpsTestImport(t, h, "bps", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := ojson.NewDoc(w.Body.Bytes()).Get("auth_id").String()
	rejected, _ := m.GetByID(id)
	rejected.RequiresSignIn = true
	rejected.RejectedRefreshToken = "refresh-credential"
	rejected.RejectedAccessToken = "old-access"
	rejected.Unavailable = true
	rejected.Status = coreauth.StatusError
	rejected.LastError = &coreauth.Error{Code: "unauthorized", HTTPStatus: 401, Message: "old grant rejected"}
	rejected.RefreshError = &coreauth.Error{Code: "invalid_grant", HTTPStatus: 401, Message: "old refresh rejected"}
	rejected.NextRetryAfter = time.Now().Add(time.Hour)
	rejected.NextRefreshAfter = time.Now().Add(time.Hour)
	if _, err := m.Register(coreauth.WithSkipPersist(t.Context()), rejected); err != nil {
		t.Fatal(err)
	}
	body["access_token"] = "new-access"
	body["refresh_token"] = "new-refresh"
	w = bpsTestImport(t, h, "bps", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	after, _ := m.GetByID(id)
	if after.RequiresSignIn || after.Unavailable || after.LastError != nil || after.RefreshError != nil || after.Status != coreauth.StatusActive || !after.NextRetryAfter.IsZero() || !after.NextRefreshAfter.IsZero() {
		t.Fatalf("relogin kept rejected authorization: requires=%v unavailable=%v status=%s last=%v refresh=%v retry=%v next=%v", after.RequiresSignIn, after.Unavailable, after.Status, after.LastError, after.RefreshError, after.NextRetryAfter, after.NextRefreshAfter)
	}
	if after.InferenceVerified || after.RenewalVerified {
		t.Fatal("import fabricated business verification")
	}
}

func bpsTestJWT(payload string) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
}

func TestBPSImportClaimsValidation(t *testing.T) {
	h, _ := bpsTestHandler(t)
	exp := time.Now().Add(time.Hour).Unix()
	base := bpsTestBody("bps", "user@example.com", "access")
	base["expired"] = time.Unix(exp, 0).UTC().Format(time.RFC3339)
	base["access_token"] = bpsTestJWT(bpsTestJSON(t, map[string]any{"client_id": bps.ClientID, "sub": "subject", "exp": exp, "email": "user@example.com"}))
	base["id_token"] = bpsTestJWT(bpsTestJSON(t, map[string]any{"aud": bps.ClientID, "sub": "subject", "exp": exp + 1800, "email": "user@example.com"}))
	if w := bpsTestImport(t, h, "bps", base); w.Code != 200 {
		t.Fatalf("ID token lifetime rejected: %s", w.Body.String())
	}
	for key, value := range map[string]any{"client_id": codex.ClientID, "profile": "codex", "login_method": "password", "email": "other@example.com", "expired": time.Unix(exp+1, 0).UTC().Format(time.RFC3339)} {
		copyBody := make(map[string]any)
		for k, v := range base {
			copyBody[k] = v
		}
		copyBody[key] = value
		if w := bpsTestImport(t, h, "bps", copyBody); w.Code != 400 {
			t.Fatalf("accepted inconsistent %s: %d %s", key, w.Code, w.Body.String())
		}
	}
}

type bpsTestStore struct {
	coreauth.Store
	save func(context.Context, *coreauth.Auth) (string, error)
}

func (s *bpsTestStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	return s.save(ctx, a)
}

func TestBPSImportSaveAndRegistrationFailure(t *testing.T) {
	t.Run("save", func(t *testing.T) {
		h, m := bpsTestHandler(t)
		h.tokenStore = &bpsTestStore{Store: h.tokenStore, save: func(context.Context, *coreauth.Auth) (string, error) { return "", errors.New("SECRET storage failure") }}
		w := bpsTestImport(t, h, "bps", bpsTestBody("bps", "user@example.com", "access"))
		if w.Code != 502 || strings.Contains(w.Body.String(), "SECRET") || ojson.NewDoc(w.Body.Bytes()).Get("registered").Bool() || len(m.List()) != 0 {
			t.Fatal(w.Body.String())
		}
	})
	t.Run("register", func(t *testing.T) {
		h, m := bpsTestHandler(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		store := h.tokenStore
		h.tokenStore = &bpsTestStore{Store: store, save: func(ctx context.Context, a *coreauth.Auth) (string, error) {
			path, err := store.Save(ctx, a)
			cancel()
			return path, err
		}}
		w := bpsTestRequest(t, h.ImportOAuthV8, "POST", "/v8/management/oauth/import?provider=bps", bpsTestJSON(t, bpsTestBody("bps", "user@example.com", "access")), ctx)
		if w.Code != 502 || ojson.NewDoc(w.Body.Bytes()).Get("registered").Bool() || len(m.List()) != 0 {
			t.Fatal(w.Body.String())
		}
	})
}

type bpsManagementTransport func(*http.Request) (*http.Response, error)

func (f bpsManagementTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func bpsTestStart(t *testing.T, h *Handler, transport http.RoundTripper) (string, *bpsOAuthSession) {
	t.Helper()
	ctx := context.WithValue(t.Context(), "cliproxy.roundtripper", transport)
	w := bpsTestRequest(t, h.StartOAuthV8, "POST", "/v8/management/oauth/auth-url", `{"provider":"bps","is_webui":true}`, ctx)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	state := ojson.NewDoc(w.Body.Bytes()).Get("state").String()
	session, exists := oauthSessions.Get(state)
	if !exists || session.BPS == nil {
		t.Fatal("missing BPS session")
	}
	t.Cleanup(func() { CancelOAuthSession(state) })
	return state, session.BPS
}

func bpsTokenResponse(t *testing.T, r *http.Request) (*http.Response, error) {
	t.Helper()
	if r.URL.String() != bps.TokenURL {
		t.Fatal("wrong token endpoint")
	}
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if r.Form.Get("client_id") != bps.ClientID || r.Form.Get("code_verifier") == "" || r.Form.Get("redirect_uri") != bps.RedirectURI {
		t.Fatal("wrong OAuth profile")
	}
	id := bpsTestJWT(bpsTestJSON(t, map[string]any{"aud": bps.ClientID, "sub": "subject", "email": "user@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "shared-workspace"}}))
	body := bpsTestJSON(t, map[string]any{"access_token": "exchange-access", "refresh_token": "exchange-refresh", "expires_in": 3600, "id_token": id})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func bpsTestCallback(t *testing.T, h *Handler, state, redirect string) *httptest.ResponseRecorder {
	return bpsTestRequest(t, h.PostOAuthCallbackV8, "POST", "/v8/management/oauth/callback", bpsTestJSON(t, map[string]any{"provider": "bps", "state": state, "redirect_url": redirect}), nil)
}
func bpsWaitFlow(t *testing.T, flow *bpsOAuthSession) {
	t.Helper()
	select {
	case <-flow.done:
	case <-time.After(5 * time.Second):
		t.Fatal("OAuth flow did not finish")
	}
}

func TestBPSOAuthCallbackValidationAndCompletion(t *testing.T) {
	h, m := bpsTestHandler(t)
	state, flow := bpsTestStart(t, h, bpsManagementTransport(func(r *http.Request) (*http.Response, error) { return bpsTokenResponse(t, r) }))
	for _, redirect := range []string{bps.RedirectURI + "?state=other&code=code", bps.RedirectURI + "?state=" + state + "&code=code&state=" + state, bps.RedirectURI + "/extra?state=" + state + "&code=code", bps.RedirectURI + "?state=" + state + "&code=code&extra=x"} {
		if w := bpsTestCallback(t, h, state, redirect); w.Code != 400 {
			t.Fatalf("accepted invalid callback %d %s", w.Code, w.Body.String())
		}
	}
	w := bpsTestCallback(t, h, state, bps.RedirectURI+"?state="+state+"&code=code")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	bpsWaitFlow(t, flow)
	if len(m.List()) != 1 {
		t.Fatal("OAuth credential not registered")
	}
	status := bpsTestRequest(t, h.GetAuthStatus, "GET", "/v8/management/oauth/status?state="+state, "", nil)
	if ojson.NewDoc(status.Body.Bytes()).Get("status").String() != "ok" || !ojson.NewDoc(status.Body.Bytes()).Get("saved").Bool() {
		t.Fatal(status.Body.String())
	}
	if token := m.List()[0].Metadata["id_token"]; token != nil {
		t.Fatal("BPS persisted ID token")
	}
}

func TestBPSOAuthCallbackScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		extra  url.Values
		status int
		err    string
	}{
		{name: "single", extra: url.Values{"scope": {"openid offline_access email profile organization.read"}}, status: http.StatusOK},
		{name: "duplicate", extra: url.Values{"scope": {"openid", "email"}}, status: http.StatusBadRequest, err: "conflicting_callback_parameters"},
		{name: "unknown", extra: url.Values{"scope": {"openid"}, "extra": {"unknown"}}, status: http.StatusBadRequest, err: "invalid_callback_query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, manager := bpsTestHandler(t)
			var exchanges atomic.Int32
			state, flow := bpsTestStart(t, h, bpsManagementTransport(func(r *http.Request) (*http.Response, error) {
				exchanges.Add(1)
				return bpsTokenResponse(t, r)
			}))
			query := tc.extra
			query.Set("state", state)
			query.Set("code", "synthetic-code")
			w := bpsTestCallback(t, h, state, bps.RedirectURI+"?"+query.Encode())
			if w.Code != tc.status {
				t.Fatalf("callback status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status != http.StatusOK {
				if got := ojson.NewDoc(w.Body.Bytes()).Get("error").String(); got != tc.err {
					t.Fatalf("callback error %q, want %q", got, tc.err)
				}
				if exchanges.Load() != 0 || len(manager.List()) != 0 || flow.done != nil {
					t.Fatal("rejected callback started token exchange or registered credentials")
				}
				return
			}
			bpsWaitFlow(t, flow)
			if exchanges.Load() != 1 || len(manager.List()) != 1 {
				t.Fatal("callback did not exchange and register exactly once")
			}
			auth := manager.List()[0]
			raw, err := os.ReadFile(filepath.Join(h.cfg.AuthDir, auth.FileName))
			if err != nil {
				t.Fatal(err)
			}
			if ojson.NewDoc(raw).Get("refresh_token").String() != "exchange-refresh" {
				t.Fatal("OAuth refresh token not persisted")
			}
			files, err := os.ReadDir(h.cfg.AuthDir)
			if err != nil || len(files) != 1 {
				t.Fatalf("credential files=%d, error=%v", len(files), err)
			}
			status := bpsTestRequest(t, h.GetAuthStatus, "GET", "/v8/management/oauth/status?state="+state, "", nil)
			doc := ojson.NewDoc(status.Body.Bytes())
			if status.Code != http.StatusOK || doc.Get("status").String() != "ok" || !doc.Get("saved").Bool() || !doc.Get("adopted").Bool() {
				t.Fatal(status.Body.String())
			}
		})
	}
}

func TestBPSOAuthCancelPreventsSaveAndCommitRejectsCancel(t *testing.T) {
	for _, duringCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "exchange", true: "commit"}[duringCommit], func(t *testing.T) {
			h, m := bpsTestHandler(t)
			entered, release := make(chan struct{}), make(chan struct{})
			transport := bpsManagementTransport(func(r *http.Request) (*http.Response, error) {
				if !duringCommit {
					close(entered)
					<-release
				}
				return bpsTokenResponse(t, r)
			})
			if duringCommit {
				store := h.tokenStore
				h.tokenStore = &bpsTestStore{Store: store, save: func(ctx context.Context, a *coreauth.Auth) (string, error) {
					close(entered)
					<-release
					return store.Save(ctx, a)
				}}
			}
			state, flow := bpsTestStart(t, h, transport)
			if w := bpsTestCallback(t, h, state, bps.RedirectURI+"?state="+state+"&code=code"); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("flow did not reach barrier")
			}
			w := bpsTestRequest(t, h.CancelAuthSession, "DELETE", "/v8/management/oauth/session?state="+state, "", nil)
			want := 200
			if duringCommit {
				want = 409
			}
			if w.Code != want {
				t.Fatalf("cancel status %d, want %d: %s", w.Code, want, w.Body.String())
			}
			close(release)
			bpsWaitFlow(t, flow)
			count := 0
			if duringCommit {
				count = 1
			}
			if len(m.List()) != count {
				t.Fatal("cancellation violated commit boundary")
			}
		})
	}
}
