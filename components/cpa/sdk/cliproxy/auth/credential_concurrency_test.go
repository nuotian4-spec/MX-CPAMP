package auth

import (
	"context"
	"errors"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func concurrencyFixture(t *testing.T, selector Selector) (*Manager, *claudeCancellationTestExecutor, []string) {
	t.Helper()
	m := NewManager(nil, selector, nil)
	exec := &claudeCancellationTestExecutor{}
	m.RegisterExecutor(exec)
	ids := []string{t.Name() + "-a", t.Name() + "-b"}
	for _, id := range ids {
		registry.GetGlobalRegistry().RegisterClient(id, "claude", []*registry.ModelInfo{{ID: "test-model"}, {ID: "other-model"}})
		_, err := m.Register(context.Background(), &Auth{ID: id, Provider: "claude", Status: StatusActive, Metadata: map[string]any{AttributeConcurrencyLimit: 1}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	return m, exec, ids
}
func assertActive(t *testing.T, m *Manager, id string, want int) {
	t.Helper()
	active, _ := m.capacitySnapshot()
	if active[id] != want {
		t.Fatalf("active[%s]=%d want %d", id, active[id], want)
	}
}
func awaitValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for synchronized test event")
		var zero T
		return zero
	}
}

func TestCredentialConcurrencyPoolAndPinnedAffinity(t *testing.T) {
	affinity := NewSessionAffinitySelector(&RoundRobinSelector{})
	m, _, ids := concurrencyFixture(t, affinity)
	release, err := m.AcquireCredentialGeneration(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	opts := core.Options{Headers: http.Header{"X-Session-Id": {"capacity-session"}}}
	selected, _, _, lease, err := m.pickNextMixedAdmitted(context.Background(), []string{"claude"}, "test-model", opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != ids[1] {
		t.Fatalf("selected busy credential: %s", selected.ID)
	}
	lease.release()
	// The first free selection creates affinity. Saturating it cannot silently switch to A.
	bound, ok := affinity.boundAuthForRequest([]string{"claude"}, "test-model", opts)
	if !ok || bound != ids[1] {
		t.Fatalf("affinity=%s/%v", bound, ok)
	}
	block, err := m.AcquireCredentialGeneration(context.Background(), ids[1])
	if err != nil {
		t.Fatal(err)
	}
	defer block()
	release()
	ctx := context.WithValue(context.Background(), concurrencyBudgetKey{}, &concurrencyBudget{spent: credentialWaitBudget})
	_, _, _, _, err = m.pickNextMixedAdmitted(ctx, []string{"claude"}, "test-model", opts, nil)
	if !isConcurrencyError(err) {
		t.Fatalf("expected bounded pinned wait, got %v", err)
	}
	assertActive(t, m, ids[0], 0)
}

func TestCredentialConcurrencyDefaultLifecycleAndResize(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "same", Provider: "codex", Status: StatusActive, Metadata: map[string]any{}}
	if _, err := m.Register(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	releases := make([]func(), 0, 20)
	for range 20 {
		r, err := m.AcquireCredentialGeneration(context.Background(), a.ID)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, r)
	}
	ctx := context.WithValue(context.Background(), concurrencyBudgetKey{}, &concurrencyBudget{spent: credentialWaitBudget})
	if _, err := m.AcquireCredentialGeneration(ctx, a.ID); !isConcurrencyError(err) {
		t.Fatalf("default 20 not enforced: %v", err)
	}
	_, _, err := m.Modify(context.Background(), a.ID, 0, func(current *Auth) error { current.Metadata[AttributeConcurrencyLimit] = 1; return nil })
	if err != nil {
		t.Fatal(err)
	}
	assertActive(t, m, a.ID, 20)
	base, _ := m.GetByID(a.ID)
	refreshed := base.Clone()
	refreshed.Metadata["access_token"] = "new-test-token"
	if _, err = m.UpdateRefreshedAuth(context.Background(), base, refreshed); err != nil {
		t.Fatal(err)
	}
	assertActive(t, m, a.ID, 20)
	if _, err = m.Register(context.Background(), &Auth{ID: a.ID, Provider: "codex", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	current, _ := m.GetByID(a.ID)
	if limit, _ := credentialConcurrencyLimit(current); limit != 1 {
		t.Fatalf("relogin erased override: %d", limit)
	}
	for _, r := range releases {
		r()
		r()
	}
	assertActive(t, m, a.ID, 0)
	old, err := m.AcquireCredentialGeneration(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.Remove(context.Background(), a.ID)
	if _, err = m.Register(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	next, err := m.AcquireCredentialGeneration(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer next()
	old()
	assertActive(t, m, a.ID, 1)
}

func TestCredentialConcurrencyWaitWakeQueueAndBudget(t *testing.T) {
	m, _, ids := concurrencyFixture(t, nil)
	hold, err := m.AcquireCredentialGeneration(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	defer hold()
	m.concurrency.mu.Lock()
	m.concurrency.waiting = credentialWaitLimit
	m.concurrency.mu.Unlock()
	if _, err = m.AcquireCredentialGeneration(context.Background(), ids[0]); err == nil || err.(*Error).Code != "credential_queue_full" {
		t.Fatalf("queue bound=%v", err)
	}
	m.concurrency.mu.Lock()
	m.concurrency.waiting = 0
	m.concurrency.mu.Unlock()
	// An expired accumulated budget cannot restart after a capacity notification.
	ctx := context.WithValue(context.Background(), concurrencyBudgetKey{}, &concurrencyBudget{spent: credentialWaitBudget})
	if _, err = m.AcquireCredentialGeneration(ctx, ids[0]); err == nil || err.(*Error).Code != "credential_concurrency_timeout" {
		t.Fatalf("budget=%v", err)
	}
	// Waiting observes a limit increase without waiting for an active request to finish.
	waiting := make(chan func(), 1)
	fail := make(chan error, 1)
	go func() {
		r, e := m.AcquireCredentialGeneration(context.Background(), ids[0])
		if e != nil {
			fail <- e
		} else {
			waiting <- r
		}
	}()
	if _, _, err = m.Modify(context.Background(), ids[0], 0, func(a *Auth) error { a.Metadata[AttributeConcurrencyLimit] = 2; return nil }); err != nil {
		t.Fatal(err)
	}
	release := awaitValue(t, waiting)
	release()
	assertActive(t, m, ids[0], 1)
	select {
	case err := <-fail:
		t.Fatal(err)
	default:
	}
}

func TestCredentialConcurrencyHTTPCountAndVerifyAdmission(t *testing.T) {
	for _, path := range []string{"execute", "count", "verify"} {
		t.Run(path, func(t *testing.T) {
			m, exec, ids := concurrencyFixture(t, nil)
			hold, err := m.AcquireCredentialGeneration(context.Background(), ids[0])
			if err != nil {
				t.Fatal(err)
			}
			defer hold()
			ctx := context.WithValue(context.Background(), concurrencyBudgetKey{}, &concurrencyBudget{spent: credentialWaitBudget})
			opts := core.Options{Metadata: map[string]any{core.PinnedAuthMetadataKey: ids[0]}}
			switch path {
			case "execute":
				_, err = m.Execute(ctx, []string{"claude"}, core.Request{Model: "test-model"}, opts)
			case "count":
				_, err = m.ExecuteCount(ctx, []string{"claude"}, core.Request{Model: "test-model"}, opts)
			case "verify":
				_, err = m.VerifyInference(ctx, ids[0], "test-model")
			}
			if !isConcurrencyError(err) {
				t.Fatalf("path bypassed admission: %v", err)
			}
			if exec.executeCalls.Load()+exec.countCalls.Load()+exec.streamCalls.Load() != 0 {
				t.Fatal("upstream called while capacity exhausted")
			}
			a, _ := m.GetByID(ids[0])
			if a.LastError != nil || a.Unavailable {
				t.Fatal("local queue changed upstream health")
			}
		})
	}
}

func TestCredentialConcurrencySSEHoldsUntilSourceExit(t *testing.T) {
	m, exec, ids := concurrencyFixture(t, nil)
	source := make(chan core.StreamChunk, 1)
	source <- core.StreamChunk{Payload: []byte(`data: {"type":"response.created"}`)}
	upstreamCancelled := make(chan struct{})
	allowExit := make(chan struct{})
	var once sync.Once
	exec.streamFn = func(ctx context.Context, _ *Auth) (*core.StreamResult, error) {
		go func() { <-ctx.Done(); once.Do(func() { close(upstreamCancelled) }); <-allowExit; close(source) }()
		return &core.StreamResult{Chunks: source}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := m.ExecuteStream(ctx, []string{"claude"}, core.Request{Model: "test-model"}, core.Options{Metadata: map[string]any{core.PinnedAuthMetadataKey: ids[0]}})
	if err != nil {
		t.Fatal(err)
	}
	_ = awaitValue(t, result.Chunks)
	assertActive(t, m, ids[0], 1)
	cancel()
	awaitValue(t, upstreamCancelled)
	assertActive(t, m, ids[0], 1)
	close(allowExit)
	for range result.Chunks {
	}
	assertActive(t, m, ids[0], 0)
}

func TestCredentialConcurrencyCancelledWaitDoesNotLeak(t *testing.T) {
	m, _, ids := concurrencyFixture(t, nil)
	release, err := m.AcquireCredentialGeneration(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.AcquireCredentialGeneration(ctx, ids[0]); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	snapshot := m.CredentialConcurrencySnapshot()
	if snapshot["pool_waiting"] != 0 {
		t.Fatal(snapshot)
	}
}

func TestCredentialConcurrencyCumulativeWaitAndHome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := NewManager(nil, nil, nil)
		_, err := m.Register(context.Background(), &Auth{ID: "one", Provider: "codex", Status: StatusActive, Metadata: map[string]any{AttributeConcurrencyLimit: 1}})
		if err != nil {
			t.Fatal(err)
		}
		release, err := m.AcquireCredentialGeneration(context.Background(), "one")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		done := make(chan error, 1)
		start := time.Now()
		go func() {
			r, e := m.AcquireCredentialGeneration(context.Background(), "one")
			if r != nil {
				r()
			}
			done <- e
		}()
		synctest.Wait()
		if snapshot := m.CredentialConcurrencySnapshot(); snapshot["credentials"].([]CredentialConcurrencyEntry)[0].PinnedWaiting != 1 {
			t.Fatal(snapshot)
		}
		// synctest advances virtual time; notifications must not reset the 30-second budget.
		time.Sleep(10 * time.Second)
		m.concurrency.changed("one", &Auth{Provider: "codex"})
		synctest.Wait()
		time.Sleep(20 * time.Second)
		synctest.Wait()
		select {
		case err = <-done:
			if !isConcurrencyError(err) {
				t.Fatal(err)
			}
		default:
			t.Fatal("notification reset the cumulative wait budget")
		}
		if elapsed := time.Since(start); elapsed != 30*time.Second {
			t.Fatal(elapsed)
		}
		m.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
		homeRelease, err := m.AcquireCredentialGeneration(context.Background(), "one")
		if err != nil {
			t.Fatal(err)
		}
		homeRelease()
		snapshot := m.CredentialConcurrencySnapshot()
		if snapshot["mode"] != "home" || len(snapshot) != 2 {
			t.Fatal(snapshot)
		}
		assertActive(t, m, "one", 1)
	})
}

func TestCredentialConcurrencyReplacementSeparatesOldLease(t *testing.T) {
	m := NewManager(nil, nil, nil)
	_, err := m.Register(context.Background(), &Auth{ID: "file.json", Provider: "codex", Status: StatusActive, Metadata: map[string]any{"email": "first@example.invalid", AttributeConcurrencyLimit: 1}})
	if err != nil {
		t.Fatal(err)
	}
	old, err := m.AcquireCredentialGeneration(context.Background(), "file.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Register(context.Background(), &Auth{ID: "file.json", Provider: "codex", Status: StatusActive, Metadata: map[string]any{"email": "second@example.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := m.GetByID("file.json")
	if limit, _ := credentialConcurrencyLimit(a); limit != 20 {
		t.Fatal("replacement inherited old policy")
	}
	release, err := m.AcquireCredentialGeneration(context.Background(), "file.json")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	old()
	assertActive(t, m, "file.json", 1)
}
