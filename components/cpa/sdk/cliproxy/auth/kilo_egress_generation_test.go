package auth

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type generationAttempt struct {
	number int32
	id     string
	allow  chan struct{}
}

// Gates surround only the test executor's real in-memory pool operation.
type generationExecutor struct {
	*noReplayTestExecutor
	attempts    chan generationAttempt
	failed      chan generationAttempt
	stop        chan struct{}
	tries       atomic.Int32
	snapshots   atomic.Int32
	executions  atomic.Int32
	firstChange <-chan struct{}
}

func (e *generationExecutor) EgressChanges() <-chan struct{} {
	changed := e.pool.Changes()
	if e.snapshots.Add(1) == 1 {
		e.firstChange = changed
	}
	return changed
}

func (e *generationExecutor) TryAcquireEgress(a *Auth) (*Auth, func(), bool, error) {
	step := generationAttempt{number: e.tries.Add(1), id: a.ID, allow: make(chan struct{})}
	select {
	case e.attempts <- step:
	case <-e.stop:
		return nil, nil, false, context.Canceled
	}
	select {
	case <-step.allow:
	case <-e.stop:
		return nil, nil, false, context.Canceled
	}
	selected, release, busy, err := e.noReplayTestExecutor.TryAcquireEgress(a)
	if busy || err != nil {
		step.allow = make(chan struct{})
		select {
		case e.failed <- step:
		case <-e.stop:
			return nil, nil, false, context.Canceled
		}
		select {
		case <-step.allow:
		case <-e.stop:
			return nil, nil, false, context.Canceled
		}
	}
	return selected, release, busy, err
}

func (e *generationExecutor) Execute(ctx context.Context, a *Auth, req core.Request, opts core.Options) (core.Response, error) {
	e.executions.Add(1)
	return e.noReplayTestExecutor.Execute(ctx, a, req, opts)
}

func generationFixture(t *testing.T) (*Manager, *generationExecutor, *captureResultHook, context.Context, string, []string, *kilo.ProxyLease, *kilo.ProxyLease) {
	t.Helper()
	m, base, hook, ctx, model, ids := noReplayFixture(t, 200, "execute")
	m.SetSelector(&FillFirstSelector{})
	pool, err := kilo.NewProxyPool([]kilo.Proxy{
		{ID: "private-a", AccountID: ids[0], Healthy: true},
		{ID: "private-b", AccountID: ids[1], Healthy: true},
	}, 1)
	if err != nil {
		t.Fatalf("HARNESS_FAILURE private pool: %v", err)
	}
	base.pool = pool
	e := &generationExecutor{noReplayTestExecutor: base, attempts: make(chan generationAttempt, 8), failed: make(chan generationAttempt, 8), stop: make(chan struct{})}
	m.RegisterExecutor(e)
	holdA, err := pool.TryAcquire(ids[0], kilo.AcquireOptions{})
	if err != nil {
		t.Fatalf("HARNESS_FAILURE reserve A: %v", err)
	}
	t.Cleanup(holdA.Release)
	holdB, err := pool.TryAcquire(ids[1], kilo.AcquireOptions{})
	if err != nil {
		t.Fatalf("HARNESS_FAILURE reserve B: %v", err)
	}
	t.Cleanup(holdB.Release)
	return m, e, hook, ctx, model, ids, holdA, holdB
}

func generationFailed(t *testing.T, e *generationExecutor, number int32, id string) generationAttempt {
	t.Helper()
	attempt := wakeFixReceive(t, e.attempts)
	if attempt.number != number || attempt.id != id {
		t.Fatalf("HARNESS_FAILURE attempt=%d/%s want %d/%s", attempt.number, attempt.id, number, id)
	}
	close(attempt.allow)
	failed := wakeFixReceive(t, e.failed)
	if failed.number != number || failed.id != id || e.acquired.Load() != 0 {
		t.Fatalf("HARNESS_FAILURE failure gate=%d/%s acquired=%d", failed.number, failed.id, e.acquired.Load())
	}
	return failed
}

func generationState(t *testing.T, m *Manager, e *generationExecutor, hook *captureResultHook, ids []string, active, waiting int, executed int32, results int) {
	t.Helper()
	states, _ := m.capacitySnapshot()
	total := 0
	for _, id := range ids {
		total += states[id]
	}
	m.concurrency.mu.Lock()
	waiters, pinned := m.concurrency.waiting, len(m.concurrency.pinned)
	m.concurrency.mu.Unlock()
	hook.mu.Lock()
	registered := len(hook.results)
	hook.mu.Unlock()
	if total != active || waiters != waiting || pinned != 0 || e.executions.Load() != executed || registered != results {
		t.Fatalf("HARNESS_FAILURE active=%d/%d waiting=%d/%d pinned=%d executions=%d/%d results=%d/%d", total, active, waiters, waiting, pinned, e.executions.Load(), executed, registered, results)
	}
}

func generationPool(t *testing.T, e *generationExecutor, a, b int) {
	t.Helper()
	for _, p := range e.pool.Snapshot().Proxies {
		want := a
		if p.ID == "private-b" {
			want = b
		}
		if p.Active != want {
			t.Fatalf("HARNESS_FAILURE pool %s active=%d want=%d", p.ID, p.Active, want)
		}
	}
}

func TestKiloEgressEarliestGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, e, hook, parent, model, ids, holdA, holdB := generationFixture(t)
		defer close(e.stop)
		budget := &concurrencyBudget{}
		ctx, cancel := context.WithCancel(context.WithValue(parent, concurrencyBudgetKey{}, budget))
		defer cancel()
		_, accountBefore := m.capacitySnapshot()
		exitBefore := e.pool.Changes()
		done := make(chan error, 1)
		go func() { done <- executeNoReplayMode(m, ctx, "execute", model) }()
		failedA := generationFailed(t, e, 1, ids[0])
		if e.firstChange != exitBefore || wakeFixClosed(exitBefore) {
			t.Fatal("HARNESS_FAILURE first snapshot did not precede A failure")
		}
		holdA.Release()
		exitAfterA := e.pool.Changes()
		if !wakeFixClosed(exitBefore) || wakeFixClosed(exitAfterA) {
			t.Fatal("HARNESS_FAILURE A release did not change egress generation")
		}
		close(failedA.allow)
		failedB := generationFailed(t, e, 2, ids[1])
		generationPool(t, e, 0, 1)
		if wakeFixClosed(accountBefore) || e.pool.Changes() != exitAfterA || wakeFixClosed(exitAfterA) {
			t.Fatal("HARNESS_FAILURE notification changed between A release and B failure")
		}
		close(failedB.allow)
		synctest.Wait()
		var next generationAttempt
		reselected := false
		select {
		case next = <-e.attempts:
			reselected = true
		default:
		}
		if !reselected {
			generationState(t, m, e, hook, ids, 0, 1, 0, 0)
			if e.tries.Load() != 2 || e.snapshots.Load() != 2 {
				t.Fatal("HARNESS_FAILURE missed-notification trace is not A/B with two snapshots")
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(credentialWaitBudget + time.Second):
				t.Fatal("HARNESS_FAILURE admission exceeded its original wait budget")
			}
			var capacityErr *Error
			if !errors.As(err, &capacityErr) || capacityErr.Code != "credential_concurrency_timeout" || capacityErr.HTTPStatus != 429 || budget.spent != credentialWaitBudget {
				t.Fatalf("HARNESS_FAILURE timeout=%v budget=%s", err, budget.spent)
			}
			generationState(t, m, e, hook, ids, 0, 0, 0, 0)
			generationPool(t, e, 0, 1)
			if e.pool.Changes() != exitAfterA || wakeFixClosed(exitAfterA) || wakeFixClosed(accountBefore) {
				t.Fatal("HARNESS_FAILURE unexpected capacity event while waiting")
			}
			holdB.Release()
			generationPool(t, e, 0, 0)
			t.Log("GENERATION_EVENT A_failed=true A_released_before_B_snapshot=true B_still_full=true retries=0 executor=0 results=0 timeout=429 budget=30s final_active=0 waiting=0")
			t.Error("EXPECTED_RED earliest closed egress generation was replaced; A remained eligible and idle until the original admission budget expired")
			return
		}
		if next.number != 3 || next.id != ids[0] {
			t.Fatalf("HARNESS_FAILURE automatic retry=%d/%s want A at 3", next.number, next.id)
		}
		generationState(t, m, e, hook, ids, 1, 0, 0, 0)
		close(next.allow)
		if err := wakeFixReceive(t, done); err != nil {
			t.Fatalf("HARNESS_FAILURE A execution after genuine release: %v", err)
		}
		generationState(t, m, e, hook, ids, 0, 0, 1, 1)
		generationPool(t, e, 0, 1)
		if e.tries.Load() != 3 || e.acquired.Load() != 1 || e.released.Load() != 1 || e.posts.Load() != 1 || len(e.ids) != 1 || e.ids[0] != ids[0] {
			t.Fatal("HARNESS_FAILURE automatic retry did not execute A exactly once")
		}
		hook.mu.Lock()
		success := hook.results[0].Success
		hook.mu.Unlock()
		if !success {
			t.Fatal("HARNESS_FAILURE execution result was not successful")
		}
		holdB.Release()
		generationPool(t, e, 0, 0)
		t.Logf("GENERATION_EVENT A_failed=true A_released_before_B=true B_still_full=true automatic_retry=A total_tries=3 executor=1 results=1 success=true final_active=0 waiting=0 budget=%s", budget.spent)
	})
}

func TestKiloEgressGenerationReset(t *testing.T) {
	for _, finish := range []string{"cancel", "budget"} {
		t.Run(finish, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, e, hook, parent, model, ids, holdA, holdB := generationFixture(t)
				defer close(e.stop)
				budget := &concurrencyBudget{spent: 29 * time.Second}
				ctx, cancel := context.WithCancel(context.WithValue(parent, concurrencyBudgetKey{}, budget))
				defer cancel()
				_, accountBefore := m.capacitySnapshot()
				done := make(chan error, 1)
				go func() { done <- executeNoReplayMode(m, ctx, "execute", model) }()
				failedA := generationFailed(t, e, 1, ids[0])
				holdA.Release()
				reholdA, err := e.pool.TryAcquire(ids[0], kilo.AcquireOptions{})
				if err != nil {
					t.Fatalf("HARNESS_FAILURE replacement holder: %v", err)
				}
				defer reholdA.Release()
				fresh := e.pool.Changes()
				close(failedA.allow)
				failedB := generationFailed(t, e, 2, ids[1])
				close(failedB.allow)
				for index, id := range ids {
					retried := generationFailed(t, e, int32(index+3), id)
					close(retried.allow)
				}
				synctest.Wait()
				generationState(t, m, e, hook, ids, 0, 1, 0, 0)
				generationPool(t, e, 1, 1)
				select {
				case attempt := <-e.attempts:
					t.Fatalf("closed generation caused extra selection %d/%s", attempt.number, attempt.id)
				default:
				}
				if e.tries.Load() != 4 || e.snapshots.Load() != 2 || e.pool.Changes() != fresh || wakeFixClosed(fresh) || wakeFixClosed(accountBefore) {
					t.Fatalf("generation reset failed: tries=%d snapshots=%d", e.tries.Load(), e.snapshots.Load())
				}
				started := time.Now()
				if finish == "cancel" {
					cancel()
				}
				var terminal error
				select {
				case terminal = <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("HARNESS_FAILURE final admission event missing")
				}
				if finish == "cancel" && !errors.Is(terminal, context.Canceled) {
					t.Fatalf("cancel=%v", terminal)
				}
				if finish == "budget" && (!isConcurrencyError(terminal) || budget.spent != 30*time.Second || time.Since(started) != time.Second) {
					t.Fatalf("budget error=%v spent=%s elapsed=%s", terminal, budget.spent, time.Since(started))
				}
				generationState(t, m, e, hook, ids, 0, 0, 0, 0)
				reholdA.Release()
				holdB.Release()
				generationPool(t, e, 0, 0)
				t.Log(fmt.Sprintf("GENERATION_EVENT phase=reset finish=%s tries=4 snapshots=2 extra_selection=false executor=0 results=0 final_active=0 waiting=0 budget=%s", finish, budget.spent))
			})
		})
	}
}
