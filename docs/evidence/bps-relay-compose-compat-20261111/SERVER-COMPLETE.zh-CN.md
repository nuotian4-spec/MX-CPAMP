# OVH r2 切换完成

2026-11-11，用户在SSH真实TTY执行新r2工具的apply并输入一次DEPLOY，回传完整结果：

```text
stage=awaiting_confirmation
将短暂停止 Worker、Manager、CPA，可能中断现有登录和模型请求；将写入 Caddy 下载路由并在原容器重载。输入 DEPLOY：DEPLOY
stage=pre_switch_drift
stage=stopping_apps
stage=backing_up
stage=publishing_candidate
stage=starting_candidates
stage=publishing_caddy
stage=reloading_caddy
stage=caddy_runtime_health
stage=authenticated_health
stage=caddy_https_routes
stage=complete
release_complete state=/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2 backup=/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2/backup
```

固定工具提交`602aee042d6ba161096c71c4a9e4cce57f996c87`与用户prepare时两文件摘要匹配。本回执证明该工具完成了切前漂移核验、三应用停态备份及切换、原Caddy配置写入与重载生效核验、候选身份/挂载/配置链及设置回读、正负鉴权和内部受信HTTPS路由检查。状态目录与配套Compose、wrapper须保留，不重复prepare/apply。

停态备份为`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2/backup`。备份归档内容检查在apply内完成；目录与文件权限、当前镜像完整ID的独立文本回执待用户补齐。独审只读核本次完成结论与以下最小入口，无需重复健康检查。

```bash
sudo bash <<'CPA_FINAL_VERIFY'
set -eu
ids=$(docker ps -aq --filter label=com.docker.compose.project=cpa-stack)
test -n "$ids"
docker inspect --format '{{.Name}} image={{.Config.Image}} id={{.Image}} status={{.State.Status}}' $ids
BACKUP='/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2/backup'
stat -c 'backup_dir_mode=%a' "$BACKUP"
find "$BACKUP" -maxdepth 1 -type f -printf '%f bytes=%s mode=%m\n'
CPA_FINAL_VERIFY
```

预期四业务服务running、备份目录700、顶层备份文件600；只展示镜像与文件元数据，不读取凭据正文。无SQL/schema迁移、运行时DELETE、新的真实登录或模型请求。公网有效图片GET/HEAD、官方模型抓图和更新后页面体验不由本次内部路由通过替代。

当前责任人：用户回传上述只读身份/权限结果，并在原域名刷新面板查看更新。下一责任人：主会话核结果后收口服务器发布；业务验收按实际操作另记，不重复已确认的2FA验收。
