# 工具诊断候选与验收边界

日期：2026-11-11。主线固定构建输入 `ef4d287ea3298a7680f8fc21e82db77417bbe634`。这是本地候选，未推送或部署；用户新增授权后，已完成下述独立工具往返验收。

## 当前结果

上一轮[真实回执](../phase2-tool-image-20261111/REAL-RECEIPT.jsonl)保持不变：`gpt-6-luna` 文本200、工具首轮502、原生图片200，共三次客户端模型请求。没有工具结果续轮，工具链尚未通过。旧回执缺少错误分支信息，现有证据没有确定历史502的原因。

本轮[独立工具验收](BUSINESS.zh-CN.md)在用户新增授权后完成：登录批次一次，工具首轮和完整历史续轮均HTTP200，合法ping及最终pong通过，共两次客户端模型请求，exit0。没有主动刷新或重复文本/图片；清理及七项输入复核通过。旧失败仍独立保留，成功不用于反推其原因。

[13场景离线对照](../bps-tool502-diagnostic/exact-handler-matrix.log)固定原请求，经过真实公开处理器、AuthManager和Executor：规范包装SSE、直接工具SSE及JSON均成功；多种可达失败可得到相同公开502。这些是合成回包，不是实际上游的复现。

诊断候选增加固定 `error.diagnostics`：阶段、错误码、实际HTTP状态和流内语义状态。原文、响应正文、工具参数、凭据和身份值不进入诊断。公开错误呈现与内部调度错误分离；请求、工具与图片语义、repair及重试策略保持原状。

## 独审与证据复用

- 产品源提交 `b8ad2cd`、`46aa62c`，主线对应 `eaa4527`、`69135b6`；最终产品目录与 `46aa62cd366ec06889ab3817ae78142bb55924d1` 无diff。
- [诊断证据](../bps-tool502-diagnostic/README.md)包括32组公开Responses/Chat普通与SSE契约；最终呈现接线后补跑受影响32组。404旧/新共16次模拟dispatch对照核实内部文本、错误码、原有冷却及精确自定义规则行为一致；三项呈现优先级专项通过。均exit0，独立diff-check通过。
- [工具离线证据](OFFLINE-RESULTS.zh-CN.md)包括27场景、双层白名单及Docker/fetch替身，均exit0。首轮只接受唯一空参ping，续轮不接受任何工具调用；完整历史及call_id配对保留。最终工具目录与源提交 `3a889de` 无diff，诊断模块与已测依赖等价，复用其专项。
- 两条线的独立只读审查均通过。没有重复全仓测试、Manager构建或旧七包race；本次不改变共享绑定、并发或图片状态逻辑，旧证据仍只覆盖原来声明的范围。

## 固定构建

| 组件 | 镜像 | 实际ID | revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-tool-diagnostic-20261111` | `sha256:a4428de0eeda96d76cfa5733eed190ea8b45cd49083bf6b881f4bc9dfc1f5933` | `ef4d287ea3298a7680f8fc21e82db77417bbe634` |
| Manager | `mx-cpamp:bps-tools-images-20261111` | `sha256:6732b8d85cfade68fce21ae286ede773d8151a397c64eedbea0b4618d4b6cd73` | `bd573d0439a925d811e25aeeb575ac91feb5796d` |
| Worker | `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

CPA仅用原Dockerfile最终构建一次，exit0，[构建日志](cpa-build.log)。命令在仓库根执行：

```powershell
$env:http_proxy='http://127.0.0.1:7890'; $env:https_proxy='http://127.0.0.1:7890'
docker build --progress plain --build-arg 'HTTP_PROXY=http://host.docker.internal:7890' --build-arg 'HTTPS_PROXY=http://host.docker.internal:7890' --build-arg 'NO_PROXY=localhost,127.0.0.1,::1' --build-arg 'VERSION=bps-tool-diagnostic-20261111' --build-arg 'COMMIT=ef4d287ea3298a7680f8fc21e82db77417bbe634' --build-arg BUILD_DATE=2026-11-11T02:16:01Z --label 'org.opencontainers.image.revision=ef4d287ea3298a7680f8fc21e82db77417bbe634' --tag mx-cpa:bps-tool-diagnostic-20261111 --file components/cpa/Dockerfile components/cpa
```

`BUILD_DATE`的实际值以构建日志为准。最终运行镜像ENV只有PATH和TZ，没有构建代理。Manager、Worker各自目录相对表中revision无diff，复用旧产物且不重标revision。

## 无凭证预检

固定Node `C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`，使用新[入口](run.mjs)，省略 `--execute`：

```powershell
node docs/evidence/bps-tool-diagnostic-20261111/run.mjs --cpa-image mx-cpa:bps-tool-diagnostic-20261111 --cpa-image-id sha256:a4428de0eeda96d76cfa5733eed190ea8b45cd49083bf6b881f4bc9dfc1f5933 --cpa-revision ef4d287ea3298a7680f8fc21e82db77417bbe634 --manager-image mx-cpamp:bps-tools-images-20261111 --manager-image-id sha256:6732b8d85cfade68fce21ae286ede773d8151a397c64eedbea0b4618d4b6cd73 --manager-revision bd573d0439a925d811e25aeeb575ac91feb5796d --worker-image mx-oauth-login:phase2-local-20261111 --worker-image-id sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758 --worker-revision 95dd9ca2c16d5469c43ce7c910f38ff271af45e3
```

[预检回执](preflight.jsonl)exit0：逐组件ID/revision匹配、按不可变ID启动、实际容器Image复核、服务健康及管理鉴权通过。只读根、tmpfs、无日志、无公开端口；清理确认、token_state_retained=false、全部七项运行输入未漂移。预检没有读取测试凭据，没有登录批次、刷新或模型请求。

原始构建与预检日志保留于 `C:/Users/子羽/.codex/artifacts/mx-bps-tool-diagnostic-*-20261111.*`，仓库副本仅去除行尾空白。迁移/schema/SQL路径命中检查为空；本次没有数据库迁移、数据清理或生产写入。

## 下一步

用户已明确授权，按登录BPS一次、主动刷新零次、工具首轮一次、合法ping后续轮一次完成隔离验收，实际模型请求两次，exit0。没有重复文本或图片、更换凭证、回退Codex或追加请求。

当前责任人：主会话，候选、预检及本轮Responses普通工具往返已完成。下一责任人：主会话整理剩余验收边界；HTTPS中转、手工BPS OAuth、Codex正向2FA及OVH现场验收仍保留待验状态，完整验收后再统一准备发布。
