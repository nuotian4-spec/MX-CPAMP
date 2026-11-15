package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSParityCorrectionManagerBoundaries(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, mode := range []string{"exhausted", "failover-B", "403", "403-canceled"} {
			t.Run(fmt.Sprintf("%s/stream=%v", mode, stream), func(t *testing.T) {
				cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				m := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
				m.SetConfig(cfg)
				m.RegisterExecutor(NewBPSExecutor(cfg))
				ids := []string{"a-correction-" + t.Name(), "b-correction-" + t.Name()}
				for i, id := range ids {
					_, err := m.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": fmt.Sprintf("account-%d", i), "sub": fmt.Sprintf("sub-%d", i)}})
					if err != nil {
						t.Fatal(err)
					}
					registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: "configured-model"}})
					defer registry.GetGlobalRegistry().UnregisterClient(id)
				}
				counts := map[string]int{}
				base, cancel := context.WithCancel(context.Background())
				defer cancel()
				ctx := context.WithValue(base, "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(r *http.Request) (*http.Response, error) {
					account := r.Header.Get("chatgpt-account-id")
					counts[account]++
					status := 200
					item := map[string]any{"id": "fc", "type": "function_call", "name": "run_officejs", "call_id": "call", "arguments": `{"summary":"Call client tool","code":"original","extended_summary":"","references":[],"destructive":false}`}
					if mode == "failover-B" && account == "account-0" {
						item["name"] = "unknown_native"
					}
					if strings.HasPrefix(mode, "403") && counts[account] == 2 {
						status = 403
						if mode == "403-canceled" {
							cancel()
						}
					}
					terminal := ojson.NewDocString(bpsTestTerminal)
					_ = terminal.Set("response.output", []any{item})
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
				})))
				req := coreexecutor.Request{Model: "configured-model", Payload: []byte(`{"model":"configured-model","input":"run","tools":[{"type":"custom","name":"functions.exec"}]}`)}
				opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}
				var err error
				if stream {
					var res *coreexecutor.StreamResult
					res, err = m.ExecuteStream(ctx, []string{"bps"}, req, opts)
					if err == nil {
						for chunk := range res.Chunks {
							if chunk.Err != nil {
								err = chunk.Err
							}
						}
					}
				} else {
					_, err = m.Execute(ctx, []string{"bps"}, req, opts)
				}
				a, b := 3, 0
				if mode == "failover-B" {
					a, b = 1, 1
				}
				if strings.HasPrefix(mode, "403") {
					a = 2
				}
				if err == nil || counts["account-0"] != a || counts["account-1"] != b {
					t.Fatalf("err=%v counts=%v", err, counts)
				}
				if strings.HasPrefix(mode, "403") {
					current, _ := m.GetByID(ids[0])
					if len(current.PermissionSuspensions) != 1 {
						t.Fatalf("suspension=%+v", current.PermissionSuspensions)
					}
					opts.Metadata = map[string]any{coreexecutor.PinnedAuthMetadataKey: ids[0]}
					_, err = m.Execute(ctx, []string{"bps"}, req, opts)
					if err == nil || counts["account-0"] != 2 {
						t.Fatalf("suspended auth executed: %v %v", err, counts)
					}
				}
			})
		}
	}
}
