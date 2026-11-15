package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func parityAuth() *coreauth.Auth {
	return &coreauth.Auth{ID: "parity-a", Provider: "bps", Metadata: map[string]any{"access_token": "synthetic", "account_id": "account-a", "sub": "subject-a"}}
}

func TestBPSParityThresholdAndFast(t *testing.T) {
	for _, format := range []sdktranslator.Format{sdktranslator.FormatOpenAIResponse, sdktranslator.FormatOpenAI} {
		for _, fast := range []bool{false, true} {
			for _, conflict := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/fast=%v/conflict=%v", format, fast, conflict), func(t *testing.T) {
					cfg := &config.Config{BPS: config.BPSConfig{FastMode: fast}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
					if conflict {
						cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "configured-model"}}, Params: map[string]any{"context_management": []any{map[string]any{"type": "compaction", "compact_threshold": 920000}}}}}
					}
					posts := 0
					ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(r *http.Request) (*http.Response, error) {
						posts++
						body, _ := io.ReadAll(r.Body)
						doc := ojson.ParseBytes(body)
						if doc.Get("context_management.0.compact_threshold").Int() != 200000 {
							t.Fatalf("threshold: %s", body)
						}
						if fast && doc.Get("service_tier").String() != "priority" || !fast && doc.Get("service_tier").Exists() {
							t.Fatalf("tier: %s", body)
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + bpsTestTerminal + "\n\n"))}, nil
					})))
					body := []byte(`{"model":"configured-model","input":"hello","service_tier":"priority","context_management":[{"type":"compaction","compact_threshold":920000}]}`)
					if format == sdktranslator.FormatOpenAI {
						body = []byte(`{"model":"configured-model","messages":[{"role":"user","content":"hello"}],"service_tier":"priority"}`)
					}
					_, err := NewBPSExecutor(cfg).Execute(ctx, parityAuth(), coreexecutor.Request{Model: "configured-model", Payload: body}, coreexecutor.Options{SourceFormat: format})
					if conflict {
						if err == nil || posts != 0 {
							t.Fatalf("conflict err=%v posts=%d", err, posts)
						}
					} else if err != nil || posts != 1 {
						t.Fatalf("err=%v posts=%d", err, posts)
					}
				})
			}
		}
	}
}

func parityCompactionTerminal(output string) string {
	return `{"type":"response.completed","response":{"id":"resp_compact","object":"response","status":"completed","model":"configured-model","output":` + output + `}}`
}

func TestBPSParityCompactionEvents(t *testing.T) {
	state := `{"id":"cmp_native","type":"compaction","encrypted_content":"synthetic-state"}`
	done := `{"type":"response.output_item.done","response_id":"resp_compact","output_index":0,"item":` + state + `}`
	for _, tc := range []struct {
		name, prefix, output string
		valid                bool
	}{
		{"terminal-only", "", "[" + state + "]", true},
		{"duplicate-done", "data: " + done + "\n\ndata: " + done + "\n\n", "[" + state + "]", true},
		{"two-states", "", "[" + state + `,{"id":"cmp_other","type":"compaction","encrypted_content":"other"}]`, false},
		{"same-id-conflict", "data: " + done + "\n\n", `[{"id":"cmp_native","type":"compaction","encrypted_content":"other"}]`, false},
		{"zero", "", "[]", false},
		{"empty", "", `[{"id":"cmp_native","type":"compaction","encrypted_content":""}]`, false},
		{"tool-with-invalid-compaction", "", `[{"id":"fc_mix","type":"function_call","call_id":"call_mix","name":"echo","arguments":"{}"},` + state + `,{"id":"cmp_other","type":"compaction","encrypted_content":"other"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			posts := 0
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(r *http.Request) (*http.Response, error) {
				posts++
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.prefix + "data: " + parityCompactionTerminal(tc.output) + "\n\n"))}, nil
			})))
			body := []byte(`{"model":"configured-model","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}],"input":[{"role":"user","content":"hello"},{"type":"compaction_trigger"}]}`)
			result, err := NewBPSExecutor(cfg).ExecuteStream(ctx, parityAuth(), coreexecutor.Request{Model: "configured-model", Payload: body}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
			if err != nil {
				t.Fatal(err)
			}
			doneCount, completed := 0, 0
			var failure error
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					failure = chunk.Err
				}
				if !tc.valid && strings.Contains(string(chunk.Payload), "function_call") {
					t.Fatalf("tool leaked before compaction validation: %s", chunk.Payload)
				}
				for _, line := range strings.Split(string(chunk.Payload), "\n") {
					doc := ojson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
					if doc.Get("type").String() == "response.output_item.done" && doc.Get("item.type").String() == "compaction" {
						doneCount++
					}
					if doc.Get("type").String() == "response.completed" {
						completed++
					}
				}
			}
			if tc.valid {
				if failure != nil || doneCount != 1 || completed != 1 {
					t.Fatalf("err=%v done=%d complete=%d", failure, doneCount, completed)
				}
			} else if failure == nil || doneCount != 0 || completed != 0 {
				t.Fatalf("invalid err=%v done=%d complete=%d", failure, doneCount, completed)
			}
			if posts != 1 {
				t.Fatalf("posts=%d", posts)
			}
		})
	}
}

func TestBPSParityEncryptedHistory(t *testing.T) {
	for _, omit := range []bool{true, false} {
		for _, current := range []bool{true, false} {
			t.Run(fmt.Sprintf("omit=%v/current=%v", omit, current), func(t *testing.T) {
				cfg := &config.Config{BPS: config.BPSConfig{OmitEncryptedHistory: &omit}, BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				posts := 0
				ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(r *http.Request) (*http.Response, error) {
					posts++
					body, _ := io.ReadAll(r.Body)
					if strings.Contains(string(body), "CIPHERTEXT") || !strings.Contains(string(body), "Encrypted content omitted") || !strings.Contains(string(body), "keep me") {
						t.Fatalf("history=%s", body)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + bpsTestTerminal + "\n\n"))}, nil
				})))
				body := []byte(`{"model":"configured-model","input":[{"role":"assistant","content":[{"type":"output_text","text":"keep me"},{"type":"encrypted_content","encrypted_content":"CIPHERTEXT"}]},{"role":"user","content":"current task"}]}`)
				if current {
					body = []byte(`{"model":"configured-model","input":[{"role":"user","content":[{"type":"encrypted_content","encrypted_content":"CIPHERTEXT"}]}]}`)
				}
				_, err := NewBPSExecutor(cfg).Execute(ctx, parityAuth(), coreexecutor.Request{Model: "configured-model", Payload: body}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
				if omit && !current {
					if err != nil || posts != 1 {
						t.Fatalf("err=%v posts=%d", err, posts)
					}
				} else if err == nil || posts != 0 {
					t.Fatalf("err=%v posts=%d", err, posts)
				}
			})
		}
	}
}

func TestBPSParityCompactionNoStateOnFailure(t *testing.T) {
	for _, mode := range []string{"eof", "canceled", "403"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			posts := 0
			ctx = context.WithValue(ctx, "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(r *http.Request) (*http.Response, error) {
				posts++
				status := 200
				if mode == "403" {
					status = 403
				}
				if mode == "canceled" {
					cancel()
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"cmp_no","type":"compaction","encrypted_content":"state"}}` + "\n\n"))}, nil
			})))
			result, err := NewBPSExecutor(cfg).ExecuteStream(ctx, parityAuth(), coreexecutor.Request{Model: "configured-model", Payload: []byte(`{"model":"configured-model","input":[{"type":"compaction_trigger"}]}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
			if err == nil {
				for chunk := range result.Chunks {
					if strings.Contains(string(chunk.Payload), "encrypted_content") {
						t.Fatal("state leaked")
					}
					if chunk.Err != nil {
						err = chunk.Err
					}
				}
			}
			if err == nil {
				err = ctx.Err()
			}
			if err == nil || posts != 1 {
				t.Fatalf("err=%v posts=%d", err, posts)
			}
			entries, _ := os.ReadDir(filepath.Join(config.BPSStateDirectory(cfg), "bindings"))
			if len(entries) != 0 {
				t.Fatal("failure signed a state")
			}
		})
	}
}
