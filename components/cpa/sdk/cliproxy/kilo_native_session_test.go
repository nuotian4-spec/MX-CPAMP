package cliproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type kiloSessionSeen struct{ Account, Session string }

func kiloSessionUpstream(t *testing.T, seen chan kiloSessionSeen) http.HandlerFunc {
	var seq atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		session := r.Header.Get("X-Session-Id")
		if session == "" || session != r.Header.Get("X-KILOCODE-TASKID") || session != r.Header.Get("x-session-affinity") {
			t.Error("upstream session headers disagree")
		}
		seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), session}
		id := fmt.Sprintf("resp-chain-%d", seq.Add(1))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":%q,\"status\":\"in_progress\"}}\n\n", id)
		w.(http.Flusher).Flush()
		terminal := ojson.NewDocString(kiloTerminal)
		_ = terminal.Set("response.id", id)
		output := terminal.Get("response.output").Array()
		items := make([]any, 0, len(output)+1)
		for _, item := range output {
			items = append(items, item.Value())
		}
		items = append(items, map[string]any{"type": "function_call", "id": "fc-" + id, "call_id": "call-" + id, "name": "chain_tool", "arguments": "{\"exact\":1}", "status": "completed"})
		_ = terminal.Set("response.output", items)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", terminal.String())
		w.(http.Flusher).Flush()
	}
}
func kiloSessionRequest(t *testing.T, f *kiloNativeFixture, path, key, session, body string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest("POST", f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Content-Type", "application/json")
	if session != "" {
		r.Header.Set("Session-Id", session)
	}
	response, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}
func restartKiloSessionFixture(t *testing.T, f *kiloNativeFixture) {
	t.Helper()
	f.server.Close()
	accounts := f.manager.List()
	f.hook = &kiloNativeHook{}
	f.manager = coreauth.NewManager(nil, nil, f.hook)
	f.manager.SetConfig(f.cfg)
	service := &Service{cfg: f.cfg, coreManager: f.manager}
	for _, old := range accounts {
		a, err := f.manager.Register(context.Background(), &coreauth.Auth{ID: old.ID, Provider: old.Provider, Status: old.Status, Disabled: old.Disabled, Metadata: old.Metadata})
		if err != nil {
			t.Fatal(err)
		}
		service.registerExecutorForAuth(a, false)
		if !service.registerModelsForAuth(context.Background(), a) {
			t.Fatal("restart model registration failed")
		}
	}
	registered, _ := f.manager.Executor("kilo")
	f.executor = registered.(*native.KiloExecutor)
	server := api.NewServer(f.cfg, f.manager, access.NewManager(), filepath.Join(t.TempDir(), "config.yaml"), api.WithLocalManagementPassword("synthetic-management-key"))
	f.server = httptest.NewServer(server.Handler())
	t.Cleanup(f.server.Close)
	f.server.Client().Transport = &http.Transport{Proxy: nil}
	t.Cleanup(func() { f.server.Client().CloseIdleConnections() })
	for _, p := range f.executor.KiloProxySnapshot().Proxies {
		if p.Active != 0 || p.Owner != "" {
			t.Fatal("restart restored a proxy lease")
		}
	}
	for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
		if a.Active != 0 {
			t.Fatal("restart restored account occupancy")
		}
	}
}
func TestKiloSessionTwoTurnsAndRestart(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			seen := make(chan kiloSessionSeen, 4)
			f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
			status, data := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "chain-one", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"first","stream":%t}`, stream))
			if status != 200 {
				t.Fatalf("first=%d %s", status, data)
			}
			first := <-seen
			f.assertReleased(t, 1)
			status, data = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "conflicting-extra-header", fmt.Sprintf(`{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":"next","stream":%t}`, stream))
			if status != 200 {
				t.Fatalf("second=%d %s", status, data)
			}
			if next := <-seen; next != first {
				t.Fatalf("binding moved: %v -> %v", first, next)
			}
			f.assertReleased(t, 2)
			restartKiloSessionFixture(t, f)
			status, data = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "", fmt.Sprintf(`{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-2","input":"after restart","stream":%t}`, stream))
			if status != 200 {
				t.Fatalf("restart=%d %s", status, data)
			}
			if next := <-seen; next != first {
				t.Fatalf("restart changed identity/session: %v -> %v", first, next)
			}
			f.assertReleased(t, 1)
			if f.posts.Load() != 3 || f.connects.Load() < 2 {
				t.Fatalf("posts/connects=%d/%d", f.posts.Load(), f.connects.Load())
			}
			t.Log("three POSTs; same AuthID/session before and after fresh Manager/Executor; CONNECT transport; account and proxy occupancy zero")
		})
	}
}
func TestKiloSessionToolsAndUnavailability(t *testing.T) {
	for _, protocol := range []string{"responses", "chat/completions"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", protocol, stream), func(t *testing.T) {
				seen := make(chan kiloSessionSeen, 4)
				f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
				input := `"input":"first"`
				if protocol == "chat/completions" {
					input = `"messages":[{"role":"user","content":"first"}]`
				}
				code, body := kiloSessionRequest(t, f, "/v1/"+protocol, "synthetic-kilo-key", "tools-chain", fmt.Sprintf(`{"model":"gpt-6.1-sol",%s,"stream":%t}`, input, stream))
				if code != 200 {
					t.Fatalf("first=%d %s", code, body)
				}
				first := <-seen
				f.assertReleased(t, 1)
				input = `"input":[{"type":"function_call_output","call_id":"call-resp-chain-1","output":"exact tool output"}]`
				if protocol == "chat/completions" {
					input = `"messages":[{"role":"tool","tool_call_id":"call-resp-chain-1","content":"exact tool output"}]`
				}
				code, body = kiloSessionRequest(t, f, "/v1/"+protocol, "synthetic-kilo-key", "", fmt.Sprintf(`{"model":"gpt-6.1-sol",%s,"stream":%t}`, input, stream))
				if code != 200 {
					t.Fatalf("tool=%d %s", code, body)
				}
				if next := <-seen; next != first {
					t.Fatalf("tool changed identity: %v", next)
				}
				f.assertReleased(t, 2)
				<-f.requests
				forwarded := <-f.requests
				if !strings.Contains(string(forwarded), "call-resp-chain-1") || !strings.Contains(string(forwarded), "exact tool output") {
					t.Fatalf("tool output/call identity changed: %s", forwarded)
				}
				a, _ := f.manager.GetByID(kiloAccountForMachine(t, f, first.Account))
				a.Disabled = true
				a.Status = coreauth.StatusDisabled
				if _, err := f.manager.Update(context.Background(), a); err != nil {
					t.Fatal(err)
				}
				code, body = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "", `{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-2","input":"must not move"}`)
				if code != 409 {
					t.Fatalf("disabled=%d %s", code, body)
				}
				if f.posts.Load() != 2 {
					t.Fatal("disabled continuation sent POST")
				}
				f.assertReleased(t, 2)
			})
		}
	}
}
func TestKiloSessionExpiryAndCallerIsolation(t *testing.T) {
	seen := make(chan kiloSessionSeen, 5)
	f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
	code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "expiry-chain", `{"model":"gpt-6.1-sol","input":"first"}`)
	if code != 200 {
		t.Fatalf("first=%d %s", code, body)
	}
	<-seen
	f.assertReleased(t, 1)
	f.cfg.APIKeys = append(f.cfg.APIKeys, "synthetic-other-kilo")
	f.cfg.ClientKeyScopes = append(f.cfg.ClientKeyScopes, config.ClientKeyScope{APIKey: "synthetic-other-kilo", Channel: "kilo"})
	restartKiloSessionFixture(t, f)
	code, _ = kiloSessionRequest(t, f, "/v1/responses", "synthetic-other-kilo", "expiry-chain", `{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":"wrong caller"}`)
	if code != 409 {
		t.Fatalf("cross caller=%d", code)
	}
	dir := filepath.Join(config.KiloStateDirectory(f.cfg), "bindings")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		doc := ojson.NewDoc(raw)
		for i := range doc.Get("bindings").Array() {
			_ = doc.Set(fmt.Sprintf("bindings.%d.expires_at", i), time.Unix(1, 0).UTC().Format(time.RFC3339Nano))
		}
		if err = os.WriteFile(path, doc.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, payload := range []string{`{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":"expired"}`, `{"model":"gpt-6.1-sol","input":"expired session"}`, `{"model":"gpt-6.1-sol","input":"expired session again"}`} {
		code, _ = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "expiry-chain", payload)
		if code != 409 {
			t.Fatalf("expired=%d", code)
		}
	}
	if f.posts.Load() != 1 {
		t.Fatal("expired/cross caller emitted POST")
	}
	f.assertReleased(t, 0)
	code, body = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "brand-new-key", `{"model":"gpt-6.1-sol","input":"new"}`)
	if code != 200 {
		t.Fatalf("new=%d %s", code, body)
	}
	<-seen
	f.assertReleased(t, 1)
}
func TestKiloSessionConcurrentFirstTurn(t *testing.T) {
	seen := make(chan kiloSessionSeen, 4)
	f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "concurrent-first", `{"model":"gpt-6.1-sol","input":"first"}`)
			if code != 200 {
				t.Errorf("concurrent=%d %s", code, body)
			}
		}()
	}
	close(start)
	wg.Wait()
	first, second := <-seen, <-seen
	if first != second {
		t.Fatalf("first binding split: %v != %v", first, second)
	}
	if f.posts.Load() != 2 {
		t.Fatal("incorrect POST count")
	}
	f.assertReleased(t, 2)
}

func TestKiloSessionStorageFailureBeforeAndAfterPOST(t *testing.T) {
	for _, after := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("after=%t/stream=%t", after, stream), func(t *testing.T) {
				var f *kiloNativeFixture
				seen := make(chan kiloSessionSeen, 3)
				upstream := kiloSessionUpstream(t, seen)
				f = newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if after {
						dir := filepath.Join(config.KiloStateDirectory(f.cfg), "bindings")
						entries, err := os.ReadDir(dir)
						if err != nil {
							t.Error(err)
							return
						}
						for _, entry := range entries {
							if strings.HasSuffix(entry.Name(), ".json") {
								if err = os.WriteFile(filepath.Join(dir, entry.Name()), []byte("corrupt synthetic state"), 0600); err != nil {
									t.Error(err)
									return
								}
							}
						}
					}
					upstream(w, r)
				})
				if !after {
					dir := config.KiloStateDirectory(f.cfg)
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "bindings"), []byte("synthetic blocked directory"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "storage", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"first","stream":%t}`, stream))
				if !after {
					if code != 503 || f.posts.Load() != 0 {
						t.Fatalf("pre-post storage=%d posts=%d %s", code, f.posts.Load(), body)
					}
					f.assertReleased(t, 1)
					return
				}
				if f.posts.Load() != 1 {
					t.Fatal("post failure replayed")
				}
				if code < 400 && !strings.Contains(string(body), "error") {
					t.Fatalf("post failure invisible: %d %s", code, body)
				}
				if strings.Contains(string(body), "response.completed") {
					t.Fatal("storage failure emitted success")
				}
				f.assertReleased(t, 1)
			})
		}
	}
}
func TestKiloSessionFailedObservedResponseRetainsOwner(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			seen := make(chan kiloSessionSeen, 3)
			var seq atomic.Int32
			success := kiloSessionUpstream(t, seen)
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if seq.Add(1) > 1 {
					success(w, r)
					return
				}
				seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), r.Header.Get("X-Session-Id")}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"failed-created","status":"in_progress"}}`+"\n\n")
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, `data: {"type":"response.failed","response":{"id":"failed-created","status":"failed"}}`+"\n\n")
			})
			code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "failed-owner", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"first","stream":%t}`, stream))
			if code < 400 && !strings.Contains(string(body), "error") {
				t.Fatal("missing error")
			}
			first := <-seen
			f.assertReleased(t, 1)
			restartKiloSessionFixture(t, f)
			code, body = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "", `{"model":"gpt-6.1-sol","previous_response_id":"failed-created","input":"continue"}`)
			if code != 200 {
				t.Fatalf("continue=%d %s", code, body)
			}
			if next := <-seen; next != first {
				t.Fatalf("failed mapping moved: %v -> %v", first, next)
			}
			if f.posts.Load() != 2 {
				t.Fatal("incorrect POST count")
			}
			f.assertReleased(t, 1)
		})
	}
}

func TestKiloSessionCancellationRetainsOwner(t *testing.T) {
	seen := make(chan kiloSessionSeen, 3)
	started := make(chan struct{})
	upstreamCanceled := make(chan struct{})
	var n atomic.Int32
	success := kiloSessionUpstream(t, seen)
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) > 1 {
			success(w, r)
			return
		}
		seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), r.Header.Get("X-Session-Id")}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"cancel-created","status":"in_progress"}}`+"\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(upstreamCanceled)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"first","stream":true}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session-Id", "cancel-owner")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "cancel-created") {
			break
		}
	}
	<-started
	first := <-seen
	cancel()
	response.Body.Close()
	select {
	case <-upstreamCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not canceled")
	}
	f.assertReleased(t, 1)
	for _, a := range f.manager.List() {
		if a.Disabled || a.Unavailable || a.Status != coreauth.StatusActive || !a.NextRetryAfter.IsZero() {
			t.Fatal("cancellation penalized account")
		}
	}
	restartKiloSessionFixture(t, f)
	code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "", `{"model":"gpt-6.1-sol","previous_response_id":"cancel-created","input":"continue"}`)
	if code != 200 {
		t.Fatalf("continuation=%d %s", code, body)
	}
	if next := <-seen; next != first {
		t.Fatal("cancel changed binding")
	}
	if f.posts.Load() != 2 {
		t.Fatal("cancel replay")
	}
	f.assertReleased(t, 1)
}
func TestKiloSessionAccountDeletionOrganizationAndTokenRefresh(t *testing.T) {
	for _, change := range []string{"deleted", "organization", "token-refresh"} {
		t.Run(change, func(t *testing.T) {
			seen := make(chan kiloSessionSeen, 3)
			f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
			code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "identity", `{"model":"gpt-6.1-sol","input":"first"}`)
			if code != 200 {
				t.Fatalf("first=%d %s", code, body)
			}
			first := <-seen
			f.assertReleased(t, 1)
			a, _ := f.manager.GetByID(kiloAccountForMachine(t, f, first.Account))
			switch change {
			case "deleted":
				f.manager.Remove(context.Background(), a.ID)
			case "organization":
				a.Metadata["organization_id"] = "different-organization"
				if _, err := f.manager.Update(context.Background(), a); err != nil {
					t.Fatal(err)
				}
			case "token-refresh":
				a.CredentialVersion++
				a.Metadata["refresh_token"] = "synthetic-new-refresh"
				a.LastRefreshedAt = time.Now()
				if _, err := f.manager.Update(context.Background(), a); err != nil {
					t.Fatal(err)
				}
			}
			code, body = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "", `{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":"next"}`)
			if change == "token-refresh" {
				if code != 200 {
					t.Fatalf("refresh=%d %s", code, body)
				}
				if next := <-seen; next != first {
					t.Fatal("refresh changed account/session")
				}
				if f.posts.Load() != 2 {
					t.Fatal("refresh POST count")
				}
				f.assertReleased(t, 2)
			} else {
				if code != 409 || f.posts.Load() != 1 {
					t.Fatalf("invalid identity=%d posts=%d", code, f.posts.Load())
				}
				f.assertReleased(t, 1)
			}
		})
	}
}
func TestKiloSessionHistoryMissAndPayloadRuleBoundary(t *testing.T) {
	seen := make(chan kiloSessionSeen, 3)
	f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
	for _, body := range []string{`{"model":"gpt-6.1-sol","previous_response_id":"missing","input":"next"}`, `{"model":"gpt-6.1-sol","input":[{"type":"function_call_output","call_id":"missing","output":"next"}]}`, `{"model":"gpt-6.1-sol","input":[{"role":"assistant","content":"old answer"},{"role":"user","content":"next"}]}`} {
		code, _ := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "unseen-history", body)
		if code != 409 {
			t.Fatalf("history miss=%d", code)
		}
	}
	if f.posts.Load() != 0 {
		t.Fatal("history miss POST")
	}
	f.assertReleased(t, 0)
}

func TestKiloSessionPayloadRulesAndAgentMessages(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			seen := make(chan kiloSessionSeen, 3)
			f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
			next := f.cfg.CloneForRuntime()
			next.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"unknown_source": "last-rule", "reasoning.effort": "low"}}}
			f.executor.UpdateConfig(next)
			source := fmt.Sprintf(`{"model":"gpt-6.1-sol","input":[{"type":"agent_message","author":"peer","recipient":"self","content":[{"type":"encrypted_content","encrypted_content":"synthetic-ciphertext"}]}],"stream":%t}`, stream)
			code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "agent-first", source)
			if code != 200 {
				t.Fatalf("agent=%d %s", code, body)
			}
			<-seen
			wire := <-f.requests
			if ojson.GetBytes(wire, "unknown_source").String() != "last-rule" || !strings.Contains(string(wire), "synthetic-ciphertext") {
				t.Fatalf("adaptation/rules=%s", wire)
			}
			f.assertReleased(t, 1)
			next = next.CloneForRuntime()
			next.Payload.Override[0].Params = map[string]any{"previous_response_id": "unowned-response"}
			f.executor.UpdateConfig(next)
			code, _ = kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "agent-first", `{"model":"gpt-6.1-sol","input":"next"}`)
			if code != 409 || f.posts.Load() != 1 {
				t.Fatalf("payload bypass=%d posts=%d", code, f.posts.Load())
			}
			f.assertReleased(t, 2)
		})
	}
}
func TestKiloSessionCustomToolContinuation(t *testing.T) {
	for _, protocol := range []string{"responses", "chat/completions"} {
		t.Run(protocol, func(t *testing.T) {
			seen := make(chan kiloSessionSeen, 3)
			var n atomic.Int32
			success := kiloSessionUpstream(t, seen)
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if n.Add(1) > 1 {
					success(w, r)
					return
				}
				seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), r.Header.Get("X-Session-Id")}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"custom-created","status":"in_progress"}}`+"\n\n")
				_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"custom-created","status":"completed","output":[{"type":"custom_tool_call","id":"ct1","call_id":"custom-1","name":"raw_tool","input":"exact raw arguments","status":"completed"}]}}`+"\n\n")
			})
			code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "custom", `{"model":"gpt-6.1-sol","input":"first"}`)
			if code != 200 {
				t.Fatalf("custom first=%d %s", code, body)
			}
			if !strings.Contains(string(body), "exact raw arguments") {
				t.Fatal("custom input changed")
			}
			first := <-seen
			f.assertReleased(t, 1)
			payload := `{"model":"gpt-6.1-sol","input":[{"type":"custom_tool_call_output","call_id":"custom-1","output":"exact custom output"}]}`
			if protocol == "chat/completions" {
				payload = `{"model":"gpt-6.1-sol","messages":[{"role":"tool","tool_call_id":"custom-1","content":"exact custom output"}]}`
			}
			code, body = kiloSessionRequest(t, f, "/v1/"+protocol, "synthetic-kilo-key", "", payload)
			if code != 200 {
				t.Fatalf("custom continuation=%d %s", code, body)
			}
			if next := <-seen; next != first {
				t.Fatal("custom moved account/session")
			}
			<-f.requests
			wire := <-f.requests
			found := false
			for _, item := range ojson.GetBytes(wire, "input").Array() {
				if item.Get("type").String() == "custom_tool_call_output" && item.Get("call_id").String() == "custom-1" && item.Get("output").String() == "exact custom output" {
					found = true
				}
			}
			if !found {
				t.Fatalf("custom wire=%s", wire)
			}
			if f.posts.Load() != 2 {
				t.Fatal("custom replay")
			}
			f.assertReleased(t, 2)
		})
	}
}

func TestKiloSessionBusyOwnerWaitsWithoutSwitching(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	seen := make(chan kiloSessionSeen, 3)
	var n atomic.Int32
	success := kiloSessionUpstream(t, seen)
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) > 1 {
			success(w, r)
			return
		}
		seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), r.Header.Get("X-Session-Id")}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"held-response","status":"in_progress"}}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		terminal := ojson.NewDocString(kiloTerminal)
		_ = terminal.Set("response.id", "held-response")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", terminal.String())
	})
	for _, a := range f.manager.List() {
		if a.Attributes == nil {
			a.Attributes = map[string]string{}
		}
		a.Attributes[coreauth.AttributeConcurrencyLimit] = "1"
		if _, err := f.manager.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest("POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"hold","stream":true}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session-Id", "busy-owner")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "held-response") {
			break
		}
	}
	first := <-seen
	done := make(chan int, 1)
	go func() {
		code, _ := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "", `{"model":"gpt-6.1-sol","previous_response_id":"held-response","input":"wait"}`)
		done <- code
	}()
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			if a.AuthID == kiloAccountForMachine(t, f, first.Account) && a.PinnedWaiting > 0 {
				waiting = true
			}
			if a.AuthID != kiloAccountForMachine(t, f, first.Account) && a.Active != 0 {
				t.Fatal("continuation reserved another account")
			}
		}
		if waiting {
			break
		}
		select {
		case code := <-done:
			t.Fatalf("continuation returned before owner release: %d", code)
		default:
		}
		runtime.Gosched()
	}
	if !waiting || f.posts.Load() != 1 {
		t.Fatalf("owner wait=%t posts=%d", waiting, f.posts.Load())
	}
	unblock()
	_, _ = io.Copy(io.Discard, reader)
	response.Body.Close()
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("after release=%d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiter not released")
	}
	if next := <-seen; next != first {
		t.Fatal("busy owner binding moved")
	}
	if f.posts.Load() != 2 {
		t.Fatal("busy owner replay")
	}
	f.assertReleased(t, 2)
}
func TestKiloSessionStateOutsideCredentialTree(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream)
	f.cfg.Kilo.StateDir = filepath.Join(f.cfg.AuthDir, "inside")
	f.manager.SetConfig(f.cfg)
	code, _ := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", "unsafe-state", `{"model":"gpt-6.1-sol","input":"first"}`)
	if code != 503 || f.posts.Load() != 0 {
		t.Fatalf("credential-tree state=%d POST=%d", code, f.posts.Load())
	}
	f.assertReleased(t, 0)
}

func TestKiloSessionPreviousPublishesSessionAlias(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("conflict=%t/stream=%t", conflict, stream), func(t *testing.T) {
				seen := make(chan kiloSessionSeen, 8)
				f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
				posts := 0
				send := func(key, body string) kiloSessionSeen {
					t.Helper()
					code, data := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", key, body)
					if code != 200 {
						t.Fatalf("alias request=%d %s", code, data)
					}
					posts++
					observed := <-seen
					f.assertReleased(t, posts)
					return observed
				}
				first := send("owner-a", `{"model":"gpt-6.1-sol","input":"first"}`)
				if conflict {
					other := send("alias-b", `{"model":"gpt-6.1-sol","input":"other session"}`)
					if other.Session == first.Session {
						t.Fatal("test did not create two records")
					}
				}
				adopted := send("alias-b", fmt.Sprintf(`{"model":"gpt-6.1-sol","previous_response_id":"resp-chain-1","input":"adopt alias","stream":%t}`, stream))
				if adopted != first {
					t.Fatal("previous lost priority")
				}
				third := send("alias-b", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":[{"role":"assistant","content":"prior answer"},{"role":"user","content":"third turn"}],"stream":%t}`, stream))
				if third != first {
					t.Fatal("session-only third turn changed ownership")
				}
				before := posts
				restartKiloSessionFixture(t, f)
				posts = 0
				after := send("alias-b", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"after restart","stream":%t}`, stream))
				if after != first {
					t.Fatal("session alias lost after restart")
				}
				if int(f.posts.Load()) != before+1 {
					t.Fatal("alias request replayed")
				}
				entries, err := os.ReadDir(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings"))
				if err != nil {
					t.Fatal(err)
				}
				owners := 0
				for _, entry := range entries {
					raw, err := os.ReadFile(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings", entry.Name()))
					if err != nil {
						t.Fatal(err)
					}
					for _, record := range ojson.GetBytes(raw, "bindings").Array() {
						for _, key := range record.Get("sessions").Array() {
							if key.String() == "alias-b" {
								owners++
							}
						}
					}
				}
				if owners != 1 {
					t.Fatalf("session alias has %d owners", owners)
				}
			})
		}
	}
}

func TestKiloSessionRegisteredCallHistoryIsolation(t *testing.T) {
	for _, kind := range []string{"function", "custom", "chat"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", kind, stream), func(t *testing.T) {
				seen := make(chan kiloSessionSeen, 6)
				var seq atomic.Int32
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					n := seq.Add(1)
					seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), r.Header.Get("X-Session-Id")}
					terminal := ojson.NewDocString(kiloTerminal)
					_ = terminal.Set("response.id", fmt.Sprintf("resp-history-%d", n))
					_ = terminal.Set("response.output", []any{map[string]any{"type": "function_call", "id": fmt.Sprintf("fc-%d", n), "call_id": fmt.Sprintf("func-%d", n), "name": "tool", "arguments": "{}", "status": "completed"}, map[string]any{"type": "custom_tool_call", "id": fmt.Sprintf("ct-%d", n), "call_id": fmt.Sprintf("custom-%d", n), "name": "raw", "input": "raw args", "status": "completed"}})
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: %s\n\n", terminal.String())
				})
				for i, key := range []string{"history-a", "history-b"} {
					code, body := kiloSessionRequest(t, f, "/v1/responses", "synthetic-kilo-key", key, `{"model":"gpt-6.1-sol","input":"first"}`)
					if code != 200 {
						t.Fatalf("seed=%d %s", code, body)
					}
					f.assertReleased(t, i+1)
				}
				first, second := <-seen, <-seen
				if first.Session == second.Session {
					t.Fatal("test did not create two registered records")
				}
				path := "/v1/responses"
				payload := func(id string) string {
					if kind == "chat" {
						return fmt.Sprintf(`{"model":"gpt-6.1-sol","previous_response_id":"resp-history-1","messages":[{"role":"assistant","tool_calls":[{"id":%q,"type":"function","function":{"name":"tool","arguments":"{}"}}]},{"role":"tool","tool_call_id":"func-1","content":"known result"}],"stream":%t}`, id, stream)
					}
					callType := "function_call"
					field := "arguments"
					if kind == "custom" {
						callType = "custom_tool_call"
						field = "input"
					}
					return fmt.Sprintf(`{"model":"gpt-6.1-sol","previous_response_id":"resp-history-1","input":[{"type":%q,"call_id":%q,"name":"tool",%q:"{}"},{"type":"function_call_output","call_id":"func-1","output":"known result"}],"stream":%t}`, callType, id, field, stream)
				}
				good, bad := "func-1", "func-2"
				if kind == "custom" {
					good, bad = "custom-1", "custom-2"
				}
				if kind == "chat" {
					path = "/v1/chat/completions"
				}
				for _, id := range []string{bad, "call_kilo_agent_forged"} {
					code, _ := kiloSessionRequest(t, f, path, "synthetic-kilo-key", "history-a", payload(id))
					if code != 409 || f.posts.Load() != 2 {
						t.Fatalf("foreign declaration=%d posts=%d", code, f.posts.Load())
					}
					f.assertReleased(t, 2)
				}
				code, body := kiloSessionRequest(t, f, path, "synthetic-kilo-key", "history-a", payload(good))
				if code != 200 {
					t.Fatalf("own declaration=%d %s", code, body)
				}
				if next := <-seen; next != first {
					t.Fatal("own history changed account/session")
				}
				f.assertReleased(t, 3)
				// The input was authorized before rules; a rule may not inject another record's call.
				next := f.cfg.CloneForRuntime()
				callType := "function_call"
				field := "arguments"
				if kind == "custom" {
					callType = "custom_tool_call"
					field = "input"
				}
				next.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"input": []any{map[string]any{"type": callType, "call_id": bad, "name": "tool", field: "{}"}}}}}
				f.executor.UpdateConfig(next)
				code, _ = kiloSessionRequest(t, f, path, "synthetic-kilo-key", "history-a", payload(good))
				if code != 409 || f.posts.Load() != 3 {
					t.Fatalf("rule-injected declaration=%d posts=%d", code, f.posts.Load())
				}
				f.assertReleased(t, 4)
			})
		}
	}
}
