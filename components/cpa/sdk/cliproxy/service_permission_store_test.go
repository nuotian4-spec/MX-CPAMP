package cliproxy

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestBPSPermissionStoreWithoutTimedCooldownPersistence(t *testing.T) {
	cfg := &config.Config{AuthDir: t.TempDir(), SaveCooldownStatus: false}
	s := &Service{}
	store := s.resolveCooldownStateStore(cfg)
	if store == nil {
		t.Fatal("BPS permission store disabled with ordinary cooldowns")
	}
	records := []coreauth.CooldownStateRecord{{AuthID: "bps-test", Provider: "bps", PermissionEpoch: 3}, {AuthID: "codex-control", Provider: "codex", NextRetryAfter: time.Now().Add(time.Hour)}}
	if err := store.Save(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || len(loaded) != 1 || loaded[0].Provider != "bps" {
		t.Fatalf("ordinary provider persistence changed: %+v %v", loaded, err)
	}
	disabled := false
	cfg.BPSProtocol.Enabled = &disabled
	if s.resolveCooldownStateStore(cfg) != nil {
		t.Fatal("disabled BPS changed ordinary persistence")
	}
}
