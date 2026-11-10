# 诊断字段隔离复测工具

本工具接续2026-11-10已验证的隔离构型，保留旧工具和回执原文。工具本身不持有账号；真实登录只能由P9完成诊断独审后另行触发。旧测试凭证已修改，不作为输入来源。

## 调用与输入

```text
node run.mjs --preflight
node run.mjs --profile codex
node run.mjs --profile bps
node run.mjs --profile both
```

默认profile为both，顺序Codex、BPS。每个profile最多发一次Manager登录POST；失败不重试同profile，保存或停止结果未知时中断后续profile。仅实际登录模式从stdin接收`{"email":"EMAIL","password":"PASSWORD","totp_secret":"BASE32_SEED"}`；preflight不读取或提交该材料。秘密不放argv、env、输入文件、日志或回执。

非秘密候选参数：`--cpa-image`、`--manager-image`、`--worker-image`可指定已有本地镜像，默认均为20261110-rc1；工具只inspect/run，不拉取镜像。每次记录实际完整镜像ID。

可选择在旧镜像上只读叠加候选输入：`--worker-source ABSOLUTE_SRC_DIR`挂载到`/app/src`；`--manager-binary ABSOLUTE_LINUX_BINARY`挂载独立Linux可执行文件。使用叠加时必须提供`--source-revision FULL_40_HEX_SHA`。工具记录镜像ID、revision、两个叠加标志与输入摘要，结束复核输入未漂移；不会写额外校验文件。叠加是本地诊断配置，不等同完整候选镜像或生产部署。

已构建的诊断候选仅用于本轮无凭证preflight：产品源revision为`a5eaebbb777152ce71900af447289e61e8088316`，Manager临时binary的ldflags SourceCommit为短值`a5eaebb`。本地Manager产物与Worker源码叠加通过健康检查，也不代表最终页面构建、正式镜像或真实登录验收通过。

## 脱敏诊断契约

B线终态字段为`stage/reason`，保留`status/error_code`，没有`http_status`。工具自己的`stage`表示images、ready、create、login、cleanup等执行阶段；任务字段分别映射：

| 来源 | 输出字段 |
|---|---|
| Manager task.stage/reason | login_stage/login_reason |
| 原Worker task.stage/reason | worker_stage/worker_reason |
| Manager/Worker error_code | error_code/worker_error_code |

`diagnostics.mjs`按最终契约列举stage/reason/status/error_code；未知字段或未知枚举丢弃/置null，不转发原异常或上游正文。旧基线没有诊断字段时输出null，不宣称字段已验证。Worker仅在原Tasks.finish之后将脱敏终态写入tmpfs，避免Manager清理原任务后丢失稳定码；原授权、身份验证、Transport、PKCE和固定profile不变。

stage枚举：input、web_login、authorize、email、email_otp、password、totp、workspace_session、callback、token_exchange、credential_validation、cpa_import、unknown。

reason枚举：security_check、email_verification_required、phone_verification_required、account_setup_required、unexpected_login_page、session_selection_required、totp_factor_missing、interaction_required、password_rejected、totp_rejected、callback_invalid、token_exchange_failed、credentials_incomplete、identity_mismatch、invalid_profile、transport_error、protocol_error、unknown。

## 隔离与证据边界

三组件无端口发布、只读根、日志none；CPA凭据/config、Manager数据/config、Worker共享key与脱敏诊断在tmpfs。管理key和真实材料只经stdin输入。结束后按本次label核无容器残留，只有清理确认才报告token状态未保留；清理未确认则为null。无模型调用、无主动刷新、无真实OAuth回调测试、无生产连接。

Worker原Tasks过滤父环境，测试专用proxy-runner在import原login.mjs前固定注入`host.docker.internal:7890`。这只是当前本地网络配置，不切换出口，也不证明生产VPS网络等价。preflight检查三组件健康、管理鉴权、代理TCP端口与容器边界，不创建登录任务；真实字段仍待候选终态回执。

成功仍要求Manager succeeded、CPA当前注册列表匹配同auth_id/provider/2FA来源、CPA download确认保存且保留access/refresh token；只输出布尔，不输出内容。非成功profile使退出码1。POST回执丢失只按预知task_id查询一次，不重POST；查询未确认即停止。

待验项：新凭证下各通道真实终态stage/reason、成功时CPA保存注册、正式Manager/Worker镜像与完整revision、真实业务与生产验收。preflight和脱敏契约检查均不替代这些结果。
