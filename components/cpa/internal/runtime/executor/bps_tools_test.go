package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func bpsNativeToolTerminal(t *testing.T) string {
	t.Helper()
	arguments := ojson.NewDocString(`{"summary":"Call client tool","extended_summary":"","destructive":false,"references":[]}`)
	_ = arguments.Set("code", `{"name":"get_weather","arguments":{"city":"Tokyo"}}`)
	terminal := ojson.NewDocString(bpsTestTerminal)
	_ = terminal.Set("response.output", []any{map[string]any{"id": "fc_native", "type": "function_call", "call_id": "call_native", "name": "run_officejs", "arguments": arguments.String(), "status": "completed"}})
	return terminal.String()
}

func TestBPSToolsRoundTripProtocols(t *testing.T) {
	for _, format := range []sdktranslator.Format{sdktranslator.FormatOpenAIResponse, sdktranslator.FormatOpenAI} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", format, streaming), func(t *testing.T) {
				cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				auth := &coreauth.Auth{ID: "original", Provider: "bps", Metadata: map[string]any{"access_token": "token", "account_id": "workspace", "sub": "user"}}
				calls := 0
				transport := bpsTestTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), "get_weather") {
						t.Fatalf("tool catalog absent: %s", body)
					}
					terminal := bpsNativeToolTerminal(t)
					if calls == 2 {
						if strings.Contains(string(body), bpsbinding.Prefix) || !strings.Contains(string(body), "call_native") || !strings.Contains(string(body), "sunny") {
							t.Fatalf("native history was not restored: %s", body)
						}
						terminal = bpsTestTerminal
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal + "\n\n"))}, nil
				})
				ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
				doc := ojson.NewDocString(`{"model":"configured-model","input":[{"role":"user","content":"Check weather"}],"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]}`)
				if format == sdktranslator.FormatOpenAI {
					doc = ojson.NewDocString(`{"model":"configured-model","messages":[{"role":"user","content":"Check weather"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]}`)
				}
				run := func(payload []byte) (string, string) {
					opts := coreexecutor.Options{SourceFormat: format, OriginalRequest: payload, Metadata: map[string]any{coreexecutor.CallerScopeMetadataKey: "caller"}}
					req := coreexecutor.Request{Model: "configured-model", Payload: payload}
					// A new executor each turn exercises the persistent identity binding.
					exec := NewBPSExecutor(cfg)
					if !streaming {
						result, err := exec.Execute(ctx, auth, req, opts)
						if err != nil {
							t.Fatal(err)
						}
						id := ojson.GetBytes(result.Payload, "output.0.call_id").String()
						if format == sdktranslator.FormatOpenAI {
							id = ojson.GetBytes(result.Payload, "choices.0.message.tool_calls.0.id").String()
						}
						return id, string(result.Payload)
					}
					result, err := exec.ExecuteStream(ctx, auth, req, opts)
					if err != nil {
						t.Fatal(err)
					}
					id, output := "", ""
					for chunk := range result.Chunks {
						if chunk.Err != nil {
							t.Fatal(chunk.Err)
						}
						output += string(chunk.Payload)
						for _, line := range strings.Split(string(chunk.Payload), "\n") {
							line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
							root := ojson.Parse(line)
							candidate := root.Get("item.call_id").String()
							if format == sdktranslator.FormatOpenAI {
								candidate = root.Get("choices.0.delta.tool_calls.0.id").String()
							}
							if candidate != "" {
								id = candidate
							}
						}
					}
					return id, output
				}
				id, first := run(doc.Bytes())
				if !bpsbinding.IsCallID(id) || !strings.Contains(first, "get_weather") || strings.Contains(first, "run_officejs") {
					t.Fatalf("client tool missing or native tool leaked: %s", first)
				}
				if format == sdktranslator.FormatOpenAIResponse {
					_ = doc.Set("input", []any{map[string]any{"role": "user", "content": "Check weather"}, map[string]any{"type": "function_call", "call_id": id, "name": "get_weather", "arguments": `{"city":"Tokyo"}`}, map[string]any{"type": "function_call_output", "call_id": id, "output": "sunny"}})
				} else {
					_ = doc.Set("messages", []any{map[string]any{"role": "user", "content": "Check weather"}, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "get_weather", "arguments": `{"city":"Tokyo"}`}}}}, map[string]any{"role": "tool", "tool_call_id": id, "content": "sunny"}})
				}
				auth.Metadata["access_token"] = "refreshed-token"
				_, second := run(doc.Bytes())
				if calls != 2 || !strings.Contains(second, "hello") {
					t.Fatalf("round trip failed: calls=%d output=%s", calls, second)
				}
				raw, err := os.ReadFile(filepath.Join(cfg.BPSProtocol.StateDir, "bindings", id+".json"))
				if err != nil {
					t.Fatal(err)
				}
				for _, secret := range []string{"Tokyo", "sunny", "get_weather", "refreshed-token"} {
					if strings.Contains(string(raw), secret) {
						t.Fatalf("history or token persisted: %s", raw)
					}
				}
			})
		}
	}
}

func TestBPSFailedToolDoesNotRepairOrPublish(t *testing.T) {
	cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
	auth := &coreauth.Auth{ID: "original", Provider: "bps", Metadata: map[string]any{"access_token": "token", "account_id": "workspace", "sub": "user"}}
	calls := 0
	transport := bpsTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		terminal := strings.ReplaceAll(bpsNativeToolTerminal(t), "get_weather", "undeclared_tool")
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal + "\n\n"))}, nil
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
	payload := []byte(`{"input":"weather","tools":[{"type":"function","name":"get_weather","parameters":{"type":"object"}}]}`)
	result, err := NewBPSExecutor(cfg).ExecuteStream(ctx, auth, coreexecutor.Request{Model: "configured-model", Payload: payload}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload})
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for chunk := range result.Chunks {
		failed = failed || chunk.Err != nil
		if strings.Contains(string(chunk.Payload), "function_call") {
			t.Fatal("invalid tool was published")
		}
	}
	if !failed || calls != 1 {
		t.Fatalf("invalid tool result: failed=%v calls=%d", failed, calls)
	}
	entries, _ := os.ReadDir(filepath.Join(cfg.BPSProtocol.StateDir, "bindings"))
	if len(entries) != 0 {
		t.Fatal("invalid tool created a binding")
	}
}

func TestBPSImagesDisabledAfterPayloadRules(t *testing.T) {
	disabled := false
	cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{ImagesEnabled: &disabled}, Payload: config.PayloadConfig{Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "configured-model"}}, Params: map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "https://example.invalid/image.png"}}}}}}}}}
	calls := 0
	transport := bpsTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("unexpected upstream request")
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
	auth := &coreauth.Auth{Provider: "bps", Metadata: map[string]any{"access_token": "token", "account_id": "workspace"}}
	_, err := NewBPSExecutor(cfg).Execute(ctx, auth, coreexecutor.Request{Model: "configured-model", Payload: []byte(`{"input":"text only"}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err == nil || calls != 0 || !strings.Contains(err.Error(), "image input is disabled") {
		t.Fatalf("final payload image policy: calls=%d err=%v", calls, err)
	}
}
