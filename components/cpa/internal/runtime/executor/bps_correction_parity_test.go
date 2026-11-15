package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSParityToolCorrections(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name             string
			successAt, posts int
			ok               bool
		}{{"first", 2, 2, true}, {"second", 3, 3, true}, {"exhausted", 4, 3, false}, {"missing-call", 0, 1, false}, {"unknown-native", 0, 1, false}, {"correction403", 0, 2, false}, {"parallel-limit", 0, 1, false}} {
			t.Run(fmt.Sprintf("%s/stream=%v", tc.name, stream), func(t *testing.T) {
				cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				posts := 0
				capture := &captureAnyProviderUsagePlugin{provider: "bps", records: make(chan usage.Record, 8)}
				usage.RegisterNamedPlugin(t.Name(), capture)
				t.Cleanup(func() { usage.RegisterNamedPlugin(t.Name(), noopClaudeUsagePlugin{}) })
				const rawCode = "text('original')\n"
				ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(r *http.Request) (*http.Response, error) {
					posts++
					body, _ := io.ReadAll(r.Body)
					if r.Header.Get("x-openai-account-id") != "account-a" {
						t.Fatal("correction switched account")
					}
					if posts > 1 && !strings.Contains(string(body), "invalid_client_tool_transport") {
						t.Fatalf("missing repair feedback: %s", body)
					}
					if tc.name == "correction403" && posts == 2 {
						return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("synthetic"))}, nil
					}
					arguments := ojson.NewDocString(`{"summary":"Call client tool","extended_summary":"","destructive":false,"references":[]}`)
					_ = arguments.Set("code", rawCode)
					if tc.name == "parallel-limit" {
						_ = arguments.Set("summary", "codex2api.custom/functions.exec")
					}
					if tc.successAt > 0 && posts >= tc.successAt {
						_ = arguments.Set("summary", "codex2api.custom/functions.exec")
						_ = arguments.Set("code", "rewritten by repair")
					}
					item := map[string]any{"id": fmt.Sprintf("fc_%d", posts), "type": "function_call", "call_id": fmt.Sprintf("call_%d", posts), "name": "run_officejs", "arguments": arguments.String()}
					if tc.name == "missing-call" {
						delete(item, "call_id")
					}
					if tc.name == "unknown-native" {
						item["name"] = "unknown_native"
					}
					terminal := ojson.NewDocString(bpsTestTerminal)
					_ = terminal.Set("response.id", fmt.Sprintf("resp_attempt_%d", posts))
					_ = terminal.Set("response.output", []any{item})
					if tc.name == "parallel-limit" {
						second := map[string]any{}
						for k, v := range item {
							second[k] = v
						}
						second["id"] = "fc_second"
						second["call_id"] = "call_second"
						_ = terminal.Set("response.output", []any{item, second})
					}
					wire := "data: " + terminal.String() + "\n\n"
					if posts == 1 {
						wire = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_attempt_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" + wire
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
				})))
				body := []byte(`{"model":"configured-model","input":"run","tools":[{"type":"custom","name":"functions.exec"}]}`)
				if tc.name == "parallel-limit" {
					doc := ojson.NewDoc(body)
					_ = doc.Set("parallel_tool_calls", false)
					body = doc.Bytes()
				}
				req := coreexecutor.Request{Model: "configured-model", Payload: body}
				opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}
				exec := NewBPSExecutor(cfg)
				var err error
				output := ""
				if stream {
					var result *coreexecutor.StreamResult
					result, err = exec.ExecuteStream(ctx, parityAuth(), req, opts)
					if err == nil {
						for chunk := range result.Chunks {
							output += string(chunk.Payload)
							if chunk.Err != nil {
								err = chunk.Err
							}
						}
					}
				} else {
					var result coreexecutor.Response
					result, err = exec.Execute(ctx, parityAuth(), req, opts)
					output = string(result.Payload)
				}
				if (err == nil) != tc.ok || posts != tc.posts {
					t.Fatalf("err=%v posts=%d want=%d output=%s", err, posts, tc.posts, output)
				}
				if tc.ok && (!strings.Contains(output, "original") || strings.Contains(output, "rewritten by repair") || strings.Contains(output, "resp_attempt_2") || strings.Contains(output, "resp_attempt_3")) {
					t.Fatalf("correction identity/raw changed: %s", output)
				}
				if tc.name == "unknown-native" && (err == nil || !strings.Contains(err.Error(), `"reason":"unsupported_native_tool"`) || !strings.Contains(err.Error(), `"catalog_count":1`) || strings.Contains(err.Error(), "original")) {
					t.Fatalf("diagnostics=%v", err)
				}
				select {
				case record := <-capture.records:
					if record.Detail.Iterations == nil || len(*record.Detail.Iterations) != tc.posts {
						t.Fatalf("iterations=%+v", record.Detail.Iterations)
					}
					known := tc.posts
					if tc.name == "correction403" {
						known--
					}
					if record.Detail.InputTokens != int64(known*3) || record.Detail.OutputTokens != int64(known) || record.Detail.TotalTokens != int64(known*4) {
						t.Fatalf("aggregate=%+v", record.Detail)
					}
					for i, item := range *record.Detail.Iterations {
						want := "bps_initial"
						if i > 0 {
							want = "bps_correction"
						}
						if item.Type != want {
							t.Fatalf("iteration type=%s", item.Type)
						}
						if i >= known {
							if item.InputTokens != nil || item.OutputTokens != nil {
								t.Fatal("unknown usage became zero")
							}
						} else if item.InputTokens == nil || *item.InputTokens != 3 || item.OutputTokens == nil || *item.OutputTokens != 1 {
							t.Fatalf("per POST usage=%+v", item)
						}
					}
				case <-time.After(5 * time.Second):
					t.Fatal("missing usage record")
				}
			})
		}
	}
}
