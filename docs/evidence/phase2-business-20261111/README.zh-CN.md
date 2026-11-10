# BPS批次、刷新与一次公开请求验收

状态：工具语法、脱敏和离线fixture预算契约已通过；真实执行留P9独审后触发。没有读取或使用任何真实材料，Codex不在工具执行范围。旧测试token已清理，不能复用旧回执代替本轮结果。

## 明确执行与输入

默认或`--preflight`仅启动隔离三组件、核健康及管理鉴权，不读取凭证，不创建登录批次，不刷新令牌，不发模型请求。只有显式`--execute`才从stdin接收`{"email":"EMAIL","password":"PASSWORD","totp_secret":"BASE32_SEED"}`并执行以下流程：

1. Manager一次POST创建单行BPS批次，等待真实终态；POST回执丢失只按预知batch_id查询，不重POST。
2. CPA当前注册列表与保存文件一致，且仅有这一份BPS授权；核profile=excel和固定Excel client。
3. Manager GET取得当前32hex connection_ref，PATCH把该凭证并发覆盖设为3；核保存。
4. CPA受支持的`POST /v8/management/credentials/refresh`仅指定该auth name，正常刷新一次；核同auth、账号身份、profile/client、refresh token保留及并发覆盖不丢失。不修改过期字段或强造401。
5. 显式BPS目录仅包含用户确认的`gpt-6-astra`、`gpt-6.1-sol`、`gpt-5.6-sol`。公开`/v1/responses`只发一次客户端请求：模型`gpt-5.6-sol`、小文本、非流式、最多128输出token、`reasoning.effort=low`；核completed终态、BPS响应ID、非空文本和同auth成功计数增加，最后核active/waiting排空。正文和token均不输出。

任何额外交互、未知保存、刷新未知或失败立即停止，不自动重登、重POST或再次发客户端请求。客户端预算为1；CPA同凭证内部401刷新/重试仍可能发生，未做传输计数时输出`upstream_attempts:unknown`，不声称上游仅尝试一次。routing额外retry轮数在隔离配置设为0，仍不代替传输次数证据。

## 候选命令

五个参数固定候选，以下命令默认只预检；P9执行真实验收时另加`--execute`，通过受控进程stdin送入材料，不把真实值写到shell命令：

```text
node run.mjs --cpa-image mx-cpa:phase2-local-20261111 --manager-image mx-cpamp:phase2-local-20261111 --worker-image mx-oauth-login:phase2-local-20261111 --source-revision 95dd9ca2c16d5469c43ce7c910f38ff271af45e3
```

亦可通过`--cpa-binary`、`--manager-binary`、`--worker-source`只读叠加Linux产物/源码；叠加必须提供完整40位revision，记录有效输入摘要并在结束复核未漂移。无叠加时逐镜像要求OCI revision与所给完整revision一致，记录实际镜像ID和revision。工具不构建、不拉镜像，不使用尚未完成的tag。

镜像预期revision默认取`--source-revision`，支持`--cpa-revision`、`--manager-revision`、`--worker-revision`逐角色显式覆盖，严格要求40位完整提交与实际OCI标签一致。每角色记录实际镜像ID、实际revision、预期revision；主会话证明产品目录未变后可明确复用旧镜像，整体source revision仍绑定新Manager提交。叠加时source revision绑定有效输入，显式角色revision仍校验对应基础镜像。`node ../phase2-integration/verify-revisions.mjs`覆盖混合revision成功校验、标签不符与非法revision反例，不访问Docker或凭据。

## 隔离和输出

复用已审真实2FA构型：随机独立容器、只读根、tmpfs业务状态、log-driver none、无端口发布、无生产挂载或Docker socket；管理key和真实材料经stdin进入容器。Worker登录器仅注入固定本地`host.docker.internal:7890`后import原login.mjs；CPA刷新/推理通过隔离requests.proxy-url使用同一代理。此配置只代表本地验收，不证明生产VPS网络等价。

诊断只放行既有stage/reason/error_code枚举；任务stage/reason映射login_stage/login_reason与worker_stage/worker_reason，响应HTTP仅输出整数。未知字段、原异常、邮箱、密码、种子、token和生成正文均不转发。所有原始响应只在校验进程内存中检查；finally删除本次label容器，实际核清理，未核实时token清理状态为null。

## 已通过与待验

`node --check`通过。`node verify-fixture.mjs`通过两种无网络场景：成功路径恰好1次batch、1次单目标refresh、1次公开请求；needs_user_action路径没有refresh和公开请求；脱敏结果不含fixture秘密标记或邮箱。

容器预生成名称在`docker run`之前登记，CLI回执失败也进入清理。`node ../phase2-integration/verify-cleanup.mjs`已通过两个工具的CLI替身反例：daemon已建容器但run返回失败，必须主动删除并核无残留；反例不启动Docker，不读取凭据。

fixture只验证工具预算、短路与输出契约，不证明产品或真实BPS可用。真实批次保存注册、所属client正常刷新、一次公开模型请求与最终镜像均待P9真实回执。
