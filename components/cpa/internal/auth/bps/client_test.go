package bps

import (
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBPSPKCEAndProfile(t *testing.T) {
	state, verifier, challenge, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(verifier))
	if len(state) < 32 || len(verifier) < 43 || challenge != base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatal("invalid PKCE")
	}
	parsed, _ := url.Parse(AuthorizationURL(state, challenge))
	query := parsed.Query()
	for key, want := range map[string]string{"client_id": ClientID, "redirect_uri": RedirectURI, "state": state, "code_challenge": challenge, "code_challenge_method": "S256", "audience": "https://api.openai.com/v1", "platform": "PC"} {
		if query.Get(key) != want {
			t.Fatalf("%s = %q", key, query.Get(key))
		}
	}
}

func TestBPSRefreshRotationAndRetention(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain", true: "rotate"}[rotate], func(t *testing.T) {
			old := map[string]any{"client_id": ClientID, "profile": "excel", "account_id": "workspace", "email": "user@example.com", "refresh_token": "old-refresh", "note": "operator note", "disabled": true, "id_token": "do-not-keep"}
			transport := testTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != TokenURL {
					t.Fatalf("token endpoint = %s", r.URL)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("client_id") != ClientID || r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("grant_type") != "refresh_token" {
					t.Fatal("wrong refresh profile")
				}
				body := `{"access_token":"new-access","expires_in":3600}`
				if rotate {
					body = `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			got, err := NewClient("").Refresh(context.WithValue(t.Context(), "cliproxy.roundtripper", transport), old)
			if err != nil {
				t.Fatal(err)
			}
			want := "old-refresh"
			if rotate {
				want = "new-refresh"
			}
			if got["refresh_token"] != want || got["note"] != "operator note" || got["disabled"] != true || got["id_token"] != nil || old["access_token"] != nil {
				t.Fatalf("metadata lost or mutated: %#v", got)
			}
		})
	}
}

func TestBPSRejectsProfileAndSanitizesFailure(t *testing.T) {
	for _, meta := range []map[string]any{{"client_id": "codex", "refresh_token": "refresh"}, {"client_id": ClientID, "profile": "codex", "refresh_token": "refresh"}} {
		if _, err := NewClient("").Refresh(t.Context(), meta); err == nil {
			t.Fatal("accepted wrong client/profile")
		}
	}
	for _, status := range []int{400, 401, 429} {
		transport := testTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"SECRET_REFRESH"}`)), Header: make(http.Header)}, nil
		})
		_, err := NewClient("").Refresh(context.WithValue(t.Context(), "cliproxy.roundtripper", transport), map[string]any{"client_id": ClientID, "refresh_token": "refresh"})
		if err == nil || strings.Contains(err.Error(), "SECRET_REFRESH") {
			t.Fatal("unsanitized failure")
		}
		if err.(*OAuthError).StatusCode() != 401 {
			t.Fatal("invalid grant is not terminal")
		}
	}
}

func jwtForTest(payload string) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
}

func TestBPSClaimsAndRefreshedIdentity(t *testing.T) {
	for _, payload := range []string{`{"sub":"user"}`, `{"aud":"codex"}`} {
		if _, err := Claims(jwtForTest(payload)); err == nil {
			t.Fatal("accepted ID token without correct audience")
		}
	}
	token := jwtForTest(`{"aud":"` + ClientID + `","sub":"other","email":"user@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"workspace"}}`)
	claims, err := Claims(token)
	if err != nil || claims["account_id"] != "workspace" {
		t.Fatalf("identity = %#v, %v", claims, err)
	}
	transport := testTransport(func(*http.Request) (*http.Response, error) {
		doc := ojson.NewDoc([]byte(`{"access_token":"access","expires_in":3600}`))
		_ = doc.Set("id_token", token)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(doc.String())), Header: make(http.Header)}, nil
	})
	_, err = NewClient("").Refresh(context.WithValue(t.Context(), "cliproxy.roundtripper", transport), map[string]any{"client_id": ClientID, "refresh_token": "refresh", "sub": "original"})
	if err == nil {
		t.Fatal("refresh replaced identity")
	}
}

func TestBPSRefreshInvalidClientPreservesTerminalCode(t *testing.T) {
	transport := testTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_client","error_description":"SECRET"}`)), Header: make(http.Header)}, nil
	})
	_, err := NewClient("").Refresh(context.WithValue(t.Context(), "cliproxy.roundtripper", transport), map[string]any{"client_id": ClientID, "refresh_token": "refresh"})
	oauthErr, ok := err.(*OAuthError)
	if !ok || oauthErr.Code != "invalid_client" || oauthErr.StatusCode() != 400 || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("terminal error lost: %v", err)
	}
}

func TestBPSAtomicStoragePreservesMetadataKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	for _, token := range []string{"first", "second"} {
		s := &TokenStorage{Metadata: map[string]any{"access_token": token, "note.with.dot": "operator", "refresh_token": "keep"}}
		if err := s.SaveTokenToFile(path); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := ojson.NewDoc(raw)
	if doc.Get("access_token").String() != "second" || doc.Get("note\\.with\\.dot").String() != "operator" {
		t.Fatal(string(raw))
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary credential retained")
	}
}
