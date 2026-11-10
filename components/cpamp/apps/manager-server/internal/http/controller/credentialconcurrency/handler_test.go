package credentialconcurrency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type auth bool

func (a auth) VerifyHeader(context.Context, string) (bool, error) { return bool(a), nil }
func TestAdminGateAndNarrowBody(t *testing.T) {
	for _, method := range []string{"GET", "PATCH", "POST"} {
		w := httptest.NewRecorder()
		(&Handler{Auth: auth(false)}).Handle(w, httptest.NewRequest(method, "/api/credential-concurrency", strings.NewReader(`{"token":"SECRET"}`)))
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{"auth_id":"a","concurrency_limit":20,"api_key":"SECRET"}`, `{} {}`} {
		w := httptest.NewRecorder()
		(&Handler{Auth: auth(true)}).Handle(w, httptest.NewRequest("PATCH", "/api/credential-concurrency", strings.NewReader(body)))
		if w.Code != 400 || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
