package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func m8bConfig(t *testing.T, f *kiloNativeFixture, id string, revision int, health, model bool, interval int) []byte {
	t.Helper()
	return m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/probes", fmt.Sprintf(`{"expected_revision":%d,"health":{"enabled":%t},"model_probe":{"enabled":%t,"model":"gpt-6.1-sol","interval_seconds":%d}}`, revision, health, model, interval), 200)
}

func m8bDue(t *testing.T, raw []byte, kind string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, ojson.GetBytes(raw, kind+".next_run_at").String())
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestKiloM8BDefaultOffAndCycles(t *testing.T) {
	var serial atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
			return
		}
		m8bSuccess(w, serial.Add(1))
	}, true)
	f.allowAutoFree = true
	id := m2Create(t, f, "")
	path := kiloAccountsURL + "/" + id + "/probes"
	raw := m2Request(t, f, "GET", path, "", 200)
	if ojson.GetBytes(raw, "health.enabled").Bool() || ojson.GetBytes(raw, "model_probe.enabled").Bool() || ojson.GetBytes(raw, "health.status").String() != "never" {
		t.Fatalf("defaults=%s", raw)
	}
	<-f.manager.ProcessKiloProbes(t.Context(), time.Now().Add(time.Hour))
	if f.posts.Load() != 0 {
		t.Fatal("default generated")
	}
	raw = m8bConfig(t, f, id, 0, true, true, 300)
	due := m8bDue(t, raw, "health")
	<-f.manager.ProcessKiloProbes(t.Context(), due.Add(-time.Nanosecond))
	if f.posts.Load() != 0 {
		t.Fatal("early generation")
	}
	for i := 0; i < 4; i++ {
		<-f.manager.ProcessKiloProbes(t.Context(), due.Add(time.Duration(i)*300*time.Second))
		view, _ := f.manager.KiloProbes(id)
		if (i%2 == 0 && view.Health.Status != "completed") || (i%2 == 1 && view.ModelProbe.Status != "completed") {
			t.Fatalf("cycle %d=%+v", i, view)
		}
	}
	raw = m2Request(t, f, "GET", path, "", 200)
	if ojson.GetBytes(raw, "health.model_calls").Int() != 2 || ojson.GetBytes(raw, "model_probe.model_calls").Int() != 2 || f.posts.Load() != 4 {
		t.Fatalf("fair cycles=%s posts=%d", raw, f.posts.Load())
	}
	for i := 0; i < 4; i++ {
		wire := <-f.requests
		if ojson.GetBytes(wire, "model").String() == "kilo-auto/free" {
			if ojson.GetBytes(wire, "messages.0.content").String() != "hi" {
				t.Fatalf("health payload=%s", wire)
			}
		} else if ojson.GetBytes(wire, "input").String() != "hi" {
			t.Fatalf("model payload=%s", wire)
		}
	}
	f.assertReleased(t, 4)
	t.Logf("default POST0; full cycles; fair 2+2; real protocols/hi; %s", raw)
}

func m8bSuccess(w http.ResponseWriter, serial int32) {
	doc := ojson.NewDocString(kiloTerminal)
	_ = doc.Set("response.id", fmt.Sprintf("resp-auto-%d", serial))
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: %s\n\n", doc.String())
}

func m8bAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not finish")
	}
}

func TestKiloM8BConfigurationBoundary(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	path := kiloAccountsURL + "/" + id + "/probes"
	valid := `{"expected_revision":0,"health":{"enabled":false},"model_probe":{"enabled":true,"model":"gpt-6.1-sol","interval_seconds":60}}`
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key", "synthetic-bps-key", "synthetic-legacy-key", ""} {
		for _, method := range []string{"GET", "PUT"} {
			m2Request(t, f, "GET", path, "", 200)
			status, _ := f.request(t, method, path, key, valid)
			if status != 401 {
				t.Fatalf("inference key allowed probes %s: %d", method, status)
			}
		}
	}
	for _, body := range []string{`{}`, `[]`, strings.Replace(valid, `"expected_revision":0`, `"expected_revision":-1`, 1), strings.Replace(valid, `"enabled":false`, `"enabled":false,"prompt":"x"`, 1), strings.Replace(valid, `"gpt-6.1-sol"`, `"unknown"`, 1), strings.Replace(valid, `"gpt-6.1-sol"`, `""`, 1), strings.Replace(valid, `:60`, `:59`, 1), strings.Replace(valid, `:60`, `:86401`, 1), strings.Replace(valid, `:60`, `:60.5`, 1), strings.Replace(valid, `"model_probe"`, `"manual":true,"model_probe"`, 1)} {
		m2Request(t, f, "PUT", path, body, 400)
	}
	m2Request(t, f, "GET", kiloAccountsURL+"/missing/probes", "", 404)
	m2Request(t, f, "PUT", kiloAccountsURL+"/missing/probes", valid, 404)
	_, err := f.manager.Register(t.Context(), &coreauth.Auth{ID: "other-probe", Provider: "codex", Status: coreauth.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	m2Request(t, f, "GET", kiloAccountsURL+"/other-probe/probes", "", 404)
	m2Request(t, f, "PUT", kiloAccountsURL+"/other-probe/probes", valid, 404)
	before, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	f.manager.SetStore(m2FailStore{Store: m2FileStore(f), save: true})
	m2Request(t, f, "PUT", path, valid, 503)
	after, _ := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
	view, _ := f.manager.KiloProbes(id)
	if !bytes.Equal(before, after) || view.Revision != 0 || view.ModelProbe.Enabled {
		t.Fatal("failed save published")
	}
	f.manager.SetStore(m2FileStore(f))
	raw := m2Request(t, f, "PUT", path, valid, 200)
	first := m8bDue(t, raw, "model_probe")
	m2Request(t, f, "PUT", path, valid, 409)
	raw = m2Request(t, f, "PUT", path, strings.Replace(valid, `"expected_revision":0`, `"expected_revision":1`, 1), 200)
	if !m8bDue(t, raw, "model_probe").Equal(first) || ojson.GetBytes(raw, "revision").Int() != 1 {
		t.Fatal("same config reset revision or due")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"name":"unrelated"}`, 200)
	view, _ = f.manager.KiloProbes(id)
	if !view.ModelProbe.NextRunAt.Equal(first) {
		t.Fatal("unrelated field reset due")
	}
	<-f.manager.ProcessKiloProbes(t.Context(), first.Add(-time.Nanosecond))
	if f.posts.Load() != 0 {
		t.Fatal("configuration generated before cycle")
	}
	<-f.manager.ProcessKiloProbes(t.Context(), first)
	view, _ = f.manager.KiloProbes(id)
	if view.ModelProbe.Status != "completed" || view.ModelProbe.ModelCalls != 1 || view.Health.ModelCalls != 0 {
		t.Fatalf("model period=%+v", view)
	}
	f.assertReleased(t, 1)
	t.Log("strict DTO/auth/CAS/storage rollback/no-op/unrelated save; model60 full-cycle POST1")
}

func TestKiloM8BCancelAndLatePublish(t *testing.T) {
	for _, action := range []string{"off", "period", "disabled", "allow-direct", "identity-reload", "organization-reload", "delete-reload", "egress-config", "egress-managed", "host-cancel", "stop"} {
		t.Run(action, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
				w.(http.Flusher).Flush()
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				m8bSuccess(w, 1)
			}, true)
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			id := m2Create(t, f, "")
			raw := m8bConfig(t, f, id, 0, false, true, 60)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			due := m8bDue(t, raw, "model_probe")
			done := f.manager.ProcessKiloProbes(ctx, due)
			m8bAwait(t, started)
			<-f.manager.ProcessKiloProbes(ctx, due.Add(10*time.Minute))
			view, _ := f.manager.KiloProbes(id)
			if view.ModelProbe.Status != "running" || view.ModelProbe.ModelCalls != 1 || view.ModelProbe.SkippedCount != 1 || f.posts.Load() != 1 {
				t.Fatalf("reentry=%+v", view)
			}
			switch action {
			case "off":
				m8bConfig(t, f, id, 1, false, false, 60)
			case "period":
				m8bConfig(t, f, id, 1, false, true, 120)
			case "disabled":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
			case "allow-direct":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"allow_direct_when_proxy_full":true}`, 200)
			case "identity-reload", "organization-reload":
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 409)
				a, _ := f.manager.GetByID(id)
				if action == "identity-reload" {
					a.Metadata["api_key"] = "synthetic-replaced-token"
				} else {
					a.Metadata["organization_id"] = "00000000-0000-4000-8000-000000000001"
				}
				if _, err := f.manager.Update(t.Context(), a); err != nil {
					t.Fatal(err)
				}
			case "delete-reload":
				m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 409)
				f.manager.Remove(t.Context(), id)
			case "egress-managed":
				proxyView := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
				m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", fmt.Sprintf(`{"expected_revision":%d,"enabled":false}`, ojson.GetBytes(proxyView, "revision").Uint()), 200)
			case "egress-config":
				next := f.cfg.CloneForRuntime()
				next.Kilo.ProxyLimit++
				f.manager.SetConfig(next)
			case "host-cancel":
				cancel()
			case "stop":
				f.manager.StopKiloProbes()
			}
			m8bAwait(t, done)
			once.Do(func() { close(release) })
			if action == "delete-reload" {
				m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/probes", "", 404)
			} else {
				view, _ = f.manager.KiloProbes(id)
				if view.ModelProbe.Status != "canceled" || view.ModelProbe.ModelCalls != 1 {
					t.Fatalf("late publish=%+v", view)
				}
				if action == "off" && view.ModelProbe.NextRunAt != nil {
					t.Fatal("off retained schedule")
				}
			}
			if f.posts.Load() != 1 {
				t.Fatal("cancel replayed")
			}
			f.assertReleased(t, 1)
			t.Logf("%s: running singleflight; canceled own transport; POST1/MarkResult1; released", action)
		})
	}
}

func TestKiloM8BSkipsAndPreciseFailures(t *testing.T) {
	for _, kind := range []string{"disabled", "quota", "restricted", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if kind == "restricted" {
					m6Failure(`{"error":{"code":"account_restricted"}}`, 403)(w, r)
				} else {
					m6Failure(m6QuotaFailure, 200)(w, r)
				}
			}, true)
			id := m2Create(t, f, "")
			raw := m8bConfig(t, f, id, 0, false, true, 60)
			due := m8bDue(t, raw, "model_probe")
			want := 0
			if kind == "disabled" {
				m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
			}
			if kind == "unavailable" {
				a, _ := f.manager.GetByID(id)
				a.Unavailable = true
				a.NextRetryAfter = time.Now().Add(time.Hour)
				if _, err := f.manager.Update(t.Context(), a); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "quota" || kind == "restricted" {
				<-f.manager.ProcessKiloProbes(t.Context(), due)
				view, _ := f.manager.KiloProbes(id)
				if view.ModelProbe.Status != "failed" || view.ModelProbe.ModelCalls != 1 {
					t.Fatalf("real failure=%+v", view)
				}
				want = 1
			}
			<-f.manager.ProcessKiloProbes(t.Context(), due.Add(time.Minute))
			view, _ := f.manager.KiloProbes(id)
			code := "account_restricted"
			if kind == "disabled" {
				code = "account_disabled"
			}
			if kind == "unavailable" {
				code = "account_unavailable"
			}
			if view.ModelProbe.Status != "skipped" || view.ModelProbe.ErrorCode == nil || *view.ModelProbe.ErrorCode != code || f.posts.Load() != int32(want) {
				t.Fatalf("skip=%+v", view)
			}
			f.assertReleased(t, want)
			t.Logf("%s skip=%s modelPOST=%d", kind, code, want)
		})
	}
}

func TestKiloM8BRestartAndCounters(t *testing.T) {
	var serial atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) { m8bSuccess(w, serial.Add(1)) }, true)
	id := m2Create(t, f, "")
	raw := m8bConfig(t, f, id, 0, false, true, 60)
	<-f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "model_probe"))
	m8Manual(t, f, id, "gpt-6.1-sol")
	m2Request(t, f, "GET", kiloAccountsURL, "", 200)
	m2Request(t, f, "GET", kiloAccountsURL+"/"+id+"/quota", "", 200)
	f.usageHandler = func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, m5UsageBody) }
	m5Bind(t, f, id, 0, `{"kind":"imported","access_token":"synthetic-imported","chatgpt_account_id":"imported-account"}`)
	m5Assert(t, m5Query(t, f, id, 1), "success", "")
	proxyView := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	m2Request(t, f, "POST", kiloProxiesURL+"/local-proxy/recheck", fmt.Sprintf(`{"expected_revision":%d}`, ojson.GetBytes(proxyView, "revision").Uint()), 200)
	m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	view, _ := f.manager.KiloProbes(id)
	if view.ManualModelCalls != 1 || view.ModelProbe.ModelCalls != 1 || view.Health.ModelCalls != 0 || f.posts.Load() != 2 {
		t.Fatalf("separate counts=%+v", view)
	}
	f.manager.StopKiloProbes()
	<-f.manager.ProcessKiloProbes(t.Context(), time.Now().Add(24*time.Hour))
	if f.posts.Load() != 2 {
		t.Fatal("stopped generated")
	}
	m2Reload(t, f)
	view, _ = f.manager.KiloProbes(id)
	if view.Revision != 1 || !view.ModelProbe.Enabled || view.ModelProbe.Status != "scheduled" || view.ManualModelCalls != 0 || view.ModelProbe.ModelCalls != 0 || view.ModelProbe.LastStartedAt != nil {
		t.Fatalf("restart=%+v", view)
	}
	<-f.manager.ProcessKiloProbes(t.Context(), view.ModelProbe.NextRunAt.Add(-time.Nanosecond))
	if f.posts.Load() != 2 {
		t.Fatal("restart replayed debt")
	}
	<-f.manager.ProcessKiloProbes(t.Context(), *view.ModelProbe.NextRunAt)
	view, _ = f.manager.KiloProbes(id)
	if view.ModelProbe.ModelCalls != 1 || view.ModelProbe.Status != "completed" || f.posts.Load() != 3 {
		t.Fatalf("restart next full cycle=%+v", view)
	}
	f.assertReleased(t, 3)
	t.Log("manual1/model1/health0; account/quota reads and query model0; stop no task; disk reload config only; next full cycle1")
}

func TestKiloM8BPayloadRejectionNotCounted(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	next := f.cfg.CloneForRuntime()
	next.Payload.Filter = []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: []string{"input"}}}
	f.manager.SetConfig(next)
	f.executor.UpdateConfig(next)
	raw := m8bConfig(t, f, id, 0, false, true, 60)
	<-f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "model_probe"))
	m8Manual(t, f, id, "gpt-6.1-sol")
	view, _ := f.manager.KiloProbes(id)
	if view.ModelProbe.ModelCalls != 0 || view.ManualModelCalls != 0 || f.posts.Load() != 0 || view.ModelProbe.Status != "failed" {
		t.Fatalf("payload rejection counted=%+v", view)
	}
	f.assertReleased(t, 2)
}

func TestKiloM8BManualFailureFencesOlderSuccess(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var once sync.Once
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			m8bSuccess(w, 1)
		} else {
			m6Failure(m6QuotaFailure, 200)(w, r)
		}
	}, true)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	id := m2Create(t, f, "")
	raw := m8bConfig(t, f, id, 0, false, true, 60)
	done := f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "model_probe"))
	m8bAwait(t, started)
	body := m8Manual(t, f, id, "gpt-6.1-sol")
	if ojson.GetBytes(body, "status").String() != "failed" {
		t.Fatal("manual failure absent")
	}
	before := f.manager.KiloRestriction(id)
	once.Do(func() { close(release) })
	m8bAwait(t, done)
	view, _ := f.manager.KiloProbes(id)
	if view.ModelProbe.Status != "canceled" || !reflect.DeepEqual(before, f.manager.KiloRestriction(id)) || before.Reason != "quota_exhausted" {
		t.Fatalf("late success=%+v", view)
	}
	f.assertReleased(t, 2)
}

func TestKiloM8BConfigurationCancelsOnlyOwnRequest(t *testing.T) {
	autoStarted, userStarted, userRelease := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var once sync.Once
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		w.(http.Flusher).Flush()
		if n == 1 {
			close(autoStarted)
			<-r.Context().Done()
			return
		}
		close(userStarted)
		<-userRelease
		m8bSuccess(w, n)
	}, true)
	t.Cleanup(func() { once.Do(func() { close(userRelease) }) })
	id := m2Create(t, f, "")
	raw := m8bConfig(t, f, id, 0, false, true, 60)
	done := f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "model_probe"))
	m8bAwait(t, autoStarted)
	userDone := m8Async(t.Context(), f, "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"keep user"}`)
	m8bAwait(t, userStarted)
	m8bConfig(t, f, id, 1, false, true, 60)
	f.manager.SetStore(m2FailStore{Store: m2FileStore(f), save: true})
	m2Request(t, f, "PUT", kiloAccountsURL+"/"+id+"/probes", `{"expected_revision":1,"health":{"enabled":false},"model_probe":{"enabled":false,"model":"gpt-6.1-sol","interval_seconds":60}}`, 503)
	view, _ := f.manager.KiloProbes(id)
	if view.ModelProbe.Status != "running" || !view.ModelProbe.Enabled {
		t.Fatal("no-op or failed persistence canceled running task")
	}
	f.manager.SetStore(m2FileStore(f))
	m8bConfig(t, f, id, 1, false, false, 60)
	m8bAwait(t, done)
	if active, _ := m8Counts(f, id); active != 1 {
		t.Fatalf("other user's lease canceled: %d", active)
	}
	select {
	case err := <-userDone:
		t.Fatalf("other user ended early: %v", err)
	default:
	}
	once.Do(func() { close(userRelease) })
	if err := <-userDone; err != nil {
		t.Fatal(err)
	}
	view, _ = f.manager.KiloProbes(id)
	if view.ModelProbe.Status != "canceled" || view.ModelProbe.ModelCalls != 1 || view.ManualModelCalls != 0 {
		t.Fatalf("mixed calls=%+v", view)
	}
	f.assertReleased(t, 2)
	t.Log("in-flight no-op and failed save preserve task; successful off cancels auto only; ordinary request completed; leases0")
}

func TestKiloM8BQueuedCancelAndHealthIncomplete(t *testing.T) {
	t.Run("queued", func(t *testing.T) {
		f := newKiloNativeFixture(t, kiloSuccessStream, true)
		id := m2Create(t, f, `,"concurrency_limit":1`)
		hold, err := f.manager.AcquireCredentialGeneration(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		defer hold()
		raw := m8bConfig(t, f, id, 0, false, true, 60)
		done := f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "model_probe"))
		m8Wait(t, func() bool { active, waiting := m8Counts(f, id); return active == 1 && waiting == 1 })
		m8bConfig(t, f, id, 1, false, false, 60)
		m8bAwait(t, done)
		view, _ := f.manager.KiloProbes(id)
		if view.ModelProbe.Status != "canceled" || view.ModelProbe.ModelCalls != 0 || f.posts.Load() != 0 {
			t.Fatalf("queued cancel=%+v", view)
		}
		hold()
		f.assertReleased(t, 0)
	})
	t.Run("health-incomplete", func(t *testing.T) {
		f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n\n")
		}, true)
		f.allowAutoFree = true
		id := m2Create(t, f, "")
		raw := m8bConfig(t, f, id, 0, true, false, 300)
		<-f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "health"))
		view, _ := f.manager.KiloProbes(id)
		if view.Health.Status != "incomplete" || view.Health.ModelCalls != 1 || view.Health.ErrorCode == nil || *view.Health.ErrorCode != "incomplete" {
			t.Fatalf("health incomplete=%+v", view)
		}
		f.assertReleased(t, 1)
	})
}

func TestKiloM8BModelChangeClearsPriorObservation(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprintf("running=%t", running), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if running {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"old model\"}\n\n")
					w.(http.Flusher).Flush()
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}
				m8bSuccess(w, 1)
			}, true)
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			id := m2Create(t, f, "")
			raw := m8bConfig(t, f, id, 0, false, true, 60)
			done := f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "model_probe"))
			if running {
				m8bAwait(t, started)
			} else {
				m8bAwait(t, done)
			}
			path := kiloAccountsURL + "/" + id + "/probes"
			m2Request(t, f, "PUT", path, `{"expected_revision":1,"health":{"enabled":false},"model_probe":{"enabled":true,"model":"gpt-6-astra","interval_seconds":60}}`, 200)
			m8bAwait(t, done)
			once.Do(func() { close(release) })
			view, _ := f.manager.KiloProbes(id)
			if view.ModelProbe.Status != "scheduled" || view.ModelProbe.Model != "gpt-6-astra" || view.ModelProbe.LastStartedAt != nil || view.ModelProbe.LastCompletedAt != nil || view.ModelProbe.ErrorCode != nil || view.ModelProbe.ModelCalls != 1 {
				t.Fatalf("new model inherited prior observation=%+v", view)
			}
			m2Request(t, f, "PUT", path, `{"expected_revision":2,"health":{"enabled":false},"model_probe":{"enabled":false,"model":"","interval_seconds":60}}`, 200)
			view, _ = f.manager.KiloProbes(id)
			if view.ModelProbe.Status != "never" || view.ModelProbe.NextRunAt != nil || view.ModelProbe.ModelCalls != 1 {
				t.Fatalf("disabled new model observation=%+v", view)
			}
			f.assertReleased(t, 1)
			t.Logf("old running=%t; new model scheduled/unknown then disabled/never; cumulative calls1; late old result fenced", running)
		})
	}
}

func TestKiloM8BNewDisabledConfigAndAggregateStatus(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, `,"enabled":false`)
	status := m2Request(t, f, "GET", "/v8/management/kilo/status", "", 200)
	if ojson.GetBytes(status, "auto_account_probe_enabled").Bool() || ojson.GetBytes(status, "auto_model_probe_enabled").Bool() {
		t.Fatal("default aggregate enabled")
	}
	raw := m8bConfig(t, f, id, 0, true, true, 300)
	status = m2Request(t, f, "GET", "/v8/management/kilo/status", "", 200)
	if !ojson.GetBytes(status, "auto_account_probe_enabled").Bool() || !ojson.GetBytes(status, "auto_model_probe_enabled").Bool() {
		t.Fatal("aggregate ignored saved configuration")
	}
	<-f.manager.ProcessKiloProbes(t.Context(), m8bDue(t, raw, "health"))
	view, _ := f.manager.KiloProbes(id)
	if view.Health.Status != "skipped" || view.ModelProbe.Status != "skipped" || f.posts.Load() != 0 {
		t.Fatalf("new disabled config generated=%+v", view)
	}
	m8bConfig(t, f, id, 1, false, false, 300)
	status = m2Request(t, f, "GET", "/v8/management/kilo/status", "", 200)
	if ojson.GetBytes(status, "auto_account_probe_enabled").Bool() || ojson.GetBytes(status, "auto_model_probe_enabled").Bool() {
		t.Fatal("disabled aggregate remained enabled")
	}
	f.assertReleased(t, 0)
}
