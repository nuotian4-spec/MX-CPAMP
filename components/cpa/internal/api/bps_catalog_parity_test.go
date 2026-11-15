package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestBPSParityDynamicCatalogPublicHTTP(t *testing.T) {
	for _, kind := range []string{"additional_tools", "tool_search_output"} {
		t.Run(kind, func(t *testing.T) {
			cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"catalog-key"}}, BPS: config.BPSConfig{Models: []string{"gpt-6-luna"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			server := newTestServerWithConfig(t, cfg)
			manager := server.handlers.AuthManager
			manager.SetConfig(cfg)
			manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
			authID := "catalog-" + kind
			_, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: authID, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": authID, "sub": "synthetic-sub"}})
			if err != nil {
				t.Fatal(err)
			}
			registry.GetGlobalRegistry().RegisterClient(authID, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", UserDefined: true}})
			defer registry.GetGlobalRegistry().UnregisterClient(authID)
			posts := 0
			bad := false
			manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
				posts++
				wire, _ := io.ReadAll(r.Body)
				if r.Header.Get("chatgpt-account-id") != authID {
					t.Fatal("continuation switched identity")
				}
				terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_catalog","status":"completed","model":"gpt-6-luna","output":[]}}`)
				name := "echo"
				if bad {
					name = "missing"
				}
				if posts == 2 {
					if !strings.Contains(string(wire), "call_catalog") || !strings.Contains(string(wire), "tool result") {
						t.Fatalf("history=%s", wire)
					}
				} else {
					_ = terminal.Set("response.output", []any{map[string]any{"id": "fc_catalog", "type": "function_call", "namespace": "n", "name": name, "call_id": "call_catalog", "arguments": `{"v":2}`, "status": "completed"}})
					if bad {
						args := ojson.NewDocString(`{"summary":"Call client tool","extended_summary":"","destructive":false,"references":[]}`)
						_ = args.Set("code", `{"name":"n.missing","arguments":{"secret":"PRIVATE_ARGUMENT_MARKER"}}`)
						_ = terminal.Set("response.output", []any{map[string]any{"id": "fc_new", "type": "function_call", "name": "run_officejs", "call_id": "call_new", "arguments": args.String()}})
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
			}))
			catalog := map[string]any{"type": kind, "tools": []any{map[string]any{"type": "namespace", "name": "n", "tools": []any{map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "object", "properties": map[string]any{"v": map[string]any{"type": "string"}}}}}}}}
			explicit := map[string]any{"type": "namespace", "name": "n", "tools": []any{map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "object", "properties": map[string]any{"v": map[string]any{"type": "integer"}}, "required": []string{"v"}}}}}
			input := []any{catalog, map[string]any{"role": "user", "content": "run"}}
			doc := ojson.NewDocString(`{"model":"gpt-6-luna"}`)
			_ = doc.Set("tools", []any{explicit})
			_ = doc.Set("input", input)
			request := func(body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer catalog-key")
				w := httptest.NewRecorder()
				server.engine.ServeHTTP(w, r)
				return w
			}
			first := request(doc.String())
			if first.Code != 200 {
				t.Fatalf("first=%d %s", first.Code, first.Body.String())
			}
			call := ojson.GetBytes(first.Body.Bytes(), "output.0")
			if call.Get("namespace").String() != "n" || call.Get("name").String() != "echo" {
				t.Fatalf("call=%s", call.Raw)
			}
			input = append(input, call.Value(), map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "tool result"})
			_ = doc.Set("input", input)
			second := request(doc.String())
			if second.Code != 200 || posts != 2 {
				t.Fatalf("second=%d posts=%d %s", second.Code, posts, second.Body.String())
			}
			bad = true
			third := request(doc.String())
			if third.Code != 502 || posts != 3 || !strings.Contains(third.Body.String(), `"catalog_count":1`) || !strings.Contains(third.Body.String(), `"reason":"outside_catalog"`) || strings.Contains(third.Body.String(), "PRIVATE_") {
				t.Fatalf("unknown=%d posts=%d %s", third.Code, posts, third.Body.String())
			}
		})
	}
}
