package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type failoverHeaderTransport struct {
	bpsDiagnosticTransport
	selected func(*coreauth.Auth)
}

func (p failoverHeaderTransport) RoundTripperFor(auth *coreauth.Auth) http.RoundTripper {
	if p.selected != nil {
		p.selected(auth)
	}
	return p.bpsDiagnosticTransport
}

func TestBPSFailoverHeaderHTTP(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = bpsDiagnosticTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline fixture rejects unstubbed network")
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		for _, stream := range []bool{false, true} {
			for _, passthrough := range []bool{false, true} {
				for _, scenario := range []string{"failover", "first_success", "B_selected_without_POST", "model_mismatch"} {
					t.Run(fmt.Sprintf("%s/stream=%v/passthrough=%v/%s", path, stream, passthrough, scenario), func(t *testing.T) {
						cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"synthetic-key"}, PassthroughHeaders: passthrough}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
						cfg.BPS.Models = []string{"header-model"}
						server := newTestServerWithConfig(t, cfg)
						manager := server.handlers.AuthManager
						manager.SetConfig(cfg)
						manager.SetSelector(&coreauth.FillFirstSelector{})
						manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
						for _, id := range []string{"header-A", "header-B", "header-C"} {
							auth := &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": id, "sub": id}}
							if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), auth); err != nil {
								t.Fatal(err)
							}
							model := "header-model"
							if scenario == "model_mismatch" && id != "header-A" {
								model = "other-model"
							}
							registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: model, Type: "bps", UserDefined: true}})
							t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
						}
						var posted []string
						selectedB := false
						manager.SetRoundTripperProvider(failoverHeaderTransport{
							selected: func(auth *coreauth.Auth) {
								if auth.ID == "header-B" {
									selectedB = true
									if scenario == "B_selected_without_POST" {
										manager.MarkResult(context.Background(), coreauth.Result{AuthID: auth.ID, Provider: "bps", Model: "header-model", Error: &coreauth.Error{HTTPStatus: 403}, PermissionDenial: &coreauth.PermissionDenial{Source: "http", UpstreamHTTPStatus: 403, SemanticStatus: 403}})
									}
								}
							},
							bpsDiagnosticTransport: func(req *http.Request) (*http.Response, error) {
								if req.Method != http.MethodPost || req.URL.String() != "https://bps.openai.com/basispoints/api/responses" {
									return nil, errors.New("unexpected synthetic destination")
								}
								id := req.Header.Get("chatgpt-account-id")
								posted = append(posted, id)
								name, tokens := "echo", 18
								if id == "header-A" && scenario != "first_success" {
									name, tokens = "missing", 9
								}
								terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_header","object":"response","status":"completed","model":"header-model","output":[],"usage":{"input_tokens":11,"output_tokens":7}}}`)
								_ = terminal.Set("response.usage.total_tokens", tokens)
								_ = terminal.Set("response.output", []any{map[string]any{"type": "function_call", "id": "fc_header", "call_id": "call_header", "name": name, "arguments": `{}`, "status": "completed"}})
								return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Bps-Failover-Count": {"99"}, "x-bPs-failoVer-cOunt": {"99"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
							},
						})
						payload := ojson.NewDocString(`{"model":"header-model","input":"Call echo","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}`)
						if path == "/v1/chat/completions" {
							payload = ojson.NewDocString(`{"model":"header-model","messages":[{"role":"user","content":"Call echo"}],"tools":[{"type":"function","function":{"name":"echo","parameters":{"type":"object"}}}],"stream_options":{"include_usage":true}}`)
						}
						_ = payload.Set("stream", stream)
						req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload.String()))
						req.Header.Set("Authorization", "Bearer synthetic-key")
						req.Header.Set("Content-Type", "application/json")
						out := httptest.NewRecorder()
						server.engine.ServeHTTP(out, req)
						wantPosts, wantHeader := []string{"header-A"}, ""
						if scenario == "failover" {
							wantPosts, wantHeader = append(wantPosts, "header-B"), "1"
						}
						if !reflect.DeepEqual(posted, wantPosts) {
							t.Fatalf("POSTs=%v want=%v", posted, wantPosts)
						}
						if scenario == "B_selected_without_POST" && !selectedB {
							t.Fatal("fixture did not reach B selection")
						}
						if got := out.Result().Header.Get("X-BPS-Failover-Count"); got != wantHeader {
							t.Errorf("actual HTTP header=%q want=%q POSTs=%v", got, wantHeader, posted)
						}
						if scenario == "failover" || scenario == "first_success" {
							if out.Code != http.StatusOK || !strings.Contains(out.Body.String(), "call_bps_") {
								t.Fatalf("success response changed: HTTP=%d body=%s", out.Code, out.Body.String())
							}
							if !strings.Contains(out.Body.String(), `"total_tokens":18`) || strings.Contains(out.Body.String(), `"total_tokens":27`) || strings.Contains(out.Body.String(), "X-BPS-Failover-Count") {
								t.Fatal("header metadata or combined usage leaked into output body")
							}
						}
						t.Logf("POSTs=%v header=%q passthrough=%v", posted, out.Result().Header.Get("X-BPS-Failover-Count"), passthrough)
					})
				}
			}
		}
	}
}
