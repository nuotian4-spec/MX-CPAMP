# 隔离真实 2FA 验收工具

此工具使用已有 rc1 的 CPA、Manager、Worker 镜像，每个 profile 仅创建一次登录任务，依次 Codex、BPS；不调用模型、不主动刷新令牌、不接生产。真实材料由调用方经进程 stdin 提供，本文及脚本不包含账号。工具自身不运行即不发起登录。

```text
node run.mjs --preflight
node run.mjs
```

第二条从 stdin 接收一个 JSON 对象：`{"email":"EMAIL","password":"PASSWORD","totp_secret":"BASE32_SEED"}`。调用方使用进程管道输入，不将秘密写入命令行、环境变量、临时输入文件或聊天回执。不要通过带真实值的 shell 命令构造 stdin。

`--preflight` 不读凭证、不创建登录任务；只启动隔离组件、核内部健康和鉴权、检查本地代理 TCP 端口可达并清理。代理端口可达不代表官方认证可用。

CPA 凭据与配置、Manager SQLite 与配置、Worker 共享认证密钥及脱敏诊断都在容器 tmpfs；只读绑定目录仅包含这些无秘密脚本。无端口映射、无业务卷、无 Docker socket，容器日志驱动为 `none`。随机管理密钥和真实登录材料均通过 stdin 进入容器。结束后按本次随机资源名清理，并核无带本次 label 的容器残留。

## 网络与证据语义

Worker 原 Tasks 会过滤代理环境。测试专用 `proxy-runner.mjs` 在导入原镜像 `/app/src/login.mjs` 前设置本机 `host.docker.internal:7890` 代理；授权、协议和传输仍使用原产品实现。该网络注入只属于本地测试配置，**不证明原镜像在生产 VPS 的网络行为等价**，不写入产品源码或生产部署。

`worker.mjs` 继承原 Tasks，只在原终态回调后记录状态与白名单错误码到 `/tmp`，不记录登录材料、token、原始响应或异常。`check.mjs` 报告 Manager 终态和可用的原 Worker 稳定错误码；缺失时为 null，不推断失败原因。

成功要求：Manager 任务终态为 succeeded 且有 auth_id；CPA v8 凭证列表从当前 authManager.List 构建，匹配相应 auth_id、provider 与2FA来源以核实热注册；CPA download 返回已保存的同通道凭证，含 access/refresh token 和2FA来源。列表中的 source 可因文件存在变为 file，不作为注册否决条件。令牌内容只在校验进程内存中检查，不输出；全部临时凭据随后随容器清理。BPS 未配置模型目录也可证明保存和热注册，不代表模型可调用。

`needs_user_action` 或确定失败结束该 profile，不自动重试，并使最终进程退出码非0；可按授权继续另一 profile。POST回执丢失时只按预先生成的 task_id 查询，不重发POST；查询仍未确认即停止核实。保存/停止结果未知时不继续另一 profile。任何工具故障只输出固定 harness 错误码，不输出命令错误正文。最终 `cleanup.verified=true` 才表示本次容器资源清理核验通过，并报告token状态未保留；未核实时该项为null。原始密码/TOTP不持久保存。

本工具不等同于二期批量导入或凭证并发功能实现，也不验收 BPS 手工 OAuth 回调、生产登录、刷新或模型业务。

## 无凭证入口辅助探针

`node run-entry-probe.mjs` 在随机、只读、无日志、tmpfs容器内，使用同一rc1 Worker、当前测试代理、原Transport与ProtocolClient，分别对固定ChatGPT根入口和Auth根入口各发一次GET。不会读取测试账号、发送登录POST、跟随重定向、切代理或处理安全检查页面；分类直接使用core的原判别结果。输出仅目标标签、HTTP状态、安全检查分类布尔与稳定transport_error，并独立核清理；脱敏结果为 `entry-probe-receipt.jsonl`。

该探针是执行时入口响应的辅助证据，不反推前一次真实登录的具体失败阶段，不代表2FA、BPS回调兑换或生产网络验收通过。
