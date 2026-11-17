# Kilo 公开错误交付功能映射

日期：2026-11-17。原包基线：`67445a35ba450468948447492b560c204afa7321`。候选位置：`C:/Users/子羽/.codex/worktrees/kilo-public-errors-20261117/MX-CPAMP`。

本表映射基线 CPA、Manager、登录服务组合到本次 Kilo 错误呈现候选，不把供应商未公开能力算作本站已实现功能。状态使用“未做、实施中、已验证、受外部条件阻塞”；“已验证”同时注明证据层级，静态核对不代替运行或生产验收。

产品冻结为18个CPA路径：13修改、5新增，准确清单见 `../A/frozen-inputs.json`，执行说明见 `../A/EXECUTION-RECEIPT-20261117.zh-CN.md`。下表的本地专项结论引用该冻结输入；独立S/V最终放行与生产回执分开记录。

正式提交材料以`../CURRENT.zh-CN.md`的9文件白名单为准。下表引用的原始日志、overlay、命令/结果文本和临时产物只在候选目录本地保留，不随本次提交发布。

## 本次变化

| 原包能力或缺口 | 新候选约定 | 状态 | 证据与完成条件 |
| --- | --- | --- | --- |
| 上游拒绝隐藏原始正文，但固定文案带 Kilo | 普通客户端错误不含品牌、供应商URL、凭据或私有正文 | 已验证 | 本地专项：A `targeted.log` 的ErrorShapes、FinalErrorBoundaries；流中补证见 `incremental.log`、`opaque-stream.log` |
| 大小、预处理繁忙错误码含 `kilo_` | 改公开码，保留内部机器码、状态与请求作用域 | 已验证 | 本地专项：PayloadKeepsInternalContract、UpstreamRequestErrorsNeutral；Busy取消/释放见 `incremental.log` |
| 连接、坏帧、转换或未完成流仍可能含品牌 | 在现有 Kilo 错误出口统一固定公开类别和提示 | 已验证 | 本地TLS+CONNECT专项：A `targeted.log` 的NativeProtocolAndConnection，覆盖断开/坏帧/无终态 |
| 多类失败合并为 `request_failed` | 明确限流、额度、认证、访问、输入、连接/协议；未知仍为通用失败 | 已验证 | 本地专项：A `targeted.log` 分类；501/503服务失败最终边界见 `admission.log`，RC0 |
| 内部限制、冷却、重试与公开文案混用风险 | 内部事实和现有策略保持，公开呈现后置 | 已验证 | 本地专项：精确403/额度码、状态/作用域/NoReplay及POST数断言；内部恢复策略文件不在18路径中 |
| SSE失败具有独立终态 | 保留一次失败终态；不补成功、不重放请求 | 已验证 | 本地专项：A `incremental.log` 原六项；`opaque-stream.log` 完整样例双协议，单错误/POST1/无completed |
| 诊断已有请求ID与错误事件机制 | 同一请求的公开ID可关联内部原因 | 已验证 | 本地专项：真实HTTP与已开始流请求ID关联见A `targeted.log` / `incremental.log`，同时断言秘密不进入新诊断 |
| 第三方仅给通用SSE错误帧 | 验证本站可能路径，保留第三方根因未知 | 已验证 | 本地专项：HTTP拒绝/HTTP200流内错误；完整样例普通四组合及已开始双协议，Responses事件序号2、不重试 |
| 第三方真实状态、机器码和服务版本缺失 | 明确缺证，不借本站复现冒充第三方归因 | 受外部条件阻塞 | 若需确定第三方根因，由该请求维护方提供同一请求的脱敏事实 |

## 原有能力承接

| 原包能力 | 新候选承接方式 | 状态 | 证据与边界 |
| --- | --- | --- | --- |
| Kilo、Codex、BPS三类客户端Key | 继续按通道隔离，推理Key不获得管理权限 | 已验证 | 静态：`internal/channelscope/scope.go:21-27`、`internal/config/client_key_scopes.go`；A handlers合成Codex/BPS/空scope错误对照保持旧输出 |
| 未绑定scope的旧客户端Key | 仍不进入Kilo，不因脱敏扩大授权 | 已验证 | 静态：`internal/channelscope/scope.go:24-25` |
| Kilo账号、API Key/设备授权与启停 | 原入口、稳定账号引用及手工停用意图不变 | 已验证 | 静态：`KiloAddAccount.tsx`、`KiloDeviceForm.tsx`；本轮不真实登录 |
| 设备/客户端身份配置 | 保留原绑定和存储，不以错误修复重新生成身份 | 已验证 | 静态：A冻结18路径不含身份/存储模块；本轮不重验真实身份或设备授权 |
| 手工出口、共享池及固定IP自动独占策略 | 不加直连兜底、不调整容量、不重置历史IP记录 | 已验证 | 静态：A冻结路径不含分配模块；本地无出口503/POST0见 `admission.log`，不代替真实出口验收 |
| Codex额度来源或独立额度材料 | 保留来源身份、更新时间和未知态，不推断额度恢复 | 已验证 | 静态：`KiloAddAccount.tsx:111,271-279`、`auth/kilo_recovery.go:377-390` |
| 精确额度耗尽恢复 | 原触发码、版本/来源校验、手工停用边界不变 | 已验证 | 原恢复文件未改；A首轮auth专项与内部事实断言通过，不代表真实额度恢复 |
| 明确账号限制 | 不把普通403、429或“受限”文案判定为账号限制 | 已验证 | A分类/内部事实专项通过；真实403与HTTP200流内错误分开，不使用中文猜测 |
| 正常模型文本、搜索及既有工具协议 | 不做全局字符串替换，不改变正常内容和现有工具协议 | 已验证 | A正常模型四路径通过；Alpha九场景保留status/POST/结果/completed_queries断言；不扩增子智能体能力 |
| 原基线custom-tool续接缺口 | 10个用例原提交同样409；本次不改成功断言或续接实现 | 未做 | 指该既有缺口的修复未做；S `BASELINE-CUSTOM-TOOL-CONTROL-20261117.zh-CN.md` 已独立确认原基线，不声称所有工具功能通过 |
| Kilo专用Key的WebSocket | 保持升级前拒绝，不在此次新增支持 | 已验证 | 本地专项：A `incremental.log` 拒绝正文无品牌、501且无Upgrade |
| BPS插件、商业许可、OAuth和输入转换 | 既有功能保留；输入兼容问题继续搁置 | 已验证 | 静态：A冻结路径不含BPS产品；handlers的BPS合成错误对照保持旧输出，不代表真实BPS业务验收 |
| Manager与登录服务 | 接口、存储、启动方式不改；发布仍用现有组合资产 | 已验证 | 静态：`.github/workflows/release.yml`、`deploy/package-release.py:93-109` |
| 更新、停态备份、完整Compose链与恢复 | 复用现有更新入口，不另建维护框架 | 已验证 | 静态：`deploy/mx-update.py:237-314`、`deploy/kilo-release.py:330-370` |

上述源码路径以 `components/cpa/` 为前缀；前端组件路径以 `components/cpamp/apps/web/src/features/kilo/` 为前缀。

## 发布与真实验收

| 交付项 | 状态 | 下一责任人 / 完成证据 |
| --- | --- | --- |
| 准确产品输入清单、受影响专项及本地构建 | 已验证 | A `frozen-inputs.json` 18路径；执行回执及两平台编译RC0。下方保留总体RC1与基线限制，不称全套绿 |
| 独立审查与本地验收记录 | 已验证 | S/FINAL-REVIEW-20261117.zh-CN.md与V/ACCEPTANCE-20261117.zh-CN.md放行本轮本地候选；18项冻结输入一致 |
| 使用、备份、发布、恢复说明 | 已验证 | 本文件及 `OPERATIONS.zh-CN.md`，静态接口核对；未执行服务器操作 |
| 18路径主仓受控回迁 | 已验证 | A/controlled-integration.json：18/18与冻结候选一致，未知改动保留 |
| 本轮提交和推送 | 实施中 | 用户已明确授权；A唯一入仓写入者，S核实际远端结果 |
| 新固定版本GitHub Actions与Release | 实施中 | 版本v2026.11.17-rc.1已确定，新固定提交待实际生成；发布回执尚未取得，不以旧RC4代替 |
| 生产维护及入口健康回执 | 受外部条件阻塞 | 用户授权后使用已发布固定版本；真实轮次/镜像/健康证据 |
| 实际客户端错误出口验收 | 受外部条件阻塞 | 用户授权所需操作后给真实客户端回执；不强造上游限制 |
| 真实登录、模型、第三方计费与限制原因 | 受外部条件阻塞 | 本地合成测试不替代供应商事实；本轮未调用真实账号或模型 |

`TARGETED_RC=1`、`INCREMENTAL_RC=1` 原始结果保留。受影响错误出口通过专项闭环，最终 `ADMISSION_RC=0`、`OPAQUE_STREAM_RC=0`；10个custom续接失败由S原提交overlay确认是既有缺口。Windows及Linux构建分别 `NATIVE_BUILD_RC=0`、`LINUX_BUILD_RC=0`。这些结论只覆盖对应输入和用例，不等于历史整套测试全绿。

当前责任人：`/root/p8_kilo_release_integrator`执行提交、推送和固定版本发布，`/root/p8_kilo_release_review`核远端与发布合同。D文档责任人：`/root/p8_kilo_delivery_docs`。下一节点：主会话核实际发布回执；生产维护和真实请求尚未授权，Goal工具登记仍为blocked，整体目标尚未完成。
