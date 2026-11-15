package cliproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const m6QuotaFailure = `{"type":"error","code":"subscription_sharing_usage_limit_exceeded","message":"synthetic-private-upstream"}`

func m6Failure(frame string, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, frame)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+frame+"\n\ndata: "+kiloTerminal+"\n\n")
	}
}
func m6Fixture(t *testing.T, handler http.HandlerFunc, bind bool) (*kiloNativeFixture, string) {
	t.Helper()
	f := newKiloNativeFixture(t, handler, true)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	if bind {
		m5Codex(t, f, "codex-recovery.json", "outer-recovery", "synthetic-outer-recovery", false, false)
		m5Bind(t, f, id, 0, `{"kind":"cpa_codex","credential_id":"codex-recovery.json","chatgpt_account_id":"outer-recovery"}`)
	}
	return f, id
}
func m6Request(t *testing.T, f *kiloNativeFixture, stream bool) {
	t.Helper()
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"test","stream":%t}`, stream))
	if status < 400 {
		t.Fatal("upstream failure completed")
	}
	f.assertReleased(t, 1)
}
func m6Usage(t *testing.T, f *kiloNativeFixture, calls *atomic.Int32, reset time.Time) {
	t.Helper()
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer synthetic-outer-recovery" {
			t.Error("wrong usage path/source")
		}
		_, _ = fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":100,"reset_at":%d}}}`, reset.Unix())
	}
}
func TestKiloM6NativePreciseReasons(t *testing.T) {
	for _, sample := range []struct {
		name, body, reason string
		status             int
	}{
		{"quota", m6QuotaFailure, "quota_exhausted", 200},
		{"restricted", `{"error":{"code":"account_restricted","type":"account_restricted","message":"private"}}`, "account_restricted", 403},
		{"sse-restricted", `{"error":{"code":"account_restricted","type":"account_restricted","message":"private"}}`, "none", 200},
		{"plain403", `{"error":{"code":"ordinary_forbidden","message":"private"}}`, "none", 403},
		{"plain429", `{"error":{"code":"rate_limit_exceeded","message":"private"}}`, "none", 429},
		{"unknown", `{"code":"unknown_error","message":"private"}`, "none", 200},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", sample.name, stream), func(t *testing.T) {
				f, id := m6Fixture(t, m6Failure(sample.body, sample.status), false)
				m6Request(t, f, stream)
				v := f.manager.KiloRestriction(id)
				a, _ := f.manager.GetByID(id)
				if v.Reason != sample.reason || a.Disabled || !v.Persisted || f.posts.Load() != 1 {
					t.Fatalf("reason=%+v auth=%+v", v, a)
				}
				f.manager.ProcessKiloRecovery(context.Background(), time.Now().Add(time.Hour))
				if f.posts.Load() != 1 || f.usageDirect.Load() != 0 {
					t.Fatal("no source/restricted performed network")
				}
				if sample.reason != "none" {
					status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"blocked"}`)
					if status < 400 || f.posts.Load() != 1 {
						t.Fatal("restricted account admitted")
					}
				}
				body := m2Request(t, f, "GET", kiloAccountsURL, "", 200)
				if !ojson.GetBytes(body, "accounts.0.enabled").Bool() || ojson.GetBytes(body, "accounts.0.auto_restriction.reason").String() != sample.reason || bytes.Contains(body, []byte("private")) {
					t.Fatalf("public=%s", body)
				}
				t.Logf("reason=%s HTTP=%d stream=%t POST1 result1 released", v.Reason, sample.status, stream)
			})
		}
	}
}
func TestKiloM6NativeQuotaRecoveryAndRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint(restart), func(t *testing.T) {
			f, id := m6Fixture(t, m6Failure(m6QuotaFailure, 200), true)
			var calls atomic.Int32
			reset := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
			m6Usage(t, f, &calls, reset)
			m6Request(t, f, false)
			f.manager.ProcessKiloRecovery(context.Background(), reset.Add(-time.Second))
			v := f.manager.KiloRestriction(id)
			if v.Reason != "quota_exhausted" || v.RecoverAt == nil || !v.RecoverAt.Equal(reset) || !v.Persisted || calls.Load() != 1 {
				t.Fatalf("plan=%+v usage=%d", v, calls.Load())
			}
			raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
			if err != nil || !bytes.Contains(raw, []byte("kilo_auto_restriction")) || bytes.Contains(raw, []byte("synthetic-outer")) {
				t.Fatalf("persistence=%s err=%v", raw, err)
			}
			if restart {
				m2Reload(t, f)
				v = f.manager.KiloRestriction(id)
				if v.RecoverAt == nil || v.QueryStatus == "running" {
					t.Fatalf("restart=%+v", v)
				}
			}
			f.manager.ProcessKiloRecovery(context.Background(), reset.Add(-time.Nanosecond))
			if f.manager.KiloRestriction(id).Reason != "quota_exhausted" {
				t.Fatal("early recovery")
			}
			f.manager.ProcessKiloRecovery(context.Background(), reset)
			f.manager.ProcessKiloRecovery(context.Background(), reset.Add(time.Hour))
			if v = f.manager.KiloRestriction(id); v.Reason != "none" || !v.Persisted || calls.Load() != 1 || f.posts.Load() != 1 {
				t.Fatalf("recovery=%+v usage=%d model=%d", v, calls.Load(), f.posts.Load())
			}
			a, _ := f.manager.GetByID(id)
			if a.Disabled {
				t.Fatal("manual state modified")
			}
			m2Reload(t, f)
			if f.manager.KiloRestriction(id).Reason != "none" {
				t.Fatal("recovery not saved")
			}
			t.Log("real inference1 -> usage1 -> stored future reset -> clock due -> stored recovery; recovery model0; restart no pending/leases")
		})
	}
}

type m6FailStore struct {
	coreauth.Store
	fail   atomic.Bool
	writes atomic.Int32
}

func (s *m6FailStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	s.writes.Add(1)
	if s.fail.Load() {
		return "", errors.New("synthetic storage failure")
	}
	return s.Store.Save(ctx, a)
}
func TestKiloM6NativePersistenceDirections(t *testing.T) {
	f, id := m6Fixture(t, m6Failure(m6QuotaFailure, 200), true)
	var calls atomic.Int32
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	m6Usage(t, f, &calls, reset)
	store := &m6FailStore{Store: m2FileStore(f)}
	f.manager.SetStore(store)
	store.fail.Store(true)
	m6Request(t, f, false)
	if v := f.manager.KiloRestriction(id); v.Reason != "quota_exhausted" || v.Persisted {
		t.Fatalf("limit save failure=%+v", v)
	}
	f.manager.ProcessKiloRecovery(context.Background(), reset.Add(-time.Second))
	if v := f.manager.KiloRestriction(id); v.RecoverAt == nil || v.Persisted {
		t.Fatalf("plan save failure=%+v", v)
	}
	f.manager.ProcessKiloRecovery(context.Background(), reset)
	if f.manager.KiloRestriction(id).Reason != "quota_exhausted" {
		t.Fatal("failed save released account")
	}
	store.fail.Store(false)
	f.manager.ProcessKiloRecovery(context.Background(), reset)
	if v := f.manager.KiloRestriction(id); v.Reason != "none" || calls.Load() != 1 || f.posts.Load() != 1 {
		t.Fatalf("recovery retry=%+v", v)
	}
	m2Reload(t, f)
	if f.manager.KiloRestriction(id).Reason != "none" {
		t.Fatal("retry not persisted")
	}
	t.Log("limit/save failure protects runtime; due save failure retains restriction; retry only local save, usage1 model1 total")
}
func TestKiloM6NativeFailedStaleAndNoReset(t *testing.T) {
	for _, mode := range []string{"failed-stale", "no-reset"} {
		t.Run(mode, func(t *testing.T) {
			f, id := m6Fixture(t, m6Failure(m6QuotaFailure, 200), true)
			var calls atomic.Int32
			reset := time.Now().Add(time.Hour)
			m6Usage(t, f, &calls, reset)
			if mode == "failed-stale" {
				m5Query(t, f, id, 1)
				f.usageHandler = func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(401) }
			} else {
				f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = io.WriteString(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":100}}}`)
				}
			}
			m6Request(t, f, false)
			f.manager.ProcessKiloRecovery(context.Background(), time.Now())
			v := f.manager.KiloRestriction(id)
			if v.RecoverAt != nil {
				t.Fatalf("stale or missing reset created plan=%+v", v)
			}
			count := calls.Load()
			f.manager.ProcessKiloRecovery(context.Background(), time.Now().Add(24*time.Hour))
			if calls.Load() != count || f.posts.Load() != 1 {
				t.Fatal("failed query retried")
			}
			m6Usage(t, f, &calls, reset)
			m5Query(t, f, id, 1)
			if f.manager.KiloRestriction(id).RecoverAt == nil {
				t.Fatal("explicit successful query not consumed")
			}
			t.Logf("%s creates no plan; no periodic retry; explicit fresh success creates plan", mode)
		})
	}
}

func TestKiloM6NativeLateQuotaAndManualActions(t *testing.T) {
	for _, action := range []string{"enabled", "disabled", "key", "source", "delete"} {
		t.Run(action, func(t *testing.T) {
			f, id := m6Fixture(t, m6Failure(m6QuotaFailure, 200), true)
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseGate := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(releaseGate)
			var calls atomic.Int32
			reset := time.Now().Add(time.Hour).Truncate(time.Second)
			f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				close(started)
				<-release
				_, _ = fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":100,"reset_at":%d}}}`, reset.Unix())
			}
			m6Request(t, f, false)
			done := make(chan struct{})
			go func() { defer close(done); f.manager.ProcessKiloRecovery(context.Background(), time.Now()) }()
			<-started
			switch action {
			case "enabled":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":true}`, 200)
			case "disabled":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
			case "key":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-new-m6"}`, 200)
			case "source":
				m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id+"/quota-source", `{"expected_binding_revision":1}`, 200)
			case "delete":
				m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 204)
			}
			releaseGate()
			<-done
			v := f.manager.KiloRestriction(id)
			if v.RecoverAt != nil {
				t.Fatalf("late plan=%+v", v)
			}
			f.manager.ProcessKiloRecovery(context.Background(), reset.Add(time.Hour))
			a, exists := f.manager.GetByID(id)
			if action == "delete" {
				if exists {
					t.Fatal("deleted identity restored")
				}
			} else {
				if (action == "enabled" && v.Reason != "none") || (action != "enabled" && v.Reason != "quota_exhausted") || (action == "disabled" && !a.Disabled) {
					t.Fatalf("action=%s restriction=%+v auth=%+v", action, v, a)
				}
			}
			if calls.Load() != 1 || f.posts.Load() != 1 {
				t.Fatal("late action repeated network")
			}
			t.Logf("%s invalidates late query/old due plan; explicit same enabled=true clears only auto restriction", action)
		})
	}
}
func TestKiloM6NativeExistingPlanAndIndependentCooldown(t *testing.T) {
	f, id := m6Fixture(t, m6Failure(m6QuotaFailure, 200), true)
	var calls atomic.Int32
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	m6Usage(t, f, &calls, reset)
	m6Request(t, f, false)
	f.manager.ProcessKiloRecovery(context.Background(), time.Now())
	before := f.manager.KiloRestriction(id)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(401) }
	m5Query(t, f, id, 1)
	after := f.manager.KiloRestriction(id)
	if before.RecoverAt == nil || after.RecoverAt == nil || !before.RecoverAt.Equal(*after.RecoverAt) || after.QueryStatus != "failed" {
		t.Fatalf("failed query moved plan before=%+v after=%+v", before, after)
	}
	a, _ := f.manager.GetByID(id)
	until := reset.Add(24 * time.Hour)
	a.ModelStates = map[string]*coreauth.ModelState{"other-model": {Unavailable: true, NextRetryAfter: until, LastError: &coreauth.Error{Code: "other_failure", HTTPStatus: 403, Message: "other failure"}}}
	a.LastError = &coreauth.Error{Code: "authentication_error", HTTPStatus: 401, Message: "unauthorized"}
	a.Unavailable = true
	if _, err := f.manager.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	f.manager.ProcessKiloRecovery(context.Background(), reset)
	current, _ := f.manager.GetByID(id)
	if v := f.manager.KiloRestriction(id); v.Reason != "none" || current.LastError == nil || current.LastError.HTTPStatus != 401 || current.ModelStates["other-model"] == nil || !current.ModelStates["other-model"].NextRetryAfter.Equal(until) {
		t.Fatalf("independent state lost restriction=%+v auth=%+v", v, current)
	}
	if calls.Load() != 2 || f.posts.Load() != 1 {
		t.Fatal("recovery repeated upstream")
	}
	t.Log("failed/stale explicit query preserves same plan; recovery retains independent authentication and other-model deadline")
}
func TestKiloM6NativeRestrictedManualRestore(t *testing.T) {
	f, id := m6Fixture(t, m6Failure(`{"error":{"code":"account_restricted"}}`, 403), false)
	m6Request(t, f, false)
	before := f.manager.KiloRestriction(id)
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"name":"ordinary-edit"}`, 200)
	if v := f.manager.KiloRestriction(id); v.Reason != "account_restricted" || v.Generation != before.Generation {
		t.Fatal("ordinary edit restored account")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":true}`, 200)
	if v := f.manager.KiloRestriction(id); v.Reason != "none" || v.Generation <= before.Generation {
		t.Fatal("same-value enable did not restore")
	}
	m2Reload(t, f)
	if f.manager.KiloRestriction(id).Reason != "none" {
		t.Fatal("manual restore not durable")
	}
}
func TestKiloM6NativeHostCancellation(t *testing.T) {
	f, id := m6Fixture(t, m6Failure(m6QuotaFailure, 200), true)
	started := make(chan struct{})
	var calls atomic.Int32
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) { calls.Add(1); close(started); <-r.Context().Done() }
	m6Request(t, f, false)
	ctx, cancel := context.WithCancel(context.Background())
	f.manager.StartKiloRecovery(ctx)
	<-started
	cancel()
	f.manager.StopKiloRecovery()
	v := f.manager.KiloRestriction(id)
	if v.RecoverAt != nil || v.Reason != "quota_exhausted" || v.QueryStatus == "running" {
		t.Fatalf("cancel state=%+v", v)
	}
	f.manager.ProcessKiloRecovery(ctx, time.Now().Add(time.Hour))
	if calls.Load() != 1 || f.posts.Load() != 1 {
		t.Fatal("host cancel started work")
	}
	m2Reload(t, f)
	v = f.manager.KiloRestriction(id)
	if v.QueryStatus == "running" || v.RecoverAt != nil {
		t.Fatal("restart restored in-flight task")
	}
}

func TestKiloM6NativeFreshNoResetWithdrawsPlan(t *testing.T) {
	f, id := m6Fixture(t, m6Failure(m6QuotaFailure, 200), true)
	var calls atomic.Int32
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	m6Usage(t, f, &calls, reset)
	m6Request(t, f, false)
	f.manager.ProcessKiloRecovery(context.Background(), time.Now())
	if f.manager.KiloRestriction(id).RecoverAt == nil {
		t.Fatal("initial future plan missing")
	}
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"used_percent":0}}}`)
	}
	m5Query(t, f, id, 1)
	v := f.manager.KiloRestriction(id)
	if v.RecoverAt != nil || v.QueryStatus != "no_future_reset" || !v.Persisted {
		t.Fatalf("fresh no reset retained plan=%+v", v)
	}
	m2Reload(t, f)
	f.manager.ProcessKiloRecovery(context.Background(), reset.Add(time.Hour))
	if v = f.manager.KiloRestriction(id); v.Reason != "quota_exhausted" || v.RecoverAt != nil || calls.Load() != 2 || f.posts.Load() != 1 {
		t.Fatalf("withdrawal not durable=%+v", v)
	}
	t.Log("fresh successful query without future reset withdraws old plan durably; failed queries keep prior same-generation plan")
}
