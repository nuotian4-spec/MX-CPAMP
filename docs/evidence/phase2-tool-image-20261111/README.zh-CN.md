# BPS 文本、工具往返与图片验收

本工具按 `docs/bps-tools-images-plan.zh-CN.md` 和 ADR 0003 验收 `gpt-6-luna`。登录、正常 refresh 各一次；公开客户端模型请求最多四次。工具源码和离线fixture完成并独审通过；主会话已绑定最终镜像执行本轮，文本和原生图片通过，工具首轮HTTP502，真实结果见[业务回执](../bps-tools-images-final/BUSINESS.zh-CN.md)。

## 请求顺序与成功条件

1. 文本一次：要求精确返回 `OK`。HTTP、传输、终态或语义任一失败即停止后续业务请求，包括 403。
2. 工具首轮一次：仅定义空参 `ping`，`tool_choice=auto`。成功必须是正常 BPS completed 响应，且恰好一个 `ping` function_call、有非空 call_id、参数可解析为空对象。普通文本、未知函数、多个调用或非空参数均失败，不执行其中任何命令。
3. 工具结果一次：只在合法首轮后固定产生本地 `pong`，回传首轮完整用户输入、原响应 output 与匹配原 call_id 的 function_call_output；`tool_choice=none`，不携 previous_response_id。结果必须 completed、没有新工具调用且文本精确为 pong，不继续第三轮。
4. 独立图片一次：文本成功后，无论工具哪一轮结果如何仍执行；发送内嵌 32×32 纯红 PNG，提示仅要求识别主色，不在提示中泄漏红色答案。必须 completed、没有工具调用，且文本仅表示 red、红色或红（允许结尾句号/叹号）。HTTP 200 的蓝色、否定红色或多种颜色文本不算通过。

所有请求使用 `gpt-6-luna`、`reasoning.effort=low`、`stream=false`，无 max token 字段、客户端重试或重定向。登录/注册/刷新失败为零业务请求，文本失败一次，工具首轮失败而图片继续共三次，完整路径四次；工具单项最多两次，图片最多一次。单项失败进程最终 exit 1；全部语义通过才输出 result=passed。probes_complete 仅表示四次请求均已尝试，不代表通过。

每项输出固定 feature、probe_outcome、HTTP 整数与白名单错误分类；最终输出真实客户端计数、文本门禁、工具往返、红色语义和排空布尔值。不输出模型正文、图片、URL、原始错误消息、call_id、邮箱或凭证。上游实际尝试次数没有计量，保持 upstream_attempts=unknown。

## 隔离与入口

复用三组件验收的单行 BPS 批次、保存注册、并发覆盖设为 3、正常单目标 refresh 及身份核验。没有重登、强造过期或切换账号。随机独立容器使用只读根、tmpfs 状态、log-driver=none、无端口发布、无生产挂载或 Docker socket；本机代理保持 host.docker.internal:7890。BPS 默认原生图片与 /state/bps-state 均落入隔离 /state tmpfs。

所有镜像参数及 source-revision 必须显式提供。支持逐角色 revision 和既有只读叠加参数；OCI revision 按完整提交核验。结束复核叠加输入和六个运行脚本未漂移，finally 删除本轮 label 容器并查询确认；未确认时 token_state_retained=null。工具不构建或拉取镜像。

在主会话给出最终参数后替换以下非秘密占位符；默认只预检，不读取凭证、不建登录批次、不刷新、不发业务请求：

```powershell
node docs/evidence/phase2-tool-image-20261111/run.mjs --cpa-image CPA_IMAGE --manager-image MANAGER_IMAGE --worker-image WORKER_IMAGE --source-revision CPA_REVISION_40 --manager-revision MANAGER_REVISION_40 --worker-revision WORKER_REVISION_40
```

真实执行由主会话在同命令加 --execute，并经受控进程 stdin 输入 email/password/totp_secret JSON。真实材料不进入文件、argv、环境变量或 shell 历史。镜像 revision 尚未绑定的模板不作为可执行发布命令。无推送、部署或生产数据动作。

真实JSONL为 [REAL-RECEIPT.jsonl](REAL-RECEIPT.jsonl)，由主会话执行后保存白名单输出，退出码及预算见 [REAL-RECEIPT.meta.json](REAL-RECEIPT.meta.json)。本轮实际3次模型请求，首轮工具失败没有发续轮；没有把该状态写成四次全部成功。

## 离线验证

```powershell
node docs/evidence/phase2-tool-image-20261111/verify-fixture.mjs
node docs/evidence/phase2-tool-image-20261111/verify-diagnostics.mjs
node docs/evidence/phase2-tool-image-20261111/verify-harness.mjs
```

fixture 覆盖完整请求结构、PNG 尺寸与纯红像素、0/1/3/4 实际总预算及工具单项 0/1/2、文本 403 等失败短路、两轮工具配对和完整历史、工具失败后图片继续、HTTP 200 非工具及错误颜色、畸形终态、秘密哨兵过滤。Docker/fetch 替身覆盖默认预检零凭据业务、创建回执失败或业务失败后的清理、revision 不符与非法值、工具脚本输入漂移。离线结果不代表真实 BPS 能力通过。
