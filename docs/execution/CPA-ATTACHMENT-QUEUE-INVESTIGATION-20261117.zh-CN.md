# CPA 图片与附件排队调查及拟议方案

日期：2026-11-17。状态：调查方案已采纳，后续最小本地观测候选完成；截图慢请求尚未归因。当前输入、验证和交付状态以 CPA-ATTACHMENT-OBSERVATION-RECEIPT-20261117.zh-CN.md 为准。

## 1. 范围与基线

- 用户确认截图来自 cpa-bps，同时要求检查其他通道与 CPA 中央调度。
- 工作树：C:/Users/子羽/.codex/worktrees/kilo-latency-observation-20261117/MX-CPAMP。
- 源码 HEAD：e9196e142dd685eb8534a6f851b3564d70700b12；RC2 发布源：de91ff230d071e4f97b705593d8f49167796239e。
- RC2 生产更新状态沿用用户服务器回执；本次没有生产配置、真实请求或性能样本。
- 调查阶段仅新增本文，未修改产品、GLOSSARY、ADR 或数据库；后续获准的观测候选另见执行回执。原有两份 KILO-LATENCY 文档修改保留。
- 除另注外，源码路径以 components/cpa/ 为根，行号对应本次 HEAD；静态行为、插件声明与生产实测分别标识。

## 2. 确定发现与适用边界

### 2.1 插件自有代理池：当前已关闭

用户提供的 0.5.62-dev 隔离插件 GET /plugins 回执，其 config_fields 声明：
- session_proxy_pool_enabled 默认 false；启用后，每代理最多 5 个占用，满额排队最长 10 分钟。
- 完整会话标识保持会话代理；否则请求结束释放。
- session_proxy_idle_minutes 默认 30，范围 1–1440；执行中的占用不释放。
这说明该隔离版本声明了插件自身等待机制，不证明生产启用或截图命中；“5 个占用”不等于“5 并发生成”，也不是中央 auth.ID 默认 20 并发限额。
2026-11-17 用户确认管理页“启用代理池”已关闭。按该配置排除这条开关控制的排队路径，不再调查代理池容量或调整参数；该状态来源为用户确认，非本轮独立读取生产。

### 2.2 直接插件与内置 BPS 是不同路径

- 公共 JSON 入口先读取整 body：sdk/api/handlers/request_body.go:23–45；路由注册见 internal/api/server_routes.go:65–93。
- 流式 routeDecision.ExecutorPluginID 非空时，本层直接进入 host.ExecutePluginExecutorStream，没有先调用 AuthManager：sdk/api/handlers/handlers_stream.go:87–134、363–365；普通分支调用 AuthManager 在 425–426。
- 插件是否回调宿主模型执行、重新进入准入，需看 actual route 和 callback 元数据，不能从通道名称推断。
- cpa-bps ID 映射为 bps 通道；宿主适配器准备后调用插件，不走内置 PrepareBPSImages：internal/pluginhost/channel_scope.go:23–30；internal/pluginhost/adapters_executors.go:516–620、991–1001、1061–1084。
- 可见交付包提供 cpa-bps.so，本文未取得插件内部源码：仓库 docs/execution/BPS-PLUGIN-UPDATE-20261116.zh-CN.md:9–11。

### 2.3 中央选择锁与容量等待

- concurrencyPickMu 持锁调用 pickNextMixed，再预留凭证/出口容量，返回 lease 前释放：sdk/cliproxy/auth/credential_concurrency.go:331、406、425、435、467。
- 启用 Scheduler 插件时，pickNextMixed 进入 legacy 分支同步调用 pickViaPluginScheduler：sdk/cliproxy/auth/conductor_selection.go:2793、2867–2868。慢 Scheduler 可阻塞同一 Manager 的其他通道选择；生产是否启用、截图是否经过此路尚未证实。
- 该锁不覆盖整段模型生成；容量等待在解锁后发生，因此不是所有推理串行。
- 中央默认每凭证并发 20，可配置；共享等待者上限 1000，容量等待累计 30 秒，广播唤醒而非 FIFO：sdk/cliproxy/auth/credential_concurrency.go:19–22、66–69、230–306。
- 30 秒不包含选择锁等待、获准后附件准备和生成，不是整请求总时限。

### 2.4 内置 BPS 附件：占槽、串行与去重

- 内置路径先获得 auth.ID 槽再进入 PrepareBPSImages，准备期间占该凭证槽：sdk/cliproxy/auth/credential_concurrency.go:415–470；internal/runtime/executor/bps_executor.go:166–181。直接插件及其回调另核。
- NativeImages.Upload 按图顺序同步上传；每次上传建立 60 秒 context，多图耗时可累计：internal/basispoints/attachments.go:222–236；internal/runtime/executor/helps/bps_images.go:186–207。
- 同请求 digest 去重；非空 scope 启用跨请求缓存和同 key flight 等待，缓存最多 512 条、30 分钟；active 达 32 立即繁忙，上传前已解锁：internal/basispoints/attachments.go:204–310。
- 工具截图保留 inline，HTTPS/file_id 不进入 inline 上传；检查做流式解码哈希和图片头检查，上传时再流式解码：internal/basispoints/attachments.go:33–48、105–170。
- relay 模式对同 root 的 SetPublicOrigin + Rewrite 持 host 锁，存在串行临界区，耗时未测：internal/runtime/executor/helps/bps_images.go:143–158。以上不证明 cpa-bps 插件内部采用相同实现。

### 2.5 复制、其他附件入口与 Kilo

- 插件请求分别克隆 OriginalRequest/Payload（[]byte），RPC json.Marshal 按 []byte 编码语义再编码为 base64，Linux ABI 再 C.CBytes；可见分配/复制成本，尚无耗时占比：internal/pluginhost/adapters_executors.go:1511–1528；sdk/pluginapi/types.go:1128–1133；internal/pluginhost/rpc_client.go:268–279、360–363；internal/pluginhost/loader_unix.go:260–268。
- Kilo 仅超过 20 MiB 且条件满足才准备图片候选，最多 4 张串行压缩；全局 2 槽 select/default 满额立即 busy，不等槽：internal/runtime/executor/helps/kilo_payload.go:19–22、97–150、252–257；internal/runtime/executor/helps/kilo_image.go:28–112。
- Kilo 压缩器不处理 input_file/file_data/file_id；Chat→Codex 只是保留文件字段，不是统一下载器：internal/runtime/executor/helps/kilo_payload.go:63–94；internal/translator/codex/openai/chat-completions/codex_openai_request.go:597–620。
- /images/edits multipart 逐文件 ReadAll 后转 data URL；内置 BPS 拒绝 input_file，插件能力另核：sdk/api/handlers/openai/openai_images_handlers.go:635–659、888–965；internal/basispoints/content.go:17–43。
- 宿主普通 HTTP 分支使用可复用 transport；新 http.Client 不等于新连接，特殊 wire profile/插件自带网络另核：internal/pluginhost/http_bridge.go:218–223；internal/runtime/executor/helps/proxy_helpers.go:36–71。
- 其他 provider 重试可叠加耗时；BPS 有专门阻断，不能统一套用通用次数：sdk/api/handlers/handlers_stream.go:599、629；sdk/cliproxy/auth/conductor_stream.go:564、657；sdk/cliproxy/auth/bps_failover.go:98–99。

## 3. 截图尚未归因与指标口径

- 直接插件路径在 host 调用前启动 reporter TTFT：sdk/api/handlers/handlers_stream.go:128–134。有 auth 的适配器在 prepareExecutorCall 后、请求克隆/RPC 前启动：internal/pluginhost/adapters_executors.go:1037–1044、1061–1084。
- reporter 按首个可识别 token 事件记录，并保留首包及无 token 回退口径：internal/runtime/executor/helps/usage_helpers.go:441–449、479–513；internal/runtime/executor/helps/plugin_executor_usage.go:100–115。
- 插件准备、上传和自有等待可能进入该 TTFT；它不必然从公网接收开始，也不是纯模型时间。plugin_call 总时长不是 upload 时长。
- CPAMP 本地平均 TPS 使用 outputTokens/latency_ms，但截图标签尚未完全对应：仓库 components/cpamp/apps/web/src/features/monitoring/model/eventRows.ts:21–29、164–166。不据此解释截图精确字段。

尚缺同一请求的 actual route、生产插件版本、Scheduler 启用状态及阶段时间；当前没有“中央附件队列导致截图慢请求”的实测结论。代理池开关已由用户确认关闭。

## 4. 分层拟议架构：三步最小方案

### 第一步：确认路由与已有等待机制

确认直接插件、宿主凭证路径及回调关系、Scheduler 注册。当前代理池已关闭，不再围绕其占用排查。仅记录可从现有元数据取得的输入形式、总字节、请求关联号、重试与容量计数，不额外扫描附件正文，不采集正文/密钥。

### 第二步：分离可见阶段，保留插件边界

区分 ingress/读取请求、selection_lock_wait、credential_wait、序列化/ABI、plugin_call、可得的 upload/upstream、first_output 与下游交付。复用现有计时组件，Kilo 采样仍保持暂停；不把宿主总时长标成插件上传。
现有 host HTTP bridge 没有上传/推理阶段合同，本轮保持 unknown，不靠 URL 或正文猜阶段；自带网络时先测 host 边界，内部拆分与并行需插件作者支持。

### 第三步：按已证瓶颈做局部调整

- 中央凭证路径拆分附件准备与推理容量时，须保持凭证/会话绑定、默认 20 并发及实际配置、供应商真实请求总预算；不双倍配额，也不先传完再无限排队。插件自有代理占用规则单独处理，不套用中央限额。
- 上传并行限定请求内/全局并发，保持结果顺序、同 scope 去重、共享调用方 deadline 和取消清理；新增超时另按现有网络规则确认。
- Scheduler 改为锁外 Pick、锁内复核资格/配置/容量并原子预留；冲突有限重选，取消和回滚释放预留，防止超售/泄漏。生产未启用时不作为截图确定修复。
- 先测分配/序列化收益，再做局部减拷贝；保留既有二进制兼容，不直接删除 []byte 字段或改 ABI。

## 5. 实施优先级

P0：actual route + 生产插件版本/Scheduler 配置；P1：最小阶段元数据；P2：只修已证等待/串行/选择锁边界；P3：有数据后再优化复制。
用户已采纳方案。最小本地观测候选和受影响专项验证已完成；未改调度、限额或上传行为，未提交、发布或部署。执行与验证状态见 CPA-ATTACHMENT-OBSERVATION-RECEIPT-20261117.zh-CN.md；本文件后续性能调整仍为拟议方案。

## 6. 后续性能调整的最小验证与验收边界

| 待区分机制 | 最小离线入口 | 结果改变的下一步 |
| --- | --- | --- |
| 慢上传占槽 | 假上传回调受通道控制，同凭证文本请求并行 | 成立才评估受总预算约束的拆槽 |
| 多图串行累计 | NativeImages.Upload 输入不同合成图片，记录假回调起止 | 比较有界并行，验证原结果顺序 |
| 跨通道干扰 | 隔离同凭证/不同凭证/不同 provider，记录容量与选锁等待 | 区分凭证饱和、共享等待上限和全局锁 |
| 慢 Scheduler 持锁 | 假 Pick 可控阻塞，另一 provider 同时选择 | 验证锁外 Pick、锁内复核预留 |
| 大 body 复制 | 假 pluginClient.Call 固定回执，只变化输入大小 | 测耗时/分配量后决定兼容优化 |

当前代理池关闭，不执行满额或会话释放专项。闭源插件内部上传仍需其可信分段信息；宿主模拟不证明插件内部机制。
专项检查取消释放、不超售、结果顺序、同 scope 去重和未受影响路径；等价输入只保留一次权威证据，不复跑既有完整测试。
最终以真实同条件阶段分布、错误率、容量占用和首正文确认收益，不预设改善百分比；离线机制证据与生产性能验收分开。
调查阶段未执行上述拟议性能验证、真实请求、产品改动或提交；后续观测候选已有独立专项与构建证据，见执行回执，不与本文历史调查范围混同。两份原有 latency 文档修改保持不变；下一责任人为主会话，受控入仓和固定版本发布另按授权执行。
