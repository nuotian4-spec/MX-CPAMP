# CPA 附件分段观测独立审查

日期：2026-11-17。结论：本轮限定范围内，静态审查通过，无剩余阻塞项。

## 范围与基线

- 工作树：C:/Users/子羽/.codex/worktrees/kilo-latency-observation-20261117/MX-CPAMP。
- HEAD：e9196e142dd685eb8534a6f851b3564d70700b12；审查对象为其上的本地未提交候选。
- 产品范围：11 个 Go 路径；只增加本地 Debug 分段观测，不实施性能行为修复。
- HTTP 生命周期入口仅 Chat/Responses；其他协议不在本轮覆盖声明内。
- 产品及专项测试唯一写入者：central_admission_audit；独立审查：attachment_path_audit。
- 不改生产、调度策略、并发限额、上传并行度、超时、计费或数据库；未提交、未发布。
- 本记录限定于独立静态审查；最终文档收口仅同步三份附件调查/观测文档，保留两份 KILO-LATENCY 交付文档修改。

## 实际接线与合同

| 边界 | 源码事实与适用范围 |
| --- | --- |
| 外部生命周期 | sdk/api/handlers/request_latency.go:18-40 使用既有 Debug 开关；Kilo 路由排除，已有上下文复用，结束时输出摘要。 |
| 路由 | handlers_execution.go、handlers_stream.go 分别标识直接插件与凭证执行路径；不能把两者统称为经过当前层 AuthManager。 |
| 嵌套与 Home | internal/requestlatency/latency.go 的 WithRoute 保留外部路由，嵌套调用单独计数；Home/local/mixed 与未观测分开。 |
| 执行者分类 | adapters_executors.go 与 bps_executor.go 在实际入口提供固定类别计数；不把任意插件 ID 写入摘要。 |
| 中央准入 | credential_concurrency.go 分别记录选择锁等待与凭证容量等待；两者不是同一排队时长。 |
| 插件边界 | adapters_executors.go 在原消费者中识别宿主翻译输出；plugin_call_return 与 plugin_first_output 独立，函数返回不冒充首输出。 |
| 内置 BPS | bps_executor.go 与 helps/bps_images.go 记录内置准备、上传阶段；不外推为闭源 cpa-bps 的内部上传耗时。 |
| HTTP bridge | sdk/pluginapi/types.go:1035-1045 的 HTTPRequest 没有上传/推理阶段合同；本轮不靠 URL、正文或路径猜阶段，内部归因保持 unknown。 |

## 静态验收结果

- payload、流式消费者拓扑及插件 ABI 没有本轮观测引入的协议修改；没有增加上传、重试或调度动作。
- Debug 未开启时不创建观测状态和输出缓存，不增加大输入正文扫描；下游观测入口对空状态直接返回。
- State.Finish 在 ended 状态下返回空结果，外部请求收口一次；嵌套调用共享状态，不自行结束外部摘要。
- request_latency.go:49-59 原样返回 Write 的字节数及错误，记录错误与短写，首输出只观察完整成功写入。
- request_latency.go:64-70 显式透传 FlushError 并提供 Unwrap，保留底层刷新错误链。
- latency.go:200-202 仅在 WithRoute 已确认后输出 execution_input_bytes；其含义为执行入口 payload 长度，不是公网原始请求字节数。
- latency.go:208-223 只输出已观测首输出、阶段与嵌套计数；未观测字段缺省，不伪造零耗时或零字节。
- 摘要由固定阶段、类别、计数、耗时、结果及既有请求关联号组成；不记录正文、附件名、凭证、URL、代理地址或原始错误文本。

## 已知边界

- server_total_ms 从观测上下文创建算到 handler 收口，不是从公网接收开始的纯端到端时延。
- host_first_output_ms 是宿主成功 Write 中可识别输出的时间，不证明成功 Flush 或客户端收到；plugin_first_output 是宿主消费者/翻译边界，不是插件内部首网络字节。
- 输出识别缓冲上限为 1 MiB；超大 SSE 批次或未支持内容形态可能没有首输出字段，不能将缺省解释成零或立刻完成。
- 阶段可能重叠或累计，例如准备包含上传、插件调用包含序列化；不能将全部阶段简单相加当作总时长。
- 插件自带网络时，宿主调用耗时不能拆成已确认的上传、模型推理或插件自有队列时间。
- 用户于 2026-11-17 当前确认生产 cpa-bps“启用代理池”已关闭；来源为用户确认，非独立读取生产，本轮不再将该队列列为优先嫌疑。

## 证据与收口

- 本审查复用已读接线和直接依赖，只复核最终修正；未运行测试、构建、网络请求或生产诊断。
- 执行者最终回执：新增 11 个测试函数、23 个叶场景，以及直接受影响 Kilo 2 个函数、7 个叶场景全部通过；pluginhost/helps 定向结果为 0.215/0.251 秒、exit 0（会话 32337）。命令与完整输入清单见 CPA-ATTACHMENT-OBSERVATION-RECEIPT-20261117.zh-CN.md；此处引用权威回执，未独立复跑。
- 主会话回执：Windows go1.26.10，GOTOOLCHAIN=local / GOPROXY=off / GOSUMDB=off，go build -o NUL ./cmd/server 返回 0；tracked diff-check 与 8 个新增 Go/测试文件空白检查均为 0。独立审查未复跑。
- 静态通过与离线专项通过只证明观测候选符合边界，不证明截图根因、生产阶段分布或实际性能改善。

下一责任人：发布执行者按用户本轮批准完成受控入仓与固定版本发布，主会话核验发布结果；生产部署另行确认。

## 发布接线补证审查（2026-11-17）

结论：PASS，放行本次合同增量。

- 组件源提交：e3c643fb38ed39593e8dc4b7e17f30daa2143101；components 树：a56789000310960b7fbcfd7bc99b45a2fbeec3b1。
- 完整 source_files 与该提交精确一致，共 4676 项；相对旧合同仅 9 修改、8 新增，无删除，恰为已审 11 产品与 6 测试，Manager 无差异。
- schema、kind、source_roots、五组 test_runs、coverage、limitations 及旧 lineage 保持；production-wiring.evidence.json 与 capability_probe.py 未改，历史证据身份和精确映射门禁保留。
- 本轮观测不改变普通启动、payload、插件 ABI、流式消费与并发合同；既有专项、静态审查及构建证据足够支撑该增量，未重复测试或构建。
- 下一责任人：p8_attachment_release 按本真实结论更新合同 PASS/source_mapping_reviewed=true，执行两项 validator 与 diff-check 后精确提交、继续获准发布；不代表生产性能验收。
