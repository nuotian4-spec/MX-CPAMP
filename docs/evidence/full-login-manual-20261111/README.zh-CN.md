# 完整登录临时窗口

基线 `b48ff4f4248e4ef939406a229553982cf730cd61`，分支 `codex/full-login-window`。本工具补齐 CPA、Manager、Worker 临时拓扑；产品门控保持。默认1小时，支持 `start --ttl 900`；秒数范围60–86400，设置仅作用本工具。

## 已核输入与预检

| 组件 | ref | 固定镜像 ID | revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-inspection-repair-20261111` | `sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc` | `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b` |
| Manager | `mx-cpamp:bps-inspection-repair-20261111` | `sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429` | `de1c4356cd80363c6b28384fdf3e3a704380ec26` |
| Worker | `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

实际启动前同时核 ref、ID、平台和 revision。Worker 组件相对其镜像 revision 比较退出0，复用已有镜像；不凭 tag 推断等价，不新建产品镜像。

三脚本语法检查退出0。一次默认3600秒 `network none` 完整预检退出0，原始输出在 `preflight/PREFLIGHT.jsonl`，退出码在 `preflight/*.exit`：

- CPA 根 namespace 断网；三应用共享 namespace、根只读、tmpfs 状态、Docker 日志 none，预检无端口发布。
- Worker 以镜像原用户 node 运行，secret tmpfs 归 UID/GID1000，真实入口成功读取新随机密钥；Manager 配置同一共享密钥及内部 URL。
- Manager 能力 true、无管理员密钥401；Worker health 正确共享密钥200、错误/缺失401、未知任务404，独立于能力标志核互通。
- Codex/BPS 最近批次及活动批次均空，账号列表与模型配置为空。
- 正常 Worker 启动路径以 Tasks 相同过滤环境直接运行同一 `proxy-runner.mjs`；代理由 runner 自身设置，空 EOF 穿过原 `login.mjs` 并被拒绝，子进程退出、收集缓冲清空。未 POST 任务，未提供真实或虚假账号材料；该项不证明真实材料路径或真实登录成功。
- 默认 `ttl_seconds=3600`、实际到期时间及 `cleanup verified=true` 均有记录。

四入口沿用现有页面与组件证据：`InternalLoginCards.test.tsx` 已覆盖能力 false 禁用两处2FA但BPS手工入口独立，严格 true 后开放、切实例门控与空批次；`OAuthPage.test.tsx` 覆盖手工 OAuth 生命周期。页面同时组合两类2FA、BPS手工与内置Codex手工入口。相关源码、测试及Web依赖相对 `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44` 比较退出0，复用 `components/cpamp/tests/evidence/oauth-input-diagnostics-20261111.md` 的React专项退出0证据，不重跑或把它称作实际浏览器布局验收。输入比较结果见 `preflight/INPUT-EQUIVALENCE.json`。

## 启动与清理

由主会话审完后运行，工具返回空面板，不自动生成OAuth、提交登录或查询官方权限：

```powershell
$taskNode = 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe'
$taskScript = 'D:/MX-CPAMP/docs/evidence/full-login-manual-20261111/run.mjs'
$taskLines = & $taskNode $taskScript start
$taskStartExit = $LASTEXITCODE
$taskLines
if ($taskStartExit -ne 0) { throw "启动失败，退出码 $taskStartExit" }
$taskReady = $taskLines | ForEach-Object { $_ | ConvertFrom-Json } | Where-Object stage -eq 'ready'
$taskStateDir = $taskReady.state_dir
notepad.exe $taskReady.access_file
& $taskNode $taskScript status $taskStateDir
```

`ready` 输出包含本轮绝对 `state_dir`、临时访问文件、面板URL、时长与到期时间；密钥不输出到终端。访问文件按本机私有ACL保存，登录时关闭“记住密码”。新 PowerShell 窗口应显式设置本轮 `$taskStateDir`，不猜最新目录。

面板提供 Codex手工OAuth、Codex 2FA、BPS手工OAuth、BPS 2FA。用户按主会话确认的真实范围操作；一次只启动一个批次，另一入口存在活动批次时暂时禁用符合串行约束。输入可编辑、能力 true 或 Worker health200 均不代表真实授权、保存注册或推理成功。

```powershell
& $taskNode $taskScript verify $taskStateDir
& $taskNode $taskScript stop $taskStateDir
```

`verify` 仅核本地能力、Worker鉴权、批次是否为空及数量、启动记录；不发官方OAuth/access/刷新/推理，也不提交登录。结果仅状态/数量/布尔，不输出账号、令牌、完整回调或模型目录。`stop` 核随机label归属，清理本轮三应用和访问文件，确认无同归属容器后输出 `verified=true`。

到期只停进程，仍须显式 `stop`；完成后清除本轮浏览器登录状态。真实启动仅在 `127.0.0.1` 发布Manager随机端口，CPA与Worker端口内部使用。Manager设置 `OAUTH_LOGIN_URL=http://127.0.0.1:18417` 和 secret文件；Worker仅只读挂载本目录脚本，无业务卷、旧凭据或Docker socket。代理固定本机7890，不应用于服务器。

后台自动刷新未硬禁，真实发生次数未监测；脚本主动请求为零不代表所有后台请求为零。真实登录结果另按用户回报和业务证据判断。

## 验证命令与责任

```powershell
& $taskNode --check 'D:/MX-CPAMP/docs/evidence/full-login-manual-20261111/run.mjs'
& $taskNode --check 'D:/MX-CPAMP/docs/evidence/full-login-manual-20261111/worker.mjs'
& $taskNode --check 'D:/MX-CPAMP/docs/evidence/full-login-manual-20261111/proxy-runner.mjs'
& $taskNode $taskScript preflight
```

本轮未真实 start/OAuth/access/刷新/推理/登录提交，未改产品、旧入口或生产数据，无数据库动作、push/deploy/OVH操作。当前责任人：主会话独审与整合；下一责任人：主会话启动已审空窗口，再由用户完成已明确范围的真实操作。
