# 生产切换回执

日期：2026-11-10。交付版本：`mx-cpamp-20261110-rc1`；固定脚本提交：`3baeb518da2dbbff14befe386701a9d2815c9a9c`。执行：用户在既有 VPS 的 SSH 终端；回执核证：P9 与独立只读审查席。

用户执行固定路径 `cpa-stack-release.sh apply`，在 TTY 输入 `DEPLOY`，回传：

```text
stage=awaiting_confirmation
stage=pre_switch_drift_check
stage=stopping_apps
stage=backing_up
stage=starting_candidates
stage=authenticated_health
stage=complete
release_complete state=/opt/cpa-stack/.mx-upgrade-20261110-rc1 backup=/opt/cpa-stack/.mx-upgrade-20261110-rc1/backup
```

按已审脚本的成功条件，配置与镜像漂移检查、CPA/Manager 停止态确认、一次性备份、三个候选服务启动及内部认证探针已成功。探针覆盖 CPA 模型目录 200、Manager 已认证的不存在任务 404、Worker 认证 health 200，并确认原 Caddy 容器仍运行。未调用模型推理或真实 OAuth 登录。

备份目录为 `/opt/cpa-stack/.mx-upgrade-20261110-rc1/backup`，包含 Manager 数据、CPA 凭据/KV 及恢复所需配置、密钥与环境来源；备份内容保留在服务器私有目录。无新增 SQL/schema 迁移。

当前结论：生产发布脚本完成。仍需补实际运行容器的镜像身份、备份文件元数据和页面验收；真实账号登录、所属 client 的令牌刷新和模型业务验收待用户操作。此回执不宣称四入口真实授权或 BPS 官方手工回调兑换已通过。

下一责任人：用户执行一组只读状态/身份/备份元数据核对，并在原管理面板强制刷新确认入口；P9 根据回执继续验收。
