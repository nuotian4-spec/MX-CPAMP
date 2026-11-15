package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func delegationNotice() map[string]any {
	return map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "create_thread", "output": "<codex_delegation>HANDOFF_MARKER</codex_delegation>"}
}

func runDelegation(t *testing.T, cfg *config.Config, body []byte, caller string, stream bool, transport bpsTestTransport) ([]byte, error) {
	t.Helper()
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
	req := coreexecutor.Request{Model: "configured-model", Payload: body}
	opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: body, Metadata: map[string]any{coreexecutor.CallerScopeMetadataKey: caller, coreexecutor.CanonicalSessionIDMetadataKey: "parent"}}
	if !stream {
		res, err := NewBPSExecutor(cfg).Execute(ctx, parityAuth(), req, opts)
		return res.Payload, err
	}
	res, err := NewBPSExecutor(cfg).ExecuteStream(ctx, parityAuth(), req, opts)
	if err != nil {
		return nil, err
	}
	var output []byte
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		output = append(output, chunk.Payload...)
		output = append(output, '\n', '\n')
	}
	return output, nil
}

func delegationResponse() *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + bpsTestTerminal + "\n\n"))}
}

func TestBPSDelegationMixedBindings(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, position := range []int{0, 2, 4} {
			for _, mode := range []string{"valid", "account", "caller", "missing", "expired"} {
				t.Run(fmt.Sprintf("stream=%v/position=%d/%s", stream, position, mode), func(t *testing.T) {
					cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}, Payload: config.PayloadConfig{Filter: []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "configured-model"}}, Params: []string{"metadata.turn_id"}}}}}
					store := bpsbinding.Store{Dir: filepath.Join(config.BPSStateDirectory(cfg), "bindings")}
					identity := bpsbinding.Identity{AuthID: "parity-a", AccountID: "account-a", Subject: "subject-a"}
					if mode == "account" {
						identity.AccountID = "other-account"
					}
					if mode == "expired" {
						store.Now = func() time.Time { return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) }
					}
					record, err := store.Save("caller", "call_native", identity)
					if err != nil {
						t.Fatal(err)
					}
					id := record.CallID
					if mode == "missing" {
						id = "call_bps_00000000-0000-0000-0000-000000000000"
					}
					input := []any{map[string]any{"role": "user", "content": "BEFORE_MARKER"}, map[string]any{"type": "function_call", "call_id": id, "name": "get_weather", "arguments": `{"city":"Tokyo"}`}, map[string]any{"type": "function_call_output", "call_id": id, "output": "ERROR_MARKER agent thread limit reached"}, map[string]any{"role": "user", "content": "AFTER_MARKER"}}
					input = append(input[:position], append([]any{delegationNotice()}, input[position:]...)...)
					doc := ojson.NewDocString(`{"model":"configured-model","tools":[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}`)
					_ = doc.Set("input", input)
					original := doc.String()
					posts := 0
					caller := "caller"
					if mode == "caller" {
						caller = "other-caller"
					}
					_, err = runDelegation(t, cfg, doc.Bytes(), caller, stream, func(r *http.Request) (*http.Response, error) {
						posts++
						raw, _ := io.ReadAll(r.Body)
						wire := string(raw)
						root := ojson.ParseBytes(raw)
						if strings.Contains(wire, bpsbinding.Prefix) || !strings.Contains(wire, "call_native") || !strings.Contains(wire, "ERROR_MARKER") || root.Get("metadata.turn_id").Exists() {
							t.Fatalf("binding/filter/result changed: %s", raw)
						}
						if r.Header.Get("x-openai-account-id") != "account-a" {
							t.Fatal("account changed")
						}
						before, after, handoff := strings.Index(wire, "BEFORE_MARKER"), strings.Index(wire, "AFTER_MARKER"), strings.Index(wire, "HANDOFF_MARKER")
						if before < 0 || after <= before || handoff < 0 || position == 0 && handoff >= before || position == 4 && handoff <= after || position == 2 && (handoff <= before || handoff >= after) {
							t.Fatalf("order changed: %s", raw)
						}
						found := false
						for _, item := range root.Get("input").Array() {
							if strings.Contains(item.Raw, "HANDOFF_MARKER") {
								found = true
								if item.Get("role").String() != "user" || item.Get("type").String() != "message" {
									t.Fatal("handoff role changed")
								}
							}
						}
						if !found {
							t.Fatal("handoff missing")
						}
						return delegationResponse(), nil
					})
					if mode == "valid" {
						if err != nil || posts != 1 {
							t.Fatalf("err=%v posts=%d", err, posts)
						}
					} else {
						if err == nil || posts != 0 {
							t.Fatalf("identity bypass err=%v posts=%d", err, posts)
						}
					}
					if doc.String() != original {
						t.Fatal("caller input mutated")
					}
				})
			}
		}
	}
}

func TestBPSDelegationRejectsLookalikesAndContent(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, mode := range []string{"empty_id", "null_id", "numeric_id", "fake_id", "send_message_to_thread", "open_tag", "close_tag", "empty_tag", "nonstring", "audio", "encrypted"} {
			t.Run(fmt.Sprintf("stream=%v/%s", stream, mode), func(t *testing.T) {
				notice := delegationNotice()
				switch mode {
				case "empty_id":
					notice["call_id"] = ""
				case "null_id":
					notice["call_id"] = nil
				case "numeric_id":
					notice["call_id"] = 12
				case "fake_id":
					notice["call_id"] = "call_bps_00000000-0000-0000-0000-000000000000"
				case "namespace":
					notice["namespace"] = "other"
				case "name":
					notice["name"] = "other"
				case "send_message_to_thread":
					notice["name"] = "send_message_to_thread"
				case "open_tag":
					notice["output"] = "<codex_delegation>task"
				case "close_tag":
					notice["output"] = "task</codex_delegation>"
				case "empty_tag":
					notice["output"] = "<codex_delegation> </codex_delegation>"
				case "nonstring":
					notice["output"] = []any{"task"}
				case "orphan":
					delete(notice, "namespace")
					delete(notice, "name")
					notice["output"] = "ordinary result"
				case "audio":
					notice["content"] = []any{map[string]any{"type": "input_audio", "data": "synthetic"}}
				case "encrypted":
					notice["content"] = []any{map[string]any{"type": "encrypted_content", "encrypted_content": "synthetic"}}
				}
				cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				doc := ojson.NewDocString(`{"model":"configured-model"}`)
				_ = doc.Set("input", []any{notice})
				posts := 0
				_, err := runDelegation(t, cfg, doc.Bytes(), "caller", stream, func(*http.Request) (*http.Response, error) { posts++; return delegationResponse(), nil })
				if err == nil || posts != 0 {
					t.Fatalf("invalid notification accepted: err=%v posts=%d", err, posts)
				}
			})
		}
	}
}

func TestBPSDelegationLegacyNormalization(t *testing.T) {
	for _, mode := range []string{"namespace", "name", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			notice := delegationNotice()
			switch mode {
			case "namespace":
				notice["namespace"] = "other"
			case "name":
				notice["name"] = "other"
			case "orphan":
				delete(notice, "namespace")
				delete(notice, "name")
				notice["output"] = "ORPHAN_MARKER"
			}
			cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			doc := ojson.NewDocString(`{"model":"configured-model"}`)
			_ = doc.Set("input", []any{notice})
			binding := helps.NewBPSBindings(cfg, parityAuth(), coreexecutor.Options{})
			if err := binding.Restore(ojson.NewDoc(doc.Bytes())); err == nil {
				t.Fatal("strict Restore accepted a lookalike")
			}
			posts := 0
			_, err := runDelegation(t, cfg, doc.Bytes(), "caller", false, func(r *http.Request) (*http.Response, error) {
				posts++
				body, _ := io.ReadAll(r.Body)
				root := ojson.ParseBytes(body)
				found := false
				for _, item := range root.Get("input").Array() {
					if strings.Contains(item.Raw, "HANDOFF_MARKER") || strings.Contains(item.Raw, "ORPHAN_MARKER") {
						found = true
						if item.Get("type").String() != "message" || item.Get("role").String() != "user" {
							t.Fatal("legacy normalization changed")
						}
					}
				}
				if !found {
					t.Fatal("legacy content lost")
				}
				return delegationResponse(), nil
			})
			if err != nil || posts != 1 {
				t.Fatalf("legacy normalization err=%v posts=%d", err, posts)
			}
		})
	}
}
