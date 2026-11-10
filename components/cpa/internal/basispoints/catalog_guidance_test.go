package basispoints

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func catalogGuidanceCount(items []any) int {
	count := 0
	for _, raw := range items {
		item, _ := raw.(object)
		content, _ := item["content"].([]any)
		for _, value := range content {
			part, _ := value.(object)
			if text(item["role"]) == "developer" && strings.HasPrefix(text(part["text"]), "Current client tool identity:") {
				count++
			}
		}
	}
	return count
}

// 只跳过桥接追加的末尾目录提示，保留原历史及压缩触发项的相对次序。
func historyWithoutCatalogReminder(t *testing.T, prepared object) []any {
	t.Helper()
	items := mustTestValue[[]any](t, prepared["input"])
	index := len(items) - 1
	if index >= 0 {
		if last, _ := items[index].(object); text(last["type"]) == "compaction_trigger" {
			index--
		}
	}
	if index < 0 || catalogGuidanceCount([]any{items[index]}) == 0 {
		return items
	}
	reminder := mustTestValue[object](t, items[index])
	require.Equal(t, "message", reminder["type"])
	require.Equal(t, "developer", reminder["role"])
	require.Equal(t, 1, catalogGuidanceCount(items))
	history := append([]any(nil), items[:index]...)
	return append(history, items[index+1:]...)
}

func assertCatalogGuidanceAtTail(t *testing.T, prepared object, legacy object, compaction bool) {
	t.Helper()
	items := mustTestValue[[]any](t, prepared["input"])
	index := len(items) - 1
	if compaction {
		require.Equal(t, "compaction_trigger", mustTestValue[object](t, items[index])["type"])
		index--
	}
	last := mustTestValue[object](t, items[index])
	require.Equal(t, "developer", last["role"])
	content := mustTestValue[[]any](t, last["content"])
	require.Len(t, content, 1)
	guidance := text(mustTestValue[object](t, content[0])["text"])
	require.True(t, strings.HasPrefix(guidance, "Current client tool identity:"), "当前目录约束必须位于旧示例之后")
	require.Contains(t, guidance, "exact namespace and name")
	require.Contains(t, guidance, "not authorization")
	require.Contains(t, guidance, "Do not infer or abbreviate")
	require.Contains(t, guidance, "without executing historical operations again")
	require.Equal(t, legacy, items[index-1], "不得重写原客户端 developer 消息")
	require.Equal(t, 1, catalogGuidanceCount(items))
}

// 若后置旧示例再次成为最后的工具指令，或插入动作破坏历史与压缩布局，此测试必须失败。
func TestCatalogGuidanceFollowsHistoryAndLegacyExamples(t *testing.T) {
	for _, cached := range []bool{false, true} {
		for _, compaction := range []bool{false, true} {
			t.Run(fmt.Sprintf("cached=%t/compaction=%t", cached, compaction), func(t *testing.T) {
				history := contractLongHistory()
				cache := new(ReplayCache)
				if cached {
					mustPrepare(t, contractSource("collaboration", history), "guidance", cache)
				}
				legacy := message("developer", "Historical example: functions.collaboration.spawn_agent and collaboration.send_message.")
				source := contractSource("collaboration-optimize", history)
				input := mustTestValue[[]any](t, source["input"])
				if compaction {
					input = append(input, object{"type": "compaction_trigger"})
				}
				source["input"] = append(input, legacy)
				before, err := json.Marshal(source)
				require.NoError(t, err)
				prepared, bridge := mustPrepare(t, source, "guidance", cache)
				assertCatalogGuidanceAtTail(t, prepared, legacy, compaction)
				assertContractHistory(t, prepared, history)
				after, err := json.Marshal(source)
				require.NoError(t, err)
				require.Equal(t, before, after)
				valid := nativeCall(object{"name": "collaboration-optimize.send_message", "arguments": object{"message": "synthetic"}})
				call, err := bridge.translateCall(valid)
				require.NoError(t, err)
				require.Equal(t, "collaboration-optimize", call["namespace"])
				_, err = bridge.translateCall(nativeCall(object{"name": "collaboration.send_message", "arguments": object{}}))
				require.Error(t, err, "指导消息不是别名授权")
			})
		}
	}
}

func TestCatalogGuidanceRequiresHistoryAndCallableCatalog(t *testing.T) {
	for _, mode := range []string{"no_history", "choice_none", "choice_none_top_level", "empty_catalog"} {
		t.Run(mode, func(t *testing.T) {
			legacy := message("developer", "Original developer message.")
			source := contractSource("collaboration-optimize", contractLongHistory())
			switch mode {
			case "no_history":
				source = contractSource("collaboration-optimize", nil)
			case "choice_none", "choice_none_top_level":
				source["tool_choice"] = "none"
				if mode == "choice_none_top_level" {
					source["tools"] = []any{contractNamespace("collaboration-optimize")}
				}
			case "empty_catalog":
				source["input"] = contractLongHistory()
			}
			source["input"] = append(mustTestValue[[]any](t, source["input"]), legacy)
			prepared, bridge := mustPrepare(t, source, "inactive-guidance", nil)
			items := mustTestValue[[]any](t, prepared["input"])
			require.Zero(t, catalogGuidanceCount(items))
			require.Equal(t, legacy, items[len(items)-1])
			if strings.HasPrefix(mode, "choice_none") {
				require.Empty(t, bridge.tools, "显式关闭工具时，顶层及追加目录均不构成调用授权")
				protocolFound := false
				for _, raw := range items {
					item, _ := raw.(object)
					if text(item["role"]) != "developer" {
						continue
					}
					content, _ := item["content"].([]any)
					for _, value := range content {
						part, _ := value.(object)
						protocolFound = protocolFound || strings.Contains(text(part["text"]), "Do not call Excel, Office, workbook or connector tools")
					}
				}
				require.True(t, protocolFound, "显式关闭工具时应保留禁止调用的协议")
			}
		})
	}
}

func TestCatalogGuidanceReprepareKeepsOneReminderAndStructuredOutput(t *testing.T) {
	legacy := message("developer", "Historical example: collaboration.send_message.")
	source := contractSource("collaboration-optimize", contractLongHistory())
	source["input"] = append(mustTestValue[[]any](t, source["input"]), legacy, object{"type": "compaction_trigger"})
	source["text"] = object{"format": testStructuredFormat()}
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	body, bridge, err := PrepareWithCatalog(raw, "reprepare-guidance", new(ReplayCache), new(CatalogCache))
	require.NoError(t, err)
	for iteration := 0; iteration < 3; iteration++ {
		var prepared object
		require.NoError(t, decode(body, &prepared))
		assertCatalogGuidanceAtTail(t, prepared, legacy, true)
		require.Contains(t, string(body), "exactly one JSON value")
		require.NotNil(t, bridge.structured)
		require.NoError(t, bridge.structured.validate(object{"output": []any{structuredMessage(`{"answer":9007199254740993}`)}}))
		require.Error(t, bridge.structured.validate(object{"output": []any{structuredMessage(`{"answer":1}`)}}))
		reprepared, next, err := bridge.Reprepare(raw)
		require.NoError(t, err)
		require.JSONEq(t, string(body), string(reprepared), "重准备不能累积指导消息或改写请求元数据")
		body, bridge = reprepared, next
	}
}
