package kilo

import (
	"errors"
	"sync"
	"testing"
)

func newTestPool(t *testing.T, proxies []Proxy, limit int) *ProxyPool {
	t.Helper()
	pool, err := NewProxyPool(proxies, limit)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func acquireTest(t *testing.T, pool *ProxyPool, account string, options AcquireOptions) *ProxyLease {
	t.Helper()
	lease, err := pool.TryAcquire(account, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	return lease
}

func TestDefaultLimitAndValidation(t *testing.T) {
	pool := newTestPool(t, []Proxy{{ID: "public", Healthy: true}}, 0)
	if pool.Snapshot().Limit != 10 {
		t.Fatal("default limit must be ten")
	}
	for range 10 {
		acquireTest(t, pool, "a", AcquireOptions{})
	}
	if _, err := pool.TryAcquire("a", AcquireOptions{}); !errors.Is(err, ErrProxyCapacity) {
		t.Fatalf("eleventh request: %v", err)
	}
	for _, limit := range []int{-1, 101} {
		if _, err := NewProxyPool(nil, limit); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}
	for _, limit := range []int{1, 100} {
		if err := pool.SetLimit(limit); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{-1, 0, 101} {
		if err := pool.SetLimit(limit); err == nil {
			t.Fatalf("accepted update %d", limit)
		}
		if pool.Snapshot().Limit != 100 {
			t.Fatal("invalid update changed limit")
		}
	}
}

func TestPrivateFirstAndPublicLastRelease(t *testing.T) {
	pool := newTestPool(t, []Proxy{
		{ID: "public", Healthy: true},
		{ID: "private-a", AccountID: "a", Healthy: true},
	}, 2)
	first := acquireTest(t, pool, "a", AcquireOptions{})
	second := acquireTest(t, pool, "a", AcquireOptions{})
	if first.Route().ProxyID != "private-a" || second.Route().ProxyID != "private-a" {
		t.Fatal("public won over private")
	}
	third := acquireTest(t, pool, "a", AcquireOptions{})
	fourth := acquireTest(t, pool, "a", AcquireOptions{})
	if third.Route().ProxyID != "public" {
		t.Fatal("private exhaustion did not use public")
	}
	third.Release()
	if _, err := pool.TryAcquire("b", AcquireOptions{}); !errors.Is(err, ErrProxyCapacity) {
		t.Fatalf("shared active public proxy: %v", err)
	}
	fourth.Release()
	other := acquireTest(t, pool, "b", AcquireOptions{})
	if other.Route().ProxyID != "public" {
		t.Fatal("public not reusable after last release")
	}
	first.Release()
	second.Release()
	other.Release()
	snapshot := pool.Snapshot()
	if snapshot.Proxies[0].Owner != "" || snapshot.Proxies[0].Active != 0 {
		t.Fatalf("public retained: %+v", snapshot)
	}
	if snapshot.Proxies[1].Owner != "a" || snapshot.Proxies[1].Active != 0 {
		t.Fatalf("private owner lost: %+v", snapshot)
	}
}

func TestDirectTruthTable(t *testing.T) {
	cases := []struct {
		name    string
		proxies []Proxy
		occupy  []string
		options AcquireOptions
		want    RouteMode
		wantErr error
	}{
		{name: "empty_even_enabled", options: AcquireOptions{AllowDirectWhenProxyFull: true}, wantErr: ErrProxyUnavailable},
		{name: "all_failed_even_enabled", proxies: []Proxy{{ID: "failed"}}, options: AcquireOptions{AllowDirectWhenProxyFull: true}, wantErr: ErrProxyUnavailable},
		{name: "other_private_is_not_eligible", proxies: []Proxy{{ID: "private-b", AccountID: "b", Healthy: true}}, options: AcquireOptions{AllowDirectWhenProxyFull: true}, wantErr: ErrProxyUnavailable},
		{name: "healthy_spare_default", proxies: []Proxy{{ID: "p", Healthy: true}}, want: RouteProxy},
		{name: "healthy_spare_enabled_still_proxy", proxies: []Proxy{{ID: "p", Healthy: true}}, options: AcquireOptions{AllowDirectWhenProxyFull: true}, want: RouteProxy},
		{name: "full_default_disabled", proxies: []Proxy{{ID: "p", Healthy: true}}, occupy: []string{"a"}, wantErr: ErrProxyCapacity},
		{name: "full_enabled", proxies: []Proxy{{ID: "p", Healthy: true}}, occupy: []string{"a"}, options: AcquireOptions{AllowDirectWhenProxyFull: true}, want: RouteDirect},
		{name: "other_owner_full_enabled", proxies: []Proxy{{ID: "p", Healthy: true}}, occupy: []string{"b"}, options: AcquireOptions{AllowDirectWhenProxyFull: true}, want: RouteDirect},
		{name: "mixed_failed_and_spare", proxies: []Proxy{{ID: "failed"}, {ID: "healthy", Healthy: true}}, options: AcquireOptions{AllowDirectWhenProxyFull: true}, want: RouteProxy},
		{name: "mixed_failed_and_full", proxies: []Proxy{{ID: "failed"}, {ID: "healthy", Healthy: true}}, occupy: []string{"a"}, options: AcquireOptions{AllowDirectWhenProxyFull: true}, want: RouteDirect},
		{name: "connect_failure_never_direct", proxies: []Proxy{{ID: "p", Healthy: true}}, occupy: []string{"a"}, options: AcquireOptions{AllowDirectWhenProxyFull: true, RetryAfterProxyFailure: true}, wantErr: ErrProxyCapacity},
		{name: "connect_failure_can_choose_healthy", proxies: []Proxy{{ID: "failed"}, {ID: "healthy", Healthy: true}}, options: AcquireOptions{AllowDirectWhenProxyFull: true, RetryAfterProxyFailure: true}, want: RouteProxy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := newTestPool(t, tc.proxies, 1)
			for _, account := range tc.occupy {
				acquireTest(t, pool, account, AcquireOptions{})
			}
			lease, err := pool.TryAcquire("a", tc.options)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || lease != nil {
					t.Fatalf("got lease=%v err=%v", lease, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Release()
			if lease.Route().Mode != tc.want {
				t.Fatalf("route=%+v want=%s", lease.Route(), tc.want)
			}
			if tc.want == RouteDirect && lease.Route().ProxyID != "" {
				t.Fatal("direct carried a proxy ID")
			}
		})
	}
}

func TestOtherOwnerSpareIsNotFull(t *testing.T) {
	pool := newTestPool(t, []Proxy{{ID: "p", Healthy: true}}, 10)
	acquireTest(t, pool, "b", AcquireOptions{})
	if _, err := pool.TryAcquire("a", AcquireOptions{AllowDirectWhenProxyFull: true}); !errors.Is(err, ErrProxyCapacity) {
		t.Fatalf("exclusive ownership is not actual saturation: %v", err)
	}
}

func TestLowerLimitKeepsInFlight(t *testing.T) {
	pool := newTestPool(t, []Proxy{{ID: "p", Healthy: true}}, 3)
	leases := []*ProxyLease{acquireTest(t, pool, "a", AcquireOptions{}), acquireTest(t, pool, "a", AcquireOptions{}), acquireTest(t, pool, "a", AcquireOptions{})}
	if err := pool.SetLimit(1); err != nil {
		t.Fatal(err)
	}
	if got := pool.Snapshot().Proxies[0]; got.Active != 3 || got.Limit != 1 {
		t.Fatalf("interrupted in-flight: %+v", got)
	}
	for _, lease := range leases[:2] {
		lease.Release()
	}
	if _, err := pool.TryAcquire("a", AcquireOptions{}); !errors.Is(err, ErrProxyCapacity) {
		t.Fatalf("acquired at lowered limit: %v", err)
	}
	leases[2].Release()
	acquireTest(t, pool, "a", AcquireOptions{})
}

func TestReleaseIdempotentAndConcurrent(t *testing.T) {
	pool := newTestPool(t, []Proxy{{ID: "p", Healthy: true}}, 1)
	lease := acquireTest(t, pool, "a", AcquireOptions{})
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(lease.Release)
	}
	wg.Wait()
	lease.Release()
	if got := pool.Snapshot().Proxies[0]; got.Active != 0 || got.Owner != "" {
		t.Fatalf("bad release: %+v", got)
	}
	var absent *ProxyLease
	absent.Release()
}

func TestConcurrentPublicExclusivity(t *testing.T) {
	pool := newTestPool(t, []Proxy{{ID: "p", Healthy: true}}, 100)
	type result struct {
		account string
		lease   *ProxyLease
		err     error
	}
	results := make(chan result, 32)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 32 {
		account := "a"
		if i%2 == 1 {
			account = "b"
		}
		wg.Go(func() {
			<-start
			lease, err := pool.TryAcquire(account, AcquireOptions{})
			results <- result{account, lease, err}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	owner := ""
	var held []*ProxyLease
	for result := range results {
		if result.err != nil {
			if !errors.Is(result.err, ErrProxyCapacity) {
				t.Fatal(result.err)
			}
			continue
		}
		if owner != "" && owner != result.account {
			t.Fatal("public shared across accounts")
		}
		owner = result.account
		held = append(held, result.lease)
	}
	if len(held) != 16 {
		t.Fatalf("one owner's acquisitions=%d want=16", len(held))
	}
	for _, lease := range held {
		lease.Release()
	}
	if pool.Snapshot().Proxies[0].Active != 0 {
		t.Fatal("leaked slots")
	}
}

func TestHealthAndChangeNotifications(t *testing.T) {
	pool := newTestPool(t, []Proxy{{ID: "p", Healthy: true}}, 1)
	before := pool.Changes()
	lease := acquireTest(t, pool, "a", AcquireOptions{})
	assertChanged(t, before)
	before = pool.Changes()
	if err := pool.SetHealthy("p", false); err != nil {
		t.Fatal(err)
	}
	assertChanged(t, before)
	if got := pool.Snapshot().Proxies[0]; got.Active != 1 || got.Healthy {
		t.Fatalf("health interrupted lease: %+v", got)
	}
	if _, err := pool.TryAcquire("a", AcquireOptions{AllowDirectWhenProxyFull: true}); !errors.Is(err, ErrProxyUnavailable) {
		t.Fatal(err)
	}
	before = pool.Changes()
	lease.Release()
	assertChanged(t, before)
	before = pool.Changes()
	if err := pool.SetHealthy("p", true); err != nil {
		t.Fatal(err)
	}
	assertChanged(t, before)
	before = pool.Changes()
	if err := pool.SetLimit(2); err != nil {
		t.Fatal(err)
	}
	assertChanged(t, before)
	acquireTest(t, pool, "b", AcquireOptions{})
}

func assertChanged(t *testing.T, changed <-chan struct{}) {
	t.Helper()
	select {
	case <-changed:
	default:
		t.Fatal("missing capacity notification")
	}
}

func TestSnapshotIsDetachedAndOrdered(t *testing.T) {
	input := []Proxy{{ID: "first", Healthy: true}, {ID: "second", AccountID: "a", Healthy: true}}
	pool := newTestPool(t, input, 0)
	input[0].ID = "changed"
	snapshot := pool.Snapshot()
	if snapshot.Proxies[0].ID != "first" || snapshot.Proxies[1].ID != "second" {
		t.Fatal(snapshot)
	}
	snapshot.Proxies[0].Healthy = false
	snapshot.Proxies[0].Active = 99
	if got := pool.Snapshot().Proxies[0]; !got.Healthy || got.Active != 0 {
		t.Fatalf("snapshot mutation leaked: %+v", got)
	}
}

func TestProxyAndDirectRetainSameExternalAdmission(t *testing.T) {
	pool := newTestPool(t, []Proxy{{ID: "p", Healthy: true}}, 1)
	// Stand in for two CPA-issued admission releases, not another limiter.
	active := 2
	var first, second sync.Once
	releaseFirst := func() { first.Do(func() { active-- }) }
	releaseSecond := func() { second.Do(func() { active-- }) }
	proxy := acquireTest(t, pool, "a", AcquireOptions{})
	direct := acquireTest(t, pool, "a", AcquireOptions{AllowDirectWhenProxyFull: true})
	if proxy.Route().Mode != RouteProxy || direct.Route().Mode != RouteDirect {
		t.Fatal("missing distinct routes")
	}
	proxy.Release()
	proxy.Release()
	direct.Release()
	direct.Release()
	if active != 2 {
		t.Fatal("proxy lease released external account capacity")
	}
	releaseFirst()
	releaseSecond()
	releaseSecond()
	if active != 0 {
		t.Fatal("external lease was not released exactly once")
	}
}

func TestInvalidConfigurationDoesNotMutatePool(t *testing.T) {
	for _, input := range [][]Proxy{{{ID: ""}}, {{ID: "same"}, {ID: "same"}}} {
		if _, err := NewProxyPool(input, 1); err == nil {
			t.Fatal("accepted invalid proxy identity")
		}
	}
	pool := newTestPool(t, []Proxy{{ID: "p", Healthy: true}}, 1)
	if _, err := pool.TryAcquire("", AcquireOptions{}); err == nil {
		t.Fatal("accepted empty account ID")
	}
	if err := pool.SetHealthy("missing", false); err == nil {
		t.Fatal("accepted missing proxy")
	}
	if got := pool.Snapshot().Proxies[0]; got.Active != 0 || !got.Healthy {
		t.Fatal(got)
	}
}
