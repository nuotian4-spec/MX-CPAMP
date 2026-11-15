package oauthlogin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func TestBPSOAuthTerminalDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		code, stage, outcome string
		submitted            bool
	}{
		{"oauth_exchange_failed", "token_exchange", "not_saved", true},
		{"oauth_identity_invalid", "credential_validation", "not_saved", true},
		{"oauth_target_identity_mismatch", "credential_validation", "not_saved", true},
		{"credential_commit_failed", "cpa_import", "unknown", true},
		{"unknown or expired state", "session", "unknown", true},
		{"unknown or expired state", "session", "not_submitted", false},
		{"SECRET_CANARY", "unknown", "unknown", true},
	} {
		t.Run(tc.code+tc.outcome, func(t *testing.T) {
			var statusCalls atomic.Int32
			cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveBuiltinBPSConfig(w, r) {
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "auth-url"):
					jsonReply(w, map[string]any{"url": "https://auth.invalid/SECRET_CANARY", "state": "state"})
				case strings.HasSuffix(r.URL.Path, "callback"):
					jsonReply(w, map[string]any{"status": "ok"})
				default:
					statusCalls.Add(1)
					if r.URL.Query().Get("provider") != "bps" {
						t.Error("missing provider")
					}
					jsonReply(w, map[string]any{"status": "error", "error": tc.code, "http_status": 400, "reason": "invalid_grant", "detail": "SECRET_CANARY", "stage": "SECRET_CANARY"})
				}
			}))
			defer cpa.Close()
			s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "key"}}, nil, "", "")
			ctx := context.Background()
			if _, err := s.OAuth(ctx, "POST", "auth-url", map[string]string{"provider": "bps"}); err != nil {
				t.Fatal(err)
			}
			if tc.submitted {
				if _, err := s.OAuth(ctx, "POST", "callback", map[string]string{"state": "state", "redirect_url": "https://callback.invalid/?state=state&code=SECRET_CANARY"}); err != nil {
					t.Fatal(err)
				}
			}
			out, err := s.OAuth(ctx, "GET", "status", map[string]string{"state": "state"})
			if err != nil {
				t.Fatal(err)
			}
			if out["stage"] != tc.stage || out["save_outcome"] != tc.outcome || out["reason"] != "invalid_grant" {
				t.Fatalf("outcome: %#v", out)
			}
			raw, _ := json.Marshal(out)
			for _, secret := range []string{"SECRET_CANARY", "state\"", "auth.invalid", "callback.invalid"} {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("leak: %s", raw)
				}
			}
			cacheExpected := tc.code != "unknown or expired state" && tc.code != "SECRET_CANARY"
			if cacheExpected && (s.sessions["state"].terminal == nil || s.sessions["state"].release != nil) {
				t.Fatal("missing terminal cache or retained lock")
			}
			if !cacheExpected && s.sessions["state"].terminal != nil {
				t.Fatal("未知结果不应阻止同会话只读核对")
			}
			if s.sessions["state"].release != nil {
				t.Fatal("非等待结果仍占用全局写锁")
			}
			// The first result may be lost between Manager and the browser.
			again, err := s.OAuth(ctx, "GET", "status", map[string]string{"state": "state"})
			wantCalls := int32(1)
			if !cacheExpected {
				wantCalls = 2
			}
			if err != nil || again["stage"] != tc.stage || again["save_outcome"] != tc.outcome || statusCalls.Load() != wantCalls {
				t.Fatal("terminal reread lost diagnostics", again, err)
			}
			if cacheExpected || tc.submitted {
				if _, err := s.OAuth(ctx, "POST", "callback", map[string]string{"state": "state", "redirect_url": "https://callback.invalid/?state=state&code=SECRET_CANARY"}); !errors.Is(err, ErrBusy) {
					t.Fatal("terminal permitted callback")
				}
			}
			cached, _ := json.Marshal(s.sessions["state"].terminal)
			for _, secret := range []string{"SECRET_CANARY", "state\"", "url", "code_verifier", "redirect_url"} {
				if strings.Contains(string(cached), secret) {
					t.Fatal("terminal cache leaked material", string(cached))
				}
			}
			s.sessions["state"].created = time.Now().Add(-26 * time.Minute)
			if _, err := s.OAuth(ctx, "GET", "status", map[string]string{"state": "state"}); !errors.Is(err, ErrNotFound) {
				t.Fatal("expired terminal readable")
			}
		})
	}
}

func TestWorkerHTTPDiagnosticsNormalizeAndBatch(t *testing.T) {
	for _, tc := range []struct {
		input             string
		status            int
		endpoint, subtype string
		header            *bool
	}{
		{`{}`, 0, "unknown", "unknown", nil},
		{`{"http_status":403,"endpoint_category":"chatgpt_home","security_check_subtype":"html_forbidden_unclassified","challenge_header_present":false}`, 403, "chatgpt_home", "html_forbidden_unclassified", boolPointer(false)},
		{`{"http_status":401,"endpoint_category":"oauth_token","security_check_subtype":"challenge_header","challenge_header_present":true}`, 401, "oauth_token", "challenge_header", boolPointer(true)},
		{`{"http_status":"SECRET_CANARY","endpoint_category":{"secret":"SECRET_CANARY"},"security_check_subtype":"SECRET_CANARY","challenge_header_present":"SECRET_CANARY"}`, 0, "unknown", "unknown", nil},
		{`{"http_status":600,"challenge_header_present":null}`, 0, "unknown", "unknown", nil},
		{`{"http_status":403.5}`, 0, "unknown", "unknown", nil},
	} {
		var raw workerTask
		if err := json.Unmarshal([]byte(tc.input), &raw); err != nil {
			t.Fatal(err)
		}
		d := raw.rawHTTPDiagnostics.safe()
		if d.EndpointCategory != tc.endpoint || d.SecurityCheckSubtype != tc.subtype {
			t.Fatalf("diagnostic: %#v", d)
		}
		if tc.status == 0 && d.HTTPStatus != nil || tc.status != 0 && (d.HTTPStatus == nil || *d.HTTPStatus != tc.status) {
			t.Fatal("invalid status")
		}
		if tc.header == nil && d.ChallengeHeaderPresent != nil || tc.header != nil && (d.ChallengeHeaderPresent == nil || *d.ChallengeHeaderPresent != *tc.header) {
			t.Fatal("invalid header")
		}
		b := batchTaskSnapshot(&Task{HTTPDiagnostics: d, Status: "needs_user_action"})
		encoded, _ := json.Marshal(b)
		if strings.Contains(string(encoded), "SECRET_CANARY") || !strings.Contains(string(encoded), `"http_status":`) {
			t.Fatal(string(encoded))
		}
	}
}

func boolPointer(value bool) *bool { return &value }

func TestOAuthHTTPFailureDiagnostics(t *testing.T) {
	got := OAuthRequestDiagnostic("auth-url", &upstreamError{status: 401, code: "SECRET_CANARY"})
	if got["error_code"] != "oauth_start_failed" || got["save_outcome"] != "not_submitted" {
		t.Fatal(got)
	}
	if *got["http_status"].(*int) != 401 {
		t.Fatal(got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "SECRET_CANARY") {
		t.Fatal(string(encoded))
	}
}

func TestCodexOAuthTerminalCleanupUnchanged(t *testing.T) {
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveBuiltinBPSConfig(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "auth-url") {
			jsonReply(w, map[string]any{"url": "https://auth.invalid/", "state": "state"})
			return
		}
		jsonReply(w, map[string]any{"status": "error", "error": "SECRET_CANARY"})
	}))
	defer cpa.Close()
	s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "key"}}, nil, "", "")
	ctx := context.Background()
	if _, err := s.OAuth(ctx, "POST", "auth-url", map[string]string{"provider": "codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OAuth(ctx, "GET", "status", map[string]string{"state": "state"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OAuth(ctx, "GET", "status", map[string]string{"state": "state"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("changed non-BPS terminal cleanup", err)
	}
}
