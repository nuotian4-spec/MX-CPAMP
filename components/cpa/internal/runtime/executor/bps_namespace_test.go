package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBPSExecutorDirectNamespaceCatalog(t *testing.T) {
	for _, kind := range []string{"function", "custom"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", kind, stream), func(t *testing.T) {
				cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
				auth := &coreauth.Auth{ID: "namespace-test", Provider: "bps", Metadata: map[string]any{"access_token": "synthetic-token", "account_id": "synthetic-account", "sub": "synthetic-subject"}}
				payload := ojson.NewDocString(`{"model":"configured-model","input":"synthetic","tools":[]}`)
				_ = payload.Set("tools", []any{map[string]any{"type": "namespace", "name": "a", "tools": []any{map[string]any{"type": kind, "name": "echo", "parameters": map[string]any{"type": "object"}}}}})
				item := map[string]any{"type": "function_call", "id": "fc_native", "call_id": "call_namespace", "name": "echo", "namespace": "a", "arguments": `{"value":9007199254740993}`, "status": "completed"}
				if kind == "custom" {
					item["type"] = "custom_tool_call"
					item["input"] = "line1\nline2\n"
					delete(item, "arguments")
				}
				terminal := ojson.NewDocString(`{"type":"response.completed","response":{"id":"resp_ns","object":"response","status":"completed","model":"configured-model"}}`)
				_ = terminal.Set("response.output", []any{item})
				calls := 0
				ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + terminal.String() + "\n\n"))}, nil
				})))
				executor := NewBPSExecutor(cfg)
				req := coreexecutor.Request{Model: "configured-model", Payload: payload.Bytes()}
				opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: payload.Bytes()}
				var output strings.Builder
				if stream {
					result, err := executor.ExecuteStream(ctx, auth, req, opts)
					if err != nil {
						t.Fatal(err)
					}
					for chunk := range result.Chunks {
						if chunk.Err != nil {
							t.Fatal(chunk.Err)
						}
						output.Write(chunk.Payload)
					}
				} else {
					result, err := executor.Execute(ctx, auth, req, opts)
					if err != nil {
						t.Fatal(err)
					}
					output.Write(result.Payload)
				}
				if calls != 1 || !strings.Contains(output.String(), `"namespace":"a"`) {
					t.Fatalf("namespace not relayed through native executor: calls=%d %s", calls, output.String())
				}
				matches := regexp.MustCompile(`"call_id":"(call_bps_[^"]+)"`).FindAllStringSubmatch(output.String(), -1)
				if len(matches) == 0 {
					t.Fatal("client binding identity missing")
				}
				for _, match := range matches {
					if match[1] != matches[0][1] {
						t.Fatal("binding identity changed across stream lifecycle")
					}
				}
			})
		}
	}
}
