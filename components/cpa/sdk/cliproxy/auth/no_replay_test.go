package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type noReplayRoundTripper func(*http.Request) (*http.Response, error)

func (f noReplayRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type noReplayTestExecutor struct {
	pool                                 *kilo.ProxyPool
	transport                            http.RoundTripper
	mode                                 string
	cancel                               context.CancelFunc
	posts, refreshes, acquired, released atomic.Int32
	ids                                  []string
}

func (*noReplayTestExecutor) Identifier() string               { return "kilo" }
func (e *noReplayTestExecutor) EgressChanges() <-chan struct{} { return e.pool.Changes() }
func (e *noReplayTestExecutor) TryAcquireEgress(a *Auth) (*Auth, func(), bool, error) {
	lease, err := e.pool.TryAcquire(a.ID, kilo.AcquireOptions{})
	if errors.Is(err, kilo.ErrProxyCapacity) {
		return nil, nil, true, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	e.acquired.Add(1)
	clone := a.Clone()
	clone.Runtime = lease.Route()
	return clone, func() { lease.Release(); e.released.Add(1) }, false, nil
}
func (e *noReplayTestExecutor) submit(ctx context.Context, a *Auth) error {
	e.ids = append(e.ids, a.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://kilo.invalid/api/v1/responses", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	core.MarkUpstreamAttempt(ctx)
	resp, err := e.transport.RoundTrip(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if e.cancel != nil {
		e.cancel()
		return context.Canceled
	}
	if resp.StatusCode >= 400 {
		return &Error{Code: "synthetic_upstream", Message: http.StatusText(resp.StatusCode), HTTPStatus: resp.StatusCode}
	}
	return nil
}
func (e *noReplayTestExecutor) Execute(ctx context.Context, a *Auth, _ core.Request, _ core.Options) (core.Response, error) {
	return core.Response{}, e.submit(ctx, a)
}
func (e *noReplayTestExecutor) CountTokens(ctx context.Context, a *Auth, r core.Request, o core.Options) (core.Response, error) {
	return e.Execute(ctx, a, r, o)
}
func (e *noReplayTestExecutor) ExecuteStream(ctx context.Context, a *Auth, _ core.Request, _ core.Options) (*core.StreamResult, error) {
	err := e.submit(ctx, a)
	if e.mode == "stream" || e.cancel != nil {
		return nil, err
	}
	ch := make(chan core.StreamChunk, 1)
	switch e.mode {
	case "bootstrap":
		ch <- core.StreamChunk{Err: err}
	case "done":
		ch <- core.StreamChunk{Payload: []byte("data: [DONE]\n\n")}
	}
	close(ch)
	return &core.StreamResult{Chunks: ch}, nil
}
func (e *noReplayTestExecutor) Refresh(_ context.Context, a *Auth) (*Auth, error) {
	e.refreshes.Add(1)
	return a, nil
}
func (e *noReplayTestExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func noReplayFixture(t *testing.T, status int, mode string) (*Manager, *noReplayTestExecutor, *captureResultHook, context.Context, string, []string) {
	t.Helper()
	pool, err := kilo.NewProxyPool([]kilo.Proxy{{ID: "synthetic-proxy", Healthy: true}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	e := &noReplayTestExecutor{pool: pool, mode: mode}
	e.transport = noReplayRoundTripper(func(r *http.Request) (*http.Response, error) {
		e.posts.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/responses" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	hook := &captureResultHook{}
	m := NewManager(nil, nil, hook)
	m.SetConfig(&config.Config{AuthDir: t.TempDir()})
	m.SetRetryConfig(3, 0, 3)
	m.RegisterExecutor(e)
	ids := []string{t.Name() + "-a", t.Name() + "-b"}
	model := t.Name() + "-model"
	for _, id := range ids {
		registry.GetGlobalRegistry().RegisterClient(id, "kilo", []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
		if _, err := m.Register(context.Background(), &Auth{ID: id, Provider: "kilo", Status: StatusActive, Metadata: map[string]any{"access_token": "synthetic", "refresh_token": "synthetic", KiloMachineIDKey: uuid.NewString()}}); err != nil {
			t.Fatal(err)
		}
	}
	return m, e, hook, channelscope.With(context.Background(), "kilo"), model, ids
}
func executeNoReplayMode(m *Manager, ctx context.Context, mode, model string) error {
	req := core.Request{Model: model, Payload: []byte("{}")}
	opts := core.Options{Metadata: map[string]any{core.CallerScopeMetadataKey: "synthetic-no-replay-caller"}}
	switch mode {
	case "execute":
		_, err := m.Execute(ctx, []string{"kilo"}, req, opts)
		return err
	case "count":
		_, err := m.ExecuteCount(ctx, []string{"kilo"}, req, opts)
		return err
	default:
		result, err := m.ExecuteStream(ctx, []string{"kilo"}, req, opts)
		if err != nil {
			return err
		}
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				err = chunk.Err
			}
		}
		return err
	}
}
func assertNoReplayReleased(t *testing.T, m *Manager, e *noReplayTestExecutor, ids []string) {
	t.Helper()
	if got := e.posts.Load(); got != 1 {
		t.Fatalf("POST=%d want 1", got)
	}
	if len(e.ids) != 1 {
		t.Fatalf("attempted accounts=%v", e.ids)
	}
	if e.refreshes.Load() != 0 {
		t.Fatalf("refresh=%d", e.refreshes.Load())
	}
	if e.acquired.Load() != 1 || e.released.Load() != 1 {
		t.Fatalf("egress acquired/released=%d/%d", e.acquired.Load(), e.released.Load())
	}
	for _, id := range ids {
		assertActive(t, m, id, 0)
	}
	for _, p := range e.pool.Snapshot().Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatalf("proxy leaked: %+v", p)
		}
	}
}
func TestNoReplayRecordsOnceAndReleasesJointLease(t *testing.T) {
	for _, mode := range []string{"execute", "count", "stream", "bootstrap"} {
		for _, status := range []int{401, 429, 502} {
			t.Run(fmt.Sprintf("%s/%d", mode, status), func(t *testing.T) {
				m, e, hook, ctx, model, ids := noReplayFixture(t, status, mode)
				err := executeNoReplayMode(m, ctx, mode, model)
				if statusCodeFromError(err) != status || !channelscope.NoReplay(err) {
					t.Fatalf("lost status or marker: %v", err)
				}
				assertNoReplayReleased(t, m, e, ids)
				hook.mu.Lock()
				defer hook.mu.Unlock()
				if len(hook.results) != 1 {
					t.Fatalf("result registrations=%d", len(hook.results))
				}
				result := hook.results[0]
				if result.Error == nil || result.Error.HTTPStatus != status || result.Success {
					t.Fatalf("classification=%+v", result)
				}
				a, _ := m.GetByID(result.AuthID)
				if a.Failed != 1 {
					t.Fatalf("failure accounting=%d", a.Failed)
				}
			})
		}
	}
}
func TestNoReplayCanceledAttemptIsAvailabilityNeutral(t *testing.T) {
	for _, mode := range []string{"execute", "count", "stream"} {
		t.Run(mode, func(t *testing.T) {
			m, e, hook, parent, model, ids := noReplayFixture(t, 200, mode)
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			e.cancel = cancel
			if err := executeNoReplayMode(m, ctx, mode, model); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel=%v", err)
			}
			assertNoReplayReleased(t, m, e, ids)
			hook.mu.Lock()
			defer hook.mu.Unlock()
			if len(hook.results) != 1 {
				t.Fatalf("result registrations=%d", len(hook.results))
			}
			a, _ := m.GetByID(hook.results[0].AuthID)
			if a.Status != StatusActive || a.Unavailable || a.LastError != nil {
				t.Fatalf("cancellation changed availability: %+v", a)
			}
		})
	}
}
func TestNoReplayEmptyAndDoneStreamsDoNotRedispatch(t *testing.T) {
	for _, mode := range []string{"empty", "done"} {
		t.Run(mode, func(t *testing.T) {
			m, e, hook, ctx, model, ids := noReplayFixture(t, 200, mode)
			err := executeNoReplayMode(m, ctx, mode, model)
			if err == nil || !channelscope.NoReplay(err) {
				t.Fatalf("missing terminal error: %v", err)
			}
			assertNoReplayReleased(t, m, e, ids)
			hook.mu.Lock()
			defer hook.mu.Unlock()
			if len(hook.results) != 1 || hook.results[0].Success {
				t.Fatalf("result registrations=%+v", hook.results)
			}
		})
	}
}
func TestJointEgressAdmissionReleasesAccountWhileWaiting(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprint(pinned), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, e, _, ctx, model, ids := noReplayFixture(t, 200, "execute")
				blocker, err := e.pool.TryAcquire("outside", kilo.AcquireOptions{})
				if err != nil {
					t.Fatal(err)
				}
				opts := core.Options{}
				if pinned {
					opts.Metadata = map[string]any{core.PinnedAuthMetadataKey: ids[0]}
				}
				done := make(chan error, 1)
				go func() {
					a, _, _, lease, err := m.pickNextMixedAdmitted(ctx, []string{"kilo"}, model, opts, nil)
					if err == nil {
						if pinned && a.ID != ids[0] {
							err = fmt.Errorf("changed pinned account: %s", a.ID)
						}
						lease.release()
					}
					done <- err
				}()
				synctest.Wait()
				for _, id := range ids {
					assertActive(t, m, id, 0)
				}
				if e.acquired.Load() != 0 {
					t.Fatal("exit acquired while full")
				}
				blocker.Release()
				synctest.Wait()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if e.acquired.Load() != 1 || e.released.Load() != 1 {
					t.Fatal("joint lease not released exactly once")
				}
			})
		})
	}
}
func TestJointEgressAdmissionUsesRemainingBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, e, _, ctx, model, ids := noReplayFixture(t, 200, "execute")
		hold, err := e.pool.TryAcquire("outside", kilo.AcquireOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Release()
		budget := &concurrencyBudget{spent: 29 * time.Second}
		ctx = context.WithValue(ctx, concurrencyBudgetKey{}, budget)
		start := time.Now()
		_, _, _, _, err = m.pickNextMixedAdmitted(ctx, []string{"kilo"}, model, core.Options{}, nil)
		if !isConcurrencyError(err) || time.Since(start) != time.Second || budget.spent != 30*time.Second {
			t.Fatalf("wait=%s spent=%s error=%v", time.Since(start), budget.spent, err)
		}
		for _, id := range ids {
			assertActive(t, m, id, 0)
		}
	})
}

func TestChannelScopeManagerBoundary(t *testing.T) {
	for _, scope := range []string{"", "codex", "bps"} {
		t.Run(scope, func(t *testing.T) {
			m, e, _, _, model, _ := noReplayFixture(t, 401, "execute")
			ctx := channelscope.With(context.Background(), scope)
			for _, mode := range []string{"execute", "count", "stream"} {
				if err := executeNoReplayMode(m, ctx, mode, model); err == nil {
					t.Fatalf("scope %q reached kilo", scope)
				}
			}
			if e.posts.Load() != 0 || e.acquired.Load() != 0 {
				t.Fatal("out-of-scope account or upstream touched")
			}
		})
	}
}
