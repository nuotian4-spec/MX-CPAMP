package api

import (
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
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestBPSParityCompactionPublicHTTP(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, old := range []bool{false, true} {
			if stream && old {
				continue
			}
			t.Run(fmt.Sprintf("stream=%v/legacy=%v", stream, old), func(t *testing.T) {
				cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"compact-key"}}, BPS: config.BPSConfig{Models: []string{"gpt-6-luna"}}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				server := newTestServerWithConfig(t, cfg)
				manager := server.handlers.AuthManager
				manager.SetConfig(cfg)
				manager.RegisterExecutor(executor.NewBPSExecutor(cfg))
				for _, id := range []string{"compact-a", "compact-b"} {
					_, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"type": "bps", "access_token": id, "account_id": id, "sub": "subject-" + id}})
					if err != nil {
						t.Fatal(err)
					}
					registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: "gpt-6-luna", Type: "bps", OwnedBy: "bps", UserDefined: true}, {ID: "gpt-other", Type: "bps", OwnedBy: "bps", UserDefined: true}})
					defer registry.GetGlobalRegistry().UnregisterClient(id)
				}
				posts, account := 0, ""
				manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
					posts++
					body, _ := io.ReadAll(r.Body)
					root := ojson.ParseBytes(body)
					if root.Get("context_management.0.compact_threshold").Int() != 200000 {
						t.Fatalf("threshold=%s", body)
					}
					output := `[{"id":"cmp_native","type":"compaction","encrypted_content":"synthetic-state"}]`
					if posts == 1 {
						account = r.Header.Get("x-openai-account-id")
						if !strings.Contains(string(body), "compaction_trigger") {
							t.Fatal("trigger missing")
						}
					} else {
						if r.Header.Get("x-openai-account-id") != account || !strings.Contains(string(body), "cmp_native") || strings.Contains(string(body), bpsbinding.Prefix) {
							t.Fatalf("wrong continuation: %s", body)
						}
						output = `[{"id":"msg_tail","type":"message","role":"assistant","content":[{"type":"output_text","text":"continued"}],"status":"completed"}]`
					}
					terminal := `{"type":"response.completed","response":{"id":"resp_compact","object":"response","status":"completed","model":"gpt-6-luna","output":` + output + `}}`
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal + "\n\n"))}, nil
				}))
				request := func(path, body string) *httptest.ResponseRecorder {
					r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
					r.Header.Set("Authorization", "Bearer compact-key")
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					server.engine.ServeHTTP(w, r)
					return w
				}
				path := "/v1/responses"
				if old {
					path += "/compact"
				}
				body := fmt.Sprintf(`{"model":"gpt-6-luna","stream":%v,"input":[{"role":"user","content":"compact"},{"type":"compaction_trigger"}]}`, stream)
				first := request(path, body)
				if first.Code != 200 {
					t.Fatalf("first=%d %s", first.Code, first.Body.String())
				}
				state := ojson.GetBytes(first.Body.Bytes(), "output.0")
				if stream {
					for _, line := range strings.Split(first.Body.String(), "\n") {
						root := ojson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
						if root.Get("type").String() == "response.output_item.done" && root.Get("item.type").String() == "compaction" {
							state = root.Get("item")
						}
					}
				}
				if !bpsbinding.IsCallID(state.Get("id").String()) {
					t.Fatalf("state missing: %s", first.Body.String())
				}
				next := ojson.NewDocString(`{"model":"gpt-6-luna","input":[]}`)
				_ = next.Set("input", []any{state.Value(), map[string]any{"role": "user", "content": "continue"}})
				second := request("/v1/responses", next.String())
				if second.Code != 200 || posts != 2 || !strings.Contains(second.Body.String(), "continued") {
					t.Fatalf("second=%d posts=%d %s", second.Code, posts, second.Body.String())
				}

				_ = next.Set("model", "gpt-other")
				wrongModel := request("/v1/responses", next.String())
				if wrongModel.Code != 409 || posts != 2 {
					t.Fatalf("model conflict=%d posts=%d %s", wrongModel.Code, posts, wrongModel.Body.String())
				}
				_ = next.Set("model", "gpt-6-luna")
				original, _ := manager.GetByID(account)
				for _, mode := range []string{"disabled", "identity"} {
					changed := original.Clone()
					if mode == "disabled" {
						changed.Disabled = true
					} else {
						changed.Metadata["sub"] = "changed-sub"
					}
					if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), changed); err != nil {
						t.Fatal(err)
					}
					rejected := request("/v1/responses", next.String())
					if rejected.Code != 409 || posts != 2 {
						t.Fatalf("%s=%d posts=%d", mode, rejected.Code, posts)
					}
					if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), original.Clone()); err != nil {
						t.Fatal(err)
					}
				}
				_ = next.Set("input.0.encrypted_content", "tampered")
				bad := request("/v1/responses", next.String())
				if bad.Code != 409 || posts != 2 {
					t.Fatalf("changed state=%d posts=%d %s", bad.Code, posts, bad.Body.String())
				}
				_ = next.Set("input.0.id", "cmp_unknown")
				missing := request("/v1/responses", next.String())
				if missing.Code != 400 || posts != 2 {
					t.Fatalf("missing=%d posts=%d", missing.Code, posts)
				}
				_ = next.Set("input.0", state.Value())
				pathRecord := filepath.Join(config.BPSStateDirectory(cfg), "bindings", state.Get("id").String()+".json")
				rawRecord, err := os.ReadFile(pathRecord)
				if err != nil {
					t.Fatal(err)
				}
				record := ojson.NewDoc(rawRecord)
				_ = record.Set("expires", "2000-01-01T00:00:00Z")
				if err = os.WriteFile(pathRecord, record.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				expired := request("/v1/responses", next.String())
				if expired.Code != 409 || posts != 2 {
					t.Fatalf("expired=%d posts=%d", expired.Code, posts)
				}

			})
		}
	}
}
