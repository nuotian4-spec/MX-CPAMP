package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

func assertKiloPublicErrorClean(t *testing.T, body []byte) {
	t.Helper()
	lower := strings.ToLower(string(body))
	for _, forbidden := range []string{"kilo", "private-upstream", "synthetic-secret", "bearer", "authorization", "access_token"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("public error leaked %q: %s", forbidden, body)
		}
	}
}

func TestKiloPublicNativeProtocolAndConnection(t *testing.T) {
	for _, sample := range []struct {
		name, body, encoding, code string
		disconnect, started        bool
	}{
		{name: "invalid-json", body: "data: {private-upstream}\n\n", code: "upstream_response_invalid"},
		{name: "invalid-encoding", encoding: "private-upstream", code: "upstream_response_invalid"},
		{name: "incomplete", body: ": heartbeat\n\n", code: "upstream_response_invalid"},
		{name: "connection", disconnect: true, code: "upstream_connection_failed"},
		{name: "started-invalid-json", body: "data: {private-upstream}\n\n", code: "upstream_response_invalid", started: true},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", sample.name, stream), func(t *testing.T) {
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if sample.disconnect {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("X-Upstream-Secret", "Bearer synthetic-secret")
					if sample.encoding != "" {
						w.Header().Set("Content-Encoding", sample.encoding)
					}
					if sample.started {
						_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"synthetic-response\",\"model\":\"openai/gpt-6.1-sol\",\"status\":\"in_progress\"}}\n\n")
						w.(http.Flusher).Flush()
					}
					_, _ = io.WriteString(w, sample.body)
				})
				f.cfg.SDKConfig.PassthroughHeaders = true
				req, err := http.NewRequest("POST", f.server.URL+"/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"synthetic-input","stream":%t}`, stream)))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
				req.Header.Set("Content-Type", "application/json")
				resp, err := f.server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := 502
				if sample.started && stream {
					wantStatus = 200
				}
				if resp.StatusCode != wantStatus || !bytes.Contains(body, []byte(sample.code)) || resp.Header.Get("X-Upstream-Secret") != "" {
					t.Fatalf("HTTP=%d headers=%v body=%s", resp.StatusCode, resp.Header, body)
				}
				assertKiloPublicErrorClean(t, body)
				f.assertReleased(t, 1)
				if f.posts.Load() != 1 || f.hook.snapshot()[0].Success {
					t.Fatalf("replayed or succeeded: posts=%d", f.posts.Load())
				}
				if sample.started && stream && bytes.Count(body, []byte("event: error")) != 1 {
					t.Fatalf("expected exactly one error event: %s", body)
				}
				if bytes.Contains(body, []byte("response.completed")) {
					t.Fatal("failure completed")
				}
			})
		}
	}
}

func TestKiloPublicNativeRequestIDCorrelation(t *testing.T) {
	kiloPublicRequestIDCorrelation(t, false)
}

func TestKiloPublicNativeStartedRequestIDCorrelation(t *testing.T) {
	kiloPublicRequestIDCorrelation(t, true)
}

func kiloPublicRequestIDCorrelation(t *testing.T, started bool) {
	t.Helper()
	oldHooks := log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	hook := logtest.NewGlobal()
	t.Cleanup(func() { log.StandardLogger().ReplaceHooks(oldHooks) })
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if started {
			_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"synthetic-response\",\"model\":\"openai/gpt-6.1-sol\",\"status\":\"in_progress\"}}\n\n")
			w.(http.Flusher).Flush()
		}
		_, _ = io.WriteString(w, "event: error\ndata: {\"code\":\"private-upstream https://kilo.ai/?token=synthetic-secret\",\"message\":\"Bearer synthetic-secret\"}\n\n")
	})
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"synthetic-prompt-not-for-logs","stream":%t}`, started))
	assertKiloPublicErrorClean(t, body)
	requestID := ojson.GetBytes(body, "error.request_id").String()
	wantStatus := 502
	if started {
		wantStatus = 200
		for _, line := range bytes.Split(body, []byte("\n")) {
			if bytes.HasPrefix(line, []byte("data:")) {
				if id := ojson.GetBytes(bytes.TrimSpace(line[5:]), "error.request_id").String(); id != "" {
					requestID = id
				}
			}
		}
		if bytes.Count(body, []byte("event: error")) != 1 || bytes.Contains(body, []byte("response.completed")) {
			t.Fatalf("unexpected stream terminal: %s", body)
		}
	}
	if status != wantStatus || requestID == "" {
		t.Fatalf("missing public correlation: HTTP=%d %s", status, body)
	}
	matched := 0
	for _, entry := range hook.AllEntries() {
		if entry.Message != "upstream request failed" || entry.Data["request_id"] != requestID {
			continue
		}
		matched++
		if entry.Data["provider"] != "kilo" || entry.Data["upstream_status"] != 200 || entry.Data["code"] != "unrecognized" || entry.Data["public_code"] != "request_failed" || entry.Data["stream_started"] != started {
			t.Fatalf("wrong diagnostic=%v", entry.Data)
		}
		for _, forbidden := range []string{"synthetic-secret", "synthetic-prompt", "https://"} {
			if strings.Contains(fmt.Sprint(entry.Data), forbidden) {
				t.Fatalf("diagnostic leaked=%v", entry.Data)
			}
		}
		t.Logf("correlated request_id=%s status=%v upstream_status=%v public_code=%v stream_started=%v", requestID, entry.Data["status"], entry.Data["upstream_status"], entry.Data["public_code"], entry.Data["stream_started"])
	}
	if matched != 1 {
		t.Fatalf("correlated diagnostics=%d", matched)
	}
	f.assertReleased(t, 1)
	if f.posts.Load() != 1 {
		t.Fatalf("POST=%d", f.posts.Load())
	}
}

func TestKiloPublicNativeEarlyRejections(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	for _, path := range []string{"/v1/responses", "/v1/responses/compact", "/v1/chat/completions"} {
		for _, stream := range []bool{false, true} {
			status, body := f.request(t, "POST", path, "synthetic-kilo-key", fmt.Sprintf(`{"model":"https://kilo.ai/private-upstream?token=synthetic-secret","input":"synthetic-input","stream":%t}`, stream))
			if status < 400 {
				t.Fatalf("unknown model accepted: %d %s", status, body)
			}
			assertKiloPublicErrorClean(t, body)
		}
	}
	for _, body := range []string{
		`{"id":"s","model":"gpt-6.1-sol","commands":{"private-upstream https://kilo.ai":[]}}`,
		`{"id":"s","model":"https://kilo.ai/private-upstream","commands":{"search_query":[{"q":"test"}]}}`,
	} {
		status, response := f.request(t, "POST", "/v1/alpha/search", "synthetic-kilo-key", body)
		if status < 400 {
			t.Fatalf("invalid search accepted: %d %s", status, response)
		}
		assertKiloPublicErrorClean(t, response)
		if !ojson.GetBytes(response, "error.completed_queries").Exists() {
			t.Fatalf("missing completed count: %s", response)
		}
	}
	if f.posts.Load() != 0 {
		t.Fatalf("rejection reached upstream: %d", f.posts.Load())
	}
}

func TestKiloPublicNativeAdmissionAndCompact(t *testing.T) {
	for _, name := range []string{"no-accounts", "unusable-egress", "compact"} {
		t.Run(name, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloSuccessStream)
			path := "/v1/responses"
			wantStatus := 503
			switch name {
			case "no-accounts":
				for _, a := range f.manager.List() {
					a.Disabled = true
					if _, err := f.manager.Update(context.Background(), a); err != nil {
						t.Fatal(err)
					}
				}
			case "unusable-egress":
				f.probeHandler = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "not-an-ip") }
			case "compact":
				path, wantStatus = "/v1/responses/compact", 501
			}
			status, body := f.request(t, "POST", path, "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"synthetic-input"}`)
			if status != wantStatus || ojson.GetBytes(body, "error.code").String() != "request_failed" {
				t.Fatalf("admission status=%d body=%s", status, body)
			}
			assertKiloPublicErrorClean(t, body)
			if ojson.GetBytes(body, "error.request_id").String() == "" {
				t.Fatalf("missing request ID: %s", body)
			}
			if f.posts.Load() != 0 || f.connects.Load() != 0 {
				t.Fatalf("rejection sent POST=%d CONNECT=%d", f.posts.Load(), f.connects.Load())
			}
			for _, result := range f.hook.snapshot() {
				if result.Success {
					t.Fatal("rejection succeeded")
				}
			}
		})
	}
}
