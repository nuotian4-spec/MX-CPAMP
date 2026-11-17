# Kilo 公开错误调查与修复方案

日期：2026-11-17。基线：`67445a35ba450468948447492b560c204afa7321`。
状态：方案已采用，本轮本地候选已通过S独立审查及V验收；生产配置及运行状态未修改。既有未提交改动保留；交付当前态见 `../evidence/kilo-public-errors-20261117/CURRENT.zh-CN.md`。

## 结论

1. 当前实现已对 Kilo 上游拒绝隐藏原始正文，但没有完整隐藏供应商标识。公开 `message` 和部分公开 `code` 明含 `Kilo` / `kilo_`。
2. 多种不同原因被公开合并为 `request_failed`。去掉品牌与保留可操作的失败类别，是同一公开错误边界的两个要求。
3. 第三方样例的中文“请求受限，请稍后重试。”未在本次核查的 CPA 源码中找到。样例缺少真实上游 HTTP 状态、机器码及关联记录，不能据此判断限流、额度耗尽、账号限制或认证失效，也不归因为本站故障。
4. BPS 输入兼容问题按用户要求搁置；本方案不涉及 BPS 二进制、授权或输入转换。

## 当前链路与证据

本节路径均相对仓库根；行号对应上述本地基线。本轮为静态调查，不代表生产复现或运行测试通过。

| 环节 | 已证事实 | 代码位置 |
| --- | --- | --- |
| 上游 HTTP / SSE 错误归一 | `KiloResponseError` 保留内部 code、type、真实 upstreamStatus 和 CPA status，不保留上游原文 | `components/cpa/internal/runtime/executor/helps/kilo_response.go:11-88` |
| 公开拒绝正文 | 普通失败为 `Kilo upstream response failed`；加密输入和413也有 Kilo 文案，413还含 `kilo_upstream_request_too_large` | 同文件 `30-37` |
| 本地请求预算 | 公开码为 `kilo_request_too_large` / `kilo_image_preparation_busy`，文案含 Kilo；`Error()` 直接复用 `PublicErrorBody()` | `components/cpa/internal/runtime/executor/helps/kilo_payload.go:35-57` |
| 传输故障 | 底层连接错误被替换成固定 `Kilo upstream connection failed`，缺少出口为固定 Kilo 文案；本处未证实泄漏底层 URL、代理凭据或原始网络错误 | `components/cpa/internal/runtime/executor/helps/kilo_runtime.go:392-411` |
| 协议 / 转换失败 | 部分压缩、坏帧、不完整流及转换失败文案含 Kilo，非 `KiloResponseError` 路径仍使用错误文本 | `components/cpa/internal/runtime/executor/helps/kilo_protocol.go:107-123,159-175,219-227`；`components/cpa/internal/runtime/executor/kilo_executor.go:580-595,644-650` |
| 公开呈现边界 | 调度后才取 `PublicErrorBody()`；包装保留原 cause；终态鉴权和直接响应有更高优先级 | `components/cpa/sdk/api/handlers/handlers_execution.go:385-434` |
| Responses 清理 | 对 Bearer、密钥字段及嵌套敏感键做清理；不是供应商名称隐藏规则 | `components/cpa/sdk/api/handlers/openai/openai_responses_handlers.go:1298-1439` |
| 事件序号 | `sequence_number` 取事件计数或原始序号，不表示重试次数 | 同文件 `1499-1521` |
| 内部限制状态 | 精确额度码才进入 Kilo 额度耗尽；真实403且明确 `account_restricted` 才进入账号限制 | `components/cpa/sdk/cliproxy/auth/kilo_recovery.go:85-97,144-177` |
| 现有回归用例 | 已断言原始上游私有正文不泄漏、失败不冒充完成、单次请求不重放；未把所有公开 Kilo 标识列入禁止项 | `components/cpa/sdk/cliproxy/kilo_native_fidelity_test.go:14-59,121-153` |

Kilo 专用访问密钥的 WebSocket 请求在升级前被拒绝，现有用例 `components/cpa/sdk/api/handlers/openai/kilo_scope_test.go:12-24` 对此有明确约束。本轮以 HTTP JSON 与 SSE 为主，不把通用 WebSocket 错误编码器自动算作已证实的 Kilo 可达链路，也不扩建 WebSocket 支持。

## 第三方样例的解释

```text
event: error
data: {"code":"request_failed","error":{"code":"request_failed","message":"请求受限，请稍后重试。","type":"api_error"},"message":"请求受限，请稍后重试。","param":null,"sequence_number":2,"type":"error"}
```

- 这是一帧失败事件。`request_failed` / `api_error` 信息粒度不足，中文文案不是原始原因的证据。
- 若经过当前错误归并，真实 HTTP 400以上的多种状态、HTTP200流内错误映射出的401/429、以及未识别事件的502都可能具有公开 `request_failed`。本次未确认第三方使用相同版本或相同包装链路。
- `param:null` 未提供字段定位；`sequence_number:2` 不是“重试两次”。
- 需要区分实际原因时，只需该请求已有的真实上游状态、机器码/type、是否已输出数据及关联请求ID；不需要令牌、完整提示词、账号密码或重新发真实模型请求。该请求不属于用户，不要求用户在自己的服务器找第三方日志。

授权、额度、访问策略及瞬时限流是不同的失败原因，应以该请求的真实状态和机器码区分，公开 `request_failed` 不足以反推其中哪一类。供应商的错误与计费说明见下列官方入口；这些链接不证明本仓库使用的适配通道或第三方事件与 Gateway 完全相同。

- [Gateway API Reference：Error codes](https://kilo.ai/docs/gateway/api-reference)
- [Gateway Usage & Billing：Rate limiting / Organization controls](https://kilo.ai/docs/gateway/usage-and-billing)

## 最小架构

```text
上游 HTTP / SSE / 传输结果
  → Kilo 内部错误事实（原机器码、真实状态、请求作用域、禁止重放）
  → 现有调度、账号限制和恢复逻辑（保持不变）
  → 调度后的公开错误呈现（固定文案 + 允许的通用码）
  → 现有 HTTP JSON / SSE 编码与终态处理

内部错误事实 → 现有错误事件/日志 + 现有 request_id → 已鉴权管理诊断
```

### 公开错误边界

- 复用既有 `PublicErrorBody()` 和调度后呈现入口，不添加网关、全局字符串替换器、独立日志数据库或新的维护框架。
- 固定公开文案和公开码，覆盖上游拒绝、本地请求预算、连接失败、协议/转换失败；在最终失败呈现处按实际通道/执行器归属应用，不依靠错误文本是否包含 Kilo 来猜归属。
- `message`、`code`、`type`、嵌套 `error` 与错误响应头使用相同规则；只允许协议所需字段，不透传上游任意错误对象或响应头。保留已经采用的终态鉴权优先级。
- 供应商身份只从普通客户端错误中隐藏；管理员仍能识别 Kilo 通道。模型正常输出、用户文字、正常模型标识不作全局替换。
- 内部 code/type/upstream_status、CPA status、`IsRequestScoped`、`NoReplay` 及原始 cause 保留。尤其应先解除 `KiloPayloadError.Error()` 与公开正文的耦合，避免改公开码时顺带改变内部错误语义。
- 复用现有请求ID做关联，不新造哈希、指纹或第二套诊断编号。若当前公开响应没有请求ID，沿用既有请求ID以响应头或受控字段提供关联；不编码账号或供应商信息。

### 公开分类建议

分类来自已识别的内部事实，不解析中文提示猜原因。此表是目标契约，不是当前已实现行为；公开映射不改变内部调度、冷却或重试规则。

| 事实 | 公开码示例 | 公开提示及行动 |
| --- | --- | --- |
| 明确瞬时限流；或真实429但无更明确额度含义 | `rate_limit_exceeded` | 请求频率受限，请稍后重试；不声称封号，不编造恢复时间 |
| 明确额度不足/用量上限码 | `upstream_quota_exhausted` | 服务额度不足，请联系管理员；不引导立即反复重试 |
| 明确认证失效 | 沿用既有 `upstream_authentication_required` 契约 | 上游授权失效，请联系管理员；不混淆为客户端密钥错误 |
| 真实403且明确账号限制码 | `upstream_access_restricted` | 服务访问受限，请联系管理员；不推断具体风控原因 |
| 只有普通403 / 402，无精确账号限制证据 | `upstream_access_denied` / `upstream_payment_required` | 分别提示访问或计费条件未满足，不写成已封号或本站欠费 |
| 请求过大 / 明确加密输入无效 | `request_too_large` / `invalid_encrypted_content` | 请求过大 / 输入校验失败；保留请求级、不重放行为 |
| 本地预处理容量不足 | `request_preparation_busy` | 请求处理繁忙，请稍后再试；与上游额度区分 |
| 连接失败 / 协议坏帧或流异常中断 | `upstream_connection_failed` / `upstream_response_invalid` | 服务连接失败 / 响应异常中断；已发送请求不自动重放 |
| 其它未知失败 | 保留 `request_failed` | 请求失败，请联系管理员并提供请求ID；不统一宣称“受限” |

本轮不新增自动重试。SSE 已开始时，使用既有失败事件形态并且只终止一次，不伪造成功完成，也不尝试改写已经发送的 HTTP 状态。

### 内部诊断

沿用已有错误事件和日志，仅补齐需要区分故障的字段：请求ID、通道、真实上游HTTP状态、内部机器码/type、公开类别、是否已开始输出。未知机器字段不原样作为公开消息；日志中的未知动态内容按既有日志清理规则处理。

现有错误事件队列受运行模式和快照条件约束（`components/cpa/sdk/cliproxy/auth/error_events.go:50-79,87-118,140-149`），因此不能以“有结构定义”宣称所有请求已落盘。采用方案后以合成用例验证该次错误确实能按请求ID关联，不要求保存原始正文。

## 实施与验收

分两段交付，不碰 BPS、数据库、账户身份/代理池或发布脚本。

1. **公开呈现收口**：P8 修改 Kilo 错误呈现及必要的调度后接线；优先在 `helps/kilo_response.go`、`helps/kilo_payload.go` 与既有 Kilo 失败出口修正；只有确有接线缺口才修改统一 handler。共享输出接线由一个执行者负责。
2. **定向契约验证**：另一 P8 独立审查公开输出和内部行为；以现有 `kilo_native_fidelity_test.go`、`public_execution_error_test.go`、Responses 错误专项用例为基础扩充，不重跑无关登录、部署或 BPS 套件。

| 必须检测的失败 | 验收条件 |
| --- | --- |
| 固定文案、公开码遗漏品牌 | 覆盖 HTTP错误、流首帧前失败、已开始流失败、本地413/503、连接/协议失败；公开错误无 Kilo / kilo_ /供应商URL/凭据/原始私有正文 |
| “脱敏”抹掉故障分类 | 瞬时限流、精确额度、认证、明确限制和未知失败分别命中预期类别；未知失败不冒充限流 |
| 呈现修改污染内部状态 | 修改前后内部 code/type/upstream_status、CPA status、请求作用域和NoReplay保持，精确限制仍只按原条件触发 |
| 流中失败被吞或重复重放 | 仅一个失败终态，无后续 completed；上游POST计数不增加；序号仍是事件序号 |
| 跨通道串改 | 一个 Codex 与一个 BPS 合成错误对照保持原行为；不执行真实账号请求 |
| 无法排障 | 同一合成请求的公开请求ID与内部结构化错误可对应，内部不记录秘密/原始提示词 |

执行时只运行本次新增/受影响的专项用例；代码变化后按 CPA 仓库规则执行一次编译。调查阶段未运行产品测试或构建，不把现有测试源码当作本轮运行通过证据。

当前责任人：用户确认后续入仓/发布授权；P9按授权组织执行。本地实施、S独立审查及V验收已完成。本地交付不等于生产完成；提交推送、发布、部署、服务重启或真实模型调用仍需本轮明确授权。没有数据库动作。原基线custom工具续接缺口及验证边界见本轮验收记录。
