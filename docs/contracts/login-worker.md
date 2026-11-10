# 内部登录 Worker Interface

HTTP 仅 Compose 内网 `http://oauth-login:18417`。每请求必须 `Authorization: Bearer <shared-secret>`；secret 从 `OAUTH_LOGIN_SECRET_FILE=/run/secrets/oauth_login_secret` 读取，至少32字节。Manager 地址配置 `OAUTH_LOGIN_URL`，同名 secret 文件配置。响应 `Cache-Control: no-store`，无正文日志。

- `POST /tasks`：JSON `{task_id?:UUID,profile:"codex"|"bps",email,password,totp_secret}`。202返回 `{id,profile,status:"running",created_at}`；单个进行中任务，忙时409；输入400；正文16KiB。Manager应提供task_id，POST响应丢失后仍可按同ID取消。
- `GET /tasks/{id}`：状态及仅服务间可读成功结果 `{result:{profile:"codex"|"excel",client_id,access_token,refresh_token,id_token,account_id,email,expired:RFC3339}}`。bps任务固定Excel profile/client。
  失败终态另带脱敏 `stage`、`reason`；两者只允许固定枚举，未知为 `unknown`。`stage` 表示执行位置，不表示验证成功；`reason` 表示已识别的交互类别，不归因账号、地区或代理。Manager任务接口透传相同字段，实际进入保存时改为 `stage=cpa_import`。
- `DELETE /tasks/{id}`：运行中杀整个进程组，待退出返回cancelled；终态只删内存任务与授权结果，返回原status与 `result_cleared:true`，不宣称撤回CPA账号。Manager导入结束后必须清理。
- `GET /health`：同样需要secret，返回 `{status:"ok"}`。
- `DELETE /tasks`：仅 Manager 进程首次创建任务前调用，终止并清理前一次 Manager 进程遗留的任务与结果；成功后才接收新任务。启动时不从磁盘恢复密码或授权结果。

未知路径/方法404，未认证401，未知任务404。error_code仅枚举，无原始异常、回调URL。状态 running / succeeded / failed / needs_user_action / cancelled / expired。25分钟超时，终态结果保留最多5分钟，重启不恢复任务。

密码/TOTP只在当次内存、子进程stdin与官方TLS请求使用，不经argv/env/输入文件/checkpoint/SQLite/log。授权结果走fd3私有pipe；任务stdout/stderr丢弃。Python TLS和SDK进程继承任务进程组；取消、超时和完成均清理组。容器必须 `init:true` 回收子孙进程，不映射公网、不挂业务数据或Docker socket。

Worker succeeded只指取得授权；Manager仅在CPA真实保存注册成功后标自己的任务成功。Worker不导入账号、不刷新token、不保存密码/TOTP作无人值守重登。额外验证立即needs_user_action，无自动重试。

固定授权：Codex client `app_EMoamEEZ73f0CkXaXp7hrann`、回调 `http://localhost:1455/auth/callback`；BPS client `app_fnr0pYvVwwFDocDumLG3H2Bp`、回调 `https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback`、兑换 `/oauth/token?unified=true`。每次仅一份profile授权。TLS验证启用，不接受任务目标源/代理/TLS开关。

真实OpenAI登录与BPS手工OAuth回调可兑换性待用户授权验收；假上游证据只证明本地契约。来源与许可见 `components/oauth-login/NOTICE.zh-CN.md`。

诊断阶段：`input/web_login/authorize/email/email_otp/password/totp/workspace_session/callback/token_exchange/credential_validation/cpa_import/unknown`。Codex只有命中邮箱验证码分支才标 `email_otp`；仅命中MFA条件才标 `totp`，后续跳转恢复所属阶段。

诊断原因：`security_check/email_verification_required/phone_verification_required/account_setup_required/unexpected_login_page/session_selection_required/totp_factor_missing/interaction_required/password_rejected/totp_rejected/callback_invalid/token_exchange_failed/credentials_incomplete/identity_mismatch/invalid_profile/transport_error/protocol_error/unknown`。不返回原始异常、HTTP正文、URL查询、Cookie或令牌。额外交互仍以 `needs_user_action` 结束，无自动重试。
