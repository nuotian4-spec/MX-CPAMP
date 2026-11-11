package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestBPSToolsPublicHTTPRoundTrip(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", path, streaming), func(t *testing.T) {
				cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"tool-test-key"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				cfg.BPS.Models = []string{"gpt-6-luna"}
				server := newTestServerWithConfig(t, cfg)
				manager := server.handlers.AuthManager
				manager.SetConfig(cfg)
				manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
				for _, id := range []string{"tool-original-a", "tool-original-b"} {
					_, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"type": "bps", "access_token": id, "account_id": id, "sub": "user-" + id}})
					if err != nil {
						t.Fatal(err)
					}
					registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}})
					defer registry.GetGlobalRegistry().UnregisterClient(id)
				}
				calls, firstAccount := 0, ""
				transport := bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_tool_test","object":"response","status":"completed","model":"gpt-6-luna","output":[]}}`)
					if calls == 1 {
						firstAccount = r.Header.Get("x-openai-account-id")
						arguments := ojson.NewDocString(`{"summary":"Call client tool","extended_summary":"","destructive":false,"references":[]}`)
						_ = arguments.Set("code", `{"name":"get_weather","arguments":{"city":"Tokyo"}}`)
						_ = terminal.Set("response.output", []any{map[string]any{"id": "fc_native", "type": "function_call", "call_id": "call_native", "name": "run_officejs", "arguments": arguments.String(), "status": "completed"}})
					} else {
						if r.Header.Get("x-openai-account-id") != firstAccount {
							t.Fatal("tool continuation switched credentials")
						}
						if strings.Contains(string(body), bpsbinding.Prefix) || !strings.Contains(string(body), "call_native") || !strings.Contains(string(body), "sunny") {
							t.Fatalf("native history missing: %s", body)
						}
						_ = terminal.Set("response.output", []any{map[string]any{"id": "msg_answer", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "Weather is sunny"}}}})
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
				})
				manager.SetRoundTripperProvider(transport)
				doc := ojson.NewDocString(`{"model":"gpt-6-luna","input":[{"role":"user","content":"Check weather"}],"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]}`)
				chat := path == "/v1/chat/completions"
				if chat {
					doc = ojson.NewDocString(`{"model":"gpt-6-luna","messages":[{"role":"user","content":"Check weather"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]}`)
				}
				_ = doc.Set("stream", streaming)
				request := func() *httptest.ResponseRecorder {
					r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(doc.String()))
					r.Header.Set("Authorization", "Bearer tool-test-key")
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					server.engine.ServeHTTP(w, r)
					return w
				}
				first := request()
				if first.Code != 200 {
					t.Fatalf("first turn HTTP %d: %s", first.Code, first.Body.String())
				}
				id := ""
				if !streaming {
					id = ojson.GetBytes(first.Body.Bytes(), "output.0.call_id").String()
					if chat {
						id = ojson.GetBytes(first.Body.Bytes(), "choices.0.message.tool_calls.0.id").String()
					}
				} else {
					for _, line := range strings.Split(first.Body.String(), "\n") {
						root := ojson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
						candidate := root.Get("item.call_id").String()
						if chat {
							candidate = root.Get("choices.0.delta.tool_calls.0.id").String()
						}
						if candidate != "" {
							id = candidate
						}
					}
				}
				if !bpsbinding.IsCallID(id) {
					t.Fatalf("public tool identity missing: %s", first.Body.String())
				}
				if !chat {
					_ = doc.Set("input", []any{map[string]any{"role": "user", "content": "Check weather"}, map[string]any{"type": "function_call", "call_id": id, "name": "get_weather", "arguments": `{"city":"Tokyo"}`}, map[string]any{"type": "function_call_output", "call_id": id, "output": "sunny"}})
				} else {
					_ = doc.Set("messages", []any{map[string]any{"role": "user", "content": "Check weather"}, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "get_weather", "arguments": `{"city":"Tokyo"}`}}}}, map[string]any{"role": "tool", "tool_call_id": id, "content": "sunny"}})
				}
				second := request()
				if second.Code != 200 || calls != 2 || !strings.Contains(second.Body.String(), "Weather is sunny") {
					t.Fatalf("second turn HTTP %d, calls=%d: %s", second.Code, calls, second.Body.String())
				}
				t.Logf("path=%s stream=%v first_http=%d second_http=%d upstream_calls=%d same_credential=true", path, streaming, first.Code, second.Code, calls)
			})
		}
	}
}
