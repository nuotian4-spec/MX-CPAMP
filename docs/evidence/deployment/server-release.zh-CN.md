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

用户补充的生产只读回执确认四容器均 `running`：

| 服务 | 实际镜像 | 实际完整镜像 ID |
|---|---|---|
| CPA | `mx-cpa:20261110-rc1` | `sha256:58d0177203d8f4e4968cebc60c59bdb0e236404a05c27656a38351dc1022ab93` |
| Manager | `mx-cpamp:20261110-rc1` | `sha256:85a8f76852c08de220ba132490cbdf234b87f1bdeba91a6e0d9bfa905769a25c` |
| Worker | `mx-oauth-login:20261110-rc1` | `sha256:64168881ce990f58d24736d1f8488549457ed2c775a072c1d0572bd524b7926b` |
| Caddy | `caddy:2.10.2-alpine` | `sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d` |

三候选镜像完整 ID 与已验证分发清单匹配，Caddy 镜像与原基线一致。备份目录权限 `0700`；`data.tar.gz` 为 20826 字节，`environment-sources.tar.gz` 为 69 字节，`contents.txt` 为 244 字节，三文件均 `0600`。

当前结论：服务器部署核验通过。页面、真实账号登录、所属 client 的令牌刷新和模型业务验收待用户操作。此回执不宣称四入口真实授权或 BPS 官方手工回调兑换已通过。

下一责任人：用户在原管理面板强制刷新确认四入口，随后分通道进行真实登录验收；P9 根据页面结果和脱敏错误码继续核证。
