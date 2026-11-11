# 最终候选首次离线集成回执

- 产品输入：`95dd9ca2c16d5469c43ce7c910f38ff271af45e3`，原Dockerfile三个最终候选镜像，无叠加。
- 工具输入：`02a3f24d98337956edef32aa1cef42b9c2bdb1bb`。
- 完整命令、UTC起止与真实退出码见[执行元数据](final-candidate-20261111.meta.json)；脱敏stdout见[原始回执](final-candidate-20261111.jsonl)。
- 退出码：`1`。首次运行，未重试。
- 通过：三镜像OCI revision一致；只读根、tmpfs状态、关闭Docker日志、无端口发布、无外网；三组件健康，未认证Manager请求401。
- 失败：批次查询`batch_redaction`。
- 未执行：后续BFF覆盖/恢复、配置凭证独立限流、跨模型共享、排队取消/在途取消及唤醒集成断言。
- 清理：`verified=true`、`token_state_retained=false`；叠加输入复核`unchanged=true`，本次无叠加。

## 问题归类

批次接口契约`docs/contracts/login-batches.zh-CN.md`要求摘要不含原邮箱。Manager `BatchItem`的`email_hint`已脱敏，但嵌入`Task.auth_id`直接返回CPA导入响应的AuthID。

CPA `importedRecord`在`components/cpa/internal/api/handlers/management/auth_files_bps.go`使用`codex.CredentialFileName(email, plan, hash, true)`作为`Auth.ID`与文件名；`components/cpa/internal/auth/codex/filename.go`的命名模板包含完整邮箱。Manager成功导入后原样保存这个ID至公开Task，所以首个成功导入后的批次摘要必然包含完整合成邮箱，触发该断言。现有批次单测用`account.json`作导入返回值，未覆盖此跨组件路径。

这是产品摘要契约冲突，尚未改变断言或产品。公开Task结构没有密码、TOTP或token字段，本次失败不构成这些秘密泄露的证据。下一责任人为主会话安排的B实现席，需确定摘要ID与真实凭证定位的兼容方式；修复后再补受影响集成，不以本回执代表通过。

本次没有使用真实凭据、官方登录、令牌刷新或真实模型请求。
