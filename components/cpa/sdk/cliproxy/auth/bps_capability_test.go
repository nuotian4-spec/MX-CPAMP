package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
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
		providers, err := filterBPSProviders(tc.ctx, []string{"codex", "bps"}, coreexecutor.Request{Payload: []byte(tc.payload)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
		if err != nil {
			t.Fatal(err)
		}
		if len(providers) != tc.count || providers[0] != "codex" {
			t.Fatalf("providers = %v", providers)
		}
	}
}

func TestBPSCapabilityFailurePreservesCompatibleRouting(t *testing.T) {
	for _, operation := range []string{"execute", "count", "stream"} {
		for _, scenario := range []string{"bps-only", "mixed", "empty", "compatible-unavailable"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				manager := NewManager(nil, nil, nil)
				bps := &selectedCapabilityCaptureExecutor{provider: "bps"}
				codex := &selectedCapabilityCaptureExecutor{provider: "codex"}
				manager.RegisterExecutor(bps)
				manager.RegisterExecutor(codex)
				providers := []string{"bps"}
				if scenario == "mixed" || scenario == "compatible-unavailable" {
					providers = append(providers, "codex")
				}
				if scenario == "empty" {
					providers = nil
				}
				for _, provider := range providers {
					if provider == "codex" && scenario == "compatible-unavailable" {
						continue
					}
					id := "bps-capability-" + provider
					if _, err := manager.Register(context.Background(), &Auth{ID: id, Provider: provider, Status: StatusActive}); err != nil {
						t.Fatal(err)
					}
					registry.GetGlobalRegistry().RegisterClient(id, provider, []*registry.ModelInfo{{ID: "gpt-5.6-sol", UserDefined: true}})
					defer registry.GetGlobalRegistry().UnregisterClient(id)
				}
				req := coreexecutor.Request{Model: "gpt-5.6-sol", Payload: []byte(`{"model":"gpt-5.6-sol","input":"Reply with OK.","max_output_tokens":128,"reasoning":{"effort":"low"},"stream":false}`)}
				opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}
				var err error
				switch operation {
				case "execute":
					_, err = manager.Execute(context.Background(), providers, req, opts)
				case "count":
					_, err = manager.ExecuteCount(context.Background(), providers, req, opts)
				case "stream":
					var stream *coreexecutor.StreamResult
					stream, err = manager.ExecuteStream(context.Background(), providers, req, opts)
					if stream != nil {
						for chunk := range stream.Chunks {
							if chunk.Err != nil {
								t.Fatal(chunk.Err)
							}
						}
					}
				}
				if len(bps.requests) != 0 {
					t.Fatal("unsupported request reached BPS")
				}
				if scenario == "mixed" {
					if err != nil || len(codex.requests) != 1 {
						t.Fatalf("compatible route: calls=%d error=%v", len(codex.requests), err)
					}
					return
				}
				var authErr *Error
				if !errors.As(err, &authErr) {
					t.Fatalf("expected typed error, got %v", err)
				}
				if scenario == "bps-only" {
					if authErr.StatusCode() != 400 || !authErr.IsRequestScoped() || authErr.Message != "BPS does not support max_output_tokens" {
						t.Fatalf("unsupported request error=%v", err)
					}
				} else if authErr.IsRequestScoped() || authErr.StatusCode() == 400 {
					t.Fatalf("availability failure changed to request error: %v", err)
				}
				if scenario == "empty" && authErr.Code != "provider_not_found" {
					t.Fatalf("empty-provider error changed: %v", err)
				}
			})
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
