package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

func TestBPSExactPing502Diagnostic(t *testing.T) {
	raw, err := os.ReadFile(os.Getenv("BPS_DIAG_REQUEST"))
	if err != nil {
		t.Fatal("exact request fixture unavailable")
	}
	for _, scenario := range []string{"valid_envelope_sse", "valid_direct_sse", "valid_envelope_json", "invalid_transport", "invalid_schema", "missing_call_id", "upstream_inband_failure", "incomplete_terminal", "malformed_sse", "truncated_sse", "missing_subject", "binding_storage_failure", "upstream_http_502"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"synthetic-key"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			cfg.BPS.Models = []string{"gpt-6-luna"}
			if scenario == "binding_storage_failure" {
				cfg.BPSProtocol.StateDir = filepath.Join(cfg.BPSProtocol.StateDir, "occupied")
				if os.WriteFile(cfg.BPSProtocol.StateDir, []byte("occupied"), 0600) != nil {
					t.Fatal("fixture setup failed")
				}
			}
			server := newTestServerWithConfig(t, cfg)
			manager := server.handlers.AuthManager
			manager.SetConfig(cfg)
			manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
			auth := &coreauth.Auth{ID: "diagnostic", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic-token", "account_id": "synthetic-account", "sub": "synthetic-subject"}}
			if scenario == "missing_subject" {
				delete(auth.Metadata, "sub")
			}
			if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), auth); err != nil {
				t.Fatal("fixture registration failed")
			}
			registry.GetGlobalRegistry().RegisterClient(auth.ID, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}})
			defer registry.GetGlobalRegistry().UnregisterClient(auth.ID)
			calls := 0
			transport := bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal("synthetic body read failed")
				}
				if !bytes.Contains(body, []byte("ping")) || ojson.GetBytes(body, "model").String() != "gpt-6-luna" {
					t.Fatal("request protocol did not preserve declared tool/model")
				}
				args := ojson.NewDocString(`{"summary":"Call declared tool","extended_summary":"","destructive":false,"references":[]}`)
				_ = args.Set("code", `{"name":"ping","arguments":{}}`)
				item := map[string]any{"type": "function_call", "name": "run_officejs", "id": "fc_synthetic", "call_id": "call_synthetic", "arguments": args.String(), "status": "completed"}
				switch scenario {
				case "valid_direct_sse":
					item["name"] = "ping"
					item["arguments"] = "{}"
				case "invalid_transport":
					item["arguments"] = "not-json"
				case "invalid_schema":
					_ = args.Set("code", `{"name":"ping","arguments":{"unexpected":true}}`)
					item["arguments"] = args.String()
				case "missing_call_id":
					delete(item, "call_id")
				}
				terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_synthetic","object":"response","status":"completed","model":"gpt-6-luna","output":[]}}`)
				_ = terminal.Set("response.output", []any{item})
				wire := "data: " + terminal.String() + "\n\n"
				contentType := "text/event-stream"
				status := 200
				switch scenario {
				case "valid_envelope_json":
					wire = terminal.Get("response").Raw
					contentType = "application/json"
				case "upstream_inband_failure":
					wire = "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_synthetic\",\"status\":\"failed\",\"error\":{\"code\":\"permission_denied\",\"status\":403}}}\n\n"
				case "incomplete_terminal":
					wire = "data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_synthetic\",\"status\":\"incomplete\"}}\n\n"
				case "malformed_sse":
					wire = "data: not-json\n\n"
				case "truncated_sse":
					wire = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_synthetic\"}}\n\n"
				case "upstream_http_502":
					status = 502
					wire = "{}"
					contentType = "application/json"
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			})
			manager.SetRoundTripperProvider(transport)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
			req.Header.Set("Authorization", "Bearer synthetic-key")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.engine.ServeHTTP(response, req)
			expected := 502
			if strings.HasPrefix(scenario, "valid_") {
				expected = 200
			}
			if response.Code != expected || calls != 1 {
				t.Fatalf("unexpected structural result: http=%d expected=%d synthetic_upstream_calls=%d", response.Code, expected, calls)
			}
			public := ojson.ParseBytes(response.Body.Bytes())
			code, kind := public.Get("error.code").String(), public.Get("error.type").String()
			if expected == 502 && (code != "internal_server_error" || kind != "server_error") {
				t.Fatal("public error shape differs from observed failure")
			}
			toolValid := public.Get("output.0.name").String() == "ping" && public.Get("output.0.arguments").String() == "{}"
			if expected == 200 && !toolValid {
				t.Fatal("empty-schema tool translation failed")
			}
			category := "unknown"
			if expected == 502 {
				classify := exec.Command(os.Getenv("BPS_DIAG_NODE"), "--input-type=module", "-e", `import {readFileSync} from "node:fs"; import {pathToFileURL} from "node:url"; const {publicErrorDiagnostics}=await import(pathToFileURL(process.env.BPS_DIAG_DIAGNOSTICS)); process.stdout.write(JSON.stringify(publicErrorDiagnostics(JSON.parse(readFileSync(0,"utf8")))));`)
				classify.Stdin = bytes.NewReader(response.Body.Bytes())
				safe, err := classify.Output()
				if err != nil {
					t.Fatal("fixed diagnostic classifier failed")
				}
				category = ojson.GetBytes(safe, "public_error_category").String()
			}
			if expected == 200 {
				category = "success"
			}
			t.Logf("scenario=%s http=%d public_code=%s public_type=%s category=%s synthetic_upstream_calls=%d tool_valid=%v", scenario, response.Code, code, kind, category, calls, toolValid)
		})
	}
}
