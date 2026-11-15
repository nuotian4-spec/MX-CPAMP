package auth

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func kiloBudgetFixture(t *testing.T) (*Manager, context.Context, core.Request, core.Options, string) {
	t.Helper()
	cfg := &config.Config{AuthDir: filepath.Join(t.TempDir(), "auths")}
	m := NewManager(nil, nil, nil)
	m.SetConfig(cfg)
	ctx := withConcurrencyBudget(context.Background())
	req := core.Request{Model: "gpt-6.1-sol", Payload: []byte(`{"input":"new"}`)}
	opts := core.Options{Headers: http.Header{"Session-Id": []string{"budget-key"}}, Metadata: map[string]any{core.CallerScopeMetadataKey: "synthetic-caller"}}
	key := filepath.Join(config.KiloStateDirectory(cfg), "bindings") + "\x00synthetic-caller\x00budget-key"
	return m, ctx, req, opts, key
}
func TestKiloBindingGateConsumesSharedBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, ctx, req, opts, key := kiloBudgetFixture(t)
		hold, err := kilobinding.Lock(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		defer hold()
		type result struct {
			ctx     context.Context
			release func()
			err     error
		}
		done := make(chan result, 1)
		go func() {
			next, _, release, err := m.bindKiloHistory(ctx, []string{"kilo"}, req, opts)
			done <- result{next, release, err}
		}()
		synctest.Wait()
		time.Sleep(25 * time.Second)
		hold()
		synctest.Wait()
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		defer r.release()
		budget := ctx.Value(concurrencyBudgetKey{}).(*concurrencyBudget)
		if budget.spent != 25*time.Second {
			t.Fatalf("gate spent=%s", budget.spent)
		}
		if _, limited := r.ctx.Deadline(); limited {
			t.Fatal("gate deadline leaked into execution context")
		}
		started := time.Now()
		err = m.waitCredentialCapacity(r.ctx, make(chan struct{}), "original-auth")
		if !isConcurrencyError(err) || time.Since(started) != 5*time.Second || budget.spent != 30*time.Second {
			t.Fatalf("account wait=%s spent=%s error=%v", time.Since(started), budget.spent, err)
		}
		if m.concurrency.waiting != 0 || len(m.concurrency.pinned) != 0 {
			t.Fatal("waiting counters not released")
		}
		time.Sleep(time.Minute)
		if r.ctx.Err() != nil {
			t.Fatal("admission timeout canceled execution context")
		}
		t.Log("virtual gate25s + account5s = one30s budget; no downstream execution deadline")
	})
}
func TestKiloBindingGateTimeoutAndCancellationSettle(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, ctx, req, opts, key := kiloBudgetFixture(t)
				hold, err := kilobinding.Lock(ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				defer hold()
				budget := ctx.Value(concurrencyBudgetKey{}).(*concurrencyBudget)
				if mode == "timeout" {
					budget.spent = 25 * time.Second
					started := time.Now()
					_, _, release, err := m.bindKiloHistory(ctx, []string{"kilo"}, req, opts)
					release()
					if !isConcurrencyError(err) || time.Since(started) != 5*time.Second || budget.spent != 30*time.Second {
						t.Fatalf("timeout spent=%s err=%v", budget.spent, err)
					}
				} else {
					canceled, cancel := context.WithCancel(ctx)
					done := make(chan error, 1)
					go func() {
						_, _, release, err := m.bindKiloHistory(canceled, []string{"kilo"}, req, opts)
						release()
						done <- err
					}()
					synctest.Wait()
					time.Sleep(7 * time.Second)
					cancel()
					synctest.Wait()
					if err := <-done; !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel=%v", err)
					}
					if budget.spent != 7*time.Second {
						t.Fatalf("cancel spent=%s", budget.spent)
					}
				}
				if m.concurrency.waiting != 0 || len(m.concurrency.pinned) != 0 {
					t.Fatal("gate wait counters leaked")
				}
				hold()
				again, err := kilobinding.Lock(context.Background(), key)
				if err != nil {
					t.Fatal(err)
				}
				again()
			})
		})
	}
}
