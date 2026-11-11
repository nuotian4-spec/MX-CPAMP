package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestBPSFileStoreReloadAndSave(t *testing.T) {
	dir := t.TempDir()
	store := &FileTokenStore{}
	store.SetBaseDir(dir)
	auth := &coreauth.Auth{ID: "bps-test.json", FileName: "bps-test.json", Provider: "bps", Metadata: map[string]any{"type": "bps", "access_token": "first", "refresh_token": "refresh-first", "account_id": "account", "email": "test@example.com", "login_method": "2fa"}}
	if _, err := store.Save(context.Background(), auth); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.List(context.Background())
	if err != nil || len(loaded) != 1 {
		t.Fatalf("reload: %v, %d", err, len(loaded))
	}
	loaded[0].Metadata["access_token"] = "rotated"
	loaded[0].Metadata["refresh_token"] = "refresh-rotated"
	if _, err = store.Save(context.Background(), loaded[0]); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, auth.ID))
	if err != nil {
		t.Fatal(err)
	}
	if ojson.GetBytes(data, "access_token").String() != "rotated" || ojson.GetBytes(data, "refresh_token").String() != "refresh-rotated" || ojson.GetBytes(data, "login_method").String() != "2fa" {
		t.Fatal("BPS persisted fields lost after reload")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".bps-credential-*"))
	if len(leftovers) != 0 {
		t.Fatal("temporary BPS credentials left behind")
	}
}
