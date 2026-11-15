package auth

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// This virtual-time test supplements the native Router/Executor/Transport
// tests with the exact remaining admission budget; it sends no generation.
func TestKiloM8DiagnosticUsesRemainingAdmissionBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, e, _, ctx, model, ids := noReplayFixture(t, 200, "execute")
		a, _ := m.GetByID(ids[0])
		a.Metadata[KiloMachineIDKey] = uuid.NewString()
		a.Disabled = true
		if _, err := m.Update(ctx, a); err != nil {
			t.Fatal(err)
		}
		a, _ = m.GetByID(ids[0])
		d := &kiloDiagnostic{manager: m, identity: KiloIdentity(a), credential: a.CredentialVersion, epoch: a.RegistrationEpoch, restriction: kiloRestriction(a).Generation, disabled: true, manual: true, model: model}
		ctx = context.WithValue(ctx, kiloDiagnosticKey{}, d)
		budget := &concurrencyBudget{spent: 29 * time.Second}
		ctx = context.WithValue(ctx, concurrencyBudgetKey{}, budget)
		hold, err := e.pool.TryAcquire("outside", kilo.AcquireOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Release()
		start := time.Now()
		_, _, _, _, err = m.pickNextMixedAdmitted(ctx, []string{"kilo"}, model, core.Options{Metadata: map[string]any{core.PinnedAuthMetadataKey: ids[0]}}, nil)
		if !isConcurrencyError(err) || time.Since(start) != time.Second || budget.spent != 30*time.Second {
			t.Fatalf("wait=%s budget=%s error=%v", time.Since(start), budget.spent, err)
		}
		assertActive(t, m, ids[0], 0)
		if _, deadline := ctx.Deadline(); deadline || ctx.Err() != nil || m.concurrency.waiting != 0 || e.posts.Load() != 0 {
			t.Fatal("admission budget escaped into execution or leaked")
		}
		t.Log("manual Disabled diagnostic: prior29s + shared egress1s = cumulative30s; account0 POST0; no inference deadline")
	})
}
