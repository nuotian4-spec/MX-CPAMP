package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executionregistry"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestBPSDelegationPublicAndHome(t *testing.T) {
	for _, home := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, canceled := range []bool{false, true} {
				t.Run(fmt.Sprintf("home=%v/stream=%v/cancel=%v", home, stream, canceled), func(t *testing.T) {
					cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"delegation-key"}}, BPS: config.BPSConfig{Models: []string{"gpt-6-luna"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
					server := newTestServerWithConfig(t, cfg)
					manager := server.handlers.AuthManager
					manager.SetConfig(cfg)
					manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
					const id = "delegation-account"
					_, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"type": "bps", "access_token": "synthetic", "account_id": id, "sub": "synthetic-sub"}})
					if err != nil {
						t.Fatal(err)
					}
					registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}})
					defer registry.GetGlobalRegistry().UnregisterClient(id)
					var lifecycle *executionregistry.Registry
					if home {
						homeCfg := *cfg
						homeCfg.Home.Enabled = true
						manager.SetConfig(&homeCfg)
						lifecycle = executionregistry.New()
						manager.PublishHomeDispatch(&alphaHomeDispatcher{provider: "bps"}, lifecycle, 1)
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					posts := 0
					manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
						posts++
						body, _ := io.ReadAll(r.Body)
						if !strings.Contains(string(body), "HANDOFF_MARKER") || !strings.Contains(string(body), "thread handoff context") {
							t.Fatalf("handoff lost: %s", body)
						}
						if canceled {
							cancel()
							return nil, context.Canceled
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + `{"type":"response.completed","response":{"id":"resp_handoff","object":"response","status":"completed","model":"gpt-6-luna","output":[{"id":"msg_handoff","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"HANDOFF_ACCEPTED"}]}]}}` + "\n\n"))}, nil
					}))
					body := fmt.Sprintf(`{"model":"gpt-6-luna","stream":%v,"input":[{"role":"user","content":"BEFORE_MARKER"},{"type":"function_call_output","namespace":"codex_app","name":"create_thread","output":"<codex_delegation>HANDOFF_MARKER</codex_delegation>"},{"role":"user","content":"AFTER_MARKER"}]}`, stream)
					req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)).WithContext(ctx)
					req.Header.Set("Authorization", "Bearer delegation-key")
					req.Header.Set("Content-Type", "application/json")
					out := httptest.NewRecorder()
					server.engine.ServeHTTP(out, req)
					if posts != 1 {
						t.Fatalf("posts=%d status=%d body=%s", posts, out.Code, out.Body.String())
					}
					if !canceled && (out.Code != 200 || !strings.Contains(out.Body.String(), "HANDOFF_ACCEPTED")) {
						t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
					}
					if canceled && strings.Contains(out.Body.String(), "HANDOFF_ACCEPTED") {
						t.Fatal("cancellation became success")
					}
					if lifecycle != nil {
						wait, done := context.WithTimeout(context.Background(), time.Second)
						defer done()
						if err := lifecycle.WaitPending(wait); err != nil {
							t.Fatal(err)
						}
						if pending := lifecycle.FreezeInFlight(time.Now()).Executions; len(pending) != 0 {
							t.Fatalf("Home selection leaked: %+v", pending)
						}
						if err := lifecycle.Drain(wait); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestBPSDelegationPublicIdentityRejects(t *testing.T) {
	for _, mode := range []string{"nested_encrypted", "nested_call", "top_encrypted", "missing_before", "missing_after", "send_message_to_thread"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"delegation-key"}}, BPS: config.BPSConfig{Models: []string{"gpt-6-luna"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			server := newTestServerWithConfig(t, cfg)
			manager := server.handlers.AuthManager
			manager.SetConfig(cfg)
			manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
			id := "delegation-reject"
			_, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": id, "sub": "subject"}})
			if err != nil {
				t.Fatal(err)
			}
			registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}})
			defer registry.GetGlobalRegistry().UnregisterClient(id)
			notice := map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "create_thread", "output": "<codex_delegation>task</codex_delegation>"}
			input := []any{notice}
			missing := map[string]any{"type": "function_call_output", "call_id": "call_bps_00000000-0000-0000-0000-000000000000", "output": "real output"}
			switch mode {
			case "nested_encrypted":
				notice["content"] = []any{map[string]any{"type": "encrypted_content", "encrypted_content": "synthetic"}}
			case "nested_call":
				notice["content"] = []any{map[string]any{"type": "function_call", "call_id": "call_nested"}}
			case "top_encrypted":
				notice["encrypted_content"] = "synthetic"
			case "missing_before":
				input = []any{missing, notice}
			case "missing_after":
				input = append(input, missing)
			case "send_message_to_thread":
				notice["name"] = "send_message_to_thread"
			}
			doc := ojson.NewDocString(`{"model":"gpt-6-luna"}`)
			_ = doc.Set("input", input)
			posts := 0
			manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(*http.Request) (*http.Response, error) { posts++; return nil, fmt.Errorf("unexpected request") }))
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(doc.String()))
			req.Header.Set("Authorization", "Bearer delegation-key")
			req.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			server.engine.ServeHTTP(out, req)
			if posts != 0 || (out.Code != 400 && out.Code != 409) {
				t.Fatalf("status=%d posts=%d body=%s", out.Code, posts, out.Body.String())
			}
		})
	}
}
