package oauthlogin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type auth bool

func (a auth) VerifyHeader(context.Context, string) (bool, error) { return bool(a), nil }
func TestAdminGate(t *testing.T) {
	h := &Handler{Auth: auth(false)}
	for _, path := range []string{"/api/oauth-login/tasks", "/api/oauth-login/tasks/id", "/api/oauth-login/oauth/callback"} {
		w := httptest.NewRecorder()
		h.Handle(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"password":"PASSWORD_SECRET"}`)))
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
		if strings.Contains(w.Body.String(), "PASSWORD_SECRET") {
			t.Fatal("密码泄露")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("未禁缓存")
		}
	}
}
