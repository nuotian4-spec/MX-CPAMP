# CPA 内核 v8.0.80 升级

## 固定输入

| 输入 | 固定身份 |
|---|---|
| MX-CPAMP 整合基线 | `966ad2f29e8b4d1848438b6ee055c1cdc5f20ff1` |
| 上游共同基线 | v8.0.63 / `6db7d4b839d86c5b24e17bbe73bcd4db18ef1b1a` |
| 原样导入提交 | `be8fae57583f75237c5c90be1924eeab84de0f0f` |
| 上游目标 | v8.0.80 / `f5c5681a8361c9da53e93c60b6507816eee7468b` |
| 上游目标 tree | `36bef95f17cb09c90b1e48174a32832e6fcc19c7` |

CPA 使用目标上游完整增量与本站定制三方整合。上游增量涉及 942 路径：修改 759、新增 182、删除 1；本站相对原样导入有 606 项定制路径。双方交集 78 路径，其中 49 自动合并、29 人工整合。

## 宿主与请求路径

- 启动入口保留本站 capability probe 的独立执行路径；普通启动使用上游 `run()`、退出码和清理逻辑。
- v8 凭证上传采用上游多身份 parser、文件 snapshot 和按源文件 reconciliation。Kilo JSON、现有 Kilo 文件在 v8/v0 分流前拒绝；parser 产生的每个身份在所属文件写盘前执行 Kilo 专用入口与原生 BPS 替代限制。显式 Kilo JSON、已有 Kilo 文件保持原有全批上传预检，全部删除保持原有预检；普通 multipart 保留上游部分成功语义，parser 拒绝某文件不撤销其他已成功文件。
- 配置持久化保留显式 BPS false 值、BPS protocol 和 client-key-scopes；同时接入上游 shutdown-timeout 与新插件配置字段。
- 插件 executor 接入上游调用归属记账；本站可信通道映射、Responses-to-Chat 流观察、冷更新维护状态和插件目录路由继续使用既有实现。
- 请求上下文同时携带上游 continuity 状态、本站 BPS 请求状态、通道 scope 与延迟观测。BPS/Kilo 禁止重放条件继续约束首次输出前重试。
- Codex 双向 WebSocket 保留异步 generation admission 和 lease 生命周期；上游 session instruction receipt 在真正准备发送时绑定，日志使用对应的脱敏请求体。断连归类同时覆盖本地 compaction 已交付内容和仍在等待 admission 的未发送请求。
- 本地并发租约等待实际 source 退出后释放，Home 取消保持独立的快速结束选择语义。取消时已观察到的 BPS typed refusal、NoReplay 终态和可信 retry hint 保留一次结果记录；Kilo Execute 与 Count 共用同一记账入口。

## 持久状态

本轮没有新增数据库 schema 或部署迁移。Codex 刷新 journal 已存在于整合基线；新增逻辑只在确认尚未发送时释放本实例的 pending claim，已发送且结果未知的 claim 继续保留。发行更新的停态备份覆盖三应用挂载目录及 auth-dir 内的 `.codex-refresh`，继续使用现有更新入口。

## 针对性回归

新增 `TestUploadAuthFileV8RejectsRestrictedParsedIdentityBeforeWrite`，覆盖 Kilo、原生 BPS 的新建和覆盖共 4 个场景：被拒绝的多身份上传不写入文件、不替换原身份、不执行持久化 hook。

统一验证覆盖 CPA 模块编译、Go 测试与 Linux 插件加载边界。既有 scope、BPS continuation/no-replay、Kilo 独立账号、插件维护和模型发现测试与上游测试共同执行。Manager、Worker 的产品输入保持既有基线。

管理测试配置独立的 update-site lock/state 目录，新增 API key 同时提供对应 channel scope。Windows 工作目录测试在清理临时目录前恢复原目录；断言使用完整 JSON 语义比较。生产维护守卫、通道准入和租约顺序保持有效。

## 验证结果

结论：本轮目标验收边界通过。最终组件 tree 为 `1163a73a8ef7a1b46944a0fe14a996181b8a3258`；Linux CGO 构建与主验证 tree 为 `609ae8c59118c30069e0f7a26338a01fb660b606`。两者只相差 ETag、Manager 通道密钥桥接两个测试文件，产品构建输入一致，两项测试均有最终通过记录。

| 验证边界 | 有效记录 | 结果 |
|---|---|---|
| Go 1.26.10、Linux/amd64、CGO 构建 | `linux-final-build.log`、`CLIProxyAPI-final` | 退出码 0 |
| 实际 BPS 动态库加载、注册与许可读取 | `linux-plugin-probe.json` | 0.5.86-dev 已注册、生效；许可返回待激活；容器 network=none |
| 管理 API、插件宿主、翻译器及取消接缝 | `linux-release-validation.jsonl`、`linux-final-targeted.jsonl` | 所选边界通过；ETag 单独使用最终记录 |
| ETag 与通道 scope 一致的密钥变更 | `etag-final.jsonl` | 通过 |
| Kilo 取消后记账与租约释放 | `kilo-probe-cancel-final.jsonl` | 11 个子场景通过 |
| Kilo 首字、结局与取消观测 | `linux-latency-isolated.jsonl` | Linux 独立进程内 3 个根测试及子场景通过 |
| 通道范围接线 | `linux-release-validation.jsonl` | 合同要求 108/108 通过 |
| Manager/Web/CPA 通道密钥、手动测试、自动探测 | `m10-joint.jsonl` 的手动及探测记录、`m10-channel-final.jsonl` | 3 个父桥与 3 个 Manager 子测试，6/6 通过 |
| 原生 BPS、流式、旧通道接线 | `windows-integration-retest.jsonl` 的对应已完成记录 | 合同要求分别 3/3、2/2、2/2 通过 |

完整有效证据与命令保存在 `C:/Users/子羽/.codex/artifacts/cpa-core-8080-20261121/VALIDATION.md` 和 `VALIDATION.json`。插件隔离探测后仅发生取消路径产品变更；插件 ABI、注册、元数据与许可路由输入未变，该证据只复用于插件加载边界。最终可执行文件在全部产品修改后构建。

接线合同映射 4876 个组件文件，本轮组件差异 952 路径：新增 183、修改 768、删除 1。合同保留原五组历史证据的输入与结论，另记本轮 121 项有效通过结果；临时 index 核对未改变真实 index。独立审查见 [审查回执](CPA-UPSTREAM-V8.0.80-REVIEW-20261121.zh-CN.md)。

## 已知验证边界

- 全模块测试保留既有失败，结论按本次有效专项和精确基线对照给出，不声明全量全绿。
- 13 个 auth 根测试在固定本站基线同样失败；Home 搜索取消后 HTTP 499 与 Kilo alpha 输入拒绝 HTTP 400 的两项旧断言也已在基线复现，本轮保持原处理逻辑。
- BPS 长历史及工具续接的既有缺口不属于本轮上游整合修复。
- Linux 正式源码导出统一使用 Git `core.autocrlf=input` 规范化换行，Windows 文件系统读取差异不作为生产 Linux 结论。有效验证以最终输入和日志记录为准。
- UX1 持久化、IP 准入、AE1 及 Windows 永久绑定存储的既有测试失败均有固定基线对照。SDK 合并运行存在默认 usage manager 的测试间干扰；相应延迟断言采用独立 Linux 进程的通过结果，合并包日志仍保留失败状态。
- AIStudio 取消的两个模式在目标上游独立运行及 Linux 整合运行均通过；安装客户端依赖的真实 Alpha 搜索验收未执行。`linux-failed-subset.jsonl` 来自失效导出，不用于本轮验收。

## 交付与限制

正式发行沿用仓库 `release.yml`、镜像清单、接线合同和 `mx-update.sh`。来源、候选验证、正式镜像与生产业务验收分别记录。

本地测试使用合成凭据与测试服务；Linux 插件探测只核加载、注册和许可状态读取。商业许可激活、真实自动授权、模型目录上游恢复及真实模型推理不属于本地结论。
