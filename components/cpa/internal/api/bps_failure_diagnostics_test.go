package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestBPSFailureDiagnosticsPublicProtocols(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct {
				name, stage, code string
				upstream, inband  int
			}{
				{"tool-invalid", "tool_validation", "tool_validation_failed", 200, 0},
				{"identity-missing", "binding_identity", "identity_incomplete_or_changed", 200, 0},
				{"storage-failed", "binding_storage", "storage_unavailable", 200, 0},
				{"inband-403", "upstream_inband", "upstream_failed", 200, 403},
				{"inband-429", "upstream_inband", "upstream_failed", 200, 429},
				{"inband-unknown", "upstream_inband", "unknown", 200, 0},
				{"http-429", "upstream_http", "http_rejected", 429, 0},
				{"http-503", "upstream_http", "http_rejected", 503, 0},
			} {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", path, stream, tc.name), func(t *testing.T) {
					cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"synthetic-key"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
					cfg.BPS.Models = []string{"gpt-6-luna"}
					if tc.name == "storage-failed" {
						cfg.BPSProtocol.StateDir = filepath.Join(cfg.BPSProtocol.StateDir, "occupied")
						if os.WriteFile(cfg.BPSProtocol.StateDir, []byte("occupied"), 0600) != nil {
							t.Fatal("fixture setup failed")
						}
					}
					server := newTestServerWithConfig(t, cfg)
					manager := server.handlers.AuthManager
					manager.SetConfig(cfg)
					manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
					auth := &coreauth.Auth{ID: "failure-diagnostic", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "PRIVATE_TOKEN_MARKER", "account_id": "synthetic-account", "sub": "PRIVATE_SUBJECT_MARKER"}}
					if tc.name == "identity-missing" {
						delete(auth.Metadata, "sub")
					}
					if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), auth); err != nil {
						t.Fatal("fixture registration failed")
					}
					registry.GetGlobalRegistry().RegisterClient(auth.ID, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}})
					defer registry.GetGlobalRegistry().UnregisterClient(auth.ID)
					calls := 0
					transport := bpsDiagnosticTransport(func(*http.Request) (*http.Response, error) {
						calls++
						args := ojson.NewDocString(`{"summary":"Call declared tool","extended_summary":"","destructive":false,"references":[]}`)
						_ = args.Set("code", `{"name":"ping","arguments":{}}`)
						if tc.name == "tool-invalid" {
							_ = args.Set("code", `{"name":"PRIVATE_TOOL_MARKER","arguments":{"secret":"PRIVATE_ARGUMENT_MARKER"}}`)
						}
						terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_diagnostic","object":"response","status":"completed","model":"gpt-6-luna","output":[]}}`)
						_ = terminal.Set("response.output", []any{map[string]any{"type": "function_call", "id": "fc_diagnostic", "call_id": "call_diagnostic", "name": "run_officejs", "arguments": args.String(), "status": "completed"}})
						if strings.HasPrefix(tc.name, "inband-") {
							terminal = ojson.NewDocString(`{"type":"response.failed","response":{"id":"resp_diagnostic","status":"failed","error":{"message":"PRIVATE_BODY_MARKER","code":"PRIVATE_CODE_MARKER"}}}`)
							if tc.inband > 0 {
								_ = terminal.Set("response.error.status", tc.inband)
								_ = terminal.Set("response.error.code", map[int]string{403: "permission_denied", 429: "rate_limit_exceeded"}[tc.inband])
							}
						}
						prefix := ""
						if stream && tc.upstream == 200 {
							prefix = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_diagnostic\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_diagnostic\",\"item_id\":\"msg_probe\",\"output_index\":0,\"content_index\":0,\"delta\":\"probe\"}\n\n"
						}
						return &http.Response{StatusCode: tc.upstream, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(prefix + "data: " + terminal.String() + "\n\n"))}, nil
					})
					manager.SetRoundTripperProvider(transport)
					payload := ojson.NewDocString(`{"model":"gpt-6-luna","reasoning":{"effort":"low"},"input":"Call ping once.","tools":[{"type":"function","name":"ping","parameters":{"type":"object","properties":{},"additionalProperties":false}}],"tool_choice":"auto"}`)
					if path == "/v1/chat/completions" {
						payload = ojson.NewDocString(`{"model":"gpt-6-luna","messages":[{"role":"user","content":"Call ping once."}],"tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object","properties":{},"additionalProperties":false}}}],"tool_choice":"auto"}`)
					}
					_ = payload.Set("stream", stream)
					req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload.Bytes()))
					req.Header.Set("Authorization", "Bearer synthetic-key")
					req.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					server.engine.ServeHTTP(response, req)
					wantStatus := 502
					if tc.upstream != 200 {
						wantStatus = tc.upstream
					} else if stream {
						wantStatus = 200
					}
					if response.Code != wantStatus || calls != 1 {
						t.Fatalf("HTTP/attempt contract changed: status=%d calls=%d", response.Code, calls)
					}
					if strings.Contains(response.Body.String(), "PRIVATE_") {
						t.Fatal("private marker escaped into public response")
					}
					var publicError ojson.Result
					if response.Code != 200 {
						publicError = ojson.GetBytes(response.Body.Bytes(), "error")
					} else {
						for _, line := range strings.Split(response.Body.String(), "\n") {
							root := ojson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
							if candidate := root.Get("error"); candidate.IsObject() {
								publicError = candidate
							}
							if candidate := root.Get("response.error"); candidate.IsObject() {
								publicError = candidate
							}
						}
					}
					wantCode, wantType := "internal_server_error", "server_error"
					if tc.upstream == 429 {
						wantCode, wantType = "rate_limit_exceeded", "rate_limit_error"
					}
					if publicError.Get("code").String() != wantCode || publicError.Get("type").String() != wantType {
						t.Fatal("public error code/type contract changed")
					}
					diag := publicError.Get("diagnostics")
					if !diag.IsObject() {
						t.Fatal("diagnostics is missing or not an object")
					}
					if diag.Get("stage").String() != tc.stage || diag.Get("code").String() != tc.code || diag.Get("upstream_http_status").Int() != int64(tc.upstream) || diag.Get("inband_status").Int() != int64(tc.inband) {
						t.Fatal("diagnostic classification or status provenance changed")
					}
					if len(diag.Map()) != 4 {
						t.Fatal("unexpected diagnostic fields")
					}
					if tc.inband == 0 && diag.Get("inband_status").Raw != "null" {
						t.Fatal("unknown in-band status must be null")
					}
					t.Logf("stage=%s code=%s public_http=%d upstream_http=%d inband_status=%d calls=%d", tc.stage, tc.code, response.Code, tc.upstream, tc.inband, calls)
				})
			}
		}
	}
}
