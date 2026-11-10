package management

import (
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestBPSLoginSummaryIsV8Only(t *testing.T) {
	h := &Handler{}
	for _, provider := range []string{"codex", "bps"} {
		t.Run(provider, func(t *testing.T) {
			profile := "codex"
			if provider == "bps" {
				profile = "excel"
			}
			auth := &coreauth.Auth{ID: provider + "-list", Provider: provider, Status: coreauth.StatusActive, Attributes: map[string]string{"runtime_only": "true"}, Metadata: map[string]any{"type": provider, "profile": profile, "login_method": "2fa", "access_token": "private-access", "refresh_token": "private-refresh"}}
			entry := h.buildAuthFileEntry(auth, authFileQuotaContext{v8: true})
			if entry["login_method"] != "2fa" || entry["profile"] != profile || entry["provider"] != provider {
				t.Fatalf("login summary missing: %#v", entry)
			}
			for _, key := range []string{"access_token", "refresh_token"} {
				if _, ok := entry[key]; ok {
					t.Fatalf("credential exposed: %s", key)
				}
			}
			legacy := h.buildAuthFileEntry(auth, authFileQuotaContext{})
			for _, key := range []string{"login_method", "profile"} {
				if _, ok := legacy[key]; ok {
					t.Fatalf("v0 response changed: %s", key)
				}
			}
		})
	}
}
