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

func wakeFixOptions(id string, pinned bool) core.Options {
	opts := core.Options{Metadata: map[string]any{core.CallerScopeMetadataKey: "synthetic-wake-fix"}}
	if pinned {
		opts.Metadata[core.PinnedAuthMetadataKey] = id
	}
	return opts
}

func wakeFixExecute(m *Manager, ctx context.Context, model, id string, pinned bool) error {
	_, err := m.Execute(ctx, []string{"kilo"}, core.Request{Model: model, Payload: []byte("{}")}, wakeFixOptions(id, pinned))
	return err
}

type wakeFixAttempt struct {
	number int32
	allow  chan struct{}
}

// The gate controls only the test executor; the admission implementation is unchanged.
type wakeFixExecutor struct {
	*noReplayTestExecutor
	attempts     chan wakeFixAttempt
	returned     chan wakeFixAttempt
	pinned       bool
	stop         chan struct{}
	tries        atomic.Int32
	executeCalls atomic.Int32
}

func (e *wakeFixExecutor) TryAcquireEgress(a *Auth) (*Auth, func(), bool, error) {
	attempt := wakeFixAttempt{number: e.tries.Add(1), allow: make(chan struct{})}
	select {
	case e.attempts <- attempt:
	case <-e.stop:
		return nil, nil, false, context.Canceled
	}
	select {
	case <-attempt.allow:
	case <-e.stop:
		return nil, nil, false, context.Canceled
	}
	selected, release, busy, err := e.noReplayTestExecutor.TryAcquireEgress(a)
	if (busy || err != nil) && e.returned != nil {
		completed := wakeFixAttempt{number: attempt.number, allow: make(chan struct{})}
		select {
		case e.returned <- completed:
		case <-e.stop:
			return nil, nil, false, context.Canceled
		}
		select {
		case <-completed.allow:
		case <-e.stop:
			return nil, nil, false, context.Canceled
		}
	}
	return selected, release, busy, err
}

func (e *wakeFixExecutor) Execute(ctx context.Context, a *Auth, req core.Request, opts core.Options) (core.Response, error) {
	e.executeCalls.Add(1)
	return e.noReplayTestExecutor.Execute(ctx, a, req, opts)
}

func wakeFixReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("HARNESS_FAILURE synchronized event missing")
		var zero T
		return zero
	}
}

func wakeFixClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func wakeFixFixture(t *testing.T) (*Manager, *wakeFixExecutor, *captureResultHook, context.Context, string, string) {
	t.Helper()
	m, base, hook, ctx, model, ids := noReplayFixture(t, 200, "execute")
	m.Remove(context.Background(), ids[1])
	executor := &wakeFixExecutor{
		noReplayTestExecutor: base,
		attempts:             make(chan wakeFixAttempt, 8),
		stop:                 make(chan struct{}),
	}
	m.RegisterExecutor(executor)
	return m, executor, hook, ctx, model, ids[0]
}

func wakeFixCounters(t *testing.T, m *Manager, e *wakeFixExecutor, hook *captureResultHook, id string, active, waiting int, executions, results int) {
	t.Helper()
	states, _ := m.capacitySnapshot()
	m.concurrency.mu.Lock()
	actualWaiting, pinned := m.concurrency.waiting, len(m.concurrency.pinned)
	m.concurrency.mu.Unlock()
	hook.mu.Lock()
	actualResults := len(hook.results)
	hook.mu.Unlock()
	expectedPinned := 0
	if e.pinned && waiting > 0 {
		expectedPinned = 1
	}
	if states[id] != active || actualWaiting != waiting || pinned != expectedPinned || int(e.executeCalls.Load()) != executions || actualResults != results {
		t.Fatalf("HARNESS_FAILURE counters active=%d/%d waiting=%d/%d pinned=%d executor=%d/%d results=%d/%d",
			states[id], active, actualWaiting, waiting, pinned, e.executeCalls.Load(), executions, actualResults, results)
	}
}

func wakeFixPoolActive(e *wakeFixExecutor) int {
	active := 0
	for _, proxy := range e.pool.Snapshot().Proxies {
		active += proxy.Active
	}
	return active
}

func TestKiloEgressRollbackSilent(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("silent_rollback/pinned_%t", pinned), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, e, hook, parent, model, id := wakeFixFixture(t)
				defer close(e.stop)
				e.pinned = pinned
				blocker, err := e.pool.TryAcquire("synthetic-outside-holder", kilo.AcquireOptions{})
				if err != nil {
					t.Fatalf("HARNESS_FAILURE external egress hold: %v", err)
				}
				defer blocker.Release()
				exitG0 := e.EgressChanges()
				ctxA, cancelA := context.WithCancel(parent)
				defer cancelA()
				doneA := make(chan error, 1)
				go func() { doneA <- wakeFixExecute(m, ctxA, model, id, pinned) }()
				first := wakeFixReceive(t, e.attempts)
				if first.number != 1 {
					t.Fatalf("HARNESS_FAILURE first selection=%d", first.number)
				}
				close(first.allow)
				synctest.Wait()
				wakeFixCounters(t, m, e, hook, id, 0, 1, 0, 0)
				_, accountG1 := m.capacitySnapshot()
				auth, ok := m.GetByID(id)
				limit, _ := credentialConcurrencyLimit(auth)
				if !ok || limit <= 1 || wakeFixClosed(accountG1) || wakeFixClosed(exitG0) || e.EgressChanges() != exitG0 {
					t.Fatal("HARNESS_FAILURE initial spare account capacity or notification generation")
				}

				ctxB, cancelB := context.WithCancel(parent)
				defer cancelB()
				doneB := make(chan error, 1)
				go func() {
					_, _, _, lease, selectErr := m.pickNextMixedAdmitted(ctxB, []string{"kilo"}, model, wakeFixOptions(id, pinned), nil)
					if lease != nil {
						lease.release()
					}
					doneB <- selectErr
				}()
				second := wakeFixReceive(t, e.attempts)
				if second.number != 2 {
					t.Fatalf("HARNESS_FAILURE competing selection=%d", second.number)
				}
				wakeFixCounters(t, m, e, hook, id, 1, 1, 0, 0)
				cancelB()
				close(second.allow)
				if err := wakeFixReceive(t, doneB); !errors.Is(err, context.Canceled) {
					t.Fatalf("HARNESS_FAILURE canceled competing selector: %v", err)
				}
				synctest.Wait()

				accountClosed := wakeFixClosed(accountG1)
				_, accountG2 := m.capacitySnapshot()
				exitUnchanged := e.EgressChanges() == exitG0 && !wakeFixClosed(exitG0)
				var next wakeFixAttempt
				reselected := false
				select {
				case next = <-e.attempts:
					reselected = true
				default:
				}
				if !exitUnchanged || wakeFixPoolActive(e) != 1 || e.acquired.Load() != 0 || e.released.Load() != 0 || e.posts.Load() != 0 {
					t.Fatal("HARNESS_FAILURE egress changed or execution occurred before external release")
				}
				if reselected {
					if next.number != 3 {
						t.Fatalf("HARNESS_FAILURE repeated selection=%d", next.number)
					}
					wakeFixCounters(t, m, e, hook, id, 1, 0, 0, 0)
				} else {
					wakeFixCounters(t, m, e, hook, id, 0, 1, 0, 0)
				}
				beforeReleaseTries := e.tries.Load()
				if accountClosed || accountG2 != accountG1 || reselected || beforeReleaseTries != 2 {
					t.Fatalf("rollback must remain silent: account_closed=%t generation_changed=%t reselected=%t tries=%d", accountClosed, accountG2 != accountG1, reselected, beforeReleaseTries)
				}
				t.Logf("FIX_EVENT phase=rollback account_limit=%d original_account_generation_closed=%t account_generation_changed=%t egress_generation_unchanged=%t tries_before_real_release=%d waiting_A_reselected=%t executor=0 results=0",
					limit, accountClosed, accountG2 != accountG1, exitUnchanged, beforeReleaseTries, reselected)

				blocker.Release()
				if !wakeFixClosed(exitG0) || e.EgressChanges() == exitG0 {
					t.Fatal("HARNESS_FAILURE real egress release did not advance its generation")
				}
				if !reselected {
					next = wakeFixReceive(t, e.attempts)
				}
				if next.number != 3 {
					t.Fatalf("post-release attempt=%d want 3", next.number)
				}
				close(next.allow)
				synctest.Wait()
				if err := wakeFixReceive(t, doneA); err != nil {
					t.Fatalf("HARNESS_FAILURE waiting request after real release: %v", err)
				}
				wakeFixCounters(t, m, e, hook, id, 0, 0, 1, 1)
				if e.posts.Load() != 1 || e.acquired.Load() != 1 || e.released.Load() != 1 || wakeFixPoolActive(e) != 0 {
					t.Fatalf("HARNESS_FAILURE terminal counts memory_calls=%d acquire=%d release=%d pool_active=%d",
						e.posts.Load(), e.acquired.Load(), e.released.Load(), wakeFixPoolActive(e))
				}
				hook.mu.Lock()
				success := hook.results[0].Success
				hook.mu.Unlock()
				if !success {
					t.Fatal("HARNESS_FAILURE final result was not successful")
				}
				t.Logf("FIX_EVENT phase=real_release total_tries=%d executor=1 results=1 success=true credential_active=0 egress_active=0 waiting=0", e.tries.Load())
				t.Log("FIX_EVENT silent_rollback=true genuine_release_observed=true")
			})
		})

	}

	t.Run("waiting_cancellation_has_no_execution_or_leak", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			m, e, hook, parent, model, id := wakeFixFixture(t)
			defer close(e.stop)
			blocker, err := e.pool.TryAcquire("synthetic-outside-holder", kilo.AcquireOptions{})
			if err != nil {
				t.Fatalf("HARNESS_FAILURE cancellation external hold: %v", err)
			}
			defer blocker.Release()
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- executeNoReplayMode(m, ctx, "execute", model) }()
			attempt := wakeFixReceive(t, e.attempts)
			close(attempt.allow)
			synctest.Wait()
			wakeFixCounters(t, m, e, hook, id, 0, 1, 0, 0)
			cancel()
			synctest.Wait()
			if err := wakeFixReceive(t, done); !errors.Is(err, context.Canceled) {
				t.Fatalf("HARNESS_FAILURE waiting cancel: %v", err)
			}
			wakeFixCounters(t, m, e, hook, id, 0, 0, 0, 0)
			if e.tries.Load() != 1 || e.acquired.Load() != 0 || e.posts.Load() != 0 || wakeFixPoolActive(e) != 1 {
				t.Fatal("HARNESS_FAILURE canceled waiter mutated the external egress lease")
			}
			blocker.Release()
			if wakeFixPoolActive(e) != 0 {
				t.Fatal("HARNESS_FAILURE external cancellation fixture lease leaked")
			}
			t.Log("FIX_EVENT phase=cancel tries=1 executor=0 results=0 credential_active=0 egress_active=0 waiting=0")
		})
	})

	for _, mode := range []string{"real_account_release", "account_limit_change"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, e, hook, parent, _, id := wakeFixFixture(t)
				defer close(e.stop)
				_, _, err := m.Modify(parent, id, 0, func(a *Auth) error {
					a.Metadata[AttributeConcurrencyLimit] = 1
					return nil
				})
				if err != nil {
					t.Fatalf("HARNESS_FAILURE set account limit: %v", err)
				}
				hold, err := m.AcquireCredentialGeneration(parent, id)
				if err != nil {
					t.Fatalf("HARNESS_FAILURE real account hold: %v", err)
				}
				defer hold()
				ctx, cancel := context.WithCancel(parent)
				defer cancel()
				type acquired struct {
					release func()
					err     error
				}
				done := make(chan acquired, 1)
				go func() {
					release, acquireErr := m.AcquireCredentialGeneration(ctx, id)
					done <- acquired{release: release, err: acquireErr}
				}()
				synctest.Wait()
				_, generation := m.capacitySnapshot()
				m.concurrency.mu.Lock()
				waiting := m.concurrency.waiting
				m.concurrency.mu.Unlock()
				if waiting != 1 || wakeFixClosed(generation) {
					t.Fatal("HARNESS_FAILURE real account waiter was not blocked")
				}
				if mode == "real_account_release" {
					hold()
				} else {
					_, _, err = m.Modify(parent, id, 0, func(a *Auth) error {
						a.Metadata[AttributeConcurrencyLimit] = 2
						return nil
					})
					if err != nil {
						t.Fatalf("HARNESS_FAILURE account limit increase: %v", err)
					}
				}
				synctest.Wait()
				result := wakeFixReceive(t, done)
				if result.err != nil || result.release == nil || !wakeFixClosed(generation) {
					t.Fatalf("HARNESS_FAILURE genuine wake %s: %v", mode, result.err)
				}
				result.release()
				hold()
				wakeFixCounters(t, m, e, hook, id, 0, 0, 0, 0)
				if e.tries.Load() != 0 || wakeFixPoolActive(e) != 0 {
					t.Fatal("HARNESS_FAILURE account-only control touched egress")
				}
				t.Logf("FIX_EVENT phase=%s generation_closed=true waiter_acquired=true executor=0 results=0 credential_active=0 waiting=0", mode)
			})
		})
	}
}

func TestKiloEgressReleaseBeforeWait(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("pinned_%t", pinned), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, e, hook, parent, model, id := wakeFixFixture(t)
				defer close(e.stop)
				e.pinned = pinned
				e.returned = make(chan wakeFixAttempt, 4)
				blocker, err := e.pool.TryAcquire("synthetic-outside-holder", kilo.AcquireOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer blocker.Release()
				_, accountBefore := m.capacitySnapshot()
				exitBefore := e.EgressChanges()
				ctx, cancel := context.WithCancel(parent)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- wakeFixExecute(m, ctx, model, id, pinned) }()
				first := wakeFixReceive(t, e.attempts)
				close(first.allow)
				failed := wakeFixReceive(t, e.returned)
				if first.number != 1 || failed.number != 1 || e.acquired.Load() != 0 {
					t.Fatal("first egress attempt must have failed before the return gate")
				}
				wakeFixCounters(t, m, e, hook, id, 1, 0, 0, 0)
				blocker.Release()
				if !wakeFixClosed(exitBefore) || wakeFixClosed(accountBefore) {
					t.Fatal("only genuine egress release may notify before rollback")
				}
				close(failed.allow)
				second := wakeFixReceive(t, e.attempts)
				if second.number != 2 {
					t.Fatalf("retry after pre-wait release=%d", second.number)
				}
				close(second.allow)
				if err := wakeFixReceive(t, done); err != nil {
					t.Fatal(err)
				}
				wakeFixCounters(t, m, e, hook, id, 0, 0, 1, 1)
				if e.tries.Load() != 2 || e.acquired.Load() != 1 || e.released.Load() != 1 || wakeFixPoolActive(e) != 0 {
					t.Fatal("pre-wait release was lost or joint lease was not settled once")
				}
				t.Logf("FIX_EVENT phase=release_before_wait pinned=%t failed_attempt=1 release_before_return=true total_tries=2 executor=1 results=1 active=0 waiting=0", pinned)
			})
		})
	}
}

func TestKiloPinnedAccountChangeBeforeWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, e, hook, parent, model, id := wakeFixFixture(t)
		defer close(e.stop)
		e.pinned = true
		e.returned = make(chan wakeFixAttempt, 4)
		blocker, err := e.pool.TryAcquire("synthetic-outside-holder", kilo.AcquireOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Release()
		_, accountBefore := m.capacitySnapshot()
		exitBefore := e.EgressChanges()
		ctx, cancel := context.WithCancel(parent)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- wakeFixExecute(m, ctx, model, id, true) }()
		first := wakeFixReceive(t, e.attempts)
		close(first.allow)
		failed := wakeFixReceive(t, e.returned)
		if failed.number != 1 {
			t.Fatal("first attempt did not reach the completed-failure gate")
		}
		_, committed, err := m.Modify(parent, id, 0, func(a *Auth) error {
			a.Metadata[AttributeConcurrencyLimit] = DefaultCredentialConcurrency + 1
			return nil
		})
		if err != nil || !committed || !wakeFixClosed(accountBefore) || wakeFixClosed(exitBefore) {
			t.Fatalf("real account configuration did not notify independently: committed=%t err=%v", committed, err)
		}
		close(failed.allow)
		second := wakeFixReceive(t, e.attempts)
		if second.number != 2 {
			t.Fatal("pre-wait account generation was not consumed by a retry")
		}
		wakeFixCounters(t, m, e, hook, id, 1, 0, 0, 0)
		close(second.allow)
		failedAgain := wakeFixReceive(t, e.returned)
		close(failedAgain.allow)
		synctest.Wait()
		wakeFixCounters(t, m, e, hook, id, 0, 1, 0, 0)
		if e.tries.Load() != 2 || e.EgressChanges() != exitBefore || wakeFixClosed(exitBefore) {
			t.Fatal("account configuration retry must settle without fabricating egress capacity")
		}
		blocker.Release()
		third := wakeFixReceive(t, e.attempts)
		if third.number != 3 {
			t.Fatalf("genuine egress release attempt=%d", third.number)
		}
		close(third.allow)
		if err := wakeFixReceive(t, done); err != nil {
			t.Fatal(err)
		}
		wakeFixCounters(t, m, e, hook, id, 0, 0, 1, 1)
		if e.acquired.Load() != 1 || e.released.Load() != 1 || wakeFixPoolActive(e) != 0 {
			t.Fatal("configuration wake leaked the joint lease")
		}
		t.Log("FIX_EVENT phase=pinned_account_change_before_wait account_generation_consumed=true tries_before_release=2 total_tries=3 executor=1 results=1 active=0 waiting=0")
	})
}

func TestKiloPinnedEgressCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, e, hook, parent, model, id := wakeFixFixture(t)
		defer close(e.stop)
		e.pinned = true
		blocker, err := e.pool.TryAcquire("synthetic-outside-holder", kilo.AcquireOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Release()
		ctx, cancel := context.WithCancel(parent)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- wakeFixExecute(m, ctx, model, id, true) }()
		first := wakeFixReceive(t, e.attempts)
		close(first.allow)
		synctest.Wait()
		wakeFixCounters(t, m, e, hook, id, 0, 1, 0, 0)
		cancel()
		if err := wakeFixReceive(t, done); !errors.Is(err, context.Canceled) {
			t.Fatalf("pinned cancellation=%v", err)
		}
		wakeFixCounters(t, m, e, hook, id, 0, 0, 0, 0)
		if e.tries.Load() != 1 || wakeFixPoolActive(e) != 1 || e.acquired.Load() != 0 {
			t.Fatal("pinned cancellation changed the external holder")
		}
		blocker.Release()
		if wakeFixPoolActive(e) != 0 {
			t.Fatal("external egress holder leaked")
		}
		t.Log("FIX_EVENT phase=pinned_cancel tries=1 executor=0 results=0 active=0 waiting=0")
	})
}

func TestKiloEgressFailureRollsBackSilently(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("pinned_%t", pinned), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, e, hook, parent, model, id := wakeFixFixture(t)
				defer close(e.stop)
				e.pinned = pinned
				if err := e.pool.SetHealthy("synthetic-proxy", false); err != nil {
					t.Fatal(err)
				}
				_, accountBefore := m.capacitySnapshot()
				exitBefore := e.EgressChanges()
				ctx, cancel := context.WithCancel(parent)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- wakeFixExecute(m, ctx, model, id, pinned) }()
				first := wakeFixReceive(t, e.attempts)
				close(first.allow)
				if err := wakeFixReceive(t, done); !errors.Is(err, kilo.ErrProxyUnavailable) {
					t.Fatalf("health failure=%v", err)
				}
				wakeFixCounters(t, m, e, hook, id, 0, 0, 0, 0)
				_, accountAfter := m.capacitySnapshot()
				if accountAfter != accountBefore || wakeFixClosed(accountBefore) || e.EgressChanges() != exitBefore || wakeFixClosed(exitBefore) {
					t.Fatal("failed egress admission published a capacity notification")
				}
				if e.tries.Load() != 1 || e.acquired.Load() != 0 || e.released.Load() != 0 || e.posts.Load() != 0 || wakeFixPoolActive(e) != 0 {
					t.Fatal("health failure executed, retried, or retained capacity")
				}
				t.Logf("FIX_EVENT phase=health_failure pinned=%t tries=1 executor=0 results=0 account_generation_unchanged=true active=0 waiting=0", pinned)
			})
		})
	}
}

func TestCredentialReservationRollbackOnce(t *testing.T) {
	gate := &localConcurrency{}
	gate.mu.Lock()
	gate.initLocked()
	generation := gate.changedCh
	gate.mu.Unlock()
	state := &concurrencyCounter{active: 1}
	reservation := &credentialLease{gate: gate, state: state}
	done := make(chan struct{}, 8)
	for range 8 {
		go func() {
			reservation.rollbackReservation()
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
	reservation.release()
	if state.active != 0 || wakeFixClosed(generation) || gate.changedCh != generation {
		t.Fatal("reservation rollback must decrement once without a notification")
	}
	var nilLease *credentialLease
	nilLease.rollbackReservation()

	var egressReleases atomic.Int32
	delivered := &credentialLease{gate: gate, state: state, releaseEgress: func() { egressReleases.Add(1) }}
	state.active = 1
	for range 8 {
		go func() {
			delivered.release()
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
	if state.active != 0 || egressReleases.Load() != 1 || !wakeFixClosed(generation) || gate.changedCh == generation {
		t.Fatal("delivered release must decrement, notify, and release egress exactly once")
	}
	t.Log("FIX_EVENT phase=idempotence rollback_calls=8 rollback_active=0 rollback_notification=false release_calls=8 egress_releases=1 real_release_notification=true")
}
