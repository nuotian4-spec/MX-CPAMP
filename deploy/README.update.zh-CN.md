# 固定版本日常更新

首次登记站点，之后每轮只指定一个固定版本。入口只面向 Linux root、Docker Compose v2 和已使用 v8 CPA 配置的既有站点；Python 3.9 以上、curl、Docker 必须已可用。更新不创建账号、不执行迁移、不调用真实供应商模型。

## 首次登记

从已审查的固定 Release 下载 `mx-update.py`、`mx-update.sh`、`mx-update.site.example.json`，对照同版 `release.json` 核对大小与 SHA-256。把两份入口放到 `/opt/cpa-stack/bin/`，示例登记保存为 `/opt/cpa-stack/site.json`。首次安装由操作者检查下载来源与站点路径，不把示例中的 `SITE_HOST` 当实际域名。

登记真实 `root`、Compose 项目名、基础 Compose 链、CPA 配置、Kilo 状态映射、受管网络、固定 Release 来源和入口健康 URL/状态。`restore_ingress: true` 表示本轮更新确认同时授权健康后启动本轮原 Caddy；更新入口不重建 Caddy。登记文件需要 root 所有、0600，入口目录只允许受信任操作者写入。`active_release_pointer` 的父目录需已存在。

```sh
chown root:root /opt/cpa-stack/site.json
chmod 600 /opt/cpa-stack/site.json
chmod 700 /opt/cpa-stack/bin/mx-update.sh
```

## 每轮命令

```sh
/opt/cpa-stack/bin/mx-update.sh --site /opt/cpa-stack/site.json --version v2026.11.16-rc.2
```

版本号仅示意，必须选已正式发布且带齐更新资产的版本。`latest`、分支、复用旧 plan 均不作为输入。每轮使用站点锁和新的 `.mx-updates/版本-时间-随机串/` 私有目录，发现当时的容器 ID 与应用 Compose 链。首轮允许应用链为同一完整链的前缀；已有活动指针后，三应用链须与指针完全一致。

停服前完成下载校验、镜像加载、原 prepare、实际拓扑和只读鉴权健康、备份空间估算、活动指针可写测试及能力验证。空间估算不是停态备份完成证明。确认摘要包含版本、提交、镜像、当前容器、备份目录、原 Caddy ID；输入一次 `UPDATE` 后复用原 apply/start：停止应用、停态备份、安装候选配置、隔离入口、重建三应用、核健康，最后启动原 Caddy 并检查登记入口。

成功尾部将完整 Compose 链和同版 updater 写入活动指针。原安装命令下一轮会验证并转到该活动 updater；受管 Compose 入口始终保留全部 override：

```sh
/opt/cpa-stack/bin/mx-update.sh --site /opt/cpa-stack/site.json --compose ps
```

操作者后续管理应用使用该入口，避免只用基础 Compose 文件启动而丢失活动 override。旧轮次目录和原入口保留，不自动删除。

## 能力证据

- `config_policy`：候选 CPA 镜像以只读候选配置运行 `--capability-probe`，断网、无账号或数据库挂载；实际调用生产配置解析、access 鉴权和 channelscope 函数。消费者逐项核每个有效 Key 的鉴权、三渠道允许/拒绝、可见集合，以及无效/缺失 Key 拒绝。配置原始字节与源码 revision 必须一致。
- `production_wiring`：继承 M10 scope/joint 与组合 BPS 三条成功专项的原命令、冻结输入、真实退出码和日志；相关源码、测试、依赖和构建输入以 Git tree 映射绑定。增量审查缺失、源文件变化、目标测试缺失/失败/skip，打包或消费者拒绝继续。
- 两项共同通过才形成组合 scope 回执。策略函数观察不是本轮生产执行链运行；历史 Windows 合成接线不冒充新 Linux 镜像业务推理。CI 额外用实际新 CPA 镜像跑一次合成策略入口与坏配置退出，作为新 CLI 分支验收。

## 失败与保全

停服前失败不停止现有服务。维护开始后失败或 Ctrl-C，入口停止 Caddy 与三应用并记录停态核验；停止命令或停态核验失败会记录未确认，不宣称全部停止。保留新旧配置、镜像、停态备份、capability 证据和轮次日志，不自动 rollback、不回灌数据库、不重试登录、不删旧状态。

活动指针是完成发布依据；`completed.json` 记录健康收尾数据，最终指针写入失败仍按失败保全，`update-failure.json` 和 `stage` 给出实际失败状态。失败后由操作者基于该轮证据决定定向恢复，不直接删除轮次重跑。

尚未由合成专项证明的项目：真实站点一次更新、下一次真实更新、真实账号与模型推理、外部 cpa-bps 二进制行为。实际服务器更新和推理验收须另行授权和记录。
