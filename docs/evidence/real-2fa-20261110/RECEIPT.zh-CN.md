# 本地隔离真实 2FA 回执

日期：2026-11-10，北京时间。输入基线：`66736ed731535f94dfce76a1c88a752a98bcc86a`。用户提供专用测试材料并授权两通道真实登录；原始账号、密码、TOTP 不写入本回执。

## 实际结果

| 通道 | 实际登录任务数 | Manager 终态 | Worker 原稳定码 | CPA 保存并注册成功 |
|---|---|---|---|---|
| Codex 2FA | 1 | `needs_user_action` | `needs_user_action` | 未取得成功回执 |
| BPS 2FA | 1 | `needs_user_action` | `needs_user_action` | 未取得成功回执 |

测试进程退出码：1。原始脱敏行回执：[real-login-receipt.jsonl](real-login-receipt.jsonl)。每通道仅创建一次任务，终态后没有重试登录；未发送模型请求、主动刷新或生产管理请求。

`needs_user_action` 是多分支汇总分类，现有实现将额外交互、安全检查页、非预期登录页、部分 MFA/会话选择异常映射为同一码。本次回执未保留具体 HTTP 阶段，不能据此确认密码或 TOTP 正误，也不能直接归因为某个安全检查分支。

## 测试条件

- 使用与生产发布清单相同的三个 rc1 镜像完整 ID，见 JSONL 回执；产品源码和镜像未改。
- 本地测试 runner 注入 `host.docker.internal:7890` 代理后调用原 Worker 登录代码；该条件不证明 OVH VPS 的出口及真实认证表现等价。
- 实际穿过 Manager 任务 API、Worker、原真实登录实现；没有使用假授权 runner。
- 启动前四脚本 `node --check` 与无凭证 `--preflight` 退出0，隔离工具经独立只读审查。
- 管理鉴权、三服务内部健康和代理 TCP 可达检查通过；这些检查不代表官方登录成功。

## 数据与清理

登录材料通过进程 stdin 进入内存；临时配置、Manager 数据和可能生成的授权数据仅使用容器 tmpfs。容器根文件系统只读，日志驱动 `none`，无端口映射、生产数据卷或 Docker socket。

回执确认 `cleanup.verified=true`、`token_state_retained=false`；主会话另核本次 `mx.real2fa` 标签容器无残留。产品代码、部署文件、生产服务器和既有凭证保持原状。

## 入口辅助探针

随后以相同 Worker 镜像、原 Transport/ProtocolClient 和本地测试代理，对 ChatGPT 与 Auth 根入口各做一次无凭证 GET。两者均为 HTTP200，均未命中 core 的安全检查页分类；执行退出0、临时容器清理核验通过，见[入口探针回执](entry-probe-receipt.jsonl)。这只证明当前两个根入口可达，不证明登录接口或前次认证过程未遇到挑战。

当前结论：真实 2FA 验收未通过，两通道均被登录器归类为需要交互，具体分支待定位；入口探针和静态分类核对已经完成。本轮不再提交账号、不尝试求解或绕过官方挑战。下一责任人：P9 汇报真实结果，后续先补足脱敏阶段诊断，再安排有明确观测目标的验证。
