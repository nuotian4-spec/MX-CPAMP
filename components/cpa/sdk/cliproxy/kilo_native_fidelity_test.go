package cliproxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloM7ErrorShapes(t *testing.T) {
	for _, sample := range []struct {
		name, frame, code, kind string
		upstream, status        int
	}{
		{"event-error", "event: error\ndata: {\"message\":\"synthetic-private-upstream-detail\"}\n\n", "", "", 200, 502},
		{"nested-quota", "data: {\"error\":{\"code\":\"subscription_sharing_usage_limit_exceeded\",\"type\":\"usage_limit_error\",\"message\":\"synthetic-private-upstream-detail\"}}\n\n", "subscription_sharing_usage_limit_exceeded", "usage_limit_error", 200, 429},
		{"code-message", "data: {\"code\":\"synthetic_unknown_error\",\"type\":\"synthetic_error\",\"message\":\"synthetic-private-upstream-detail\"}\n\n", "synthetic_unknown_error", "synthetic_error", 200, 502},
		{"sse-restricted", "data: {\"error\":{\"code\":\"account_restricted\",\"type\":\"account_restricted\",\"message\":\"synthetic-private-upstream-detail\"}}\n\n", "account_restricted", "account_restricted", 200, 502},
		{"http-restricted", `{"error":{"code":"account_restricted","type":"account_restricted","message":"synthetic-private-upstream-detail"}}`, "account_restricted", "account_restricted", 403, 403},
	} {
		for _, protocol := range []string{"responses", "chat/completions"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", sample.name, protocol, stream), func(t *testing.T) {
					f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
						if sample.upstream != 200 {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(sample.upstream)
							_, _ = io.WriteString(w, sample.frame)
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, sample.frame+"data: "+kiloTerminal+"\n\n")
					})
					request := fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"test","messages":[{"role":"user","content":"test"}],"stream":%t}`, stream)
					status, body := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", request)
					t.Logf("public HTTP=%d body=%s", status, body)
					f.assertReleased(t, 1)
					results := f.hook.snapshot()
					if f.posts.Load() != 1 || f.connects.Load() != 1 || results[0].Success || results[0].Error == nil {
						t.Fatalf("expected one failed result and no replay: POST=%d results=%+v", f.posts.Load(), results)
					}
					result := results[0].Error
					t.Logf("result code=%s HTTP=%d message=%s", result.Code, result.HTTPStatus, result.Message)
					if status != sample.status || result.HTTPStatus != sample.status || result.Code != sample.code || ojson.Get(result.Message, "error.type").String() != sample.kind || ojson.Get(result.Message, "upstream_status").Int() != int64(sample.upstream) {
						t.Errorf("classification status=%d result=%+v", status, result)
					}
					for _, forbidden := range []string{"synthetic-private-upstream-detail", "response.completed", "kilo-ok"} {
						if bytes.Contains(body, []byte(forbidden)) || strings.Contains(result.Message, "synthetic-private-upstream-detail") {
							t.Errorf("failed response leaked or completed: %s", body)
						}
					}
					if !bytes.Contains(body, []byte("request_failed")) {
						t.Errorf("missing fixed public error: %s", body)
					}
				})
			}
		}
	}
}

func TestKiloM7PublicModelFourPaths(t *testing.T) {
	for _, protocol := range []string{"responses", "chat/completions"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", protocol, stream), func(t *testing.T) {
				f := newKiloNativeFixture(t, kiloSuccessStream)
				status, body := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"test","messages":[{"role":"user","content":"test"}],"stream":%t}`, stream))
				if status != 200 {
					t.Fatalf("HTTP=%d %s", status, body)
				}
				t.Logf("public=%s", body)
				check := func(data []byte) {
					for _, path := range []string{"model", "response.model"} {
						if model := ojson.GetBytes(data, path); model.Exists() && model.String() != "gpt-6.1-sol" {
							t.Errorf("%s=%s", path, model.String())
						}
					}
				}
				if stream {
					for _, line := range bytes.Split(body, []byte("\n")) {
						if bytes.HasPrefix(line, []byte("data:")) {
							check(bytes.TrimSpace(line[5:]))
						}
					}
				} else {
					check(body)
				}
				if !bytes.Contains(body, []byte(`"model":"gpt-6.1-sol"`)) {
					t.Error("public model missing")
				}
				if protocol == "responses" && !bytes.Contains(body, []byte(`"unknown_terminal":{"preserved":true}`)) {
					t.Error("unknown field lost")
				}
				upstream := <-f.requests
				if ojson.GetBytes(upstream, "model").String() != "openai/gpt-6.1-sol" {
					t.Errorf("upstream model=%s", upstream)
				}
				f.assertReleased(t, 1)
			})
		}
	}
}

func TestKiloM7ResponseControlFrames(t *testing.T) {
	control := ": heartbeat\n\nid: source-id\nretry: 77\n\n"
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, control+"data: "+kiloTerminal+"\n\ndata: [DONE]\n\n: after-terminal\n\n")
	})
	status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test","stream":true}`)
	if status != 200 || !bytes.HasPrefix(body, []byte(control)) || bytes.Contains(body, []byte("after-terminal")) {
		t.Fatalf("control frame/boundary HTTP=%d body=%s", status, body)
	}
	f.assertReleased(t, 1)
	t.Logf("control frames retained before terminal; upstream tail not consumed: %s", body)
}

func TestKiloM7StartedStreamErrors(t *testing.T) {
	for _, frame := range []string{
		"event: error\ndata: {\"message\":\"synthetic-private-upstream-detail\"}\n\n",
		"data: {\"error\":{\"code\":\"rate_limit_exceeded\",\"type\":\"rate_limit_error\",\"message\":\"synthetic-private-upstream-detail\"}}\n\n",
		"data: {\"code\":\"invalid_api_key\",\"type\":\"authentication_error\",\"message\":\"synthetic-private-upstream-detail\"}\n\n",
	} {
		for _, protocol := range []string{"responses", "chat/completions"} {
			t.Run(fmt.Sprintf("%s/%s", protocol, frame[:10]), func(t *testing.T) {
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-before-error\",\"model\":\"openai/gpt-6.1-sol\",\"status\":\"in_progress\"}}\n\n"+
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
					w.(http.Flusher).Flush()
					_, _ = io.WriteString(w, frame+"data: "+kiloTerminal+"\n\n")
				})
				status, body := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test","messages":[{"role":"user","content":"test"}],"stream":true}`)
				f.assertReleased(t, 1)
				result := f.hook.snapshot()[0]
				if status != 200 || result.Success || f.posts.Load() != 1 || !bytes.Contains(body, []byte("request_failed")) || bytes.Contains(body, []byte("synthetic-private")) || bytes.Contains(body, []byte("response.completed")) || bytes.Contains(body, []byte("kilo-ok")) {
					t.Fatalf("started failure HTTP=%d result=%+v body=%s", status, result, body)
				}
				wantStatus := 502
				if strings.Contains(frame, "rate_limit_error") {
					wantStatus = 429
				}
				if strings.Contains(frame, "authentication_error") {
					wantStatus = 401
				}
				if result.Error == nil || result.Error.HTTPStatus != wantStatus || ojson.Get(result.Error.Message, "upstream_status").Int() != 200 {
					t.Errorf("started classification=%+v", result.Error)
				}
				t.Logf("started HTTP200, result=%+v, POST1, released, public=%s", result.Error, body)
			})
		}
	}
}
