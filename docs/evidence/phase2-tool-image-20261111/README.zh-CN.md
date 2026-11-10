# BPS 工具与图片各一次验收

状态：工具基础已冻结。用户最新指定 `gpt-6-luna`，并要求先核 D:/sub2api 的 Excel/BPS 图片支持与协议；本目录现有 payload 仍是前一轮 `gpt-5.6-sol` 基线，待主会话确定移植范围后再适配，不作为最新真实执行入口。真实工具、图片请求均未执行。

本工具复用 `phase2-business-20261111` 的三组件隔离、单行 BPS 登录、保存注册核验、并发覆盖、正常刷新和清理流程。业务阶段仅提交工具、图片各一次客户端请求，无第三个文本请求，无工具结果后续轮次。当前交付仅完成离线验证，真实结果由主会话另记。

## 固定请求与预算

两项均请求 `POST /v1/responses`，模型 `gpt-5.6-sol`、`reasoning.effort=low`、`stream=false`，不带任何 max token 参数。

- `tools`：输入 `Call ping once.`，定义无参数、无外部副作用的 `ping` 函数，并以 `tool_choice` 指定该函数。工具只检查响应是否有该函数调用，不执行函数或提交函数结果。
- `image`：用户输入一段颜色识别文本和 `input_image`；图片为本目录 `red-32.png` 对应的内嵌 32×32 纯红色 PNG，无外部图片 URL。

登录、注册或刷新失败时不提交业务请求。业务阶段进入后，无论工具请求的 HTTP、传输或终态校验结果如何，仍提交图片一次；没有客户端重试，重定向直接报错。每项单独输出 `feature`、`probe_outcome`、HTTP 整数和白名单诊断；失败导致进程 exit 1，最终仍核排空与清理。全部成功才有 `result=passed`。`probes_complete=true` 只表示两项均已尝试。

现有 BPS 仅支持文本；预期工具、图片会在本地能力校验被拒绝。工具保留真实 HTTP 结果及固定错误分类，不输出原始错误消息、正文、URL、邮箱、凭证或生成内容。不绕过能力守卫、quota 或 challenge。CPA 内部上游尝试次数未计量，仍记录 `upstream_attempts=unknown`，不将客户端两次等同上游两次。

## 执行入口

在仓库根目录使用固定镜像与完整 revision。默认无凭证预检：

```powershell
node docs/evidence/phase2-tool-image-20261111/run.mjs --cpa-image mx-cpa:phase2-local-20261111-r2 --manager-image mx-cpamp:phase2-local-20261111-r2 --worker-image mx-oauth-login:phase2-local-20261111 --source-revision 4367adaf687ce336e81122fcdb530c0e66e6eb33 --manager-revision bb14b3a1c60f1b84b31a19ea5b1dde71edaee00f --worker-revision 95dd9ca2c16d5469c43ce7c910f38ff271af45e3
```

真实执行由主会话在同命令添加 `--execute`，经受控进程 stdin 输入 `email`、`password`、`totp_secret` JSON。真实材料不得写文件、argv、环境变量或 shell 历史。本工具不自动触发真实执行，不构建或拉取镜像，不推送或部署。

随机独立容器使用只读根、tmpfs 状态、`log-driver=none`、无端口发布、无生产挂载、无 Docker socket；本机代理保持 `host.docker.internal:7890`。镜像逐角色核完整 OCI revision。finally 删除本轮 label 下所有容器并查询确认；未确认时 `token_state_retained=null`。

## 离线命令

```powershell
node docs/evidence/phase2-tool-image-20261111/verify-fixture.mjs
node docs/evidence/phase2-tool-image-20261111/verify-diagnostics.mjs
node docs/evidence/phase2-tool-image-20261111/verify-harness.mjs
```

fixture 覆盖两次完整 payload 与预算、PNG 尺寸及像素、登录/索引/刷新失败短路、工具/图片各自 HTTP 400/403/500、传输失败、异常终态后继续下一项，以及秘密哨兵不输出。Docker CLI 替身覆盖创建成功但 CLI 返回失败、业务 exit 1 后清理、镜像 revision 不符及非法 revision 停止。所有离线测试均不访问 Docker、网络或真实账户，不证明真实 BPS 能力通过。
