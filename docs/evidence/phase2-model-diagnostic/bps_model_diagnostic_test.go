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
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type bpsDiagnosticTransport func(*http.Request) (*http.Response, error)

func (f bpsDiagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func (f bpsDiagnosticTransport) RoundTripperFor(*coreauth.Auth) http.RoundTripper { return f }

func TestPhase2BPSExactPublicRequestDiagnostic(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-5.6-sol"} {
		for _, withLimit := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/token-limit=%v", model, withLimit), func(t *testing.T) {
				cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"diagnostic-key"}}}
				cfg.BPS.Models = []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-5.6-sol"}
				server := newTestServerWithConfig(t, cfg)
				manager := server.handlers.AuthManager
				manager.SetConfig(cfg)
				manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
				auth := &coreauth.Auth{ID: "bps-diagnostic", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"type": "bps", "access_token": "synthetic-access", "account_id": "synthetic-account", "concurrency_limit": 3}}
				if _, err := manager.Register(context.Background(), auth); err != nil { t.Fatal(err) }
				models := make([]*registry.ModelInfo, 0, 3)
				for _, id := range cfg.BPS.Models { models = append(models, &registry.ModelInfo{ID: id, Object: "model", OwnedBy: "bps", Type: "bps", DisplayName: id, UserDefined: true}) }
				registry.GetGlobalRegistry().RegisterClient(auth.ID, "bps", models)
				defer registry.GetGlobalRegistry().UnregisterClient(auth.ID)
				calls := 0
				transport := bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(r.Body); if err != nil { t.Fatal(err) }
					root := ojson.ParseBytes(body)
					if root.Get("model").String() != model || root.Get("reasoning_effort").String() != "low" || !root.Get("stream").Bool() || root.Get("input.1.content.0.text").String() != "Reply with OK." { t.Fatalf("unexpected synthetic request: %s", body) }
					terminal := `{"type":"response.completed","response":{"id":"resp_diagnostic","object":"response","created_at":1,"status":"completed","model":"`+model+`","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: "+terminal+"\n\n"))}, nil
				})
				manager.SetRoundTripperProvider(transport)
				payload := `{"model":"`+model+`","input":"Reply with OK.","reasoning":{"effort":"low"},"stream":false`
				if withLimit { payload += `,"max_output_tokens":128` }; payload += `}`
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(payload))
				req.Header.Set("Authorization", "Bearer diagnostic-key")
				req.Header.Set("Content-Type", "application/json")
				req = req.WithContext(context.WithValue(req.Context(), "cliproxy.roundtripper", http.RoundTripper(transport)))
				rr := httptest.NewRecorder(); server.engine.ServeHTTP(rr, req)
				if withLimit {
					if rr.Code != 500 || calls != 0 || !strings.Contains(rr.Body.String(), "provider_not_found") { t.Fatalf("exact-input result: status=%d calls=%d body=%s", rr.Code, calls, rr.Body.String()) }
				} else {
					if rr.Code != 200 || calls != 1 || ojson.GetBytes(rr.Body.Bytes(), "status").String() != "completed" || ojson.GetBytes(rr.Body.Bytes(), "output.0.content.0.text").String() != "OK" { t.Fatalf("control result: status=%d calls=%d body=%s", rr.Code, calls, rr.Body.String()) }
				}
				t.Logf("model=%s with_max_output_tokens=%v http=%d fake_roundtripper_calls=%d body=%s", model, withLimit, rr.Code, calls, rr.Body.String())
			})
		}
	}
}
