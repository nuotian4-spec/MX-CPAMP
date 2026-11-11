package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type bpsTestTransport func(*http.Request) (*http.Response, error)

func (f bpsTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const bpsTestTerminal = `{"type":"response.completed","response":{"id":"resp_native_test","object":"response","created_at":1,"status":"completed","model":"configured-model","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`

func TestBPSExecutorTextProtocolsAndFinalBarrier(t *testing.T) {
	for _, format := range []sdktranslator.Format{sdktranslator.FormatOpenAIResponse, sdktranslator.FormatOpenAI} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", format, stream), func(t *testing.T) {
				cfg := &config.Config{Payload: config.PayloadConfig{
					Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "configured-model"}}, Params: map[string]any{"reasoning_effort": "high", "metadata.task_id": "user-task"}}},
					Filter:   []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "configured-model"}}, Params: []string{"metadata.turn_id"}}},
				}}
				calls := 0
				transport := bpsTestTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.URL.String() != helps.BPSResponsesURL || r.Header.Get("Authorization") != "Bearer excel-token" || r.Header.Get("x-openai-account-id") != "excel-account" {
						t.Fatal("BPS route or credentials changed")
					}
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					doc := ojson.NewDoc(body)
					if doc.Get("reasoning_effort").String() != "high" || doc.Get("metadata.task_id").String() != "user-task" || doc.Get("metadata.turn_id").Exists() {
						t.Fatalf("final barrier overwritten: %s", doc.String())
					}
					if doc.Get("model_selection").String() != "explicit" || doc.Get("input.#").Int() < 2 {
						t.Fatalf("protocol missing: %s", doc.String())
					}
					data := "data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_native_test\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\ndata: " + bpsTestTerminal + "\n\n"
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))}, nil
				})
				ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
				auth := &coreauth.Auth{ID: "bps-test", Provider: "bps", Metadata: map[string]any{"type": "bps", "access_token": "excel-token", "account_id": "excel-account"}}
				payload := []byte(`{"model":"configured-model","input":"hi"}`)
				if format == sdktranslator.FormatOpenAI {
					payload = []byte(`{"model":"configured-model","messages":[{"role":"user","content":"hi"}]}`)
				}
				req := coreexecutor.Request{Model: "configured-model", Payload: payload}
				opts := coreexecutor.Options{SourceFormat: format, OriginalRequest: payload}
				executor := NewBPSExecutor(cfg)
				if stream {
					result, err := executor.ExecuteStream(ctx, auth, req, opts)
					if err != nil {
						t.Fatal(err)
					}
					var output strings.Builder
					var responseEvents []string
					for chunk := range result.Chunks {
						if chunk.Err != nil {
							t.Fatal(chunk.Err)
						}
						output.Write(chunk.Payload)
						if format == sdktranslator.FormatOpenAIResponse {
							root := ojson.ParseBytes([]byte(strings.TrimSpace(strings.TrimPrefix(string(chunk.Payload), "data:"))))
							kind := root.Get("type").String()
							responseEvents = append(responseEvents, kind)
							if kind == "response.output_text.delta" && (len(responseEvents) < 3 || responseEvents[len(responseEvents)-2] != "response.content_part.added" || responseEvents[len(responseEvents)-3] != "response.output_item.added") {
								t.Fatalf("first upstream delta preceded message/content establishment: %v", responseEvents)
							}
							if kind == "response.output_item.added" && root.Get("item.id").String() != "msg_1" {
								t.Fatalf("message identity changed: %s", chunk.Payload)
							}
							if kind == "response.content_part.added" && (root.Get("item_id").String() != "msg_1" || root.Get("output_index").Int() != 0 || root.Get("content_index").Int() != 0) {
								t.Fatalf("content coordinates changed: %s", chunk.Payload)
							}
						}
					}
					if format == sdktranslator.FormatOpenAIResponse && strings.Join(responseEvents, ",") != "response.created,response.output_item.added,response.content_part.added,response.output_text.delta,response.output_text.done,response.content_part.done,response.output_item.done,response.completed" {
						t.Fatalf("first-delta lifecycle incomplete or duplicated: %v", responseEvents)
					}
					if !strings.Contains(output.String(), "resp_bps_native_test") {
						t.Fatalf("missing BPS response correlation: %s", output.String())
					}
					if strings.Contains(output.String(), `"id":"resp_native_test"`) || strings.Contains(output.String(), `"response_id":"resp_native_test"`) {
						t.Fatal("unmarked BPS response ID")
					}
					if !strings.Contains(output.String(), "hello") {
						t.Fatalf("missing text: %s", output.String())
					}
				} else {
					result, err := executor.Execute(ctx, auth, req, opts)
					if err != nil {
						t.Fatal(err)
					}
					if format == sdktranslator.FormatOpenAIResponse {
						id := ojson.GetBytes(result.Payload, "id").String()
						if !strings.HasPrefix(id, "resp_bps_") {
							t.Fatalf("unmarked response ID: %s", result.Payload)
						}
						manager := coreauth.NewManager(nil, nil, nil)
						_, followErr := manager.Execute(ctx, []string{"codex", "bps"}, coreexecutor.Request{Payload: []byte(`{"input":"next","previous_response_id":"` + id + `"}`)}, opts)
						if followErr == nil {
							t.Fatal("BPS response ID was replayed to another provider")
						}
					}
					if !strings.Contains(string(result.Payload), "hello") {
						t.Fatalf("missing text: %s", result.Payload)
					}
				}
				if calls != 1 {
					t.Fatalf("upstream calls=%d", calls)
				}
			})
		}
	}
}

func TestBPSExecutorStreamTruncation(t *testing.T) {
	transport := bpsTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"))}, nil
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
	auth := &coreauth.Auth{Provider: "bps", Metadata: map[string]any{"access_token": "test", "account_id": "account"}}
	result, err := NewBPSExecutor(nil).ExecuteStream(ctx, auth, coreexecutor.Request{Model: "configured-model", Payload: []byte(`{"input":"hi"}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for chunk := range result.Chunks {
		failed = failed || chunk.Err != nil
	}
	if !failed {
		t.Fatal("truncated stream was reported successful")
	}
}

func TestBPSExecutorBufferedTerminalProducesChatText(t *testing.T) {
	for _, unary := range []bool{false, true} {
		transport := bpsTestTransport(func(*http.Request) (*http.Response, error) {
			body := "data: " + bpsTestTerminal + "\n\n"
			contentType := "text/event-stream"
			if unary {
				body = ojson.Get(bpsTestTerminal, "response").Raw
				contentType = "application/json"
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
		auth := &coreauth.Auth{Provider: "bps", Metadata: map[string]any{"access_token": "test", "account_id": "account"}}
		result, err := NewBPSExecutor(nil).ExecuteStream(ctx, auth, coreexecutor.Request{Model: "configured-model", Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
		if err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
			output.Write(chunk.Payload)
		}
		if !strings.Contains(output.String(), "hello") || !strings.Contains(output.String(), "resp_bps_native_test") {
			t.Fatalf("buffered terminal text/id missing: %s", output.String())
		}
	}
}

func TestBPSExecutorResponsesStreamLifecycle(t *testing.T) {
	created := `{"type":"response.created","response":{"id":"resp_native_test","output":[]}}`
	added := `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"in_progress","content":[]}}`
	partAdded := `{"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`
	delta := `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}`
	textDone := `{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"hello"}`
	partDone := `{"type":"response.content_part.done","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"hello"}}`
	itemDone := `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}}`
	for _, mode := range []string{"terminal-sse", "unary", "complete-sse", "missing-done-sse"} {
		t.Run(mode, func(t *testing.T) {
			upstream := []string{bpsTestTerminal}
			if mode == "complete-sse" {
				upstream = []string{created, added, partAdded, delta, textDone, partDone, itemDone, bpsTestTerminal}
			}
			if mode == "missing-done-sse" {
				upstream = []string{created, added, partAdded, delta, bpsTestTerminal}
			}
			body, contentType := "data: "+strings.Join(upstream, "\n\ndata: ")+"\n\n", "text/event-stream"
			if mode == "unary" {
				body, contentType = ojson.Get(bpsTestTerminal, "response").Raw, "application/json"
			}
			transport := bpsTestTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
			auth := &coreauth.Auth{Provider: "bps", Metadata: map[string]any{"access_token": "test", "account_id": "account"}}
			result, err := NewBPSExecutor(nil).ExecuteStream(ctx, auth, coreexecutor.Request{Model: "configured-model", Payload: []byte(`{"input":"hi"}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
			if err != nil {
				t.Fatal(err)
			}
			var types []string
			var assembled strings.Builder
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
				root := ojson.ParseBytes([]byte(strings.TrimSpace(strings.TrimPrefix(string(chunk.Payload), "data:"))))
				kind := root.Get("type").String()
				types = append(types, kind)
				if kind == "response.output_item.added" && (root.Get("item.id").String() != "msg_1" || root.Get("item.type").String() != "message" || root.Get("item.content.#").Int() != 0) {
					t.Fatalf("invalid message establishment: %s", chunk.Payload)
				}
				if kind == "response.content_part.added" && (root.Get("part.type").String() != "output_text" || root.Get("part.text").String() != "") {
					t.Fatalf("invalid content establishment: %s", chunk.Payload)
				}
				if index := root.Get("output_index"); index.Exists() && index.Int() != 0 {
					t.Fatalf("invalid public output index: %s", chunk.Payload)
				}
				if kind == "response.output_text.delta" {
					if len(types) < 3 || types[len(types)-2] != "response.content_part.added" || types[len(types)-3] != "response.output_item.added" {
						t.Fatalf("text arrived before item/content establishment: %v", types)
					}
					assembled.WriteString(root.Get("delta").String())
				}
			}
			want := "response.created,response.output_item.added,response.content_part.added,response.output_text.delta,response.output_text.done,response.content_part.done,response.output_item.done,response.completed"
			if strings.Join(types, ",") != want || assembled.String() != "hello" {
				t.Fatalf("incomplete or duplicated lifecycle: %v text=%q", types, assembled.String())
			}
		})
	}
}

func TestBPSExecutorReasoningSuffixAndFinalBarrier(t *testing.T) {
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient("bps-reasoning-test", "bps", []*registry.ModelInfo{{ID: "configured-model", UserDefined: true, Type: "bps"}})
	defer modelRegistry.UnregisterClient("bps-reasoning-test")
	for _, format := range []sdktranslator.Format{sdktranslator.FormatOpenAIResponse, sdktranslator.FormatOpenAI} {
		for _, tc := range []struct {
			name, model, bodyEffort, override, want string
			filter                                  bool
		}{
			{name: "suffix", model: "configured-model(high)", want: "high"},
			{name: "suffix-over-body", model: "configured-model(high)", bodyEffort: "low", want: "high"},
			{name: "body", model: "configured-model", bodyEffort: "high", want: "high"},
			{name: "payload-override", model: "configured-model(high)", bodyEffort: "low", override: "low", want: "low"},
			{name: "payload-filter", model: "configured-model(high)", filter: true},
		} {
			t.Run(format.String()+"/"+tc.name, func(t *testing.T) {
				cfg := &config.Config{}
				if tc.override != "" {
					cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "configured-model"}}, Params: map[string]any{"reasoning_effort": tc.override}}}
				}
				if tc.filter {
					cfg.Payload.Filter = []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "configured-model"}}, Params: []string{"reasoning_effort"}}}
				}
				request := ojson.NewDocString(`{"input":"hi"}`)
				if format == sdktranslator.FormatOpenAI {
					request = ojson.NewDocString(`{"messages":[{"role":"user","content":"hi"}]}`)
				}
				_ = request.Set("model", tc.model)
				if tc.bodyEffort != "" {
					path := "reasoning.effort"
					if format == sdktranslator.FormatOpenAI {
						path = "reasoning_effort"
					}
					_ = request.Set(path, tc.bodyEffort)
				}
				transport := bpsTestTransport(func(r *http.Request) (*http.Response, error) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					root := ojson.ParseBytes(body)
					if root.Get("reasoning_effort").String() != tc.want || root.Get("reasoning").Exists() || root.Get("model").String() != "configured-model" {
						t.Fatalf("suffix/body/payload precedence changed: want effort=%q got %s", tc.want, body)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + bpsTestTerminal + "\n\n"))}, nil
				})
				ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
				auth := &coreauth.Auth{Provider: "bps", Metadata: map[string]any{"access_token": "test", "account_id": "account"}}
				payload := request.Bytes()
				_, err := NewBPSExecutor(cfg).Execute(ctx, auth, coreexecutor.Request{Model: tc.model, Payload: payload}, coreexecutor.Options{SourceFormat: format, OriginalRequest: payload})
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
