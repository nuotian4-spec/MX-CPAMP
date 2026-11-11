# r2最终候选离线集成回执

结果：`exit0`，本次修复后只运行一次。原始stdout见[脱敏回执](final-candidate-r2-20261111.jsonl)，完整命令、UTC起止及退出码见[执行元数据](final-candidate-r2-20261111.meta.json)。首次失败回执保持原文。

工具与整体源码提交：`bb14b3a1c60f1b84b31a19ea5b1dde71edaee00f`；工具上次改动为`efe4d4989db8899771fe24cf07ff370111eed137`，执行前已核两个工具目录无后续diff。

| 组件 | 镜像 | 实际及预期revision | 实际镜像ID |
| --- | --- | --- | --- |
| CPA | `mx-cpa:phase2-local-20261111` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` | `sha256:0bb5d7dbfc44d4822577fa4ae9f76aa81e0b5a2fd7afae2168c5ae53cbd1f47a` |
| Manager | `mx-cpamp:phase2-local-20261111-r2` | `bb14b3a1c60f1b84b31a19ea5b1dde71edaee00f` | `sha256:f72b39c0ff1759c14ced75c511a35ab0d97935249cd0446b47becd30a5c9b6b9` |
| Worker | `mx-oauth-login:phase2-local-20261111` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` |

CPA与Worker沿用主会话已核产品目录未变的旧镜像，逐角色显式指定原revision；Manager使用新镜像。三者都由工具严格核对实际OCI revision，无源码或二进制叠加。

通过项：

- 隔离：只读根、tmpfs状态、关闭Docker日志、无公开端口、共享`network none`；三组件健康、Manager未认证请求401。
- 批次：三行成功/确定失败/成功，Worker严格串行；失败继续；latest恢复终态摘要；相同batch_id不重放。所有批次响应均不含原邮箱、密码、TOTP、token或auth_id字段。
- 保存注册：成功条目16hex auth_index在CPA列表中按provider唯一定位实际凭证；逐份下载核Codex profile/client、输入邮箱、非空账号及令牌。真实ID仅供内部查询。
- BFF：OAuth文件并发覆盖真实保存、恢复默认20；配置Key覆盖与恢复默认、runtime身份稳定；其他凭证额度独立；每次PATCH复用当前connection_ref。
- 并发：跨模型共享上限、两凭证各占独立额度、公共等待、等待取消且未到模拟上游、在途取消触达模拟上游close、空闲后唤醒等待者；模拟上游pending与CPA active/pinned_waiting/pool_waiting全部归零。
- 清理：`verified=true`、`token_state_retained=false`；输入复核`unchanged=true`。

范围：使用真实CPA、Manager、Worker组件；官方登录进程与模型上游为合成fixture。没有真实凭据、官方登录、令牌刷新或真实模型请求，不代表生产部署或真实业务验收。
