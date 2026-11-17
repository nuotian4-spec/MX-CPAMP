# Kilo公开错误最终独立审查

日期：2026-11-17。审查责任人：S。
结论：放行本轮本地Kilo公开错误候选，未发现新增实质代码阻断。本回执为S最终结论，替代初审记录中的待冻结、待补出口与待编译状态。

## 输入绑定

工作树：`C:/Users/子羽/.codex/worktrees/kilo-public-errors-20261117/MX-CPAMP`。
基线及当前HEAD：`67445a35ba450468948447492b560c204afa7321`。
范围：18个CPA路径，13修改、5新增，逐项以A的`frozen-inputs.json`为准。
S于2026-11-17 05:49:34（UTC+08:00）核对A冻结清单、S已审输入清单与当前磁盘文件：路径、字节数及SHA256全部一致；18项无漂移。结果见`frozen-input-check.json`。此比对用于绑定已审代码与既有执行证据，不重复源码审查或测试。

## 核心判断

- 公开呈现与内部调度分离：原Error、机器码、状态、请求作用域、NoReplay及终态鉴权语义保留；非Kilo和成功模型正文保持原行为。
- 分类依据真实状态和精确机器事实；未知固定为request_failed，request_scoped仅4xx映射invalid_request，501/503保持服务器失败类别。第三方不透明HTTP200流错误不推断成429或账号封禁。
- 真实HTTP早退、首帧前、已开始流、Alpha及WS拒绝出口均有对应证据；WS保持升级前501，Alpha保留completed_queries及不重放语义。
- 新受控诊断用公开request_id关联内部status/upstream_status及stream_started；未知动态机器值不原样写入新日志。公开错误去除原始错误正文、供应商信息及错误头，不全局替换成功文本。

## 执行证据

已实读A执行回执及相应原始记录，复用未变输入的通过证据：

| 证据 | 原始结果 | 最终口径 |
| --- | --- | --- |
| A/targeted.log | TARGETED_RC=1 | 首轮4顶层、17失败叶子，保留原始失败结果 |
| A/incremental.log | INCREMENTAL_RC=1 | StartedStream六叶子、Busy、Alpha九场景、WS及关联等通过；当轮2个admission失败另由专项闭环 |
| A/admission.log | ADMISSION_RC=0 | 受影响helper分类及无出口503、有效compact501通过 |
| A/opaque-stream.log | OPAQUE_STREAM_RC=0 | Responses/chat完整不透明SSE已开始流通过 |
| A/native-build-result.txt | NATIVE_BUILD_RC=0 | Windows amd64、CGO_ENABLED=0编译 |
| A/linux-build-result.txt | LINUX_BUILD_RC=0 | Linux amd64、CGO_ENABLED=0交叉编译 |
| A/diff-check-result.txt | DIFF_CHECK_RC=0 | 差异空白检查通过 |

首轮StartedStream六叶子属于Windows测试名称导致fixture未触达上游，安全名称后的增量通过；Busy一叶子仅更新公开码断言后通过。首轮余下custom工具续接10叶子由S固定原提交overlay独立复现为同样失败，详见`BASELINE-CUSTOM-TOOL-CONTROL-20261117.zh-CN.md`及`baseline-overlay/`。成功断言未降低，未将既有缺口扩大为本轮修复。

## 已知问题与边界

custom工具续接10叶子为原基线已有功能缺口，另行排期。本轮不宣称整套历史测试全绿。两平台CGO_ENABLED=0编译不等于Linux/cgo插件运行或生产业务验收；第三方案例不构成本站故障及其根因证据。

本轮无数据库动作，未改BPS、更新器、插件管理、UI或账号身份/代理分配策略；没有提交、推送、部署、服务重启、真实账号或模型请求。S仅写审查证据目录。
下一责任人：P9合并A/S/D及独立V事实核验结论，完成本地候选收口；既有custom工具续接缺口单独排期。
