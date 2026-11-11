# OVH 默认绑定字段兼容准备入口

工具固定提交`602aee042d6ba161096c71c4a9e4cce57f996c87`。新TOOLS为`/opt/cpa-stack/mx-release-20261111-rc2-r2`，新STATE为`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2`。从旧失败目录的bundle复制已下载归档，并重新经过原manifest、归档摘要和镜像身份检查；不复用旧plan，不改旧状态。以下入口只prepare，成功后另由真实TTY执行apply。

已通过：现场唯一字段回执与既有empty_bind用例一致；挂载漂移反例、真实Linux新状态完整周期和独审通过。复用：产品三镜像、227177346字节归档、未变reload故障与HTTPS专项；部署工具修复未改变这些输入。不适用：SQL/schema迁移、运行时DELETE和新增真实模型请求。远端推送与固定提交文件逐字核验须由主会话取得真实结果后交付。

```bash
sudo bash <<'CPA_PREPARE_R2'
set -euo pipefail
umask 077

SOURCE='602aee042d6ba161096c71c4a9e4cce57f996c87'
TOOLS='/opt/cpa-stack/mx-release-20261111-rc2-r2'
BUNDLE='/opt/cpa-stack/.mx-upgrade-bps-relay-20261111/bundle'

install -d -o root -g root -m 0700 "$TOOLS"
for name in bps-final-release.py bps-final-release.sh; do
  curl -fLsS --proto '=https' --proto-redir '=https' \
    "https://raw.githubusercontent.com/nuotian4-spec/MX-CPAMP/$SOURCE/deploy/$name" \
    -o "$TOOLS/$name"
  chmod 0600 "$TOOLS/$name"
done

printf '%s  %s\n' \
  '75b30579d61f4ca3d3e6701950e88e83cb80341a7594ae25bb5687f514171899' \
  "$TOOLS/bps-final-release.py" \
  '73074c6d9110245ecc1777f03589c9e7f96e2a332932112a0bb81816eee35bb2' \
  "$TOOLS/bps-final-release.sh" | sha256sum -c -

bash "$TOOLS/bps-final-release.sh" prepare --bundle-dir "$BUNDLE"
CPA_PREPARE_R2
```

严格模式仅作用于sudo启动的Bash子进程，未在SSH登录Shell顶层启用。该入口不包含apply或DEPLOY，不改线上配置；原工具准备阶段会加载候选镜像并建立新私有状态目录。

主会话从本文件提取完整Bash代码块，经缓存`mx-cpa-buildenv:bps-tools-images-20261111`镜像的`/bin/bash -n`实际检查，断网、只读、无挂载，退出0；未执行准备或生产动作。两下载文件摘要取自固定提交的Git blob，且与本地已审工具逐字相同。
