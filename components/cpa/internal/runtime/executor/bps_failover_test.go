package executor

import (
	"context"
	"fmt"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"path/filepath"
	"regexp"
)

type failoverBody struct {
	io.Reader
	closed *atomic.Bool
}

func (b failoverBody) Close() error { b.closed.Store(true); return nil }

func TestBPSFailoverNative(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, scenario := range []string{"success", "B_rejected", "generic502", "EOF", "created", "same_identity", "no_B", "model_mismatch", "cancel", "403", "bound", "previous", "image", "pin", "unknown_usage", "partial_usage", "final_permission", "preft"} {
			t.Run(fmt.Sprintf("%s/stream=%v", scenario, stream), func(t *testing.T) {
				capture := &captureAnyProviderUsagePlugin{provider: "bps", records: make(chan usage.Record, 8)}
				usage.RegisterNamedPlugin(t.Name(), capture)
				t.Cleanup(func() { usage.RegisterNamedPlugin(t.Name(), noopClaudeUsagePlugin{}) })
				cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				m := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
				m.SetConfig(cfg)
				m.RegisterExecutor(NewBPSExecutor(cfg))
				ids := []string{"a-" + t.Name(), "b-" + t.Name(), "c-" + t.Name()}
				for i, id := range ids {
					account := fmt.Sprintf("account-%d", i)
					sub := fmt.Sprintf("sub-%d", i)
					if scenario == "same_identity" && i > 0 {
						account = "account-0"
						sub = "sub-0"
					}
					a := &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic", "account_id": account, "sub": sub}}
					if scenario == "no_B" && i > 0 {
						a.Disabled = true
					}
					if _, err := m.Register(context.Background(), a); err != nil {
						t.Fatal(err)
					}
					model := "configured-model"
					if scenario == "model_mismatch" && i > 0 {
						model = "other-model"
					}
					registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: model}})
					t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
				}
				raw := []byte(`{"model":"configured-model","input":"hello","tools":[{"type":"function","name":"echo","parameters":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}}]}`)
				doc := ojson.NewDoc(raw)
				switch scenario {
				case "bound":
					_ = doc.Set("input", []any{map[string]any{"type": "function_call_output", "call_id": "old", "output": "done"}})
				case "previous":
					_ = doc.Set("previous_response_id", "resp_old")
				case "image":
					_ = doc.Set("input", []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64,AA=="}}}})
				}
				raw = doc.Bytes()
				counts := map[string]int{}
				aClosed := &atomic.Bool{}
				ctx, cancel := context.WithCancel(context.Background())
				var markDelivery func()
				ctx, markDelivery = coreauth.WithBPSClientDelivery(ctx)
				defer cancel()
				transport := bpsTestTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() != helps.BPSResponsesURL {
						t.Fatalf("unexpected destination: %s", r.URL)
					}
					account := r.Header.Get("chatgpt-account-id")
					counts[account]++
					if account != "account-0" {
						for _, entry := range m.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
							if entry.AuthID == ids[0] && entry.Active != 0 {
								t.Errorf("A lease still active at B dispatch: %d", entry.Active)
							}
						}
					}
					if account != "account-0" && !aClosed.Load() {
						t.Error("B started before A body was closed")
					}
					status := 200
					name := "echo"
					args := `{"value":"ok"}`
					if account == "account-0" || scenario == "B_rejected" {
						name = "missing"
					}
					terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_failover","object":"response","status":"completed","model":"configured-model","usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}}`)
					_ = terminal.Set("response.output", []any{map[string]any{"type": "function_call", "name": name, "id": "fc_one", "call_id": "call_one", "arguments": args, "status": "completed"}})
					if account == "account-0" && scenario == "unknown_usage" {
						_ = terminal.Delete("response.usage")
					}
					if account == "account-0" && scenario == "partial_usage" {
						_ = terminal.Delete("response.usage.output_tokens")
						_ = terminal.Delete("response.usage.total_tokens")
					}
					wire := "data: " + terminal.String() + "\n\n"
					if account == "account-0" {
						switch scenario {
						case "generic502":
							status = 502
						case "403":
							status = 403
						case "EOF":
							wire = ""
						case "created":
							wire = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_failover\",\"status\":\"in_progress\"}}\n\n" + wire
						case "cancel":
							cancel()
						case "preft":
							markDelivery()
						}
					}
					closed := &atomic.Bool{}
					if account == "account-0" {
						closed = aClosed
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: failoverBody{Reader: strings.NewReader(wire), closed: closed}}, nil
				})
				ctx = context.WithValue(ctx, "cliproxy.roundtripper", http.RoundTripper(transport))
				req := coreexecutor.Request{Model: "configured-model", Payload: raw}
				opts := coreexecutor.Options{OriginalRequest: raw, SourceFormat: sdktranslator.FormatOpenAIResponse}
				if scenario == "pin" {
					opts.Metadata = map[string]any{coreexecutor.PinnedAuthMetadataKey: ids[0]}
				}
				if scenario == "final_permission" {
					calls := 0
					opts.RequestAfterAuthInterceptor = func(context.Context, coreexecutor.RequestAfterAuthInterceptRequest) coreexecutor.RequestAfterAuthInterceptResponse {
						calls++
						if calls == 2 {
							m.MarkResult(context.Background(), coreauth.Result{AuthID: ids[1], Provider: "bps", Model: "configured-model", Error: &coreauth.Error{HTTPStatus: 403}, PermissionDenial: &coreauth.PermissionDenial{Source: "http", UpstreamHTTPStatus: 403, SemanticStatus: 403}})
						}
						return coreexecutor.RequestAfterAuthInterceptResponse{}
					}
				}
				var output strings.Builder
				var err error
				if stream {
					var result *coreexecutor.StreamResult
					result, err = m.ExecuteStream(ctx, []string{"bps"}, req, opts)
					if result != nil {
						for chunk := range result.Chunks {
							if chunk.Err != nil {
								err = chunk.Err
							}
							output.Write(chunk.Payload)
						}
					}
				} else {
					var result coreexecutor.Response
					result, err = m.Execute(ctx, []string{"bps"}, req, opts)
					output.Write(result.Payload)
				}
				wantB := 0
				if scenario == "unknown_usage" || scenario == "partial_usage" || scenario == "success" || scenario == "B_rejected" || scenario == "created" && !stream {
					wantB = 1
				}
				if counts["account-1"] != wantB || counts["account-2"] != 0 {
					t.Fatalf("A=%d B=%d C=%d expected B=%d err=%v", counts["account-0"], counts["account-1"], counts["account-2"], wantB, err)
				}
				if scenario == "unknown_usage" || scenario == "partial_usage" || scenario == "success" || scenario == "created" && !stream {
					if err != nil {
						t.Fatal(err)
					}
					match := regexp.MustCompile(`"call_id":"(call_bps_[^"]+)"`).FindStringSubmatch(output.String())
					if len(match) < 2 {
						t.Fatalf("missing B binding: %s", output.String())
					}
					binding, e := (bpsbinding.Store{Dir: filepath.Join(config.BPSStateDirectory(cfg), "bindings")}).Load("", match[1])
					if e != nil {
						t.Fatal(e)
					}
					if binding.Identity.AuthID != ids[1] {
						t.Fatalf("binding=%+v", binding.Identity)
					}
				} else if err == nil {
					t.Fatalf("expected failure: %s", output.String())
				}
				if wantB == 1 {
					records := map[string]usage.Record{}
					for len(records) < 2 {
						select {
						case record := <-capture.records:
							if record.AuthID == ids[0] || record.AuthID == ids[1] {
								records[record.AuthID] = record
							}
						case <-time.After(5 * time.Second):
							t.Fatal("missing attempt usage")
						}
					}
					a, b := records[ids[0]], records[ids[1]]
					if !a.Failed || a.Detail.Iterations == nil || b.Detail.Iterations == nil {
						t.Fatalf("usage outcomes lost: A=%+v B=%+v", a, b)
					}
					ai, bi := (*a.Detail.Iterations)[0], (*b.Detail.Iterations)[0]
					if ai.Type != "bps_initial" || bi.Type != "bps_failover" {
						t.Fatalf("attempt reasons=%s/%s", ai.Type, bi.Type)
					}
					if scenario == "unknown_usage" {
						if ai.InputTokens != nil || ai.OutputTokens != nil {
							t.Fatal("unknown usage became zero")
						}
					} else if ai.InputTokens == nil || *ai.InputTokens != 7 {
						t.Fatal("A input usage lost")
					}
					if scenario == "partial_usage" && ai.OutputTokens != nil {
						t.Fatal("missing output usage became zero")
					}
					if bi.InputTokens == nil || *bi.InputTokens != 7 || bi.OutputTokens == nil || *bi.OutputTokens != 2 {
						t.Fatal("B usage lost")
					}
				}
				t.Logf("A=%d B=%d C=%d; A released=%v", counts["account-0"], counts["account-1"], counts["account-2"], aClosed.Load())
			})
		}
	}
}
