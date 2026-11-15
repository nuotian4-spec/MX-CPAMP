package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloM8ManualProtocol(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "kilo-auto/free"} {
		t.Run(model, func(t *testing.T) {
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				path := "/api/v1/responses"
				if model == "kilo-auto/free" {
					path = "/api/v1/chat/completions"
				}
				if r.URL.Path != path || r.Header.Get("X-Session-Id") == "" || r.Header.Get("X-KILOCODE-TASKID") != r.Header.Get("X-Session-Id") {
					t.Errorf("route/session=%s %s", r.URL.Path, r.Header.Get("X-Session-Id"))
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if model == "kilo-auto/free" {
					_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"auto 测试\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", kiloTerminal)
			}, true)
			f.allowAutoFree = true
			id := m2Create(t, f, "")
			body := m2Request(t, f, "POST", kiloAccountsURL+"/"+id+"/test", fmt.Sprintf(`{"model":%q,"prompt":"  synthetic 提示词  "}`, model), 200)
			if ojson.GetBytes(body, "status").String() != "completed" || ojson.GetBytes(body, "text").String() == "" || ojson.GetBytes(body, "model").String() != model {
				t.Fatalf("manual result=%s", body)
			}
			wire := <-f.requests
			if model == "kilo-auto/free" {
				if ojson.GetBytes(wire, "model").String() != model || ojson.GetBytes(wire, "messages.#").Int() != 1 || ojson.GetBytes(wire, "messages.0.role").String() != "user" || ojson.GetBytes(wire, "messages.0.content").String() != "  synthetic 提示词  " || ojson.GetBytes(wire, "input").Exists() {
					t.Fatalf("Auto Free payload=%s", wire)
				}
			} else if ojson.GetBytes(wire, "model").String() != "openai/"+model || ojson.GetBytes(wire, "input").String() != "  synthetic 提示词  " {
				t.Fatalf("Normal payload=%s", wire)
			}
			if !ojson.GetBytes(wire, "stream").Bool() || !ojson.GetBytes(body, "error_code").Exists() || ojson.GetBytes(body, "error_code").Type != ojson.Null || ojson.GetBytes(body, "started_at").String() == "" || ojson.GetBytes(body, "completed_at").String() == "" {
				t.Fatalf("missing final fields=%s", body)
			}
			t.Logf("model=%s wire=%s result=%s", model, wire, body)
			f.assertReleased(t, 1)
			if f.posts.Load() != 1 || f.hook.snapshot()[0].AuthID != id {
				t.Fatal("manual probe replayed or changed account")
			}
		})
	}
}

func m8Manual(t *testing.T, f *kiloNativeFixture, id, model string) []byte {
	t.Helper()
	return m2Request(t, f, "POST", kiloAccountsURL+"/"+id+"/test", fmt.Sprintf(`{"model":%q,"prompt":"synthetic"}`, model), 200)
}

func TestKiloM8NewDisabledAccountManualDiagnostic(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, `,"enabled":false`)
	if _, ok := f.manager.Executor("kilo"); ok {
		t.Fatal("creation unexpectedly initialized executor")
	}
	m2Request(t, f, "GET", kiloAccountsURL, "", 200)
	catalog := m2Request(t, f, "GET", "/v8/management/routing/model-definitions/kilo", "", 200)
	if len(ojson.GetBytes(catalog, "models").Array()) != 8 {
		t.Fatal("static Kilo catalog missing")
	}
	if f.posts.Load() != 0 {
		t.Fatal("creation/list/catalog generated")
	}
	status, _ := f.request(t, "POST", kiloAccountsURL+"/"+id+"/test", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","prompt":"explicit"}`)
	if status != 401 {
		t.Fatal("inference key granted diagnostics")
	}
	m2Request(t, f, "POST", kiloAccountsURL+"/missing/test", `{"model":"gpt-6.1-sol","prompt":"explicit"}`, 404)
	if _, ok := f.manager.Executor("kilo"); ok {
		t.Fatal("rejected target initialized executor")
	}
	body := m8Manual(t, f, id, "gpt-6.1-sol")
	if ojson.GetBytes(body, "status").String() != "completed" || f.posts.Load() != 1 {
		t.Fatalf("new disabled diagnostic=%s", body)
	}
	a, _ := f.manager.GetByID(id)
	if !a.Disabled || a.InferenceVerified {
		t.Fatal("diagnostic enabled or verified disabled account")
	}
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"ordinary"}`)
	if status < 400 || f.posts.Load() != 1 {
		t.Fatal("ordinary inference inherited manual exemption")
	}
	f.assertReleased(t, 1)
	t.Log("new enabled:false creation/list/catalog POST0; rejected key/target no executor; explicit manual completes POST1; account still disabled/unverified; public inference rejected")
}

func TestKiloM8ManualValidation(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	id := m2Create(t, f, "")
	path := kiloAccountsURL + "/" + id + "/test"
	for _, key := range []string{"synthetic-kilo-key", "synthetic-codex-key", "synthetic-legacy-key", ""} {
		status, body := f.request(t, "POST", path, key, `{"model":"gpt-6.1-sol","prompt":"test"}`)
		if status != 401 && status != 403 {
			t.Fatalf("inference key granted management %d %s", status, body)
		}
	}
	for _, body := range []string{`{}`, `{"model":"gpt-6.1-sol","prompt":" "}`, `{"model":"unknown","prompt":"test"}`, `{"model":"gpt-6.1-sol","prompt":"test","manual":true}`, `{"model":1,"prompt":"test"}`, `{"model":"gpt-6.1-sol","prompt":["test"]}`} {
		m2Request(t, f, "POST", path, body, 400)
	}
	m2Request(t, f, "POST", kiloAccountsURL+"/unknown/test", `{"model":"gpt-6.1-sol","prompt":"test"}`, 404)
	for _, provider := range []string{"codex", "bps"} {
		_, err := f.manager.Register(context.Background(), &coreauth.Auth{ID: "synthetic-" + provider + "-probe", Provider: provider, Status: coreauth.StatusActive})
		if err != nil {
			t.Fatal(err)
		}
		m2Request(t, f, "POST", kiloAccountsURL+"/synthetic-"+provider+"-probe/test", `{"model":"gpt-6.1-sol","prompt":"test"}`, 404)
	}
	status, _ := f.request(t, "POST", "/v1/chat/completions", "synthetic-kilo-key", `{"model":"kilo-auto/free","messages":[{"role":"user","content":"test"}]}`)
	if status < 400 {
		t.Fatal("management Auto Free became a public model")
	}
	m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test","manual":true,"kilo_diagnostic":true}`)
	if status < 400 {
		t.Fatal("public payload obtained diagnostic exemption")
	}
	_, err := f.manager.ExecuteKiloDiagnostic(context.Background(), id, "gpt-6.1-sol", "automatic", false)
	if err == nil {
		t.Fatal("automatic mode admitted disabled account")
	}
	if f.posts.Load() != 0 {
		t.Fatalf("validation reached upstream POST=%d", f.posts.Load())
	}
	f.assertReleased(t, 0)
}

func TestKiloM8ManualTerminals(t *testing.T) {
	for _, sample := range []struct {
		name, model, frames, status, text, reason string
		http                                      int
	}{
		{"normal-incomplete", "gpt-6.1-sol", `{"type":"response.incomplete","response":{"status":"incomplete","output":[{"type":"message","content":[{"type":"output_text","text":"normal partial"}]}],"error":{"message":"synthetic-private-error"},"incomplete_details":{"reason":"synthetic-private-details"}}}`, "incomplete", "normal partial", "none", 200},
		{"normal-failed", "gpt-6.1-sol", `{"type":"response.failed","response":{"status":"failed","output":[{"content":[{"type":"output_text","text":"hidden failed output"}]}],"error":{"message":"synthetic-private-error"}}}`, "failed", "", "none", 200},
		{"normal-missing", "gpt-6.1-sol", `{"type":"response.output_text.delta","delta":"not terminal"}`, "failed", "", "none", 200},
		{"auto-length", "kilo-auto/free", `{"choices":[{"delta":{"content":"auto partial"},"finish_reason":"length"},{"delta":{"content":"ignored second choice"},"finish_reason":"stop"}]}`, "incomplete", "auto partial", "none", 200},
		{"auto-missing", "kilo-auto/free", `{"choices":[{"delta":{"content":"not terminal"}}]}`, "failed", "", "none", 200},
		{"auto-error", "kilo-auto/free", `{"error":{"message":"synthetic-private-error"}}`, "failed", "", "none", 200},
		{"auto-quota", "kilo-auto/free", m6QuotaFailure, "failed", "", "quota_exhausted", 200},
		{"normal-quota", "gpt-6.1-sol", m6QuotaFailure, "failed", "", "quota_exhausted", 200},
		{"auto-restricted", "kilo-auto/free", `{"error":{"code":"account_restricted","message":"synthetic-private-error"}}`, "failed", "", "account_restricted", 403},
		{"normal-restricted", "gpt-6.1-sol", `{"error":{"type":"account_restricted","message":"synthetic-private-error"}}`, "failed", "", "account_restricted", 403},
		{"auto-ordinary403", "kilo-auto/free", `{"error":{"message":"synthetic-private-error"}}`, "failed", "", "none", 403},
		{"auto-ordinary429", "kilo-auto/free", `{"error":{"code":"rate_limit_exceeded","message":"synthetic-private-error"}}`, "failed", "", "none", 429},
		{"auto-unauthorized", "kilo-auto/free", `{"error":{"code":"invalid_api_key","message":"synthetic-private-error"}}`, "failed", "", "none", 401},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if sample.http != 200 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(sample.http)
					_, _ = io.WriteString(w, sample.frames)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", sample.frames)
			}, true)
			f.allowAutoFree = true
			id := m2Create(t, f, "")
			body := m8Manual(t, f, id, sample.model)
			if ojson.GetBytes(body, "status").String() != sample.status || ojson.GetBytes(body, "text").String() != sample.text || bytes.Contains(body, []byte("synthetic-private")) || bytes.Contains(body, []byte("ignored second")) {
				t.Fatalf("terminal=%s", body)
			}
			if f.manager.KiloRestriction(id).Reason != sample.reason {
				t.Fatalf("restriction=%+v", f.manager.KiloRestriction(id))
			}
			f.assertReleased(t, 1)
			if f.posts.Load() != 1 || f.hook.snapshot()[0].Success {
				t.Fatalf("failed probe POST=%d result=%+v", f.posts.Load(), f.hook.snapshot())
			}
			t.Logf("model=%s public=%s POST1 result1 reason=%s released", sample.model, body, sample.reason)
		})
	}
}

func TestKiloM8ManualPreservesDisabledAndRestrictions(t *testing.T) {
	for _, state := range []string{"disabled", "quota", "restricted", "auth-and-model"} {
		t.Run(state, func(t *testing.T) {
			var round atomic.Int32
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				n := round.Add(1)
				if n == 1 && (state == "quota" || state == "restricted") {
					if state == "quota" {
						m6Failure(m6QuotaFailure, 200)(w, r)
					} else {
						m6Failure(`{"error":{"code":"account_restricted","message":"private"}}`, 403)(w, r)
					}
					return
				}
				kiloSuccessStream(w, r)
			}, true)
			id := m2Create(t, f, "")
			m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
			want := 1
			if state == "quota" || state == "restricted" {
				body := m8Manual(t, f, id, "gpt-6.1-sol")
				if ojson.GetBytes(body, "status").String() != "failed" {
					t.Fatal("initial failure ignored")
				}
				f.assertReleased(t, 1)
				want++
			}
			if state == "auth-and-model" {
				seed, _ := f.manager.GetByID(id)
				seed.LastError = &coreauth.Error{HTTPStatus: 401, Message: "synthetic old auth failure"}
				seed.Status = coreauth.StatusError
				seed.Unavailable = true
				seed.NextRetryAfter = time.Now().Add(time.Hour)
				seed.ModelStates = map[string]*coreauth.ModelState{"gpt-6-astra": {Status: coreauth.StatusError, Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour)}}
				if _, err := f.manager.Update(context.Background(), seed); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := f.manager.GetByID(id)
			restriction := f.manager.KiloRestriction(id)
			body := m8Manual(t, f, id, "gpt-6.1-sol")
			if ojson.GetBytes(body, "status").String() != "completed" {
				t.Fatalf("manual unavailable=%s", body)
			}
			after, _ := f.manager.GetByID(id)
			if !after.Disabled || before.Status != after.Status || before.InferenceVerified != after.InferenceVerified || before.Unavailable != after.Unavailable || !reflect.DeepEqual(before.ModelStates, after.ModelStates) || !reflect.DeepEqual(before.LastError, after.LastError) || !before.NextRetryAfter.Equal(after.NextRetryAfter) || !reflect.DeepEqual(before.Quota, after.Quota) || !reflect.DeepEqual(restriction, f.manager.KiloRestriction(id)) {
				t.Fatalf("diagnostic success altered availability before=%+v after=%+v restriction=%+v", before, after, f.manager.KiloRestriction(id))
			}
			_, err := f.manager.ExecuteKiloDiagnostic(context.Background(), id, "gpt-6.1-sol", "automatic", false)
			if err == nil {
				t.Fatal("automatic mode inherited manual exemption")
			}
			status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"public"}`)
			if status < 400 || f.posts.Load() != int32(want) {
				t.Fatalf("public exemption POST=%d status=%d", f.posts.Load(), status)
			}
			f.assertReleased(t, want)
			t.Logf("state=%s manual completed; disabled/restriction unchanged; POST=%d released", state, want)
		})
	}
}

func m8Wait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !condition() {
		select {
		case <-deadline:
			t.Fatal("condition not observed")
		default:
			runtime.Gosched()
		}
	}
}

func m8Async(ctx context.Context, f *kiloNativeFixture, path, key, body string) <-chan error {
	done := make(chan error, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, "POST", f.server.URL+path, strings.NewReader(body))
		if err != nil {
			done <- err
			return
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		response, err := f.server.Client().Do(req)
		if err == nil {
			_, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err == nil && response.StatusCode >= 400 {
				err = fmt.Errorf("request HTTP=%d", response.StatusCode)
			}
		}
		done <- err
	}()
	return done
}

func m8Counts(f *kiloNativeFixture, id string) (active, waiting int) {
	for _, entry := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
		if entry.AuthID == id {
			return entry.Active, entry.PinnedWaiting
		}
	}
	return 0, 0
}

func TestKiloM8ManualSharesTwentySlotsAndQueueCancellation(t *testing.T) {
	started := make(chan struct{}, 20)
	finish := make(chan struct{})
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-finish
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", "resp-"+r.Header.Get("X-Session-Id"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}, true)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(finish) }) })
	f.requests = make(chan []byte, 32)
	id := m2Create(t, f, "")
	m4oDirect(t, f, id)
	done := []<-chan error{}
	for i := 0; i < 20; i++ {
		done = append(done, m8Async(context.Background(), f, "/v1/responses", "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"hold-%d"}`, i)))
	}
	for i := 0; i < 20; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			release.Do(func() { close(finish) })
			t.Fatal("normal capacity not reached")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	probe := m8Async(ctx, f, kiloAccountsURL+"/"+id+"/test", kiloManagementKey, `{"model":"gpt-6.1-sol","prompt":"queued"}`)
	m8Wait(t, func() bool { a, w := m8Counts(f, id); return a == 20 && w == 1 })
	if f.posts.Load() != 20 {
		t.Fatal("manual bypassed shared capacity")
	}
	cancel()
	<-probe
	m8Wait(t, func() bool { a, w := m8Counts(f, id); return a == 20 && w == 0 })
	release.Do(func() { close(finish) })
	for _, c := range done {
		if err := <-c; err != nil {
			t.Fatal(err)
		}
	}
	f.assertReleased(t, 20)
	t.Log("20 ordinary inference slots saturated; manual waited then canceled; POST20 results20; no 21st call")
}

func TestKiloM8ManualLateIdentityAndState(t *testing.T) {
	for _, change := range []string{"disabled", "identity", "delete"} {
		for _, success := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", change, success), func(t *testing.T) {
				started, finish := make(chan struct{}), make(chan struct{})
				var release sync.Once
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					close(started)
					<-finish
					if success {
						kiloSuccessStream(w, r)
					} else {
						m6Failure(m6QuotaFailure, 200)(w, r)
					}
				}, true)
				t.Cleanup(func() { release.Do(func() { close(finish) }) })
				id := m2Create(t, f, "")
				done := m8Async(context.Background(), f, kiloAccountsURL+"/"+id+"/test", kiloManagementKey, `{"model":"gpt-6.1-sol","prompt":"slow"}`)
				<-started
				switch change {
				case "disabled":
					m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"enabled":false}`, 200)
				case "identity":
					m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 409)
					updated, _ := f.manager.GetByID(id)
					updated.Metadata["api_key"] = "synthetic-replaced-token"
					if _, err := f.manager.Update(context.Background(), updated); err != nil {
						t.Fatal(err)
					}
				case "delete":
					m2Request(t, f, "DELETE", kiloAccountsURL+"/"+id, "", 409)
					f.manager.Remove(context.Background(), id)
				}
				before, _ := f.manager.GetByID(id)
				release.Do(func() { close(finish) })
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				after, _ := f.manager.GetByID(id)
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("late probe altered current state before=%+v after=%+v", before, after)
				}
				f.assertReleased(t, 1)
				if f.posts.Load() != 1 {
					t.Fatal("late probe replayed")
				}
				t.Logf("change=%s success=%t current unchanged POST1 result1 released", change, success)
			})
		}
	}
}

func TestKiloM8ManualInflightCancellation(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "kilo-auto/free"} {
		t.Run(model, func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, ": slow-upstream\n\n")
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
				close(stopped)
			}, true)
			f.allowAutoFree = true
			id := m2Create(t, f, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := m8Async(ctx, f, kiloAccountsURL+"/"+id+"/test", kiloManagementKey, fmt.Sprintf(`{"model":%q,"prompt":"slow"}`, model))
			<-started
			if active, _ := m8Counts(f, id); active != 1 {
				t.Fatal("inflight probe has no shared lease")
			}
			cancel()
			<-done
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("upstream cancellation missing")
			}
			f.assertReleased(t, 1)
			if f.posts.Load() != 1 || f.hook.snapshot()[0].Success {
				t.Fatal("canceled probe replayed or succeeded")
			}
			t.Log("connected slow upstream canceled; POST1 result1 account/egress released")
		})
	}
}

func TestKiloM8ManualEgressWaitAndFullDirect(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprint(direct), func(t *testing.T) {
			started := make(chan struct{}, 2)
			finish := make(chan struct{})
			var release sync.Once
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				doc := ojson.NewDocString(kiloTerminal)
				_ = doc.Set("response.id", "resp-"+r.Header.Get("X-Session-Id"))
				// The fixture has already captured the body for assertions.
				if r.Header.Get("X-Codex-Turn-State") == "hold" {
					started <- struct{}{}
					<-finish
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
			}, true)
			t.Cleanup(func() { release.Do(func() { close(finish) }) })
			id := m2Create(t, f, fmt.Sprintf(`,"allow_direct_when_proxy_full":%t`, direct))
			f.allowDirect = true
			holds := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() {
					request, _ := http.NewRequest("POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"hold"}`))
					request.Header.Set("Authorization", "Bearer synthetic-kilo-key")
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("X-Codex-Turn-State", "hold")
					response, err := f.server.Client().Do(request)
					if err == nil {
						_, err = io.ReadAll(response.Body)
						_ = response.Body.Close()
					}
					holds <- err
				}()
			}
			for i := 0; i < 2; i++ {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("proxy slots not filled")
				}
			}
			if direct {
				body := m8Manual(t, f, id, "gpt-6.1-sol")
				if ojson.GetBytes(body, "status").String() != "completed" || f.posts.Load() != 3 || f.connects.Load() != 2 {
					t.Fatalf("full direct result=%s POST=%d CONNECT=%d", body, f.posts.Load(), f.connects.Load())
				}
			} else {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := m8Async(ctx, f, kiloAccountsURL+"/"+id+"/test", kiloManagementKey, `{"model":"gpt-6.1-sol","prompt":"waiting"}`)
				m8Wait(t, func() bool { a, w := m8Counts(f, id); return a == 2 && w == 1 })
				if f.posts.Load() != 2 {
					t.Fatal("busy proxy bypassed")
				}
				cancel()
				<-done
				m8Wait(t, func() bool { a, w := m8Counts(f, id); return a == 2 && w == 0 })
			}
			release.Do(func() { close(finish) })
			for i := 0; i < 2; i++ {
				if err := <-holds; err != nil {
					t.Fatal(err)
				}
			}
			want := 2
			if direct {
				want = 3
			}
			f.assertReleased(t, want)
			t.Logf("full proxy direct=%t POST%d; waiting diagnostic held no extra account slot; released", direct, want)
		})
	}
}

func TestKiloM8ManualFailedProxyNeverDirects(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var release sync.Once
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	t.Cleanup(func() { release.Do(func() { close(finish) }) })
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-finish
		_, _ = io.WriteString(w, "not-an-ip")
	}
	id := m2Create(t, f, `,"allow_direct_when_proxy_full":true`)
	f.allowDirect = true
	done := m8Async(context.Background(), f, kiloAccountsURL+"/"+id+"/test", kiloManagementKey, `{"model":"gpt-6.1-sol","prompt":"probe"}`)
	<-started
	if active, _ := m8Counts(f, id); active != 0 {
		t.Fatal("health check retained temporary account lease")
	}
	release.Do(func() { close(finish) })
	if err := <-done; err == nil {
		t.Fatal("failed proxy probe accepted")
	}
	if f.posts.Load() != 0 {
		t.Fatal("failed proxy switched direct")
	}
	f.assertReleased(t, 0)
	t.Log("unverified egress held no account slot; failed health stayed model POST0 and no direct fallback")
}

func TestKiloM8DiagnosticPayloadBarrier(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "kilo-auto/free"} {
		for _, mode := range []string{"override-text", "delete-text", "override-model"} {
			t.Run(model+"/"+mode, func(t *testing.T) {
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					if model == "kilo-auto/free" {
						_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
					} else {
						kiloSuccessStream(w, r)
					}
				}, true)
				f.allowAutoFree = true
				id := m2Create(t, f, "")
				next := f.cfg.CloneForRuntime()
				models := []config.PayloadModelRule{{Name: "*"}}
				textPath := "input"
				if model == "kilo-auto/free" {
					textPath = "messages.0.content"
				}
				switch mode {
				case "override-text":
					next.Payload.Override = []config.PayloadRule{{Models: models, Params: map[string]any{textPath: "configured exact"}}}
				case "delete-text":
					next.Payload.Filter = []config.PayloadFilterRule{{Models: models, Params: []string{textPath}}}
				case "override-model":
					next.Payload.Override = []config.PayloadRule{{Models: models, Params: map[string]any{"model": "openai/gpt-6-astra"}}}
				}
				f.executor.UpdateConfig(next)
				body := m8Manual(t, f, id, model)
				if mode == "override-text" {
					wire := <-f.requests
					if ojson.GetBytes(wire, textPath).String() != "configured exact" || ojson.GetBytes(body, "status").String() != "completed" || f.posts.Load() != 1 {
						t.Fatalf("barrier wire=%s public=%s", wire, body)
					}
				} else if ojson.GetBytes(body, "status").String() != "failed" || f.posts.Load() != 0 {
					t.Fatalf("invalid final payload restored or sent: %s POST=%d", body, f.posts.Load())
				}
				f.assertReleased(t, 1)
				t.Logf("model=%s mode=%s POST=%d result1 released public=%s", model, mode, f.posts.Load(), body)
			})
		}
	}
}

func TestKiloM8ManualTransportFailureNoReplay(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "kilo-auto/free"} {
		t.Run(model, func(t *testing.T) {
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
			}, true)
			f.allowAutoFree = true
			id := m2Create(t, f, "")
			body := m8Manual(t, f, id, model)
			if ojson.GetBytes(body, "status").String() != "failed" || ojson.GetBytes(body, "text").String() != "" || f.posts.Load() != 1 {
				t.Fatalf("transport result=%s POST=%d", body, f.posts.Load())
			}
			f.assertReleased(t, 1)
			if f.hook.snapshot()[0].Success || f.executor.KiloProxySnapshot().Proxies[0].Healthy {
				t.Fatal("transport failure was not registered")
			}
			t.Log("after-POST transport failure: no replay/direct, POST1 result1 released")
		})
	}
}
