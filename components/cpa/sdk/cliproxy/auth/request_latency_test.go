package auth

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/requestlatency"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestRequestLatencyActualCredentialAdmission(t *testing.T) {
	for _, mode := range []string{"released", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, _, ids := concurrencyFixture(t, nil)
				hold, err := m.AcquireCredentialGeneration(context.Background(), ids[0])
				if err != nil {
					t.Fatal(err)
				}
				defer hold()
				ctx, state := requestlatency.NewContext(context.Background())
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					result, e := m.ExecuteStream(ctx, []string{"claude"}, core.Request{Model: "test-model"}, core.Options{Metadata: map[string]any{core.PinnedAuthMetadataKey: ids[0]}})
					if result != nil {
						for range result.Chunks {
						}
					}
					done <- e
				}()
				synctest.Wait()
				m.concurrency.mu.Lock()
				waiting := m.concurrency.waiting
				m.concurrency.mu.Unlock()
				if waiting != 1 {
					t.Fatalf("not waiting at real capacity boundary: %d", waiting)
				}
				if mode == "canceled" {
					cancel()
				} else {
					hold()
				}
				err = <-done
				if mode == "canceled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel changed: %v", err)
				}
				if mode == "released" && err != nil {
					t.Fatal(err)
				}
				fields := state.Finish(err, 200)
				if fields["credential_wait_count"] != 1 {
					t.Fatalf("capacity hook: %v", fields)
				}
				if fields["selection_lock_wait_count"].(int) < 1 {
					t.Fatalf("selection hook: %v", fields)
				}
				if state.Finish(nil, 200) != nil {
					t.Fatal("duplicate finish")
				}
			})
		})
	}
}

func TestRequestLatencyHomeDoesNotInventLocalWait(t *testing.T) {
	m, _, _ := concurrencyFixture(t, nil)
	m.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
	ctx, state := requestlatency.NewContext(context.Background())
	_, _, _, lease, _ := m.pickNextMixedAdmitted(ctx, []string{"claude"}, "test-model", core.Options{}, nil)
	if lease != nil {
		lease.release()
	}
	fields := state.Finish(nil, 200)
	for _, key := range []string{"selection_lock_wait_ms", "credential_wait_ms"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("Home invented local stage: %v", fields)
		}
	}
}
