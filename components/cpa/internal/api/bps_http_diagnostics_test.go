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
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

// The legacy adapter preserves the pre-diagnostic statusErr contract after a real send.
type bpsLegacy404Error struct{}

func (bpsLegacy404Error) Error() string              { return "BPS upstream returned HTTP 404" }
func (bpsLegacy404Error) StatusCode() int            { return 404 }
func (bpsLegacy404Error) RetryAfter() *time.Duration { return nil }
func (bpsLegacy404Error) IsCredentialScoped() bool   { return false }
func (bpsLegacy404Error) IsStreamTruncated() bool    { return false }
func (bpsLegacy404Error) IsTransientRateLimit() bool { return false }
func (bpsLegacy404Error) LogDiagnostic() string      { return "" }

type bpsLegacy404Executor struct{ *executor.BPSExecutor }

func (e bpsLegacy404Executor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	result, err := e.BPSExecutor.Execute(ctx, auth, req, opts)
	if err != nil {
		return result, bpsLegacy404Error{}
	}
	return result, nil
}

func (e bpsLegacy404Executor) ExecuteStream(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	result, err := e.BPSExecutor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		return result, bpsLegacy404Error{}
	}
	return result, nil
}

func TestBPSHTTP404DiagnosticsKeepDispatch(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		for _, stream := range []bool{false, true} {
			for _, customRule := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%v/custom-rule=%v", path, stream, customRule), func(t *testing.T) {
					type observation struct {
						code, message       string
						cooling, modelFound bool
						calls               int
					}
					var baseline observation
					for _, legacy := range []bool{true, false} {
						cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"synthetic-key"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
						cfg.BPS.Models = []string{"gpt-6-luna"}
						server := newTestServerWithConfig(t, cfg)
						manager := server.handlers.AuthManager
						manager.SetConfig(cfg)
						bps := executor.NewBPSExecutor(cfg)
						if legacy {
							manager.RegisterExecutor(bpsLegacy404Executor{bps})
						} else {
							manager.RegisterExecutor(bps)
						}
						auth := &coreauth.Auth{ID: fmt.Sprintf("diagnostic-404-%v", legacy), Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic-token", "account_id": "synthetic-account", "sub": "synthetic-subject"}}
						if customRule {
							auth.Metadata["request_scoped_errors"] = []config.RequestScopedErrorRule{{Status: 404, MatchRegexr: []string{`^BPS upstream returned HTTP 404$`}, Action: "stop"}}
						}
						if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), auth); err != nil {
							t.Fatal("fixture registration failed")
						}
						registry.GetGlobalRegistry().RegisterClient(auth.ID, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}})
						defer registry.GetGlobalRegistry().UnregisterClient(auth.ID)
						calls := 0
						manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(*http.Request) (*http.Response, error) {
							calls++
							return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("PRIVATE_UPSTREAM_BODY"))}, nil
						}))
						payload := `{"model":"gpt-6-luna","input":"Hello"}`
						if path == "/v1/chat/completions" {
							payload = `{"model":"gpt-6-luna","messages":[{"role":"user","content":"Hello"}]}`
						}
						doc := ojson.NewDocString(payload)
						_ = doc.Set("stream", stream)
						req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(doc.String()))
						req.Header.Set("Authorization", "Bearer synthetic-key")
						req.Header.Set("Content-Type", "application/json")
						response := httptest.NewRecorder()
						server.engine.ServeHTTP(response, req)
						public := ojson.GetBytes(response.Body.Bytes(), "error")
						if response.Code != 404 || public.Get("code").String() != "model_not_found" || public.Get("type").String() != "invalid_request_error" || calls != 1 {
							t.Fatal("public 404 classification or dispatch count changed")
						}
						if !legacy && (public.Get("diagnostics.stage").String() != "upstream_http" || public.Get("diagnostics.upstream_http_status").Int() != 404) {
							t.Fatal("public 404 diagnostics missing")
						}
						current, ok := manager.GetByID(auth.ID)
						if !ok {
							t.Fatal("credential disappeared")
						}
						seen := observation{calls: calls}
						if state := current.ModelStates["gpt-6-luna"]; state != nil {
							seen.modelFound = true
							seen.cooling = state.Unavailable && state.NextRetryAfter.After(time.Now())
							if state.LastError != nil {
								seen.code, seen.message = state.LastError.Code, state.LastError.Message
							}
						}
						if legacy {
							baseline = seen
							if customRule && seen.cooling || !customRule && (!seen.cooling || seen.code != "") {
								t.Fatal("legacy fixture does not match the existing cooldown contract")
							}
						} else if seen != baseline {
							t.Fatalf("dispatch differs from legacy: legacy_code=%q current_code=%q legacy_cooling=%v current_cooling=%v", baseline.code, seen.code, baseline.cooling, seen.cooling)
						}
						t.Logf("legacy=%v public=404/model_not_found internal_code=%q cooling=%v calls=%d", legacy, seen.code, seen.cooling, calls)
					}
				})
			}
		}
	}
}
