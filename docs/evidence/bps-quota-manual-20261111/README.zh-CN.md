# BPS 官方账号用量页面验收窗口

基线 `6d7cbc6dfcbc72ceb6ac057539c045d873873622`，固定本地候选见上一轮 `bps-quota-repair-20261111/FINAL.zh-CN.md`。本工具独立于旧窗口；旧工具、回执及凭证均保持原位。`worker.mjs`、`proxy-runner.mjs` 为原 `full-login-manual-20261111` 素材的原样副本，只读挂载到新 Worker，不读取或复制旧运行态/凭证。

| 组件 | ref | 固定镜像 ID | revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-quota-identity-20261111` | `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b` | `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` |
| Manager | `mx-cpamp:bps-quota-repair-20261111` | `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd` | `d26cbf7595ecf41ba8cbfd90c956291ac351b471` |
| Worker | `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

默认3600秒，`start --ttl 秒数` 可用60–86400。启动核 ref、ID、revision、平台；新随机 prefix `mx-bps-quota-manual-`、owner label `mx.bps-quota-manual`。CPA、Manager、Worker 共用临时命名空间，根只读、tmpfs、Docker日志 none、仅 Manager 的单个随机端口发布在 `127.0.0.1`。Worker 完整配置、secret 文件归属和7890代理沿既有路径。无业务卷、Docker socket 或旧凭证挂载。

## 独审后由主会话执行

本线只做三脚本语法和 diff-check。独审后由主会话唯一一次断网 preflight，再 start 空面板：

```powershell
$taskNode = 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe'
$taskScript = 'D:/MX-CPAMP/docs/evidence/bps-quota-manual-20261111/run.mjs'
& $taskNode $taskScript preflight
if ($LASTEXITCODE -ne 0) { throw '断网预检失败' }
$taskLines = & $taskNode $taskScript start
$taskStartExit = $LASTEXITCODE
$taskLines
if ($taskStartExit -ne 0) { throw '窗口启动失败' }
$taskReady = $taskLines | ForEach-Object { $_ | ConvertFrom-Json } | Where-Object stage -eq 'ready'
$taskStateDir = $taskReady.state_dir
notepad.exe $taskReady.access_file
& $taskNode $taskScript status $taskStateDir
```

`preflight` 的 CPA 根网络为 none，无端口，完成后按新 label 清理。`start` 创建全新空 `/state/auths`、模型配置和巡检记录，输出本轮 state_dir/访问文件/面板URL/到期时间。管理员密钥仅写本机私有ACL访问文件，不回显到终端；登录关闭“记住密码”。

建议用户本轮手工完成：

1. BPS OAuth 一次；官方登录、挑战及授权在官方页面自主完成。
2. 回到账号页，对该 BPS 手动刷新“官方账号用量”一次。
3. 打开 `/#/accounts?view=health&healthMode=server` 的服务端巡检，编辑配置，巡检凭证提供商仅选 BPS，保持自动巡检关闭，点击“保存并生效”；点击“立即服务端巡检”并在确认框确认一次。核套餐、窗口及观察/重置时间；未知与0区分，保持 keep、无自动动作。服务端结果写入 Manager，供工具只读核对。

工具不会替用户生成 OAuth、提交登录、查询 access/usage、刷新令牌、推理或启动巡检。后台自动刷新沿现有产品行为，未硬禁、实际次数未监测；脚本主动请求为零不代表全部后台请求为零。用量来自官方账号主窗口，不定义 BPS 独占或共享桶。

```powershell
& $taskNode $taskScript verify $taskStateDir
& $taskNode $taskScript stop $taskStateDir
```

`verify` 只读本地 health/能力、批次数量、凭证总数/BPS数量和最多20条已存巡检及结果。仅输出数量、布尔和白名单状态；记录是否存在来源为 chatgpt_account_usage、HTTP200且带窗口的 BPS 观察，不输出账号、窗口值或原始错误。它不查询官方、不证明页面视觉验收；主动刷新未进入巡检记录的结果也不会被该项计为观察。

`stop` 核新 owner label 归属，删除本轮三容器和访问文件，确认无残留。TTL仅停止进程，仍需stop；完成后清除本轮浏览器登录状态。无产品修改、构建、SQL/schema迁移、push/deploy或生产动作。
