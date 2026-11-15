package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSParityCreatedIsImmediate(t *testing.T) {
	for _, format := range []sdktranslator.Format{sdktranslator.FormatOpenAIResponse} {
		t.Run(format.String(), func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			finish := make(chan struct{})
			defer close(finish)
			written := make(chan struct{})
			go func() {
				defer close(written)
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native_test\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
				<-finish
				_, _ = io.WriteString(writer, "data: "+bpsTestTerminal+"\n\n")
				_ = writer.Close()
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = context.WithValue(ctx, "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
			})))
			raw := []byte(`{"input":"hi"}`)
			if format == sdktranslator.FormatOpenAI {
				raw = []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
			}
			result, err := NewBPSExecutor(&config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}).ExecuteStream(ctx, parityAuth(), coreexecutor.Request{Model: "configured-model", Payload: raw}, coreexecutor.Options{SourceFormat: format})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case chunk := <-result.Chunks:
				if chunk.Err != nil || format == sdktranslator.FormatOpenAIResponse && !strings.Contains(string(chunk.Payload), "response.created") || format == sdktranslator.FormatOpenAI && !strings.Contains(string(chunk.Payload), "assistant") {
					t.Fatalf("created=%s err=%v", chunk.Payload, chunk.Err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("created waited for terminal")
			}
			// The terminal remains blocked while created has already been delivered.
		})
	}
}
