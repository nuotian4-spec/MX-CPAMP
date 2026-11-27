# CPA v8.1.7 本地整合验证

结论：上游 v8.1.7 已整合到独立候选，新增接缝专项、Linux CGO 成品构建及 cpa-bps 动态库加载通过。保留五个已确认基线失败；本次未运行完整历史套件。未提交、推送或部署。

## 固定输入

- 工作树：`C:/Users/子羽/.codex/worktrees/cpa-core-v817-20261127/MX-CPAMP`。
- 分支：`codex/cpa-core-v817-20261127`；本站 HEAD：`b9ea2dc4c6bb377d31143970a6df1084297e4b09`。
- 官方目标：`v8.1.7` / `47f18e4d82bd72f7d2b5ecf5ede93e4837a4625e`；共同基线：`v8.0.80` / `f5c5681a8361c9da53e93c60b6507816eee7468b`。
- 最终完整候选 tree：`8e5f0df93e564220a34435a66fb6c72314e28cc6`；CPA tree：`8501bb9d2095cb9e7bc71b6bc008aa097b6670cc`。
- 原始命令、日志与成品目录：`C:/Users/子羽/.codex/artifacts/cpa-core-v817-20261127`。下述证据文件均位于该目录。

## 整合范围

纳入上游 679 项变化：434 修改、241 新增、4 删除。63 个本站交集通过三方整合，18 个冲突文件逐项解决；完整性见 `THREE-WAY-FACTS.json`、`INTEGRATION-COVERAGE.json`。最终 CPA 相对本站 HEAD 为 683 项变化：438 修改、241 新增、4 删除。

本站 BPS/Kilo、通道范围、配置脱敏、插件冷维护、请求延迟观测、动态模型发现与凭证并发持久化保留；上游配置快照/CAS、多文档配置、Home 插件授权与调度、Copilot 目录、WebSocket liveness、Gemini parts 排序同时合入。

取消处理使调用方及时返回，由排空任务持有租约至真实源流退出；BPS typed 权限结果仍记账，普通取消不写上游失败。新增 Verify 和 Antigravity credits 两个源退出租约断言。本站模型快照传入所选凭证以匹配上游池轮转签名；三个测试 fixture 沿新接口更新且保留原断言。

Manager、Web、Worker 产品输入未变，本轮没有它们的构建。SIWC 界面诊断见 [调查记录](SIWC-DIAGNOSIS.zh-CN.md)。

## 统一验证结果

环境：`golang:1.26.10-bookworm`、Linux/amd64、`CGO_ENABLED=1`、`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、Docker `network=none`。仅缺失的目标锁文件依赖通过 7890 代理补缓存；测试只访问离线 fixture。

| 边界 | 证据 | 结果 |
| --- | --- | --- |
| 配置拆分、多文档、快照、CAS、SOCKS取消 | `linux-config-proxy.jsonl`、`linux-config-nonroot.log` | 通过；真实只读权限以 nobody 补齐 |
| Home 插件/RPC/调度、通道隔离、Responses桥、冷维护 | `linux-pluginhost-r3.jsonl` | 全包 594 根通过；可选入口跳过单列 |
| 管理配置、账号并发、BPS管理与模型定义 | `linux-management-r2.jsonl`、`linux-management-r3.jsonl` | 84 根通过；CAS使用本站维护锁fixture |
| Handler 的 Home/插件/通道范围 | `linux-handlers-r2.jsonl` | 43 根通过 |
| 凭证调度、并发持久化、BPS切号与权限 | `linux-auth-r3.jsonl`、`linux-auth-cancellation-final.jsonl`、`linux-credits-cancellation-final.jsonl` | 新增边界全部通过；五个基线失败单列 |
| WebSocket duplex取消及读期限、BPS取消 | `linux-executor-r2.jsonl` | 30 根通过 |
| BPS目录发现/注册、Copilot目录 | `linux-service-r2.jsonl` | 55 根通过 |
| Gemini parts与Codex翻译接缝 | `linux-translator.jsonl` | 三包 304 根通过 |
| Codex SIWC/token/refresh、目录权限 | `linux-codex-auth.jsonl`、`linux-codex-nonroot.log` | 通过；目录权限以 nobody 补齐 |
| 最终 CPA 构建 | `BUILD-RESULT.json`、`linux-final-build.log`、`linux-final-build-info.txt` | 唯一最终构建退出 0；Linux/amd64、CGO=1 |
| 真实 cpa-bps 动态库 | `linux-plugin-probe.json`、`linux-plugin-registry.json`、`linux-plugin-license.json`、`linux-plugin-probe.log` | 真实加载/注册/许可GET通过，退出0并卸载 |

按每个包/根测试的最新有效终态去重，1796 根通过、5 根保留失败、6 根跳过；详细名单与来源在 `FINAL-TEST-RESULTS.json`。通过证据只在后续输入及直接受影响语义保持等价时复用；取消修复仅补受影响专项，未重复全套。

成品为 `CLIProxyAPI-final`，版本 `v8.1.7-mx-local`，Commit 字段绑定上述 CPA tree。供应包为本地 `cpa-bps-unified-0.5.86-linux-amd64-binary.zip`，包版本 0.5.86，实际库登记版本 0.5.86-dev；`registered=true`、`effective_enabled=true`。许可读取返回 `license_activation_required`、`active=false`。测试配置无账号，真实模型调用为 0。

## 已知限制

- 五个根失败：`TestKiloEgressEarliestGeneration`、`TestKiloEgressGenerationReset`、`TestNoReplayRecordsOnceAndReleasesJointLease`、`TestNoReplayCanceledAttemptIsAvailabilityNeutral`、`TestNoReplayEmptyAndDoneStreamsDoNotRedispatch`。同名同症状已存在于 `C:/Users/子羽/.codex/artifacts/cpa-core-8080-20261121/windows-baseline-auth-failures.jsonl`，对应固定基线 `966ad2f29e8b4d1848438b6ee055c1cdc5f20ff1`；合成 Kilo fixture 未满足既有机器身份前置。
- 六个跳过入口为两个需要共享输出目录的旧 BPS 联合 fixture，以及四个由其他测试调用的 shadow helper。权限项均已由非 root 专项补齐。
- 动态库证据覆盖新宿主加载、登记与本地许可读取；商业授权、真实账号/OAuth、上游模型和生产链路未验收。
- 本次无数据库动作，无生产配置修改、迁移或部署。正式发布需绑定后续固定本站提交。

## 来源映射与交接

`deploy/production-wiring.contract.json` 使用最终 tree 的 5114 个 CPA/CPAMP mode/blob 精确映射，保留历史接线证据的原身份，并追加本轮 lineage。映射报告见 `WIRING-FINAL.json`。临时 index 保留上游新增 `components/cpa/assets/paidaxing.gif` 的 `100755` mode；正式入仓应沿该 mode，否则源码映射校验会拦截。

独立审查见 [REVIEW](REVIEW.zh-CN.md)。下一责任人：P9 核本地候选交付与后续发布入口；生产发布状态独立记录。
