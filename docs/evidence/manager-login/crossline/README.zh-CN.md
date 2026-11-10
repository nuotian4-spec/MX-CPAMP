# 真实 CPA / Manager / Worker 离线集成

在仓库根目录使用固定 Node22.23.4 执行：

```powershell
node docs/evidence/manager-login/crossline/run.mjs mx-cpa:integration mx-cpamp:integration mx-oauth-login:candidate
```

三个参数分别为已构建 CPA、Manager、Worker 镜像。运行时打印真实完整镜像 ID。测试不构建、不拉镜像、不新建 Docker 网络；三个容器共享 CPA 的 `network none` 命名空间，唯一通信为该命名空间内的 localhost。所有配置、假服务密钥、账号文件均写入本次随机命名的临时卷；密码/TOTP 仅内存与真实 stdin/fd3。结束后清理本次容器与卷并核验清理结果，不处理其他资源。

`prepare.mjs` 生成临时配置；`worker.mjs` 使用已构建 Worker 的真实 HTTP/Tasks 实现，只把官方登录替换为 `credential-runner.mjs` 的单次合成授权。`check.mjs` 通过真实 Manager API 创建任务，穿过真实 CPA v8 handler、文件存储与热注册；不使用假 CPA HTTP 响应。

验证两通道各一个合成账号的 provider、refresh token、登录来源和脱敏任务回执。选取本次 Codex 实际注册的首个模型作为 BPS 测试模型，显式配置重复两次，核 BPS 注册与公开模型列表都去重；该模型选择只是目录契约测试，不证明上游可用。另核 health、未认证401，以及 POST已接受但响应体被丢弃后按预知任务编号取消，CPA没有第三次导入。

这份脚本不发模型请求，不连官方登录或真实账号，不提供真实 OAuth/模型业务验收证据。临时 CPA 配置变更只发生于此次本地隔离卷。
