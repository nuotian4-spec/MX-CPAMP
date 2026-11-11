package management

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestBPSFlattenedAccountIdentityListing(t *testing.T) {
	h := &Handler{}
	for _, tc := range []struct {
		name, provider, accountJSON string
		want                        string
	}{
		{"flat bps", "bps", `"synthetic-bps-account"`, "synthetic-bps-account"},
		{"empty", "bps", `""`, ""},
		{"missing", "bps", `null`, ""},
		{"bad type", "bps", `17`, ""},
		{"other provider", "claude", `"synthetic-bps-account"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "flattened.json")
			raw := []byte(`{"type":"` + tc.provider + `","profile":"excel","client_id":"app_fnr0pYvVwwFDocDumLG3H2Bp","account_id":` + tc.accountJSON + `,"email":"synthetic@example.invalid","sub":"PRIVATE_SUB","access_token":"PRIVATE_ACCESS","refresh_token":"PRIVATE_REFRESH"}`)
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var metadata map[string]any
			if err := json.Unmarshal(stored, &metadata); err != nil {
				t.Fatal(err)
			}
			auth := &coreauth.Auth{ID: "flattened.json", FileName: "flattened.json", Provider: tc.provider, Status: coreauth.StatusActive, Attributes: map[string]string{"path": file}, Metadata: metadata}
			for _, v8 := range []bool{false, true} {
				entry := h.buildAuthFileEntry(auth, authFileQuotaContext{v8: v8})
				if tc.want != "" && entry["account_id"] != tc.want {
					t.Fatal("平铺 BPS 账号身份未发布")
				}
				if tc.want == "" {
					if _, exists := entry["account_id"]; exists {
						t.Fatal("缺失身份被替代")
					}
				}
				encoded, _ := json.Marshal(entry)
				if strings.Contains(string(encoded), "PRIVATE_") || entry["id_token"] != nil {
					t.Fatal("列表泄露原始凭证")
				}
			}
		})
	}
}
