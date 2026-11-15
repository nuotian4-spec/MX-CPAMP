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

func TestBPSPermissionSuspensionHTTPNativePath(t *testing.T) {
	for _, upstream := range []int{403, 200} {
		for _, stream := range []bool{false, true} {
			for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
				t.Run(fmt.Sprintf("%s/http=%d/stream=%v", path, upstream, stream), func(t *testing.T) {
					cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"synthetic-key"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
					cfg.BPS.Models = []string{"gpt-6-luna", "control-model"}
					server := newTestServerWithConfig(t, cfg)
					manager := server.handlers.AuthManager
					manager.SetConfig(cfg)
					manager.SetRetryConfig(2, 0, 0)
					manager.SetCooldownStateStore(coreauth.NewFileCooldownStateStore(t.TempDir()))
					manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
					auth := &coreauth.Auth{ID: "permission-route", Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic-token", "account_id": "synthetic-account", "sub": "synthetic-subject"}}
					if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), auth); err != nil {
						t.Fatal(err)
					}
					registry.GetGlobalRegistry().RegisterClient(auth.ID, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}, {ID: "control-model", Type: "bps", OwnedBy: "bps", UserDefined: true}})
					t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
					calls := 0
					manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						raw, _ := io.ReadAll(r.Body)
						status := upstream
						wire := `{"type":"response.failed","response":{"id":"resp_permission","status":"failed","error":{"status":403,"code":"permission_denied"}}}`
						if ojson.GetBytes(raw, "model").String() == "control-model" {
							status = 200
							wire = `{"type":"response.completed","response":{"id":"resp_control","object":"response","status":"completed","model":"control-model","output":[{"id":"msg_control","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}]}}`
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + wire + "\n\n"))}, nil
					}))
					request := func(model string) *httptest.ResponseRecorder {
						payload := ojson.NewDocString(`{"model":"gpt-6-luna","input":"synthetic","messages":[{"role":"user","content":"synthetic"}]}`)
						_ = payload.Set("model", model)
						_ = payload.Set("stream", stream)
						req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload.String()))
						req.Header.Set("Authorization", "Bearer synthetic-key")
						req.Header.Set("Content-Type", "application/json")
						out := httptest.NewRecorder()
						server.engine.ServeHTTP(out, req)
						return out
					}
					first := request("gpt-6-luna")
					if calls != 1 {
						t.Fatalf("first request replayed: calls=%d body=%s", calls, first.Body.String())
					}
					state, _ := manager.GetByID(auth.ID)
					if len(state.PermissionSuspensions) != 1 {
						t.Fatalf("missing suspension after actual executor: http=%d body=%s state=%+v", first.Code, first.Body.String(), state.PermissionSuspensions)
					}
					view := state.PermissionSuspensions[0]
					if view.UpstreamHTTPStatus != upstream || view.SemanticStatus != 403 || view.ModelKey != "gpt-6-luna" {
						t.Fatalf("invalid provenance: %+v", view)
					}
					request("gpt-6-luna")
					if calls != 1 {
						t.Fatalf("suspended request reached upstream: calls=%d", calls)
					}
					control := request("control-model")
					if calls != 2 || control.Code != 200 {
						t.Fatalf("unrelated model blocked: calls=%d http=%d %s", calls, control.Code, control.Body.String())
					}
					state, _ = manager.GetByID(auth.ID)
					if len(state.PermissionSuspensions) != 1 {
						t.Fatal("control success cleared suspension")
					}
					if _, err := manager.ReleasePermissionSuspensions(context.Background(), state.ID, "bps", state.RegistrationEpoch, state.CredentialVersion, state.Generation, []coreauth.PermissionScope{{Scope: "model", ModelKey: "gpt-6-luna"}}); err != nil {
						t.Fatal(err)
					}
					second := auth.Clone()
					second.ID = "permission-other"
					if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), second); err != nil {
						t.Fatal(err)
					}
					registry.GetGlobalRegistry().RegisterClient(second.ID, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}})
					t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(second.ID) })
					before := calls
					request("gpt-6-luna")
					if calls != before+1 {
						t.Fatalf("403 replayed with another healthy credential: before=%d after=%d", before, calls)
					}
					request("gpt-6-luna")
					if calls != before+2 {
						t.Fatalf("fresh request did not isolate credential scope: before=%d after=%d", before, calls)
					}
					request("gpt-6-luna")
					if calls != before+2 {
						t.Fatal("both suspended credentials still dispatched")
					}
					t.Logf("executor=bps upstream_http=%d semantic=403 first=1 second=0 control=1 source=%s", upstream, view.Source)
				})
			}
		}
	}
}
