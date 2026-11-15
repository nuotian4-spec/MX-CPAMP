package management

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/bps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func bpsAssertDiagnostic(t *testing.T, h *Handler, state, code, stage, reason, outcome string, httpStatus int) {
	t.Helper()
	w := bpsTestRequest(t, h.GetAuthStatusV8, http.MethodGet, "/v8/management/oauth/status?provider=bps&state="+state, "", nil)
	doc := ojson.NewDoc(w.Body.Bytes())
	for key, want := range map[string]string{"status": "error", "error": code, "error_code": code, "stage": stage, "reason": reason, "save_outcome": outcome} {
		if doc.Get(key).String() != want {
			t.Fatalf("%s=%q, want %q: %s", key, doc.Get(key).String(), want, w.Body.String())
		}
	}
	if got := doc.Get("http_status").Raw; (httpStatus == 0 && got != "null") || (httpStatus != 0 && got != fmt.Sprint(httpStatus)) {
		t.Fatalf("http_status=%s, want %d or null", got, httpStatus)
	}
	if strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "https://") {
		t.Fatal("diagnostic leaked sensitive data")
	}
	if doc.Get("saved").Exists() || doc.Get("registered").Exists() {
		t.Fatal("diagnostic fabricated persistence evidence")
	}
}

func TestBPSOAuthExchangeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		upstream, reported int
		transportError     bool
		bodyError          bool
	}{
		{name: "invalid_grant", body: `{"error":"invalid_grant","error_description":"SECRET_CODE"}`, reason: "invalid_grant", upstream: 400, reported: 400},
		{name: "invalid_client", body: `{"error":"invalid_client","error_description":"SECRET_CLIENT"}`, reason: "invalid_client", upstream: 400, reported: 400},
		{name: "invalid_client_401", body: `{"error":"invalid_client","error_description":"SECRET_CLIENT"}`, reason: "invalid_client", upstream: 401, reported: 401},
		{name: "unknown_401", body: `{"error":"SECRET_REASON"}`, reason: "upstream_error", upstream: 401, reported: 401},
		{name: "upstream", body: `{"error":"SECRET_UPSTREAM","error_description":"https://SECRET_URL"}`, reason: "upstream_error", upstream: 502, reported: 502},
		{name: "transport", reason: "transport_error", transportError: true},
		{name: "read_body", reason: "transport_error", upstream: 200, reported: 200, bodyError: true},
		{name: "read_body_502", reason: "transport_error", upstream: 502, reported: 502, bodyError: true},
		{name: "malformed", body: `SECRET_RESPONSE`, reason: "protocol_error", upstream: 200, reported: 200},
		{name: "incomplete_tokens", body: `{"access_token":"SECRET_ACCESS"}`, reason: "protocol_error", upstream: 200, reported: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, manager := bpsTestHandler(t)
			state, flow := bpsTestStart(t, h, bpsManagementTransport(func(*http.Request) (*http.Response, error) {
				if tc.transportError {
					return nil, errors.New("SECRET_PROXY https://SECRET_URL?token=SECRET_TOKEN")
				}
				body := io.NopCloser(strings.NewReader(tc.body))
				if tc.bodyError {
					body = io.NopCloser(iotest.ErrReader(errors.New("SECRET_READ_ERROR")))
				}
				return &http.Response{StatusCode: tc.upstream, Body: body, Header: make(http.Header)}, nil
			}))
			if w := bpsTestCallback(t, h, state, bps.RedirectURI+"?state="+state+"&code=SECRET_CODE"); w.Code != http.StatusOK {
				t.Fatal(w.Body.String())
			}
			bpsWaitFlow(t, flow)
			bpsAssertDiagnostic(t, h, state, "oauth_exchange_failed", "token_exchange", tc.reason, "not_saved", tc.reported)
			if len(manager.List()) != 0 {
				t.Fatal("failed exchange saved credentials")
			}
			legacy := bpsTestRequest(t, h.GetAuthStatus, http.MethodGet, "/v0/management/get-auth-status?state="+state, "", nil)
			if legacy.Body.String() != `{"error":"oauth_exchange_failed","status":"error"}` {
				t.Fatalf("legacy contract changed: %s", legacy.Body.String())
			}
		})
	}
}

func TestBPSOAuthAuthorizationAndIdentityDiagnostics(t *testing.T) {
	t.Run("authorization", func(t *testing.T) {
		h, _ := bpsTestHandler(t)
		state, _ := bpsTestStart(t, h, bpsManagementTransport(func(*http.Request) (*http.Response, error) {
			t.Error("authorization failure attempted exchange")
			return nil, errors.New("unexpected exchange")
		}))
		q := url.Values{"state": {state}, "error": {"SECRET_ERROR"}, "error_description": {"SECRET_DESCRIPTION"}}
		if w := bpsTestCallback(t, h, state, bps.RedirectURI+"?"+q.Encode()); w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		bpsAssertDiagnostic(t, h, state, "oauth_authorization_failed", "authorize", "unknown", "not_saved", 0)
	})
	t.Run("identity", func(t *testing.T) {
		h, _ := bpsTestHandler(t)
		state, flow := bpsTestStart(t, h, bpsManagementTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"SECRET_ACCESS","refresh_token":"SECRET_REFRESH","expires_in":3600}`)), Header: make(http.Header)}, nil
		}))
		if w := bpsTestCallback(t, h, state, bps.RedirectURI+"?state="+state+"&code=SECRET_CODE"); w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		bpsWaitFlow(t, flow)
		bpsAssertDiagnostic(t, h, state, "oauth_identity_invalid", "credential_validation", "protocol_error", "not_saved", 0)
	})
	t.Run("target_identity", func(t *testing.T) {
		h, _ := bpsTestHandler(t)
		state, flow := bpsTestStart(t, h, bpsManagementTransport(func(r *http.Request) (*http.Response, error) { return bpsTokenResponse(t, r) }))
		flow.target = &oauthRecoveryTarget{AuthID: "missing-target"}
		if w := bpsTestCallback(t, h, state, bps.RedirectURI+"?state="+state+"&code=SECRET_CODE"); w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		bpsWaitFlow(t, flow)
		bpsAssertDiagnostic(t, h, state, "oauth_target_identity_mismatch", "credential_validation", "protocol_error", "not_saved", 0)
	})
}

func TestBPSOAuthPostPersistFailureHasUnknownSaveOutcome(t *testing.T) {
	h, manager := bpsTestHandler(t)
	var savedPath string
	store := h.tokenStore
	h.tokenStore = &bpsTestStore{Store: store, save: func(ctx context.Context, record *coreauth.Auth) (string, error) {
		path, err := store.Save(ctx, record)
		savedPath = path
		return path, err
	}}
	h.postAuthPersistHook = func(context.Context, *coreauth.Auth) error {
		return errors.New("SECRET_PERSIST_HOOK https://SECRET_URL")
	}
	state, flow := bpsTestStart(t, h, bpsManagementTransport(func(r *http.Request) (*http.Response, error) { return bpsTokenResponse(t, r) }))
	if w := bpsTestCallback(t, h, state, bps.RedirectURI+"?state="+state+"&code=SECRET_CODE"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	bpsWaitFlow(t, flow)
	if savedPath == "" || len(manager.List()) != 1 {
		t.Fatal("test did not reach successful persistence and registration")
	}
	if _, err := os.Stat(savedPath); err != nil {
		t.Fatal("credential was not persisted")
	}
	if filepath.Dir(savedPath) != h.cfg.AuthDir {
		t.Fatal("unexpected persistence location")
	}
	bpsAssertDiagnostic(t, h, state, "credential_commit_failed", "cpa_import", "unknown", "unknown", 0)
}

func TestBPSOAuthSessionDiagnosticsAreBoundedAndExpire(t *testing.T) {
	h, _ := bpsTestHandler(t)
	for _, mode := range []string{"missing", "expired", "unknown_error"} {
		t.Run(mode, func(t *testing.T) {
			state := "bps-diagnostics-" + mode
			if mode != "missing" {
				RegisterOAuthSession(state, "bps")
				oauthSessions.mu.Lock()
				session := oauthSessions.sessions[state]
				if mode == "expired" {
					d := newBPSOAuthDiagnostic("credential_commit_failed", "unknown", 0)
					session.BPSDiagnostic = &d
					session.ExpiresAt = time.Now().Add(-time.Minute)
				} else {
					session.Status = "SECRET_ERROR https://SECRET_URL"
				}
				oauthSessions.sessions[state] = session
				oauthSessions.mu.Unlock()
				t.Cleanup(func() {
					oauthSessions.mu.Lock()
					delete(oauthSessions.sessions, state)
					oauthSessions.mu.Unlock()
				})
			}
			code, stage := "session_missing_or_expired", "session"
			if mode == "unknown_error" {
				code, stage = "unknown", "unknown"
			}
			bpsAssertDiagnostic(t, h, state, code, stage, "unknown", "unknown", 0)
			if mode == "expired" {
				if _, ok := oauthSessions.Get(state); ok {
					t.Fatal("expired diagnostic session was retained")
				}
			}
		})
	}
	for _, invalid := range []int{-1, 0, 99, 600, 999} {
		d := newBPSOAuthDiagnostic("SECRET_CODE", "SECRET_REASON", invalid)
		if d.ErrorCode != "unknown" || d.Reason != "unknown" || d.Stage != "unknown" || d.HTTPStatus != nil || d.SaveOutcome != "unknown" {
			t.Fatal("unbounded diagnostic")
		}
	}
	d := bpsExchangeDiagnostic(errors.New("BPS OAuth transport failed SECRET_TOKEN"))
	if d.Reason != "unknown" {
		t.Fatal("classifier accepted a non-exact error message")
	}
	d = bpsExchangeDiagnostic(fmt.Errorf("SECRET_WRAPPER: %w", &bps.OAuthError{Code: "SECRET_CODE"}))
	if d.Reason != "unknown" || d.HTTPStatus != nil {
		t.Fatal("classifier accepted unlisted typed error fields")
	}
}

func TestBPSOAuthStatusV8PreservesOtherProvidersAndPending(t *testing.T) {
	h, _ := bpsTestHandler(t)
	for _, provider := range []string{"bps", "codex"} {
		state := "bps-diagnostics-pending-" + provider
		RegisterOAuthSession(state, provider)
		t.Cleanup(func() { CancelOAuthSession(state) })
		w := bpsTestRequest(t, h.GetAuthStatusV8, http.MethodGet, "/v8/management/oauth/status?provider="+provider+"&state="+state, "", nil)
		if w.Body.String() != `{"status":"wait"}` {
			t.Fatal(w.Body.String())
		}
	}
	w := bpsTestRequest(t, h.GetAuthStatusV8, http.MethodGet, "/v8/management/oauth/status?provider=codex&state=missing-codex", "", nil)
	if w.Body.String() != `{"error":"unknown or expired state","status":"error"}` {
		t.Fatalf("other provider contract changed: %s", w.Body.String())
	}
}

func TestBPSOAuthStatusV8CompletedAndInvalidState(t *testing.T) {
	h, _ := bpsTestHandler(t)
	state, flow := bpsTestStart(t, h, bpsManagementTransport(func(r *http.Request) (*http.Response, error) { return bpsTokenResponse(t, r) }))
	if w := bpsTestCallback(t, h, state, bps.RedirectURI+"?state="+state+"&code=synthetic-code"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	bpsWaitFlow(t, flow)
	w := bpsTestRequest(t, h.GetAuthStatusV8, http.MethodGet, "/v8/management/oauth/status?provider=bps&state="+state, "", nil)
	doc := ojson.NewDoc(w.Body.Bytes())
	if doc.Get("status").String() != "ok" || !doc.Get("saved").Bool() || !doc.Get("adopted").Bool() || doc.Get("error_code").Exists() {
		t.Fatal(w.Body.String())
	}
	bpsAssertDiagnostic(t, h, "", "session_missing_or_expired", "session", "unknown", "unknown", 0)
	w = bpsTestRequest(t, h.GetAuthStatusV8, http.MethodGet, "/v8/management/oauth/status?provider=bps&state=SECRET%2FSTATE", "", nil)
	if w.Code != http.StatusBadRequest || ojson.NewDoc(w.Body.Bytes()).Get("error_code").String() != "unknown" || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatal(w.Body.String())
	}
}
