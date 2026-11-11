package auth

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestBPSToolContinuationPinsOriginalIdentity(t *testing.T) {
	ctx := WithSkipPersist(context.Background())
	cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(cfg)
	original, err := manager.Register(ctx, &Auth{ID: "original", Provider: "bps", Status: StatusActive, Metadata: map[string]any{"account_id": "workspace", "sub": "user", "access_token": "old"}})
	if err != nil {
		t.Fatal(err)
	}
	store := bpsbinding.Store{Dir: filepath.Join(config.BPSStateDirectory(cfg), "bindings")}
	record, err := store.Save("caller", "native", bpsbinding.Identity{AuthID: original.ID, AccountID: "workspace", Subject: "user"})
	if err != nil {
		t.Fatal(err)
	}
	opts := coreexecutor.Options{Metadata: map[string]any{coreexecutor.CallerScopeMetadataKey: "caller"}}
	req := coreexecutor.Request{Payload: []byte(fmt.Sprintf(`{"input":[{"type":"function_call_output","call_id":%q,"output":"ok"}]}`, record.CallID))}
	providers, bound, err := manager.bindBPSToolHistory([]string{"codex", "bps"}, req, opts)
	if err != nil || len(providers) != 1 || providers[0] != "bps" || pinnedAuthIDFromMetadata(bound.Metadata) != original.ID {
		t.Fatalf("not pinned: %v %+v %v", providers, bound, err)
	}
	if pinnedAuthIDFromMetadata(opts.Metadata) != "" {
		t.Fatal("mutated caller metadata")
	}
	_, _, err = manager.Modify(ctx, original.ID, original.RegistrationEpoch, func(a *Auth) error { a.Metadata["access_token"] = "refreshed"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.bindBPSToolHistory([]string{"bps"}, req, opts); err != nil {
		t.Fatalf("token refresh broke binding: %v", err)
	}
	_, _, err = manager.Modify(ctx, original.ID, original.RegistrationEpoch, func(a *Auth) error { a.Metadata["account_id"] = "replacement"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.bindBPSToolHistory([]string{"codex", "bps"}, req, opts); err == nil {
		t.Fatal("identity replacement accepted")
	}
	if _, _, err = manager.bindBPSToolHistory([]string{"codex"}, req, opts); err == nil {
		t.Fatal("BPS history switched to Codex")
	}
}
