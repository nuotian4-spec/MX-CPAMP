# 二期发布工具独立审查

日期：2026-11-11。结论：本地发布工具、固定镜像分发及本轮真实边界证据通过独审，无待处理 P1/P2。此结论限于本地工具，不是正式发布或完整业务放行。

## 固定范围

- 工具源提交：`39eb250ef8407213a4e7ff7dea444cdc007370f1`，仅 `deploy/phase2-release.sh`、`deploy/phase2-release.py`。主线 `8fe97f9656215d7824cb90fb232a3de79090b205` 的两个 Git blob 与其一致。
- 分发证据：`8ca9f0cf28adb1a50e88f568102d687334ec5721`，四文件位于[分发目录](../phase2-release-bundle-20261111/README.zh-CN.md)。
- 边界验证：`e0d6193f2922771bc74e4f908df86c0e0e99d59e`，限[控制器及原始日志](../phase2-release-boundaries-20261111/README.zh-CN.md)。
- 审查读取新增脚本、直接来源契约、控制器和实际回执；未重复 Docker、产品构建、登录或模型测试。

## 代码与分发

1. prepare 使用独立私有状态目录，仅暂存候选配置和第三 override；原配置、wrapper、四服务及 live BPS 目录保持不变。
2. 配置来源核实际 Args、Env、工作目录和 `.env`；未知 Home、远端存储或替代配置早拒绝。BPS 状态位于 auth 扫描树外；旧状态存在或来源不明时停止。KV、Manager 数据及 data key 的实际路径由既有持久挂载覆盖。
3. 密钥按真实选择优先级核源；inline OAuth/admin/data key 不进入本次已知文件源流程。Worker 原文件来源、原权限和实际可读内容均受检查，未生成替代密钥。
4. Compose 比对保留 Source/Target/RW、端口 HostIP、网络、进程、namespace 和运行属性；prepare、切前漂移和候选健康复用同一语义检查。Caddy 保持原单文件标签链与完整容器 ID，三应用使用完整三文件链。
5. apply 仅一次真实 TTY 的 DEPLOY 确认；随后漂移核验、三应用停态确认、新私有备份、候选安装及即时正负认证健康。维护后失败保留阶段和现场，拒绝重复 apply，无自动回滚或整库回灌。
6. 原 wrapper/config 位于本轮停态归档；新 BPS state 及第三 override 在失败后和完成后均需保留。`RECOVERY.txt` 明确先核现场、再绑定本次计划与归档定向恢复。
7. 运行输出为固定阶段、白名单错误类别和私有材料位置；命令输出、配置和秘密正文不写 stdout。
8. manifest 的 ref、revision、linux/amd64 与固定候选一致，每组件精确登记 Desktop、经典 Linux 两个已验 ID；归档摘要与 pack-meta 一致。经典 Linux 实际加载日志为 exit0、清理确认；不以同名 tag 代替不可变镜像身份。

## 引用证据

| 范围 | 输入与回执 | 独审判断 |
| --- | --- | --- |
| 原 Shell 入口、prepare/apply、即时认证、新 wrapper 实际渲染、备份原字节与 0600、complete 拒重入 | `39eb250…`，[run.log](../phase2-release-boundaries-20261111/run.log)，exit0，清理确认 | 通过 |
| 无 TTY、取消、EOF、120.58 秒超时、配置/Compose 漂移、secret 权限 | `de912742…`，[matrix-shell-dependency.log](../phase2-release-boundaries-20261111/matrix-shell-dependency.log)，各断言通过；原整轮 exit1 保留 | 复用；至最终输入仅两处 POSIX shebang 变化，相关函数未变，受影响入口与 wrapper 已补验 |
| 维护后备份读取失败、三应用停态、Caddy 原 ID、无候选安装/自动恢复、拒重入 | `de912742…`，[maintenance-failure.log](../phase2-release-boundaries-20261111/maintenance-failure.log)，预期故障验收 exit0，清理确认 | 复用；相关生命周期与备份函数未变 |
| HOME_JWT、PGSTORE_DSN、替代 config、Manager inline secret 实际 inspect 反例 | `db304a8…`，[matrix-merge-first.log](../phase2-release-boundaries-20261111/matrix-merge-first.log)，四项早拒绝通过；原后续整轮 exit1 保留 | 复用；来源函数后续未变 |

控制器只替换隔离 ROOT/PROJECT/STATE/BASELINE 常量，实际调用交付函数与 Docker/Compose；未用替身跳过被验证的函数。仅控制器挂载宿主 Docker socket，应用不挂载。维护故障只移动本轮合成目录，清理限定唯一 project/label。

`git diff --check` 对工具首版及各最小增量、边界证据提交均 exit0。本轮 diff 仅部署工具和证据文档，没有产品目录、SQL/schema 迁移或数据清理改动；生产应用仍涉及新增状态挂载、配置与运行时文件写入。

## 已知边界与责任

- 适用环境为本地 Linux、Compose v2.33.0 和合成站点；OVH 实际加载、生产 Compose、当前状态/KV 来源、线上备份内容与恢复、路由和页面仍需现场核验。
- 本轮没有真实登录、刷新、收费模型请求、生产写入、推送或部署。既有在途活动 unknown 及业务待验边界保持不变。
- 原整轮失败不改写为全量通过；已过子项、输入等价复用和最终补验分别列示。临时图片及重启后的旧中转链接不纳入持久化承诺。

当前责任：独立审查已完成。下一责任人：主会话整合本记录，按完整业务与现场证据决定后续发布范围；本记录不新增生产执行授权。
