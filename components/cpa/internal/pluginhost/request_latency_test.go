package pluginhost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/requestlatency"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestRequestLatencyActualPluginStream(t *testing.T) {
	for _, mode := range []string{"text", "nested", "failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, state := requestlatency.NewContext(context.Background())
			ctx = requestlatency.WithRoute(ctx, requestlatency.DirectPlugin, false, false, 17)
			if mode == "nested" {
				ctx = requestlatency.WithRoute(ctx, requestlatency.CredentialExecutor, true, false, 999)
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			first := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"secret-")
			second := []byte("answer\"}}]}\n\n")
			started := make(chan struct{})
			executor := &fakeExecutor{identifier: "fake-provider", executeStream: func(callCtx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
				if requestlatency.From(callCtx) != state {
					t.Fatal("adapter lost context")
				}
				close(started)
				if mode == "failure" {
					return pluginapi.ExecutorStreamResponse{}, errors.New("private-call-error")
				}
				chunks := make(chan pluginapi.ExecutorStreamChunk, 2)
				if mode == "canceled" {
					go func() { <-callCtx.Done(); close(chunks) }()
				} else {
					chunks <- pluginapi.ExecutorStreamChunk{Payload: first}
					chunks <- pluginapi.ExecutorStreamChunk{Payload: second}
					close(chunks)
				}
				return pluginapi.ExecutorStreamResponse{Chunks: chunks}, nil
			}}
			host := newRouteModelHostWithRecords(capabilityRecord{id: "cpa-bps", plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{Executor: executor}}})
			raw := []byte("{\"model\":\"model\",\"messages\":[{\"role\":\"user\",\"content\":\"secret-prompt\"}]}")
			result, err := host.ExecutePluginExecutorStream(ctx, "cpa-bps", coreexecutor.Request{Model: "model", Payload: raw}, coreexecutor.Options{OriginalRequest: raw, SourceFormat: sdktranslator.FormatOpenAI, ResponseFormat: sdktranslator.FormatOpenAI, Stream: true})
			<-started
			if mode == "failure" {
				if err == nil {
					t.Fatal("plugin failure lost")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "canceled" {
					cancel()
				}
				var output []byte
				for chunk := range result.Chunks {
					output = append(output, chunk.Payload...)
				}
				if mode != "canceled" && string(output) != "{\"choices\":[{\"delta\":{\"content\":\"secret-answer\"}}]}" {
					t.Fatalf("payload altered: %q", output)
				}
			}
			fields := state.Finish(err, 200)
			for _, key := range []string{"host_prepare_count", "request_clone_count", "plugin_call_return_count", "executor_cpa_bps_count"} {
				if fields[key] != 1 {
					t.Fatalf("actual hook %s=%v fields=%v", key, fields[key], fields)
				}
			}
			key := "plugin_first_output_count"
			if mode == "nested" {
				key = "nested_plugin_first_output_count"
			}
			if mode == "text" || mode == "nested" {
				if fields[key] != 1 {
					t.Fatalf("cross-chunk output missing: %v", fields)
				}
			} else if _, ok := fields[key]; ok {
				t.Fatalf("unobserved output emitted: %v", fields)
			}
			for _, secret := range []string{"secret-prompt", "secret-answer", "private-call-error", "fake-provider"} {
				if strings.Contains(fmt.Sprint(fields), secret) {
					t.Fatal("sensitive summary")
				}
			}
		})
	}
}

type requestLatencyRPCClient struct{ calls int }

func (c *requestLatencyRPCClient) Call(context.Context, string, []byte) ([]byte, error) {
	c.calls++
	return marshalRPCResult(rpcExecutorStreamResponse{Chunks: []pluginapi.ExecutorStreamChunk{{Payload: []byte("payload")}}})
}
func (*requestLatencyRPCClient) Shutdown() {}

func TestRequestLatencyActualRPCSerialization(t *testing.T) {
	ctx, state := requestlatency.NewContext(context.Background())
	client := &requestLatencyRPCClient{}
	adapter := &rpcPluginAdapter{id: "private-plugin", host: New(), client: client}
	result, err := adapter.ExecuteStream(ctx, pluginapi.ExecutorRequest{Stream: true, Payload: []byte("secret-prompt")})
	if err != nil {
		t.Fatal(err)
	}
	for range result.Chunks {
	}
	fields := state.Finish(nil, 200)
	if client.calls != 1 || fields["rpc_serialize_count"] != 1 {
		t.Fatalf("RPC hook missing: calls=%d fields=%v", client.calls, fields)
	}
}
