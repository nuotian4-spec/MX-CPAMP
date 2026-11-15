package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSDelegationAccepted(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			posts := 0
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(r *http.Request) (*http.Response, error) {
				posts++
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "HANDOFF_MARKER") || !strings.Contains(string(body), "thread handoff context") {
					t.Fatalf("handoff lost: %s", body)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + bpsTestTerminal + "\n\n"))}, nil
			})))
			body := []byte(`{"model":"configured-model","input":[{"role":"user","content":"BEFORE_MARKER"},{"type":"function_call_output","namespace":"codex_app","name":"create_thread","output":"<codex_delegation>HANDOFF_MARKER</codex_delegation>"},{"role":"user","content":"AFTER_MARKER"}]}`)
			req := coreexecutor.Request{Model: "configured-model", Payload: body}
			opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}
			if stream {
				result, err := NewBPSExecutor(cfg).ExecuteStream(ctx, parityAuth(), req, opts)
				if err != nil {
					t.Fatal(err)
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
				}
			} else {
				if _, err := NewBPSExecutor(cfg).Execute(ctx, parityAuth(), req, opts); err != nil {
					t.Fatal(err)
				}
			}
			if posts != 1 {
				t.Fatalf("posts=%d", posts)
			}
		})
	}
}
