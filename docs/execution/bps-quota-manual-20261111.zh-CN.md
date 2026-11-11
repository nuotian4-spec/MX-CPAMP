# BPS 官方账号用量页面验收

2026-11-11。固定上一轮已验收的 CPA、Manager 和 Worker，不重建产品。[运行工具](../evidence/bps-quota-manual-20261111/README.zh-CN.md)源提交 `44297cad0670f061dd42182a57fb1541cc6c3fc0`，主线整合 `03df033`。旧工具、窗口、凭证和历史回执未修改。

## 当前窗口

- 面板：`http://127.0.0.1:3303/management.html#/oauth`。
- 访问文件：`C:/Users/子羽/.codex/artifacts/mx-bps-quota-manual-ddbbbfeb3310/本机访问.txt`，临时管理员密钥只存该本机私有文件，不写仓库或聊天。
- 状态目录：`C:/Users/子羽/.codex/artifacts/mx-bps-quota-manual-ddbbbfeb3310`；归属 `mx.bps-quota-manual=mx-bps-quota-manual-ddbbbfeb3310`。
- 截止北京时间 `2026-11-11 20:05:02.982`。到期仅停止进程，主会话仍须按本目录归属 stop 清理。

`PREFLIGHT.exit=0`：断网、空状态、三镜像身份、Manager/Worker鉴权、原代理入口空EOF均通过，清理verified=true。`START.exit=0`：正式本机窗口健康200、页面200，未认证401、认证未知会话404；Worker完整配置，账号0、批次0、巡检0。没有工具发起的OAuth、access、usage、刷新或推理；后台自动刷新沿既有产品行为，未硬禁且实际次数未监测。日志和非秘密指针见[窗口状态](../evidence/bps-quota-manual-20261111/WINDOW-STATE.json)。

访问文件与浏览器面板的应用打开请求均返回queued，尚无浏览器实际显示或真实账号页面通过结论。用户可直接使用上述链接和访问文件。

## 用户最小操作

1. 使用临时管理员密钥登录，关闭“记住密码”。在OAuth页的 **BPS OAuth** 卡片生成授权链接，官方页面完成一次授权，在面板内提交完整BPS回调，导入1份账号。
2. 打开 **账号管理 → 凭证列表**，对该BPS点一次 **刷新额度**。核 **官方账号用量** 的实际窗口、百分比与重置时间；仅有重置时间时百分比应保持未知。
3. 打开 `/#/accounts?view=health&healthMode=server`。**编辑配置**，巡检凭证提供商仅选BPS，保持自动巡检关闭，**保存并生效**；点 **立即服务端巡检**，在确认框确认一次。结果应显示BPS权限及独立的 **官方账号用量 / 观察时间**。

账号页刷新与服务端巡检分别查询一次用量，后者另查询一次BPS权限；只运行这一个巡检模式。浏览器本机巡检仅存本地浏览器结果，不能替代本轮Manager记录核验。本轮不要求发布模型、刷新令牌或模型请求；手工OAuth中的官方交互由用户完成，密码/TOTP/完整回调不进入聊天。

## 主会话接续

用户完成后，只读verify当前唯一归属的本地记录，不再主动请求官方。核账号BPS数量、服务端结果及有效官方观察布尔；它不替代用户对账号卡的页面确认。验收完成或窗口到期后stop，保留脱敏回执并确认无归属容器和访问文件残留。

```powershell
$taskNode='C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe'
$taskScript='D:/MX-CPAMP/docs/evidence/bps-quota-manual-20261111/run.mjs'
$taskStateDir='C:/Users/子羽/.codex/artifacts/mx-bps-quota-manual-ddbbbfeb3310'
& $taskNode $taskScript verify $taskStateDir
& $taskNode $taskScript stop $taskStateDir
```

验收已完成：用户导入1份BPS并完成账号页额度查看与服务端巡检；页面显示“官方账号用量”、剩余99%和重置时间，详情显示最近巡检观察时间。只读 `verify` exit0 记录1份BPS、1条服务端巡检和1条合格官方用量观察；`stop` exit0 且归属清理通过，脱敏结果见 [FINAL-RESULT](../evidence/bps-quota-manual-20261111/FINAL-RESULT.json)。

当前责任人：主会话完成本地验收收口。`manual_page_acceptance=passed`、`quota_product_live_verified=true`、`write_release_ready=false`。本地页面验收不扩展为2FA、令牌刷新、推理、其他账号或生产部署验收；无产品代码、依赖、SQL/schema迁移、推送或生产变更。
