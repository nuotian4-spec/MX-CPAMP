# BPS工具定向验收

本工具只验收 `gpt-6-luna` 的一次工具往返，默认仅做无凭证预检。真实执行需要本轮新增授权，旧的[文本、工具和图片回执](../phase2-tool-image-20261111/REAL-RECEIPT.jsonl)保持独立。

## 执行范围

- 提交一个单行BPS 2FA批次，核保存、唯一注册和身份映射。创建回执丢失时只按原批次ID查询，不再提交。
- 工具首轮最多一次，仅定义无外部副作用的空参 `ping`。必须是正常BPS终态、且恰好一个非空 `call_id` 的 `ping({})`，才继续。
- 合法首轮后固定返回本地 `pong` 一次。客户端回传完整首轮用户历史、全部响应output和匹配的function_call_output，不发送previous_response_id。最终必须没有新工具调用，文本为pong。
- 主动刷新、并发设置写入、最小文本和图片请求均不执行。模型请求实际为0、1或2次；任一失败不重发，不执行模型返回的任意命令。

回执分别记录授权上限和实际客户端提交数。`login_batch_requests` 是向Manager提交批次的次数；它不代表可计量的认证上游请求总数。实际上游模型尝试次数仍记 `unknown`，不把零普通重试写成绝无内部重发。

## 固定输入与隔离

每个组件必须同时提供镜像引用、完整sha256镜像ID和40位revision。启动前校验实际ID与OCI标签，再仅使用不可变ID启动；容器边界核验再次比较实际Image。默认不给二进制或Worker目录叠加入口。

Manager固定输入：`mx-cpamp:bps-tools-images-20261111`，ID `sha256:6732b8d85cfade68fce21ae286ede773d8151a397c64eedbea0b4618d4b6cd73`，revision `bd573d0439a925d811e25aeeb575ac91feb5796d`。

Worker固定输入：`mx-oauth-login:phase2-local-20261111`，ID `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758`，revision `95dd9ca2c16d5469c43ce7c910f38ff271af45e3`。

CPA的新增诊断候选需由最终构建回执提供真实ID和revision。三组件均无生产挂载、Docker socket或端口发布；只读根、tmpfs状态、log-driver=none。登录材料只从stdin进入内存和隔离tmpfs，不入argv、环境变量、日志或宿主文件。finally按本轮label删除并复核；未确认清理时token_state_retained为null。

六个本目录运行模块及跨目录[诊断模块](../phase2-tool-image-20261111/diagnostics.mjs)在启动前读取原始字节，收尾逐项比较；跨目录模块在容器中仅以单文件只读挂载。旧诊断模块的固定stage/code和数值字段继续经过双层白名单，不输出原始错误正文、URL、聊天、call_id、密码或token。

参数格式如下，CPA占位符须由真实构建记录替换；不加 `--execute` 时不会读取凭证、创建登录批次或发送模型请求：

```powershell
node docs/evidence/bps-tool-diagnostic-20261111/run.mjs --cpa-image CPA_IMAGE --cpa-image-id CPA_SHA256 --cpa-revision CPA_REVISION_40 --manager-image mx-cpamp:bps-tools-images-20261111 --manager-image-id sha256:6732b8d85cfade68fce21ae286ede773d8151a397c64eedbea0b4618d4b6cd73 --manager-revision bd573d0439a925d811e25aeeb575ac91feb5796d --worker-image mx-oauth-login:phase2-local-20261111 --worker-image-id sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758 --worker-revision 95dd9ca2c16d5469c43ce7c910f38ff271af45e3
```

经单独授权后，主会话在同一固定命令加 `--execute`，通过受控子进程stdin传入email/password/totp_secret JSON。脚本不会构建、拉取、推送或部署镜像，不连接生产数据库。

## 离线证据

```powershell
node docs/evidence/bps-tool-diagnostic-20261111/verify-fixture.mjs
node docs/evidence/bps-tool-diagnostic-20261111/verify-harness.mjs
```

业务fixture共27场景，覆盖零刷新与零设置写入、单批次、创建回执丢失后查询、唯一凭证映射、首轮403/502/传输失败短路、畸形工具与任意命令拒绝、首轮或续轮夹带custom_tool_call拒绝、完整历史与call_id配对、续轮失败和精确输出、0/1/2次请求计数及秘密哨兵过滤。

Docker替身覆盖ID/revision不符或非法值时零启动、不可变ID启动、容器Image不符时停止、默认预检零业务、创建回执失败/业务失败/清理失败、两个目录的运行输入漂移与双层脱敏。预检fetch替身只允许四次GET健康/鉴权请求。

这些结果只证明验收工具的执行边界。没有进行真实登录、刷新、模型请求、生产操作或数据库迁移。
