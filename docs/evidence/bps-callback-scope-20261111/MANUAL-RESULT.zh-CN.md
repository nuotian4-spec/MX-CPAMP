# 手工 BPS OAuth 真实验收结果

日期：2026-11-11。固定候选为CPA `d96e26782b4b080a703ee9668bf1c653e18cf164035b2bd9e11bcac2d11023fd` / revision `346b9479e12bc45ccb0e72556acc4cad4e9a0a04`，Manager `cdf235bee2e22f2c33af145b8fb4d27e93b4478162b8738dcadd3595f3b74c4a` / revision `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44`。用户本机运行已审入口，按明确的一次范围完成官方授权并提交新回调，随后报告登录成功。

## 结论

修复版手工BPS OAuth在本机隔离组合中完成唯一凭证保存与运行时注册，独立核验通过。没有推送、部署或操作OVH。

06:18:54 UTC，主会话运行已审verify，真实exit0；[最终回执](MANUAL-VERIFY-FINAL.jsonl)为`passed`：

| 核验项 | 实际结果 |
|---|---|
| 保存凭证总数 / BPS数 | 1 / 1 |
| 运行时凭证总数 / BPS注册数 | 1 / 1 |
| 唯一文件与运行时引用一致 | true |
| profile/client匹配 | true |
| login_method为oauth | true |
| access/refresh存在 | true |
| runtime active且未禁用 | true |

核验只输出计数和布尔值；邮箱、账号ID、文件名、完整回调、密码、TOTP与令牌不入本回执。它证明该隔离组合的手工授权保存注册，不替代生产部署、额外令牌刷新或模型请求验收。

## 模型列表为0

用户截图显示凭证模型数0。06:20:11 UTC的[实际运行配置只读摘要](MANUAL-MODEL-CATALOG.jsonl)确认`oauth.providers.bps.models`未设置，模型清单条目为0，未修改配置、未发模型请求。

启动配置见[入口](scope-manual-run.mjs)：仅给oauth.auth-dir。CPA的`service_models.go` BPS分支只从`cfg.BPS.Models`创建模型，默认目录为空；因此本轮凭证没有注册模型。模型目录和OAuth凭证注册是两个独立条件。正式配置应显式列入已确认的BPS模型；本轮没有为了显示目录而发请求或更改配置。

## 清理与独审

最终verify通过、配置诊断完成后，主会话执行已审stop，真实[exit0](MANUAL-CLEANUP.exit)，[清理回执](MANUAL-CLEANUP.jsonl)确认本轮label归属容器清除，临时访问文件删除，`token_state_retained=false`。入口127.0.0.1:2702已结束，旧链接与材料不复用。

独审只读核最终verify、实际模型配置摘要及cleanup/exit，确认上述结论，无阻断；未重跑构建/专项或读取秘密文件。后台刷新未硬阻断且未完整监测；启动器未主动刷新或调用模型，不把其0次当成完整出站观测。

## 接续

本轮手工OAuth验收完成。用户另反馈巡检日志`totalFiles=1`、`probeSetCount=0`、`sampledCount=0`、`targetTypes=[codex]`；凭证已被列表读取，但BPS未进入所选Codex集合。只读核证确认当前巡检仅支持Codex/xAI，BPS选项、后端校验和专用探针尚未接入，见[独立缺口记录](BPS-INSPECTION-GAP.zh-CN.md)。没有复用本轮账号发巡检请求。

当前责任人：主会话，已归档本轮通过证据并完成巡检支持诊断；BPS巡检待成套方案与实施。Codex正向2FA、HTTPS中转及OVH现场仍待验。无SQL/schema迁移、数据清理或生产写入；旧分发包尚未重新绑定新CPA/Manager，`write_release_ready=false`。
