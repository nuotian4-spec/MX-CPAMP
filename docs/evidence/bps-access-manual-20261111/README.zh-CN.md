# BPS 官方权限与模型页面手工验收

基线：`2f339f8c6f71fba5161198898d27f5bfaabfe775`。入口仅复用并调整旧 `scope-manual-run.mjs`，产品、旧证据和生产配置未变。

## 已完成准备

`node --check`、无账号断网预检和归属清理退出码均为 `0`。原始记录见 `preflight/PREFLIGHT.jsonl`、`preflight/PREFLIGHT.exit` 和 `preflight/NODE-CHECK.exit`。真实 `start`、OAuth、官方 access、模型保存和页面验收尚未执行。

预检固定现有最终镜像，CPA 使用 `network none`，Manager 和仅执行 Node 检查的容器共享其 namespace；无端口发布、无账号、无旧挂载、无 Worker。结果：健康和页面 `200`，未鉴权 `401`，未知本地 OAuth state `404`；凭证列表和全局模型清单均为空；BPS 设置接口存在，access 缺引用在本地返回 `400`，未到上游。`verify` 对空状态返回 `pending`，随后清理确认 `verified=true`。

| 组件 | 镜像 ref | 固定完整镜像 ID | revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-inspection-repair-20261111` | `sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc` | `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b` |
| Manager | `mx-cpamp:bps-inspection-repair-20261111` | `sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429` | `de1c4356cd80363c6b28384fdf3e3a704380ec26` |

预检 Node 镜像为现有 `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758`，只运行检查代码；无安装或产品构建。

## 启动与查看

在 PowerShell 运行；入口已整合至主仓库。`start` 仅开启空面板，返回后再由用户操作；不会创建 OAuth 会话或触发官方 access。

```powershell
$taskNode = 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe'
$taskScript = 'D:/MX-CPAMP/docs/evidence/bps-access-manual-20261111/access-manual-run.mjs'
$taskStartLines = & $taskNode $taskScript start
$taskStartExit = $LASTEXITCODE
$taskStartLines
if ($taskStartExit -ne 0) { throw "启动失败，退出码 $taskStartExit" }
$taskReady = $taskStartLines | ForEach-Object { $_ | ConvertFrom-Json } | Where-Object stage -eq 'ready'
$taskStateDir = $taskReady.state_dir
& $taskNode $taskScript status $taskStateDir
notepad.exe $taskReady.access_file
```

`ready` 输出包含本轮 `state_dir`、`access_file`、本机面板 URL 和到期时间。临时管理员密钥只在访问文件及 Manager tmpfs；按访问文件登录，关闭“记住密码”。不填写 CPA 内部地址或密钥。使用完删除本轮面板在浏览器留下的登录状态。

后续新 PowerShell 窗口应将本轮输出的绝对 `state_dir` 原样赋给 `$taskStateDir`，再执行 `status/verify/stop`。工具不查找“最新目录”。访问文件、密码、TOTP、令牌和完整官方回调不进入聊天或仓库。

## 唯一一次真实操作

1. 面板 OAuth 页面选择 BPS，用户完成一次全新的官方手工授权及面板回调导入。旧凭据不使用。完成后可执行下文 `verify`，先核保存及注册各一个；此时尚未发布模型，结果为 `pending`。
2. 进入 `#/system` 的“BPS 模型设置”，选择刚注册的 BPS 凭证，点击一次“获取官方允许模型”。观察访问允许/受限/错误、官方允许模型数量、目录展示和查询时间。
3. 仅当本次结果明确允许且返回非空目录，点“一键选用到草稿”，把全部发现结果填入草稿；再点“保存 BPS 模型清单”。核保存成功消息以及“全局已发布”数量。本轮使用全部发现结果，发现数、发布数与注册数应一致。
4. 打开账号模型页，只核全局发布数量、本站注册数量与条目展示。不要点“查询本凭证官方权限”，不启动浏览器或服务端巡检。系统设置与账号页的观察独立，账号页显示“权限未确认”符合当前操作范围。
5. 若唯一查询受限、失败、返回空目录或回执未确认，记录稳定状态并停止后续保存，不重试，不把未知目录当成空清单保存。随后执行 `verify` 和 `stop`，交由主会话判断。

打开页面、局部刷新、人工保存以及账号页打开只读取本地配置/列表，不额外发官方 access。唯一 access 由上述系统设置按钮触发。脚本没有 `probe` 动作。

## 记录与只读核验

页面目录只在当前页面观察中保留；人工保存的模型进入本轮 tmpfs 配置。验收回执只记录状态、数量和布尔，不重复保存目录、邮箱或上游身份。`verify` 读取本地注册、已保存凭证是否齐全、模型配置/注册和已有人工观察；不查询官方 access，不刷新、不推理，也不启动巡检。

用户只需回报页面截图或状态与计数；截图避开邮箱、凭据、完整回调。主会话按真实回报填写本轮 `page-observation.json`，用户无需编辑 JSON。字段如下；未确认的事实不填成成功。

| 字段 | 事实范围 |
|---|---|
| `official_access_result` | `allowed/denied/error/unconfirmed` |
| `official_access_click_count`、`official_allowed_model_count` | 实际点击数、页面官方允许数 |
| `official_model_list_seen`、`query_time_seen` | 目录和查询时间是否展示 |
| `model_save_message_seen`、`account_published_and_registered_seen` | 保存消息和账号发布/注册是否核到 |
| `account_official_query_clicked`、`inspections_started` | 是否发生额外权限查询或巡检 |

主会话完成脱敏记录后执行：

```powershell
& $taskNode $taskScript verify $taskStateDir
$taskVerifyExit = $LASTEXITCODE
Write-Output "VERIFY_EXIT=$taskVerifyExit"
```

有且仅有一个保存且 active 的 BPS、模型配置与注册一致、人工记录确认一次允许查询并保存全部发现结果时，`result=passed`。该结论同时包含本地核验和用户页面观察，`manual_observation_is_user_report=true`。缺少观察或尚未完成时 `pending` 且退出码仍为 `0`；工具/API/输入错误退出 `1`。结果写本轮私有 `verification.json`，可将该脱敏结果交主会话；不要分享 `state_dir` 内其他内容。

## 停止与边界

```powershell
& $taskNode $taskScript stop $taskStateDir
$taskStopExit = $LASTEXITCODE
Write-Output "STOP_EXIT=$taskStopExit"
```

请在15分钟内完成操作并执行 `stop`；到期停进程仍须 `stop` 清理。`stop` 按随机 label 核归属、删除本轮容器和临时访问文件，确认无同归属残留后输出 `verified=true`。仅保留非秘密状态及脱敏核验；新窗口不复用旧 tmpfs。

真实启动仅发布 `127.0.0.1` 上一个 Manager 随机端口，CPA 内部端口不发布。两服务只读根、空 tmpfs、Docker 日志 `none`、无旧挂载与 Worker；本机出口配置为 `host.docker.internal:7890`。900 秒到期由容器内 timeout 停进程，容器和访问文件仍须显式 `stop` 清理。

一次 OAuth、一次 access 是本轮人工操作约定，工具不硬性拦截 UI 重复点击。后台自动刷新没有硬禁，也没有监测其真实次数；回执中的脚本主动刷新/推理计数为零，不代表运行期间全部请求为零。查询不证明额度、真实推理或生产已部署。

无 SQL/schema 迁移。未来人工模型保存只写本轮隔离 CPA 配置。无 push、deploy 或 OVH 操作。

## 本轮预检命令

```powershell
& $taskNode --check $taskScript
& $taskNode $taskScript preflight
git -C D:/MX-CPAMP-worktrees/bps-access-inspection diff --check
```

预检不创建访问文件；其 `verify pending` 为预期空状态。最终回执仅收窄目录输出字段，启动、网络、请求、配置或清理输入等价，复用既有预检；最终语法检查另存退出码。

当前责任人：主会话核证与交付；下一责任人：用户完成一次新手工授权及页面操作，主会话核验脱敏回执并清理。`upstream_access_verified=false`、`write_release_ready=false`。
