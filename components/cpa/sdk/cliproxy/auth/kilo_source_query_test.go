package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	usage "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
)

type ae1QueryExecutor struct {
	*noReplayTestExecutor
	started, finish chan struct{}
	calls           atomic.Int32
	observation     usage.UsageObservation
}

func (e *ae1QueryExecutor) KiloManagementUsage(context.Context, string, string, string) (usage.UsageObservation, error) {
	e.calls.Add(1)
	if e.started != nil {
		close(e.started)
		<-e.finish
	}
	return e.observation, nil
}

func TestAE1ReboundTargetRejectsOlderSharedFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, a := m6Manager(t)
		key := "second-synthetic"
		b, err := m.ChangeKiloAccount(context.Background(), "", KiloAccountChanges{APIKey: &key}, true, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []struct{ id, token string }{{a.ID, "source-x"}, {b.ID, "source-y"}} {
			rev := uint64(0)
			if _, err = m.ChangeKiloAccount(context.Background(), target.id, KiloAccountChanges{QuotaSource: &KiloQuotaBinding{Kind: "imported", AccessToken: target.token}, ExpectedBindingRevision: &rev}, false, false); err != nil {
				t.Fatal(err)
			}
			if _, err = m.ChangeKiloSourcePolicy(context.Background(), target.id, 0, true, 1800, false); err != nil {
				t.Fatal(err)
			}
		}
		ae1Observe(t, m, b.ID, "100", true, nil)
		obs, err := usage.ParseUsage([]byte(`{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":1,"reset_after_seconds":300},"secondary_window":{"limit_window_seconds":604800,"used_percent":1,"reset_after_seconds":600}}}`), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		e := &ae1QueryExecutor{noReplayTestExecutor: &noReplayTestExecutor{}, started: make(chan struct{}), finish: make(chan struct{}), observation: obs}
		m.RegisterExecutor(e)
		done := make(chan error, 2)
		go func() { _, err := m.QueryKiloQuota(context.Background(), a.ID, 1); done <- err }()
		<-e.started
		time.Sleep(time.Nanosecond)
		rev := uint64(1)
		if _, err = m.ChangeKiloAccount(context.Background(), b.ID, KiloAccountChanges{QuotaSource: &KiloQuotaBinding{Kind: "imported", AccessToken: "source-x"}, ExpectedBindingRevision: &rev}, false, false); err != nil {
			t.Fatal(err)
		}
		go func() { _, err := m.QueryKiloQuota(context.Background(), b.ID, 2); done <- err }()
		synctest.Wait()
		close(e.finish)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		p, _ := m.KiloSourcePolicy(b.ID)
		if e.calls.Load() != 1 || !p.Paused || p.Status != "source_pending" {
			t.Fatalf("old flight crossed new binding: calls=%d policy=%+v", e.calls.Load(), p)
		}
		e.started, e.finish = nil, nil
		if _, err = m.QueryKiloQuota(context.Background(), b.ID, 2); err != nil {
			t.Fatal(err)
		}
		p, _ = m.KiloSourcePolicy(b.ID)
		if e.calls.Load() != 2 || p.Paused {
			t.Fatalf("fresh query did not recover: calls=%d policy=%+v", e.calls.Load(), p)
		}
	})
}
func TestAE1SourceQueryCoalescesAndRetainsActualStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := NewManager(nil, nil, nil)
		e := &ae1QueryExecutor{noReplayTestExecutor: &noReplayTestExecutor{}, started: make(chan struct{}), finish: make(chan struct{})}
		m.RegisterExecutor(e)
		material := CodexUsageCredential{AccessToken: "synthetic", AccountID: "outer"}
		results := make(chan kiloSourceQueryResult, 2)
		go func() { results <- m.queryKiloSource(context.Background(), "a", "imported", material) }()
		<-e.started
		go func() { results <- m.queryKiloSource(context.Background(), "b", "imported", material) }()
		synctest.Wait()
		close(e.finish)
		a, b := <-results, <-results
		if e.calls.Load() != 1 || !a.started.Equal(b.started) || !a.completed.Equal(b.completed) {
			t.Fatal("one source duplicated or invented a later start")
		}
	})
	m := NewManager(nil, nil, nil)
	e := &ae1QueryExecutor{noReplayTestExecutor: &noReplayTestExecutor{}}
	m.RegisterExecutor(e)
	ctx := context.WithValue(context.Background(), kiloSourceCycleKey{}, kiloSourceCycle{})
	material := CodexUsageCredential{AccessToken: "synthetic", AccountID: "outer"}
	a := m.queryKiloSource(ctx, "a", "imported", material)
	b := m.queryKiloSource(ctx, "b", "imported", material)
	if e.calls.Load() != 1 || !a.started.Equal(b.started) {
		t.Fatal("sequential cycle repeated a source query")
	}
	material.AccessToken = "replacement"
	_ = m.queryKiloSource(ctx, "b", "imported", material)
	if e.calls.Load() != 2 {
		t.Fatal("changed source reused stale observation")
	}
}
