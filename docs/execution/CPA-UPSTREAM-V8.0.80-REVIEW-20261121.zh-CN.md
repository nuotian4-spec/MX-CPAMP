# CPA v8.0.80 独立审查

结论：本轮内核整合、本站定制保护和目标验收边界通过，可进入固定提交发布。全模块历史测试仍有既存失败，范围见下文。

## 固定输入

- 本站基线：`966ad2f29e8b4d1848438b6ee055c1cdc5f20ff1`。
- 共同上游：v8.0.63 / `6db7d4b839d86c5b24e17bbe73bcd4db18ef1b1a`，原样导入提交 `be8fae57583f75237c5c90be1924eeab84de0f0f`。
- 目标上游：v8.0.80 / `f5c5681a8361c9da53e93c60b6507816eee7468b`，tree `36bef95f17cb09c90b1e48174a32832e6fcc19c7`。
- 最终组件 tree：`1163a73a8ef7a1b46944a0fe14a996181b8a3258`。
- 审查者：`core8080_review`，只读产品，引用执行方有效日志，未重复运行产品测试。
- 原始审查与验证回执：`C:/Users/子羽/.codex/artifacts/cpa-core-8080-20261121/REVIEW.md`、`VALIDATION.md`、`VALIDATION.json`。

## 来源与整合

上游 942 路径增量完整纳入：修改 759、新增 182、删除 1。182 个新增文件均存在，`assets/modelflare.png` 的删除落实，507 个本站独有文件保留。864 个没有与本站定制交叉的上游路径与固定目标一致；49 个自动合并及 29 个人工整合接缝已审查。上游文件 mode 均为 100644，本次没有模式变化；最终 Go 冲突标记为零。

- 普通启动保留上游 `run()` 和退出清理；本站 capability probe 继续独立早退出。
- Kilo 使用 `EnsureKiloExecutor` 共享实例，保持可信执行通道与管理能力；插件执行器继续检查 id、path、version 和通道范围。
- BPS 模型发现保持 attempt、RegistrationEpoch、CredentialVersion、代理和属性绑定。旧异步结果失效，AuthUpdate 仅作用于仍有效身份；失败状态不由静态模型表冒充成功。
- v8 上传保留多身份 reconciliation。显式 Kilo JSON、现有 Kilo 文件全批预检；插件 parser 的身份逐文件写前拒绝 Kilo 和被插件替代的原生 BPS。普通 multipart 保留上游 207 部分成功语义。
- 插件 ZIP 冷启动、维护所有者、恢复状态、原路径和许可入口保留。插件 usage attribution 在翻译失败记账前应用，scope 拒绝不给调用归属。
- Codex duplex 保留异步 admission、generation lease 和实际发送时绑定的 session receipt。BPS/Kilo NoReplay 先于刷新及流式、非流式重试。

## 取消、并发与持久状态

本地并发租约等待实际 source 退出后释放；Home 消费者取消保持快速结束选择。取消时已接收的 BPS typed refusal、NoReplay 终态和可信 retry hint 仅记录一次；Kilo Execute、Count 使用统一入口。对应许可持久化、取消传播、slot 释放和上游 Issue7716 边界均有本轮通过证据。

没有新增 schema 或部署迁移。Codex 刷新 journal 已在基线存在；新增逻辑在确定发送前失败时释放本实例 pending claim，已发送且结果未知时保留。现有更新入口的停态备份覆盖应用挂载目录及 auth-dir 内的 `.codex-refresh`。

## 验证结论

Linux/amd64 CGO 最终构建退出码为 0。独立比较最终 tree 与构建 tree `609ae8c59118c30069e0f7a26338a01fb660b606`，仅有 ETag、Manager 通道密钥两个测试 fixture 差异，产品输入完全一致。

管理 fixture 配置真实所需的更新站点目录、匹配的 key/scope，以及 Windows 工作目录清理顺序，生产守卫保持。最终 ETag 单项通过。三组 Manager/Web/CPA 联合边界形成 3 个父桥和 3 个 Manager 子测试通过，无跳过；通道范围 108 项及 BPS 7 项接线要求通过。Kilo 取消 11 子场景、Linux 独立进程的 3 个延迟观测根测试通过。

实际 cpa-bps 0.5.86-dev 库在 network=none 的 Linux 容器中已注册且生效，许可读取返回待激活。后续取消路径变更不影响插件 ABI、元数据、注册及许可路由，该加载证据有效。

## 已知问题与验收边界

- 不声明全模块全绿：13 个 auth 根测试、2 个 API 旧断言，以及 UX1、IP 准入、AE1、Windows 绑定存储的既存失败均有固定基线对照；BPS 长历史及工具续接缺口沿用既有边界。
- SDK 合并运行会受到默认 usage manager 的测试间干扰；延迟断言以独立 Linux 进程通过为据，原合并包失败记录保留。Windows 换行、符号链接和目录清理差异以正式 Linux 输入验证。
- AIStudio 取消模式在目标上游与 Linux 候选均通过。失效导出的 `linux-failed-subset.jsonl` 排除出验收证据。
- 未执行生产部署、真实商业许可激活、账号自动授权、上游模型目录业务或真实模型推理；依赖已安装客户端的 Alpha 搜索业务验收另计。

下一责任人：发布负责人绑定固定提交、核对接线映射并执行既有发布流程；生产应用沿用既有维护入口。
