package pluginhost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestExecutorAdapterChatToResponsesRoundTrip(t *testing.T) {
	var payload []byte
	adapter := newCurrentExecutorAdapterForTest(New(), "chat-responses", &fakeExecutor{
		identifier: "cpa-bps",
		execute: func(_ context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
			payload = append([]byte(nil), req.Payload...)
			return pluginapi.ExecutorResponse{Payload: []byte(`{"id":"resp_fixture","object":"response","status":"completed","model":"fixture-model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello back"}]}]}`)}, nil
		},
	}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
	adapter.provider = "cpa-bps"
	response, err := adapter.Execute(context.Background(), nil, coreexecutor.Request{
		Model: "fixture-model", Format: sdktranslator.FormatOpenAI,
		Payload: []byte(`{"model":"fixture-model","messages":[{"role":"user","content":"hello"}]}`),
	}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, ResponseFormat: sdktranslator.FormatOpenAI})
	if err != nil {
		t.Fatalf("Chat to Responses Execute: %v", err)
	}
	if got := ojson.GetBytesNoCopy(payload, "input.0.content.0.text").String(); got != "hello" {
		t.Fatalf("Responses input = %s", payload)
	}
	if got := ojson.GetBytesNoCopy(response.Payload, "choices.0.message.content").String(); got != "hello back" {
		t.Fatalf("Chat output = %s", response.Payload)
	}
}

func TestExecutorAdapterResponsesBridgePayloadAndOriginal(t *testing.T) {
	for _, original := range [][]byte{nil, []byte(`{"messages":[{"role":"user","content":"stale"}],"tools":[{"type":"function","function":{"name":"not_authorized"}}]}`)} {
		host := New()
		host.runtimeConfig = &config.Config{Payload: config.PayloadConfig{
			Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "fixture-model", Protocol: "openai-response", FromProtocol: "openai"}}, Params: map[string]any{"temperature": 0.75, "max_output_tokens": 17}}},
			Filter:   []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "fixture-model", Protocol: "openai-response"}}, Params: []string{"tools", "tool_choice"}}},
		}}
		var got []byte
		adapter := newCurrentExecutorAdapterForTest(host, "bridge-payload", &fakeExecutor{identifier: "cpa-bps", execute: func(_ context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
			got = append([]byte(nil), req.Payload...)
			return pluginapi.ExecutorResponse{Payload: []byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)}, nil
		}}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
		_, err := adapter.Execute(context.Background(), nil, coreexecutor.Request{Model: "fixture-model", Payload: []byte(`{"messages":[{"role":"user","content":"current"}],"tools":[{"type":"function","function":{"name":"allowed","parameters":{"type":"object"}}}],"tool_choice":"auto","temperature":0.2,"max_tokens":8}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, OriginalRequest: original})
		if err != nil {
			t.Fatal(err)
		}
		if ojson.GetBytesNoCopy(got, "tools").Exists() || ojson.GetBytesNoCopy(got, "tool_choice").Exists() || strings.Contains(string(got), "not_authorized") || ojson.GetBytesNoCopy(got, "temperature").Float() != 0.75 || ojson.GetBytesNoCopy(got, "max_output_tokens").Int() != 17 || ojson.GetBytesNoCopy(got, "input.0.content.0.text").String() != "current" {
			t.Fatal(string(got))
		}
	}
}

func TestExecutorAdapterResponsesBridgeSSEAndUsageWithoutOriginal(t *testing.T) {
	terminal := `{"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`
	wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\",\"output_index\":0,\"content_index\":0}\n\n" + "data: " + terminal + "\n\ndata: " + terminal + "\n\ndata: [DONE]\n\n"
	adapter := newCurrentExecutorAdapterForTest(New(), "bridge-framed", &fakeExecutor{identifier: "cpa-bps", executeStream: func(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
		chunks := make(chan pluginapi.ExecutorStreamChunk, len(wire))
		for i := 0; i < len(wire); i += 7 {
			end := i + 7
			if end > len(wire) {
				end = len(wire)
			}
			chunks <- pluginapi.ExecutorStreamChunk{Payload: []byte(wire[i:end])}
		}
		close(chunks)
		return pluginapi.ExecutorStreamResponse{Chunks: chunks}, nil
	}}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
	result, err := adapter.ExecuteStream(context.Background(), nil, coreexecutor.Request{Model: "fixture-model", Payload: []byte(`{"messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	terminals, usage := 0, 0
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		text.WriteString(ojson.GetBytesNoCopy(chunk.Payload, "choices.0.delta.content").String())
		if ojson.GetBytesNoCopy(chunk.Payload, "choices.0.finish_reason").String() == "stop" {
			terminals++
		}
		if ojson.GetBytesNoCopy(chunk.Payload, "usage.total_tokens").Int() == 4 {
			usage++
		}
	}
	if text.String() != "hello" || terminals != 1 || usage != 1 {
		t.Fatalf("text=%q terminals=%d usage=%d", text.String(), terminals, usage)
	}
}

func TestExecutorAdapterResponsesBridgeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter := newCurrentExecutorAdapterForTest(New(), "bridge-cancel", &fakeExecutor{identifier: "cpa-bps", executeStream: func(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
		return pluginapi.ExecutorStreamResponse{Chunks: make(chan pluginapi.ExecutorStreamChunk)}, nil
	}}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
	result, err := adapter.ExecuteStream(ctx, nil, coreexecutor.Request{Model: "fixture-model", Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case chunk, ok := <-result.Chunks:
		if ok && chunk.Err == nil {
			t.Fatalf("cancelled stream emitted success: %s", chunk.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled stream stayed open")
	}
}

func TestExecutorAdapterResponsesAndCodexSameFormatUnchanged(t *testing.T) {
	for _, format := range []sdktranslator.Format{sdktranslator.FormatOpenAIResponse, sdktranslator.FormatCodex} {
		t.Run(format.String(), func(t *testing.T) {
			raw := []byte(`{"model":"fixture-model","input":"already responses","store":true}`)
			response := []byte(`{"id":"native","object":"response","status":"completed","output":[]}`)
			var got []byte
			adapter := newCurrentExecutorAdapterForTest(New(), "bridge-same", &fakeExecutor{identifier: "cpa-bps", execute: func(_ context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
				got = append([]byte(nil), req.Payload...)
				return pluginapi.ExecutorResponse{Payload: response}, nil
			}}, []sdktranslator.Format{format}, []sdktranslator.Format{format})
			result, err := adapter.Execute(context.Background(), nil, coreexecutor.Request{Model: "fixture-model", Format: format, Payload: raw}, coreexecutor.Options{SourceFormat: format, ResponseFormat: format})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(raw) || string(result.Payload) != string(response) {
				t.Fatalf("same format changed request=%s response=%s", got, result.Payload)
			}
		})
	}
}

func TestExecutorAdapterResponsesBridgeUpstreamErrorDetails(t *testing.T) {
	for _, message := range []string{"basispoints returned an unsupported native tool; no tool was executed", "basispoints response violates the tool call count limit"} {
		body := ojson.NewDocString("{\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"output\":[]}}")
		_ = body.Set("response.error.code", "basispoints_protocol_error")
		_ = body.Set("response.error.message", message)
		adapter := newCurrentExecutorAdapterForTest(New(), "bridge-error-details", &fakeExecutor{identifier: "cpa-bps",
			execute: func(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
				return pluginapi.ExecutorResponse{Payload: body.Bytes()}, nil
			},
			executeStream: func(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
				chunks := make(chan pluginapi.ExecutorStreamChunk, 1)
				chunks <- pluginapi.ExecutorStreamChunk{Payload: body.Bytes()}
				close(chunks)
				return pluginapi.ExecutorStreamResponse{Chunks: chunks}, nil
			},
		}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
		req := coreexecutor.Request{Model: "fixture-model", Payload: []byte("{\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}")}
		for _, stream := range []bool{false, true} {
			opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: stream}
			var resultErr error
			if stream {
				result, err := adapter.ExecuteStream(context.Background(), nil, req, opts)
				if err != nil {
					t.Fatal(err)
				}
				for chunk := range result.Chunks {
					if len(chunk.Payload) > 0 {
						t.Fatal("failed stream emitted success")
					}
					if chunk.Err != nil {
						if resultErr != nil {
							t.Fatal("duplicate error")
						}
						resultErr = chunk.Err
					}
				}
			} else {
				_, resultErr = adapter.Execute(context.Background(), nil, req, opts)
			}
			if resultErr == nil {
				t.Fatal("missing executor error")
			}
			if ojson.Get(resultErr.Error(), "error.code").String() != "basispoints_protocol_error" || ojson.Get(resultErr.Error(), "error.message").String() != message {
				t.Fatalf("stream=%v error=%v", stream, resultErr)
			}
		}
	}
}

func TestExecutorAdapterResponsesBridgeFailures(t *testing.T) {
	for _, payload := range []string{
		`{"type":"response.failed","response":{"status":"failed","output":[]}}`,
		`{"object":"response","status":"cancelled","output":[]}`,
		`{"object":"response","status":"incomplete","incomplete_details":{"reason":"unknown"},"output":[]}`,
	} {
		t.Run(ojson.Get(payload, "response.status").String()+ojson.Get(payload, "status").String(), func(t *testing.T) {
			adapter := newCurrentExecutorAdapterForTest(New(), "bridge-error", &fakeExecutor{
				identifier: "cpa-bps", execute: func(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
					return pluginapi.ExecutorResponse{Payload: []byte(payload)}, nil
				},
			}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
			_, err := adapter.Execute(context.Background(), nil, coreexecutor.Request{Model: "fixture-model", Payload: []byte(`{"messages":[{"role":"user","content":"hello"}]}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
			if err == nil {
				t.Fatal("upstream failure returned successful executor response")
			}
		})
	}
}

func TestExecutorAdapterResponsesBridgeStreamLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		events     []string
		wantErrors int
	}{
		{"empty", nil, 1},
		{"failed_before_output", []string{`{"type":"response.failed","response":{"status":"failed"}}`}, 1},
		{"failed_after_output", []string{`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hello"}`, `{"type":"response.failed","response":{"status":"failed"}}`}, 1},
		{"eof_without_terminal", []string{`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hello"}`}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := newCurrentExecutorAdapterForTest(New(), "bridge-stream", &fakeExecutor{identifier: "cpa-bps", executeStream: func(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
				chunks := make(chan pluginapi.ExecutorStreamChunk, len(tc.events))
				for _, event := range tc.events {
					chunks <- pluginapi.ExecutorStreamChunk{Payload: []byte(event)}
				}
				close(chunks)
				return pluginapi.ExecutorStreamResponse{Chunks: chunks}, nil
			}}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
			result, err := adapter.ExecuteStream(context.Background(), nil, coreexecutor.Request{Model: "fixture-model", Payload: []byte(`{"messages":[{"role":"user","content":"hello"}],"stream":true}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: true})
			if err != nil {
				t.Fatal(err)
			}
			errors, terminals := 0, 0
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					errors++
				}
				if v := ojson.GetBytesNoCopy(chunk.Payload, "choices.0.finish_reason"); v.Exists() && v.Type != ojson.Null {
					terminals++
				}
			}
			if errors != tc.wantErrors || terminals != 0 {
				t.Fatalf("errors=%d terminals=%d", errors, terminals)
			}
		})
	}
}

func TestExecutorAdapterResponsesBridgeStreamCompletionOwner(t *testing.T) {
	for _, tc := range []struct {
		name        string
		event       string
		keepOpen    bool
		wantFailure bool
		withoutAuth bool
	}{
		{"failure_cancels_open_source", `{"type":"response.failed","response":{"status":"failed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`, true, true, false},
		{"failure_without_reporter_cancels_source", `{"type":"response.failed","response":{"status":"failed"}}`, true, true, true},
		{"eof_translation_failure_is_failed_usage", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","name":"outside","call_id":"c","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`, false, true, false},
		{"successful_cleanup_is_not_failure", `{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			provider := "bridge-owner-" + tc.name
			capture := newTestUsageCapturePlugin(provider)
			registerTestUsagePlugin(t, provider, capture)
			stopped := make(chan struct{})
			adapter := newCurrentExecutorAdapterForTest(New(), provider, &fakeExecutor{identifier: provider, executeStream: func(pluginCtx context.Context, _ pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
				chunks := make(chan pluginapi.ExecutorStreamChunk, 1)
				chunks <- pluginapi.ExecutorStreamChunk{Payload: []byte(tc.event)}
				if !tc.keepOpen {
					close(chunks)
				}
				go func() {
					<-pluginCtx.Done()
					if tc.keepOpen {
						close(chunks)
					}
					close(stopped)
				}()
				return pluginapi.ExecutorStreamResponse{Chunks: chunks}, nil
			}}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
			adapter.provider = provider
			var auth *coreauth.Auth
			if !tc.withoutAuth {
				auth = &coreauth.Auth{ID: provider, Provider: provider}
			}
			result, err := adapter.ExecuteStream(ctx, auth, coreexecutor.Request{Model: "fixture-model", Payload: []byte(`{"messages":[{"role":"user","content":"hello"}],"stream":true}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: true})
			if err != nil {
				t.Fatal(err)
			}
			errors := 0
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					errors++
				}
			}
			if (errors == 1) != tc.wantFailure || errors > 1 {
				t.Errorf("errors=%d wantFailure=%v", errors, tc.wantFailure)
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Error("completed translation did not cancel plugin context")
			}
			if !tc.withoutAuth {
				record := capture.waitRecord(t, time.Second)
				if record.Failed != tc.wantFailure || record.Detail.TotalTokens != 2 {
					t.Errorf("usage failed=%v total=%d wantFailure=%v", record.Failed, record.Detail.TotalTokens, tc.wantFailure)
				}
			}
			if ctx.Err() != nil {
				t.Fatal("stream cleanup cancelled caller context")
			}
		})
	}
}
