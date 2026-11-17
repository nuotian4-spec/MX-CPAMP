package cliproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

type kiloLatencyLogHook struct{ entries chan log.Fields }

func (*kiloLatencyLogHook) Levels() []log.Level { return []log.Level{log.DebugLevel} }
func (h *kiloLatencyLogHook) Fire(entry *log.Entry) error {
	if strings.HasPrefix(entry.Message, "Kilo request latency ") {
		fields := make(log.Fields, len(entry.Data))
		for key, value := range entry.Data {
			fields[key] = value
		}
		h.entries <- fields
	}
	return nil
}

type kiloLatencyUsageSample struct {
	record   usage.Record
	observed bool
}
type kiloLatencyUsagePlugin func(context.Context, usage.Record)

func (fn kiloLatencyUsagePlugin) HandleUsage(ctx context.Context, record usage.Record) {
	fn(ctx, record)
}

func kiloLatencyCapture(t *testing.T, f *kiloNativeFixture) (*kiloLatencyLogHook, <-chan kiloLatencyUsageSample) {
	t.Helper()
	logger := log.StandardLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	hook := &kiloLatencyLogHook{entries: make(chan log.Fields, 8)}
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	logger.AddHook(hook)
	logger.SetOutput(io.Discard)
	logger.SetLevel(log.DebugLevel)
	t.Cleanup(func() { logger.ReplaceHooks(oldHooks); logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })
	allowed := make(map[string]bool)
	for _, auth := range f.manager.List() {
		allowed[auth.ID] = true
	}
	items := make(chan kiloLatencyUsageSample, 8)
	name := t.Name()
	usage.RegisterNamedPlugin(name, kiloLatencyUsagePlugin(func(ctx context.Context, record usage.Record) {
		if record.Provider == "kilo" && allowed[record.AuthID] || record.Provider == "kilo-latency-barrier" && record.Model == name {
			_, observed := kilolatency.UsageTTFT(ctx)
			items <- kiloLatencyUsageSample{record, observed}
		}
	}))
	t.Cleanup(func() {
		usage.RegisterNamedPlugin(name, kiloLatencyUsagePlugin(func(context.Context, usage.Record) {}))
	})
	drainDone, drainStopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(drainStopped)
		for {
			select {
			case <-f.requests:
			case <-drainDone:
				return
			}
		}
	}()
	t.Cleanup(func() { close(drainDone); <-drainStopped })
	return hook, items
}

func kiloLatencyTake[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("latency fixture did not reach the expected event")
		var zero T
		return zero
	}
}

func kiloLatencyAssertUsageOnce(t *testing.T, items <-chan kiloLatencyUsageSample, observed bool) {
	t.Helper()
	sample := kiloLatencyTake(t, items)
	if sample.record.Provider != "kilo" || sample.observed != observed {
		t.Fatalf("usage sample=%+v", sample)
	}
	if !observed && sample.record.TTFT != 0 {
		t.Fatalf("unobserved TTFT=%v", sample.record.TTFT)
	}
	usage.PublishRecord(context.Background(), usage.Record{Provider: "kilo-latency-barrier", Model: t.Name()})
	if barrier := kiloLatencyTake(t, items); barrier.record.Provider != "kilo-latency-barrier" {
		t.Fatalf("duplicate Kilo usage=%+v", barrier)
	}
}

func kiloLatencyAssertSummary(t *testing.T, hook *kiloLatencyLogHook, outcome, mode string, text, flushed bool) log.Fields {
	t.Helper()
	fields := kiloLatencyTake(t, hook.entries)
	if fields["outcome"] != outcome || fields["mode"] != mode {
		t.Fatalf("summary=%v", fields)
	}
	for name, want := range map[string]bool{"upstream_first_text_ms": text, "cpa_first_text_ms": flushed} {
		_, exists := fields[name]
		if exists != want {
			t.Fatalf("%s present=%t want=%t: %v", name, exists, want, fields)
		}
	}
	if fields["request_id"] == "" || fields["server_total_ms"] == nil {
		t.Fatalf("missing correlation/total: %v", fields)
	}
	serialized := fmt.Sprint(fields)
	for _, secret := range []string{"latency-answer", "synthetic-kilo-key", "synthetic-kilo-token", "Bearer"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("diagnostic leaked %s", secret)
		}
	}
	select {
	case extra := <-hook.entries:
		t.Fatalf("duplicate summary: %v", extra)
	default:
	}
	return fields
}

func kiloLatencyBody(protocol string, stream bool) string {
	if protocol == "chat/completions" {
		return fmt.Sprintf(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"synthetic latency fixture"}],"stream":%t}`, stream)
	}
	return fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"synthetic latency fixture","stream":%t}`, stream)
}

func kiloLatencyStart(t *testing.T, f *kiloNativeFixture, ctx context.Context, protocol string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+"/v1/"+protocol, strings.NewReader(kiloLatencyBody(protocol, true)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("status=%d body=%s", response.StatusCode, data)
	}
	return response
}

func kiloLatencyPrelude(w http.ResponseWriter, text bool, tier bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	created := `{"type":"response.created","response":{"id":"resp-latency","model":"openai/gpt-6.1-sol","status":"in_progress","output":[]}}`
	if tier {
		created = `{"type":"response.created","response":{"id":"resp-latency","model":"openai/gpt-6.1-sol","status":"in_progress","output":[],"service_tier":"default"}}`
	}
	kiloA1Event(w, created)
	if text {
		kiloA1Event(w, `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg-latency","type":"message","role":"assistant","content":[]}}`)
		kiloA1Event(w, `{"type":"response.content_part.added","item_id":"msg-latency","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`)
		kiloA1Event(w, `{"type":"response.output_text.delta","item_id":"msg-latency","output_index":0,"content_index":0,"delta":"latency-answer"}`)
	}
}

func TestKiloLatencyNativeText(t *testing.T) {
	for _, protocol := range []string{"responses", "chat/completions"} {
		t.Run(protocol, func(t *testing.T) {
			allowTerminal := make(chan struct{})
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				kiloLatencyPrelude(w, true, false)
				select {
				case <-allowTerminal:
					kiloA1Terminal(w, "resp-latency", []any{kiloA1Message("msg-latency", "latency-answer")})
				case <-r.Context().Done():
				}
			})
			hook, items := kiloLatencyCapture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			response := kiloLatencyStart(t, f, ctx, protocol)
			defer response.Body.Close()
			scanner := bufio.NewScanner(response.Body)
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), "latency-answer") {
					break
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(scanner.Text(), "latency-answer") {
				t.Fatal("answer was not flushed before terminal release")
			}
			close(allowTerminal)
			for scanner.Scan() {
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			fields := kiloLatencyAssertSummary(t, hook, "success_text", "stream", true, true)
			for _, name := range []string{"admission_elapsed_ms", "upstream_headers_ms", "binding_read_ms", "binding_commit_ms", "selection_lock_wait_ms"} {
				if fields[name] == nil {
					t.Fatalf("missing %s: %v", name, fields)
				}
			}
			kiloLatencyAssertUsageOnce(t, items, true)
			if f.posts.Load() != 1 {
				t.Fatalf("POST count=%d", f.posts.Load())
			}
		})
	}
}

func TestKiloLatencyNativeOutcomes(t *testing.T) {
	for _, test := range []struct {
		name, outcome, mode  string
		text, flushed, known bool
	}{
		{"tool", "success_tool", "stream", false, false, false},
		{"terminal_only", "success_empty", "stream", false, false, false},
		{"nonstream", "success_text", "nonstream", true, false, true},
		{"early_usage", "success_text", "stream", true, true, false},
		{"failure_before", "failed", "stream", false, false, false},
		{"failure_after", "failed", "stream", true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			allowFailure := make(chan struct{})
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch test.name {
				case "tool":
					kiloA1ClientOutput(w, "resp-latency", []any{kiloA1Call("latency", "fixture_tool", map[string]any{"x": 1})})
				case "failure_before":
					http.Error(w, "synthetic upstream failure", http.StatusBadGateway)
				default:
					kiloLatencyPrelude(w, test.text, test.name == "early_usage")
					if test.name == "failure_after" {
						select {
						case <-allowFailure:
							kiloA1Event(w, `{"type":"response.failed","response":{"id":"resp-latency","status":"failed","error":{"code":"server_error","message":"synthetic failure"}}}`)
						case <-r.Context().Done():
						}
						return
					}
					kiloA1Terminal(w, "resp-latency", []any{kiloA1Message("msg-latency", "latency-answer")})
				}
			})
			hook, items := kiloLatencyCapture(t, f)
			code := 200
			if test.name == "failure_after" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				response := kiloLatencyStart(t, f, ctx, "responses")
				defer response.Body.Close()
				scanner := bufio.NewScanner(response.Body)
				for scanner.Scan() {
					if strings.Contains(scanner.Text(), "latency-answer") {
						break
					}
				}
				if !strings.Contains(scanner.Text(), "latency-answer") {
					t.Fatal("body not delivered before injected failure")
				}
				close(allowFailure)
				for scanner.Scan() {
				}
			} else {
				code, _ = f.request(t, http.MethodPost, "/v1/responses", "synthetic-kilo-key", kiloLatencyBody("responses", test.mode == "stream"))
			}
			if test.name == "failure_before" {
				if code < 400 {
					t.Fatalf("initial failure HTTP=%d", code)
				}
			} else if code != 200 {
				t.Fatalf("HTTP=%d", code)
			}
			kiloLatencyAssertSummary(t, hook, test.outcome, test.mode, test.text, test.flushed)
			kiloLatencyAssertUsageOnce(t, items, test.known)
			if f.posts.Load() != 1 {
				t.Fatalf("POST count=%d", f.posts.Load())
			}
		})
	}
}

func TestKiloLatencyNativeCancellation(t *testing.T) {
	for _, afterText := range []bool{false, true} {
		t.Run(fmt.Sprintf("after_text=%t", afterText), func(t *testing.T) {
			stopped := make(chan struct{})
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				kiloLatencyPrelude(w, afterText, false)
				<-r.Context().Done()
				close(stopped)
			})
			hook, items := kiloLatencyCapture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			response := kiloLatencyStart(t, f, ctx, "responses")
			defer response.Body.Close()
			marker := "response.created"
			if afterText {
				marker = "latency-answer"
			}
			scanner := bufio.NewScanner(response.Body)
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), marker) {
					break
				}
			}
			if !strings.Contains(scanner.Text(), marker) {
				t.Fatalf("missing cancellation boundary %s", marker)
			}
			cancel()
			_, _ = io.Copy(io.Discard, response.Body)
			kiloLatencyTake(t, stopped)
			kiloLatencyAssertSummary(t, hook, "canceled", "stream", afterText, afterText)
			kiloLatencyAssertUsageOnce(t, items, afterText)
			if f.posts.Load() != 1 {
				t.Fatalf("POST count=%d", f.posts.Load())
			}
		})
	}
}
