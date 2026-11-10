package auth

import (
	"context"
	"testing"

	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSProviderFilterPreservesCodex(t *testing.T) {
	for _, tc := range []struct {
		ctx     context.Context
		payload string
		count   int
	}{
		{context.Background(), `{"input":"hi"}`, 2},
		{context.Background(), `{"input":"hi","previous_response_id":"resp_old"}`, 1},
		{context.Background(), `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, 1},
		{coreexecutor.WithDownstreamWebsocket(context.Background()), `{"input":"hi"}`, 1},
	} {
		providers := filterBPSProviders(tc.ctx, []string{"codex", "bps"}, coreexecutor.Request{Payload: []byte(tc.payload)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
		if len(providers) != tc.count || providers[0] != "codex" {
			t.Fatalf("providers = %v", providers)
		}
	}
}

func TestBPSMixedUsesExistingAccountSlots(t *testing.T) {
	scheduler := newSchedulerForTest(&RoundRobinSelector{},
		&Auth{ID: "codex-a", Provider: "codex"},
		&Auth{ID: "codex-b", Provider: "codex"},
		&Auth{ID: "bps-a", Provider: "bps"},
	)
	counts := map[string]int{}
	for i := 0; i < 30; i++ {
		auth, provider, err := scheduler.pickMixed(context.Background(), []string{"codex", "bps"}, "", coreexecutor.Options{}, nil)
		if err != nil || auth == nil {
			t.Fatalf("mixed selection: %v", err)
		}
		counts[provider]++
	}
	if counts["codex"] != 20 || counts["bps"] != 10 {
		t.Fatalf("account slot ratio changed: %v", counts)
	}
}

func TestBPSReplayBoundaryDoesNotSwitchProviders(t *testing.T) {
	for _, tc := range []struct {
		providers []string
		payload   string
		pinned    bool
		fail      bool
	}{
		{[]string{"codex", "bps"}, `{"input":"hi","previous_response_id":"resp_bps_native"}`, false, true},
		{[]string{"codex"}, `{"input":"hi","previous_response_id":"resp_bps_native"}`, true, true},
		{[]string{"codex", "bps"}, `{"input":"hi","previous_response_id":"resp_unknown"}`, false, true},
		{[]string{"codex", "bps"}, `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, false, true},
		{[]string{"codex", "bps"}, `{"input":[{"type":"function_call_output","call_id":"old"}]}`, false, true},
		{[]string{"codex", "bps"}, `{"input":"hi","previous_response_id":"resp_codex"}`, true, false},
		{[]string{"codex"}, `{"input":"hi","previous_response_id":"resp_codex"}`, false, false},
		{[]string{"codex", "bps"}, `{"input":"hi"}`, false, false},
	} {
		opts := coreexecutor.Options{}
		if tc.pinned {
			opts.Metadata = map[string]any{coreexecutor.PinnedAuthMetadataKey: "original-account"}
		}
		err := validateBPSReplayBoundary(tc.providers, coreexecutor.Request{Payload: []byte(tc.payload)}, opts)
		if (err != nil) != tc.fail {
			t.Fatalf("boundary error=%v, request=%s", err, tc.payload)
		}
	}
}

func TestBPSStaleRefreshCannotOverwriteRelogin(t *testing.T) {
	ctx := WithSkipPersist(context.Background())
	manager := NewManager(nil, nil, nil)
	base, err := manager.Register(ctx, &Auth{ID: "bps-stale-refresh", Provider: "bps", Status: StatusActive, Metadata: map[string]any{"type": "bps", "access_token": "old-access", "refresh_token": "old-refresh", "notes": "operator"}})
	if err != nil {
		t.Fatal(err)
	}
	refreshed := base.Clone()
	refreshed.Metadata["access_token"] = "stale-access"
	refreshed.Metadata["refresh_token"] = "stale-refresh"
	_, found, err := manager.Modify(ctx, base.ID, base.RegistrationEpoch, func(current *Auth) error {
		current.Metadata["access_token"] = "relogin-access"
		current.Metadata["refresh_token"] = "relogin-refresh"
		return nil
	})
	if err != nil || !found {
		t.Fatalf("relogin modify: %v", err)
	}
	result, err := manager.UpdateRefreshedAuth(ctx, base, refreshed)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["access_token"] != "relogin-access" || result.Metadata["refresh_token"] != "relogin-refresh" || result.Metadata["notes"] != "operator" {
		t.Fatal("stale refresh replaced relogin or metadata")
	}
}
