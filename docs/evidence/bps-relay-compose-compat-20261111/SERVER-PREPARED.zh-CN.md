# OVH r2 准备完成

2026-11-11，来源为用户SSH真实回执。新TOOLS两文件摘要均OK，固定工具提交`602aee042d6ba161096c71c4a9e4cce57f996c87`，复用旧bundle完成prepare：

```text
/opt/cpa-stack/mx-release-20261111-rc2-r2/bps-final-release.py: OK
/opt/cpa-stack/mx-release-20261111-rc2-r2/bps-final-release.sh: OK
stage=prepare_bundle
stage=prepare_site
stage=prepared
prepared=/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2 maintenance_started=no
```

生产状态为prepared，维护未开始，尚未切换镜像或生成本轮停态备份。当前只使用新r2状态；旧失败目录和bundle保留。用户无需重新下载、覆盖脚本或重复prepare。

下一入口在SSH终端直接运行，保留stdin的TTY，不包here-doc、不通过pipe：

```bash
sudo bash /opt/cpa-stack/mx-release-20261111-rc2-r2/bps-final-release.sh apply
```

工具在真实TTY等待一次`DEPLOY`，随后即时核漂移，停止Worker、Manager、CPA，制作停态备份并切换三应用；原Caddy容器保留，写入下载路由后reload。在途登录或模型请求可能中断，活动接口旧回执为unknown，不当成零。没有SQL/schema迁移、运行时DELETE或额外真实模型测试。

已审同输入真实PTY取消/确认、完整r2生命周期和reload失败证据复用，未重复门禁。apply本轮尚未执行，以用户回传`release_complete`和后续现场身份/备份证据判断完成；失败按真实stage保留现场，不自行重跑。

当前责任人：用户在维护时段执行上述命令并输入DEPLOY、回传完整结果。下一责任人：主会话核真实切换回执，再交最小身份/备份核验及必要页面验收。
