# 二期发布真实边界验收

日期：2026-11-11。最终交付工具输入为 `39eb250ef8407213a4e7ff7dea444cdc007370f1`，来源工作树 `D:/MX-CPAMP-worktrees/phase2-release-tooling`；本分支基线 `0b890099af09d9e0819f07d97d3ccbb44618fc95`，仅新增本目录验收控制器及证据。

## 结果

| 专项 | 交付输入 | 真实命令结果 | 证据 |
| --- | --- | --- | --- |
| 最终入口、prepare、实际切换、认证健康、生成wrapper、停态备份、complete拒重入 | `39eb250ef8407213a4e7ff7dea444cdc007370f1` | exit0，清理已确认 | [run.log](run.log) |
| 无TTY、取消、EOF、原120秒确认超时、源及Compose漂移、secret权限 | `de91274298c74b073f91212b5acf90833f4a8777` | 各断言通过；整轮exit1，末尾生成wrapper执行所需bash缺失 | [matrix-shell-dependency.log](matrix-shell-dependency.log) |
| maintenance后备份失败保留、拒重入 | `de91274298c74b073f91212b5acf90833f4a8777` | 预期故障被验收，控制器exit0，清理已确认 | [maintenance-failure.log](maintenance-failure.log) |
| CPA实际Env/Args来源选择、Manager inline secret优先级 | `db304a804d407f44a1c32b8154afac031033a4f3` | 四项原函数早拒绝通过；当轮后续prepare因独立merge差异exit1 | [matrix-merge-first.log](matrix-merge-first.log) |

`de912742…` 至 `39eb250…` 只将发布入口与生成wrapper的两行shebang改为 `#!/bin/sh`，正文及确认、来源、漂移、秘密、备份和健康函数未变。最终只补受影响的真实Shell入口、生成wrapper及未完成末段，未重复120秒等待。来源函数在 `db304a8…` 后保持不变，四个已通过来源反例直接复用。原整轮exit1保留原口径，不作为整轮exit0。

## 固定运行输入

- 缓存验收镜像 `sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48`，Linux/Python、Docker CLI及Compose v2.33.0；不下载或替换Compose。
- Root冻结的归档和manifest只读挂载自 `C:/Users/子羽/.codex/artifacts/mx-cpamp-release-phase2-20261111-candidate`。交付原 `load_bundle()` 实际读取、核验和加载；不自行重打包或替换候选身份。
- 四个服务使用本机缓存的真实rc1镜像和Caddy镜像。每次唯一project/label与Linux命名根卷，应用使用真实bind来源；单文件Caddy、双文件三应用作为原始链。仅隔离内部网络，无宿主发布端口；站点凭证、数据库和业务配置全部合成。
- 控制器只替换 `ROOT/PROJECT/STATE/BASELINE` 事实常量；不替换 `prepare/apply/confirm_deploy/snapshot/backup/health/drift`，不替换Docker/Compose。`invoke.py` 调原 `main()`，异常只做白名单输出和测试定位；真实来源反例从Docker inspect取得对象，直接调用原 `sources()` / `secrets()`。
- 控制器独占本轮Docker socket，应用不挂socket。四应用关闭Docker日志；没有生产挂载、真实登录、刷新或模型请求。

## 已覆盖边界

1. 原 `phase2-release.sh` 按实际shebang执行并分派原Python入口；无参数得到原用法拒绝码。没有绕过入口的解释器校验。
2. 原prepare从冻结归档开始，真实Compose解析/合并、实际快照、源/秘密/数据key判定及内部认证健康通过；四服务完整ID不变且仍运行，配置、wrapper、秘密原字节不变，仅状态目录暂存，不提前建立live BPS目录。
3. 真实Linux PTY执行原确认函数：取消、EOF和完整120.58秒超时均停在 `awaiting_confirmation`，无维护标记；即使非TTY输入DEPLOY也拒绝。各次四服务不变。
4. 准备后分别改变CPA配置与Compose原字节，真实apply因 `protected_source_drift` 拒绝；secret改为0600时因 `worker_secret_permissions` 拒绝；均未进入维护，随后仅恢复本轮原始测试材料。
5. 真实创建容器后inspect的 `HOME_JWT`、`PGSTORE_DSN`、替代 `--config` 分别由原来源函数早拒绝；Manager同时存在file与inline Worker secret时由原秘密函数拒绝。全部零维护。
6. DEPLOY成功路径实际停止三个应用、停态备份、发布新配置/最小状态目录/三文件wrapper、启动候选并做正负认证探针。Caddy完整ID不变，三应用真实镜像/挂载/完整Compose链符合计划。实际执行新生成wrapper的 `config --format json`，三个应用指向本次候选。
7. 实际打开并遍历停态tar：Manager目录、CPA配置和两项必要secret均存在；Worker secret原字节可读且一致；归档0600。完成标记存在，完成态再次apply拒绝。
8. 独立fixture在原apply输出 `stopping_apps` 后，仅移动本轮合成Manager数据目录，导致原备份读取失败。真实保留 `stage=backing_up` 与maintenance标记；三个应用停态、Caddy原ID仍运行，没有候选安装、没有备份确认receipt、没有自动回灌或重启；再次apply以 `maintenance_requires_manual_recovery` 拒绝。
9. finally只按本轮唯一project/归属label清理，核无容器、网络或卷残留；两项最终日志均有 `fixture_cleanup=verified`。

## 命令

工作目录 `D:/MX-CPAMP-worktrees/phase2-release-boundaries`：

```powershell
& './docs/evidence/phase2-release-boundaries-20261111/run.ps1' -ReusePassed
& './docs/evidence/phase2-release-boundaries-20261111/run.ps1' -MaintenanceFailure
```

`-ReusePassed` 必须结合上述固定提交与已有日志使用；省略时执行完整确认/漂移/权限矩阵，包含真实120秒等待。`-SourceChecks` 可执行来源四反例；本次最终补验未重复这些已通过项。

## 已知边界与历史记录

- 真实Docker/Compose/文件/秘密权限/进程生命周期不等同OVH现场验收。本次使用Compose v2.33.0，生产Compose版本未由本专项确认；没有真实业务、外部OAuth或收费模型验收。
- maintenance故障由本轮外部控制器在停机后移动合成目录制造；用于验证真实错误状态保留与拒重入，不代表生产曾发生此故障。原发布命令因读取失败返回非零，验收控制器将符合预期的故障判为通过。
- `matrix-first.log`、`matrix-compose-flag.log` 保留原不支持参数的首失败；`matrix-process-drift.log`、`matrix-compose-null.log` 保留空command继承差异，`matrix-merge-first.log`、`matrix-compose-bind.log` 保留新bind默认属性差异。修复由实现线提交，本线没有放宽全对象比较或修改交付函数。
- `matrix-address-pool.log` 保留本机默认地址池耗尽；控制器仅为本轮内部网络选择经实际inventory确认无重叠的私有/28，不清理其他项目网络。`matrix-setup-unknown.log` 及早期 `setup-*.log` 保留原fixture准备记录。
- `maintenance-first.log` 的同一预期故障使用早期测试异常白名单名；最终 `maintenance-failure.log` 已采用与交付CLI一致的 `operation_failed` 名，真实阶段和行为不变。

本线无产品改动、SQL/schema迁移、生产写入、push或部署。下一责任人：C独立核证本目录与输入等价引用；主会话随后整合发布材料。
