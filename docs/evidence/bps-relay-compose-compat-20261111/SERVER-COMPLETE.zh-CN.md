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

停态备份为`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2/backup`。备份归档内容检查在apply内完成；用户已补齐[最终只读回执](SERVER-FINAL-VERIFY.txt)，三应用镜像完整ID与固定产物逐字匹配，Caddy镜像与原基线一致，四服务均running。目录权限700，receipt.json为525字节、data.tar.gz为21859字节，两文件均600。独审只读核完成结论与最终身份/权限回执通过。

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

上述命令已由用户执行，结果与预期一致；只展示镜像与文件元数据，不读取凭据正文。无SQL/schema迁移、运行时DELETE、新的真实登录或模型请求。公网有效图片GET/HEAD、官方模型抓图和更新后页面体验不由本次内部路由通过替代。

服务器发布与部署核验完成，主会话收口。状态目录及配套Compose、wrapper和备份保持原位。用户可在原域名刷新面板使用；业务体验按实际操作另记，不重复已确认的2FA验收，不追加部署或扫描。后续若有具体使用问题，由主会话按新回执处理。
