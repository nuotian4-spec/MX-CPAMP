# CPA BPS 复审修复回执

- 基线：`aeba9ee7cd7429ec271f1ff1f4a006762f244483`。
- 产品提交：`4bca838c3e65be79dc5743d417571b99f6f5f55e`。
- 范围：`components/cpa/internal/runtime/executor/bps_executor*`、`components/cpa/internal/runtime/executor/helps/bps_*`、`components/cpa/sdk/cliproxy/service_{models,bps_test}.go`。

## 结果

1. 隐藏 reasoning 后，公开 `output_item/content_part/output_text` 事件的 `output_index` 与终态过滤后的 output 数组一致；`[reasoning(0), message(1)]` 对外仅有 message(0)。所有 response 快照同步隐藏 reasoning。
2. 仅有真实 completed SSE 或 JSON 终态时，补齐缺少的 item/content 建立、文本及完成事件；完整上游事件保留，已存在事件不重复。缺少 done 的 SSE 仅补缺少的完成事件；无真实终态的错误路径保持原样。
3. 请求在 BPS 业务规范化前进入既有 `ApplyRequestThinkingDoc`，模型后缀优先于正文；显式 BPS catalog 按用户配置模型交由同一 thinking 路径处理。BPS 转换为 `reasoning_effort` 后，payload 规则仍最后覆盖或删除，后续不恢复字段。

## 证据

| 检查 | 修复前 | 修复后 |
| --- | --- | --- |
| reasoning(0)/message(1) 输出索引 | `review-p2-index-red.log`，退出码 1 | `review-p2-green.log`，退出码 0 |
| Responses 终态 SSE/JSON 生命周期及缺 done | `review-p2-lifecycle-red.log`，退出码 1；完整 SSE 对照通过 | `review-p2-green.log`，退出码 0 |
| MODEL(high)、正文 low 与 payload 优先级 | `review-p2-thinking-red.log`，退出码 1 | `review-p2-green.log`，退出码 0 |
| 实际 catalog → Manager → BPS Executor 保留 reasoning 后缀 | 同一组 reasoning 修复范围 | `review-p2-green.log`，退出码 0 |
| 原 BPS 四请求路径、截断、Chat 文本、Codex payload 与 executor 绑定定向回归 | 复用原基线证据 | `review-p2-green.log`，退出码 0 |
| 基线至产品提交 diff-check | 不适用 | `review-p2-checks.log`，退出码 0 |

最终命令：

```text
go test ./internal/runtime/executor/helps ./internal/runtime/executor ./sdk/cliproxy -run '^TestBPS|^TestPayloadBarrierCodexImageFilter$|^TestEnsureExecutorsForAuth_CodexDoesNotReplaceInNormalMode$|^TestEnsureExecutorsForAuthWithMode_CodexForceReplace$' -count=1 -v
```

最终日志绑定产品提交，3 个包全部通过。索引、生命周期、reasoning 的失败日志分别保留其对应修复前的行为，未将旧失败写成最终失败。

本次无导入契约、鉴权或凭据存储改动，无 SQL/schema/migration。未执行真实登录、模型调用、生产操作或 push。B 线按固定产品提交负责最终 CPA Linux 构建及跨组件复验；本线未重复构建镜像或 Windows server。下一责任人为独立审查席与 B 线。
