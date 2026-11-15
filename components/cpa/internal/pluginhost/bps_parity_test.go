package pluginhost

import (
	"context"
	"fmt"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"gopkg.in/yaml.v3"
)

func TestBPSParityPluginManagedPayload(t *testing.T) {
	for _, format := range []sdktranslator.Format{sdktranslator.FormatOpenAI, sdktranslator.FormatOpenAIResponse} {
		for _, mode := range []string{"nil-config", "fast-off", "fast-on", "conflict"} {
			t.Run(fmt.Sprintf("%s/%s", format, mode), func(t *testing.T) {
				host := New()
				fast := mode == "fast-on"
				if mode != "nil-config" {
					cfg := &config.Config{}
					var raw yaml.Node
					if err := yaml.Unmarshal([]byte(fmt.Sprintf("fast_mode: %v", fast)), &raw); err != nil {
						t.Fatal(err)
					}
					cfg.Plugins.Configs = map[string]config.PluginInstanceConfig{"cpa-bps": {Raw: *raw.Content[0]}}
					if mode == "conflict" {
						cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "fixture-model"}}, Params: map[string]any{"context_management": []any{map[string]any{"type": "compaction", "compact_threshold": 920000}}}}}
					}
					host.runtimeConfig = cfg
				}
				calls := 0
				adapter := newCurrentExecutorAdapterForTest(host, "cpa-bps", &fakeExecutor{identifier: "cpa-bps", execute: func(_ context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
					calls++
					doc := ojson.ParseBytes(req.Payload)
					if doc.Get("context_management.0.compact_threshold").Int() != 200000 || fast && doc.Get("service_tier").String() != "priority" || !fast && doc.Get("service_tier").Exists() {
						t.Fatalf("payload=%s", req.Payload)
					}
					return pluginapi.ExecutorResponse{Payload: []byte(`{"status":"completed","output":[],"service_tier":"default"}`)}, nil
				}}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse}, []sdktranslator.Format{sdktranslator.FormatOpenAIResponse})
				record := normalizeTestCapabilityRecord(capabilityRecord{id: "cpa-bps", plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{Executor: adapter.executor}}})
				setHostSnapshotForTest(host, true, record)
				if channel, known := adapter.ExecutionChannel(); !known || channel != "bps" {
					t.Fatal("合成BPS执行器未完成当前宿主登记")
				}
				body := []byte(`{"model":"fixture-model","input":"hi","service_tier":"priority","context_management":[{"type":"compaction","compact_threshold":920000}]}`)
				if format == sdktranslator.FormatOpenAI {
					body = []byte(`{"model":"fixture-model","messages":[{"role":"user","content":"hi"}],"service_tier":"priority"}`)
				}
				_, err := adapter.Execute(context.Background(), nil, coreexecutor.Request{Model: "fixture-model", Payload: body}, coreexecutor.Options{SourceFormat: format})
				if mode == "conflict" {
					if err == nil || calls != 0 {
						t.Fatalf("err=%v calls=%d", err, calls)
					}
				} else if err != nil || calls != 1 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
			})
		}
	}
}
