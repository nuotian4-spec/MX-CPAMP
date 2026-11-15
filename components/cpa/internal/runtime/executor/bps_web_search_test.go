package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSWebSearchPreservesClientCorrection(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			posts := 0
			ctx := coreexecutor.WithBPSWebSearch(context.Background(), func(context.Context, []byte) ([]byte, error) {
				t.Error("client correction invoked search")
				return nil, nil
			})
			ctx = context.WithValue(ctx, "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(*http.Request) (*http.Response, error) {
				posts++
				args := ojson.NewDocString(`{"summary":"Call client tool","extended_summary":"","destructive":false,"references":[]}`)
				_ = args.Set("code", "original code\n")
				if posts == 3 {
					_ = args.Set("summary", "codex2api.custom/functions.exec")
					_ = args.Set("code", "altered code")
				}
				doc := ojson.NewDocString(bpsTestTerminal)
				_ = doc.Set("response.output", []any{map[string]any{"type": "function_call", "id": fmt.Sprint("fc_", posts), "call_id": fmt.Sprint("call_", posts), "name": "run_officejs", "arguments": args.String()}})
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + doc.String() + "\n\n"))}, nil
			})))
			raw := []byte(`{"input":"run","tools":[{"type":"web_search"},{"type":"custom","name":"functions.exec"}]}`)
			e := NewBPSExecutor(&config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}})
			req := coreexecutor.Request{Model: "configured-model", Payload: raw}
			opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}
			var err error
			var output string
			if stream {
				var result *coreexecutor.StreamResult
				result, err = e.ExecuteStream(ctx, parityAuth(), req, opts)
				if err == nil {
					for chunk := range result.Chunks {
						output += string(chunk.Payload)
						if chunk.Err != nil {
							err = chunk.Err
						}
					}
				}
			} else {
				var result coreexecutor.Response
				result, err = e.Execute(ctx, parityAuth(), req, opts)
				output = string(result.Payload)
			}
			if err != nil || posts != 3 || !strings.Contains(output, "original code") || strings.Contains(output, "altered code") {
				t.Fatalf("posts=%d err=%v output=%s", posts, err, output)
			}
		})
	}
}

func TestBPSWebSearchCreatedPrecedesBufferedTerminal(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = coreexecutor.WithBPSWebSearch(ctx, func(context.Context, []byte) ([]byte, error) { t.Error("search ran before terminal"); return nil, nil })
	ctx = context.WithValue(ctx, "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
	})))
	go func() {
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_search\",\"output\":[]}}\n\n")
	}()
	result, err := NewBPSExecutor(&config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}).ExecuteStream(ctx, parityAuth(), coreexecutor.Request{Model: "configured-model", Payload: []byte(`{"input":"search","tools":[{"type":"web_search"}]}`)}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case chunk := <-result.Chunks:
		if chunk.Err != nil || !strings.Contains(string(chunk.Payload), "response.created") {
			t.Fatalf("first=%+v", chunk)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("created waited for terminal")
	}
	cancel()
	for chunk := range result.Chunks {
		if strings.Contains(string(chunk.Payload), "response.completed") {
			t.Fatal("cancellation fabricated success")
		}
	}
}
