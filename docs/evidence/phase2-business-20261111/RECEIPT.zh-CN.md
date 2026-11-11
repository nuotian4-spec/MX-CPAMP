# BPS最终候选真实业务回执

日期：2026-11-11。工具输入为 `efe4d4989db8899771fe24cf07ff370111eed137`；产品输入为 `bb14b3a1c60f1b84b31a19ea5b1dde71edaee00f`，CPA与Worker按产品目录等价核验复用 `95dd9ca` 镜像，Manager使用bb14的r2镜像。实际各组件完整ID与revision见[真实输出](final-bps-20261111.jsonl)，逐组件校验均通过。

最终候选无凭证预检退出0，见[预检输出](preflight-r2-20261111.jsonl)。真实执行经进程stdin接收专用材料，仅测试BPS，未重复Codex登录。退出1，stderr未输出；退出码及预算见[元数据](final-bps-20261111.meta.json)。

| 环节 | 实际结果 |
|---|---|
| 单行BPS批次 | succeeded，阶段cpa_import；仅创建一次批次 |
| 保存与注册 | 同一份BPS凭证保存、热注册及身份核验通过 |
| 并发覆盖 | 通过Manager设为3并保存 |
| 正常刷新 | 受支持的单目标refresh调用一次；同auth、profile、client与身份，access/refresh token均发生轮换，并发覆盖保留 |
| 模型目录 | 用户确认的三个模型通过目录核验：gpt-6-astra、gpt-6.1-sol、gpt-5.6-sol |
| 公开模型请求 | 仅一次客户端请求，gpt-5.6-sol；HTTP500，public_request_rejected；未取得正常输出及成功终态 |
| 收尾 | cleanup verified=true，token_state_retained=false，输入复核unchanged=true |

请求正文为 `model=gpt-5.6-sol`、`input="Reply with OK."`、`max_output_tokens=128`、`reasoning.effort=low`、`stream=false`。使用隔离环境中唯一的BPS凭证；没有跨账号或跨通道fallback。实际上游尝试次数未观测，保持unknown，不把一次客户端请求写成一次上游传输。

首次真实执行所用工具只保留模型HTTP状态及汇总码，没有保留可定位的错误类别；该HTTP500本身不足以判断是本地处理、上游拒绝还是其他原因。后续源码核查、离线复现及稳定脱敏分类已完成，见下一段；BPS本轮模型预算已用完，不自动再次调用。该限制不影响已经取得的批次登录、正常刷新和并发保留证据。

后续[离线诊断](../phase2-model-diagnostic/RECEIPT.zh-CN.md)已用相同版本、三模型目录和完整请求体穿过真实公开处理器，确认BPS既有能力检查会拒绝 `max_output_tokens`，过滤空池后在旧版映射为500。三个模型的替身传输调用均为0；仅删除该字段的离线对照均正常完成。产品已补充该场景的明确400，验收工具已改为省略三种max参数并增加固定错误分类。历史真实错误正文没有保留，其类别和实际上游尝试次数仍不改写；离线成功不替代真实推理。新的真实请求须另取得一次预算。

所有临时容器和令牌已确认清理。没有生产连接、推送、部署或SQL/schema迁移。Codex正向2FA、BPS手工OAuth回调与模型业务仍未完成整体验收；继续沿用完整验收后统一发布的边界。
