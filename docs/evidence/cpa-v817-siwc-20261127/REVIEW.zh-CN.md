# CPA v8.1.7 独立审查

日期：2026-11-27（Asia/Shanghai）。审查席：cpa_v817_review。结论：PASS，本地升级候选放行；五个历史基线失败与六个跳过入口保留。独审测试/构建重跑为0。

## 输入与范围

- 本站基线：`b9ea2dc4c6bb377d31143970a6df1084297e4b09`，包含 CPA v8.0.80 整合及凭证并发持久化修复。
- 上游共同基线：v8.0.80 / `f5c5681a8361c9da53e93c60b6507816eee7468b`。
- 上游目标：v8.1.7 / `47f18e4d82bd72f7d2b5ecf5ede93e4837a4625e`。
- 唯一候选：`C:/Users/子羽/.codex/worktrees/cpa-core-v817-20261127/MX-CPAMP`。
- 最终组件候选tree：`8e5f0df93e564220a34435a66fb6c72314e28cc6`；CPA tree：`8501bb9d2095cb9e7bc71b6bc008aa097b6670cc`。3223个导出CPA文件与实际候选按二进制原字节、文本LF归一比较一致。
- 三方输入清单：`C:/Users/子羽/.codex/artifacts/cpa-core-v817-20261127/THREE-WAY-FACTS.json`。文本按 LF 比较，原始归档保留。上游真实变化679路径（434修改、241新增、4删除），定制交集63路径（45自动合并、18需人工整合）。

## 本轮审查接缝

审查以本轮真实差异和定制交集为准：配置快照与CAS写入需同时保留Kilo写锁、敏感值过滤和通道Key约束；插件调度重配置、热重载和Home本地执行接入需保留可信通道限制及本站维护状态；请求取消、受限恢复和调度流程需保留BPS/Kilo NoReplay、并发租约及并发配置持久化。模型发现、epoch和凭证版本关联保持原身份约束。

普通启动及能力探测入口、准确的发布接线源码映射、依赖升级后的编译与受影响专项属于本轮验收；不将同一HEAD当作相同输入，也不以改版本显示替代完整上游整合。

## 已核对的整合接缝

- 主实施12个冲突文件已提交可审版本。`ConfigV8`保留Kilo配置锁、脱敏、秘密值保留、Home并发归属与新增Key范围校验，并以同一`onDisk`快照完成CAS；配置加载拆分后的`parse.go`仍执行BPS协议校验、并发默认值与观测配置校验。
- 插件Execute/Stream/Count入口保留通道校验，新Home本地执行授权没有替代本站`pluginChannelAllowed`与`checkChannelScope`。BPS Fast策略仍在payload规则前应用、规则后验证；Responses到Chat的终态校验与取消链保留。
- WebSocket并发租约仍在原生命周期位置，上游新增create/steer读期限管理和waiting-parent清理钩子已合入。BPS/Kilo模型分支仍保留，排除与别名处理跟随上游统一入口。
- SIWC绑定、摘要和Linux合同在v8.1.7保持；Windows新增续期就绪门控不构成界面入口缺失原因。结论及接入方案见`SIWC-DIAGNOSIS.zh-CN.md`。

- auth六文件冻结版已读：`Modify`要求落盘成功后发布，保留上游`strictPrepareVersion/baseSuperseded`；流派发保留permission复核、BPS受限切号预算与Kilo NoReplay。`VerifyInference`沿用上游版本围栏，叠加generation准入、BPS/Kilo禁止刷新重发、取消后typed拒绝记账及源流排空。
- 本站`execution_model_snapshot.go`已按上游`nextModelPoolOffset(*Auth,key,size)`新合同传入所选凭证，池轮转继续受账号与注册epoch约束。

- 取消处理区分调用方完成与源流退出。`VerifyInference`及时返回取消，由后台排空持有原generation租约，前台清空release所有权；普通取消不写失败，typed BPS拒绝与Kilo NoReplay仍记账。普通wrapper在无本地租约时异步排空；本地Mixed与Antigravity credits通过`executeStreamWithModelPool`共同入口的`!ephemeralResult && !m.HomeEnabled()`标记保持源流关闭后释放租约，Home不误标。
- 独立逐项比较上游679路径与实际候选：612个直接取上游路径仅`server_id_test.go`有已审的本站维护锁fixture适配，4个删除已执行；本站独有文件均保留。额外本站适配为`channel_scope_test.go`、`credential_concurrency_test.go`、`execution_model_snapshot.go`与`kilo_recovery_test.go`。

上述接缝与最终候选对应，新增取消验证分别覆盖调用方快速返回和源流退出后释放租约。

## 统一验证与来源映射

独审只读取统一Linux日志和实际成品回执。按包/根测试的最新有效终态，1796通过、5失败、6跳过；`FINAL-TEST-RESULTS.json`的1807项与14份对应原始日志逐项核对，状态差异为0。配置/代理、三翻译包、Codex授权、管理、Handler、插件宿主、executor与模型注册专项通过，完整命令及边界见`VALIDATION.zh-CN.md`。

取消专项`linux-auth-cancellation-final.jsonl`的9个根测试全部通过，覆盖上游stall取消、Verify取消无失败惩罚、generation准入/source退出租约与BPS typed终态；`linux-credits-cancellation-final.jsonl`的实际credits调用边界通过，取消后active仍为1，source关闭后归0。`linux-config-nonroot.log`和`linux-codex-nonroot.log`补齐4个权限根测试，均通过且无skip。

最终Linux/amd64、Go1.26.10、CGO=1构建退出0，仅构建一次，`BUILD-RESULT.json`绑定上述CPA tree。`network=none`下启动真实`CLIProxyAPI-final`，成品cpa-bps库登记`0.5.86-dev`，`registered=true`、`effective_enabled=true`；插件目录及许可GET均200，日志含正常卸载。证据为`linux-plugin-probe.log`、`linux-plugin-registry.json`、`linux-plugin-license.json`。许可返回`active=false / license_activation_required`。

实际`deploy/production-wiring.contract.json`的5114项mode/blob与最终tree精确一致，历史lineage和`production-wiring.evidence.json`保留原身份；本轮新增683项组件差异（438修改、241新增、4删除）。Manager/Web/Worker输入无变化，复用原证据。`WIRING-FINAL.json`记录映射应用成功。正式入仓保留`components/cpa/assets/paidaxing.gif`的上游`100755`模式。

## 已知限制与交接

- 五个Kilo/NoReplay根失败在`C:/Users/子羽/.codex/artifacts/cpa-core-8080-20261121/windows-baseline-auth-failures.jsonl`已有同名同症状，旧`VALIDATION.json`与`baseline-auth-command.json`关联固定基线`966ad2f29e8b4d1848438b6ee055c1cdc5f20ff1`。错误为Kilo合成fixture机器身份前置缺失及同步事件缺失；本轮不声明全模块全绿。
- 六个跳过为两项旧BPS联合fixture所需共享输出目录缺失，以及四个shadow子进程helper入口。权限项已由非root专项覆盖。
- 动态库加载通过不代表商业许可激活、真实账号/OAuth或模型业务通过。测试配置无账号；真实模型、OAuth和额度调用为0，未做生产验收。
- 本轮无数据库动作，diff未命中迁移、SQL或Prisma schema；未提交、推送或部署。SIWC只完成根因调查，界面和Manager桥接尚未实施。

下一责任人：P9收口本地升级候选与SIWC调查，裁定后续入仓、SIWC接入或发布入口。
