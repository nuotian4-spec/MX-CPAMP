package basispoints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func contractNamespace(namespace string) object {
	tools := make([]any, 0, 3)
	for _, name := range []string{"spawn_agent", "send_message", "list_agents"} {
		tools = append(tools, object{"type": "function", "name": name, "parameters": object{"type": "object"}})
	}
	return object{"type": "namespace", "name": namespace, "tools": tools}
}

func contractSource(namespace string, history []any) object {
	source := testSource()
	source["tools"] = []any{}
	source["input"] = append([]any{object{"type": "additional_tools", "tools": []any{
		contractNamespace(namespace),
		object{"type": "namespace", "name": "functions", "tools": []any{object{"type": "custom", "name": "exec"}}},
	}}}, history...)
	return source
}

// 只复现报告中的身份和条目数量，内容均为合成数据，不读取或执行采集正文。
func contractLongHistory() []any {
	names := []string{"spawn_agent", "spawn_agent", "send_message", "list_agents", "list_agents"}
	history := make([]any, 0, 42)
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("call_history_%02d", i)
		call := object{"type": "custom_tool_call", "call_id": id, "namespace": "functions", "name": "exec",
			"input": fmt.Sprintf("// 合成历史 %d\r\ntext(\"保留引号\");\n\t", i)}
		if i < len(names) {
			call = object{"type": "function_call", "call_id": id, "namespace": "collaboration", "name": names[i],
				"arguments": `{"target":"synthetic-worker","message":"合成历史\n保留内容","exact":9007199254740993}`}
		}
		history = append(history, call, object{"type": text(call["type"]) + "_output",
			"call_id": id, "output": fmt.Sprintf("recorded-result-%02d", i)})
	}
	return history
}

func assertContractHistory(t *testing.T, prepared object, history []any) {
	t.Helper()
	var items []object
	for _, raw := range mustTestValue[[]any](t, prepared["input"]) {
		item := mustTestValue[object](t, raw)
		if isTool(item) || text(item["type"]) == "function_call_output" {
			items = append(items, item)
		}
	}
	require.Len(t, items, 42)
	for i := 0; i < 21; i++ {
		call := mustTestValue[object](t, history[2*i])
		result := mustTestValue[object](t, history[2*i+1])
		require.Equal(t, call["call_id"], items[2*i]["call_id"])
		require.Equal(t, call["call_id"], items[2*i+1]["call_id"])
		require.Equal(t, result["output"], items[2*i+1]["output"])
		if i < 5 {
			envelope := historyCollisionEnvelope(t, items[2*i])
			require.Equal(t, "collaboration."+text(call["name"]), envelope["name"])
			var want object
			require.NoError(t, decode([]byte(text(call["arguments"])), &want))
			require.Equal(t, want, envelope["arguments"])
		} else {
			var outer object
			require.NoError(t, decode([]byte(text(items[2*i]["arguments"])), &outer))
			require.Equal(t, "codex2api.custom/functions.exec", outer["summary"])
			require.Equal(t, call["input"], outer["code"])
		}
	}
}

// 若目录外名字被推断授权、历史被重新执行或错误再次触发纠正，此回归必须失败。
func TestCatalogContractLongHistoryMismatchFailsClosed(t *testing.T) {
	for _, cached := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("cached=%t/stream=%t", cached, streaming), func(t *testing.T) {
				history := contractLongHistory()
				cache := new(ReplayCache)
				if cached {
					mustPrepare(t, contractSource("collaboration", history), "contract", cache)
					require.NotNil(t, cache.getForCall("contract", "call_history_00", history[0].(object)))
				}
				source := contractSource("collaboration-optimize", history)
				source["stream"] = streaming
				before, err := json.Marshal(source)
				require.NoError(t, err)
				prepared, bridge := mustPrepare(t, source, "contract", cache)
				require.True(t, bridge.hasToolHistory)
				assertContractHistory(t, prepared, history)
				after, err := json.Marshal(source)
				require.NoError(t, err)
				require.Equal(t, before, after, "原始客户端历史不应被改写")
				bad := nativeCall(object{"name": "collaboration.send_message",
					"arguments": object{"target": "PRIVATE_TARGET", "message": "PRIVATE_ARGUMENTS"}})
				wire := sse(object{"type": "response.output_item.done", "item": bad, "output_index": 0}) +
					sse(object{"type": "response.completed", "response": repairResponse("resp_mismatch", 10, 2, bad)})
				repairs := 0
				body := bridge.StreamWithRepairs(context.Background(), io.NopCloser(strings.NewReader(wire)),
					func(context.Context, object, error) (object, error) { repairs++; return nil, nil },
					func(context.Context) (io.ReadCloser, error) { repairs++; return nil, nil })
				events := repairEvents(t, body)
				require.Zero(t, repairs, "已有历史时不能增加模型请求或伪造执行结果")
				for _, event := range events {
					require.False(t, isToolEvent(text(event["type"])))
					item, _ := event["item"].(object)
					require.False(t, isTool(item), "历史和失败调用不得下发执行")
				}
				last := events[len(events)-1]
				require.Equal(t, "response.failed", last["type"])
				response := mustTestValue[object](t, last["response"])
				require.Equal(t, "resp_mismatch", response["id"])
				require.Empty(t, response["output"])
				detail := mustTestValue[object](t, response["error"])
				require.Equal(t, "basispoints_protocol_error", detail["code"])
				diagnostic := text(detail["message"])
				require.Contains(t, diagnostic, "namespace", "错误应指导核查完整工具身份，而不是诱导网络重试")
				require.Contains(t, diagnostic, "history", "错误应说明已有历史时的纠正保护")
				require.NotContains(t, diagnostic, "PRIVATE_")
				require.Nil(t, cache.get("contract", "call_native"))
			})
		}
	}
}

func TestCatalogContractExactNamesNeverInferAliases(t *testing.T) {
	for _, tc := range []struct {
		name       string
		namespaces []string
		target     string
		want       string
	}{
		{"current", []string{"collaboration-optimize"}, "collaboration-optimize.send_message", "collaboration-optimize"},
		{"legacy_explicit", []string{"collaboration"}, "collaboration.send_message", "collaboration"},
		{"both_legacy", []string{"collaboration", "collaboration-optimize"}, "collaboration.send_message", "collaboration"},
		{"both_current", []string{"collaboration", "collaboration-optimize"}, "collaboration-optimize.send_message", "collaboration-optimize"},
		{"legacy_undeclared", []string{"collaboration-optimize"}, "collaboration.send_message", ""},
		{"ambiguous_suffix", []string{"collaboration-optimize", "another-provider"}, "collaboration.send_message", ""},
		{"suffix_only", []string{"collaboration-optimize"}, "send_message", ""},
		{"case_mismatch", []string{"collaboration-optimize"}, "Collaboration-optimize.send_message", ""},
		{"stale_developer_example", []string{"collaboration-optimize"}, "functions.collaboration.send_message", ""},
	} {
		for _, direct := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/direct=%t", tc.name, direct), func(t *testing.T) {
				source := testSource()
				var tools []any
				for _, namespace := range tc.namespaces {
					tools = append(tools, contractNamespace(namespace))
				}
				source["tools"] = tools
				cache := new(ReplayCache)
				_, bridge := mustPrepare(t, source, "exact", cache)
				args := object{"target": "synthetic-worker", "message": "保留\"引号\"与换行\n", "exact": json.Number("9007199254740993")}
				native := nativeCall(object{"name": tc.target, "arguments": args})
				if direct {
					raw, err := json.Marshal(args)
					require.NoError(t, err)
					native["name"], native["arguments"] = tc.target, string(raw)
				}
				call, err := bridge.translateCall(native)
				if tc.want == "" {
					require.Error(t, err)
					require.Nil(t, call)
					require.Nil(t, cache.get("exact", "call_native"))
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.want, call["namespace"])
				require.Equal(t, "send_message", call["name"])
				require.Equal(t, "call_native", call["call_id"])
				var got object
				require.NoError(t, decode([]byte(text(call["arguments"])), &got))
				require.Equal(t, args, got)
			})
		}
	}
}

func TestCatalogContractMismatchRejectsWholeBatch(t *testing.T) {
	cache := new(ReplayCache)
	_, bridge := mustPrepare(t, contractSource("collaboration-optimize", contractLongHistory()), "atomic", cache)
	good := nativeCall(object{"name": "collaboration-optimize.send_message", "arguments": object{"message": "synthetic"}})
	good["id"], good["call_id"] = "fc_good", "call_good"
	bad := nativeCall(object{"name": "collaboration.send_message", "arguments": object{"message": "synthetic"}})
	response := repairResponse("resp_atomic", 10, 2, good, bad)
	before, err := json.Marshal(response)
	require.NoError(t, err)
	require.Error(t, bridge.translateResponse(response))
	after, err := json.Marshal(response)
	require.NoError(t, err)
	require.Equal(t, before, after, "目录错误不得产生部分转换")
	require.Nil(t, cache.get("atomic", "call_good"))
	require.Nil(t, cache.get("atomic", "call_native"))
}

func TestCatalogContractCachedHistoryDoesNotAuthorizeRemovedTool(t *testing.T) {
	replay, catalog := new(ReplayCache), new(CatalogCache)
	prepare := func(source object) *Bridge {
		t.Helper()
		raw, err := json.Marshal(source)
		require.NoError(t, err)
		_, bridge, err := PrepareWithCatalog(raw, "changed-catalog", replay, catalog)
		require.NoError(t, err)
		return bridge
	}
	source := testSource()
	source["tools"] = []any{contractNamespace("collaboration-optimize")}
	first := prepare(source)
	native := nativeCall(object{"name": "collaboration-optimize.send_message", "arguments": object{"message": "synthetic"}})
	call, err := first.translateCall(native)
	require.NoError(t, err)
	source["input"] = []any{call, object{"type": "function_call_output", "call_id": "call_native", "output": "recorded"}}
	source["tools"] = []any{}
	removed := prepare(source)
	require.True(t, removed.hasToolHistory)
	require.NotNil(t, replay.get("changed-catalog", "call_native"))
	_, err = removed.translateCall(native)
	require.Error(t, err, "历史及缓存不是授权来源")
	delete(source, "tools")
	inherited := prepare(source)
	_, err = inherited.translateCall(native)
	require.Error(t, err, "后续增量请求不得恢复已撤销工具")
}

func TestCatalogContractDiagnosticDoesNotEchoUntrustedTarget(t *testing.T) {
	_, bridge := mustPrepare(t, contractSource("collaboration-optimize", nil), "safe-diagnostic", nil)
	for _, target := range []string{"PRIVATE_TARGET", strings.Repeat("PRIVATE_TARGET\r\n", 1000)} {
		_, err := bridge.translateCall(nativeCall(object{"name": target, "arguments": object{"token": "PRIVATE_ARGUMENTS"}}))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "PRIVATE_")
		require.Less(t, len(err.Error()), 512)
		require.NotContains(t, err.Error(), "requests with tool history")
	}
}

func TestCatalogContractModelNotFoundRemainsIndependent(t *testing.T) {
	source := contractSource("collaboration-optimize", contractLongHistory())
	source["model"] = "gpt-6-astra"
	prepared, bridge := mustPrepare(t, source, "model-failure", new(ReplayCache))
	require.Equal(t, "gpt-6-astra", prepared["model"])
	wire := sse(object{"type": "response.failed", "response": object{
		"id": "resp_missing_model", "status": "failed", "output": []any{},
		"error": object{"code": "model_not_found", "message": "PRIVATE_UPSTREAM_ROUTE"},
	}})
	repairs := 0
	body := bridge.StreamWithRepairs(context.Background(), io.NopCloser(strings.NewReader(wire)),
		func(context.Context, object, error) (object, error) { repairs++; return nil, nil },
		func(context.Context) (io.ReadCloser, error) { repairs++; return nil, nil })
	events := repairEvents(t, body)
	require.Zero(t, repairs)
	last := events[len(events)-1]
	require.Equal(t, "response.failed", last["type"])
	detail := mustTestValue[object](t, mustTestValue[object](t, last["response"])["error"])
	require.Equal(t, "model_not_found", detail["code"])
	require.Equal(t, json.Number("404"), detail["status"])
	require.NotContains(t, text(detail["message"]), "tool catalog")
	require.NotContains(t, text(detail["message"]), "PRIVATE_")
}
