# CPA BPS 首个 delta 生命周期修复

- 基线：`e9fb547f238a687e332ee1bedd81b937ab26c489`。
- 产品提交：`5c90a92eef27f450c7080bc8735c88d873fc8915`。
- 改动仅为 `helps/bps_stream.go` 与直接 `bps_executor_test.go`。

首次上游文本 delta 在转发前，按实际 `item_id/output_index/content_index` 补齐缺少的 message/content 建立事件。既有建立事件沿原记录去重；终态仍只补缺少的文本与完成事件。

原有 `TestBPSExecutorTextProtocolsAndFinalBarrier` 上游 fixture 保持不变，新增原生 Responses 事件顺序、消息 identity、content 坐标及完整事件链断言。修复前记录为 `created → delta`，退出码 1；修复后完整顺序通过。

```text
go test ./internal/runtime/executor/helps ./internal/runtime/executor -run '^TestBPS|^TestPayloadBarrierCodexImageFilter$' -count=1 -v
```

最终退出码 0：helps 0.158s，executor 0.256s。覆盖首 delta、完整 SSE 无重复、仅终态 SSE、JSON 终态、缺 done 补齐及原有 BPS/Codex payload 定向回归。

- 失败证据：`review-delta-red.log`。
- 最终通过证据：`review-delta-green.log`，绑定产品提交。
- diff-check：`review-delta-checks.log`，退出码 0。

无契约、鉴权、数据库或其他产品改动；未构建镜像或 Windows server，未 push、登录或调用模型。产品源码已冻结；下一责任人为独立审查席及 B 线固定版本 Linux 构建与跨组件复验。
