package bpssettings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type bpsAdmin bool

func (a bpsAdmin) VerifyHeader(context.Context, string) (bool, error) { return bool(a), nil }
func TestBPSSettingsRequiresManagerAdmin(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "POST"} {
		w := httptest.NewRecorder()
		(&Handler{Auth: bpsAdmin(false)}).Handle(w, httptest.NewRequest(method, "/api/bps/settings", strings.NewReader(`{"admin_secret":"CANARY"}`)))
		if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "CANARY") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestBPSSettingsRejectsUnknownBodyBeforeService(t *testing.T) {
	for _, body := range []string{`{"enabled":true,"images_enabled":true,"image_transport":"native","image_relay_base_url":"","admin_secret":"CANARY"}`, `{} {}`} {
		w := httptest.NewRecorder()
		(&Handler{Auth: bpsAdmin(true)}).Handle(w, httptest.NewRequest(http.MethodPut, "/api/bps/settings", strings.NewReader(body)))
		if w.Code != 400 || strings.Contains(w.Body.String(), "CANARY") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
