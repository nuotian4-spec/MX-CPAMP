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
	for _, path := range []string{"/api/oauth-login/tasks", "/api/oauth-login/tasks/id", "/api/oauth-login/oauth/callback", "/api/oauth-login/batches", "/api/oauth-login/batches/active", "/api/oauth-login/batches/id"} {
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

func TestBatchDecodeLimitAndUnknownFields(t *testing.T) {
	for _, body := range []string{`{"password":"PASSWORD_SECRET","unexpected":true}`, `{"padding":"` + strings.Repeat("x", 1024*1024) + `"}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/oauth-login/batches", strings.NewReader(body))
		var input struct {
			Password string `json:"password"`
			Padding  string `json:"padding"`
		}
		if decodeLimit(w, r, &input, 1024*1024) || w.Code != 400 || strings.Contains(w.Body.String(), "PASSWORD_SECRET") {
			t.Fatal("批次输入边界未生效")
		}
	}
}
