# BPS 官方账号用量发布工具

2026-11-11。工具和真实边界控制器输入提交：`3c9c63aaeb865c81631f37b6f3b373edaee694c5`。基线：`c2adc4ad92a2bf564bf8dfcbaa40aee78d8c9ff3`。

独立入口为 `deploy/bps-final-release.sh` 与 `deploy/bps-final-release.py`，状态目录为 `/opt/cpa-stack/.mx-upgrade-bps-final-20261111`，第三份 Compose 为该目录的 `compose.bps-final.yaml`。旧 `phase2-release.*`、旧 prepared 和历史回执保持原内容。

## 结果与输入

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 新入口真实 Shell、prepare、PTY取消、apply | 通过，exit0 | [run.log](run.log) |
| 三应用最终镜像、正负认证、完整挂载/三文件链、Caddy保留 | 通过 | [run.log](run.log) 中 apply |
| 生成的 Linux wrapper、停态备份可读、0600、BPS状态0700、完成态拒重入 | 通过 | [run.log](run.log) |
| 本轮唯一项目的容器/网络/卷清理 | 已核无残留 | `fixture_cleanup=verified` |
| 旧生产 BASELINE、32个函数、Shell正文 | 复用，候选身份与文件名之外输入等价 | [equivalence.json](equivalence.json) |

归档及 manifest 来源：`C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111`，由 B 席独立保存和实际经典 Linux load。归档为 `mx-cpamp-images-linux-amd64.tar.gz`，227177346字节，SHA256 `256ddca4ec162bc1249ccea95ca1e36ce536b4909fa1ac89b77af1b477910c27`。新工具逐项精确核验 manifest 的 ref、revision、accepted_ids、Linux/amd64 和归档摘要，再调用真实 `docker load`。

| 角色 | ref | revision | 本次实际运行 ID |
| --- | --- | --- | --- |
| CPA | `mx-cpa:bps-quota-identity-20261111` | `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` | `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b` |
| Manager | `mx-cpamp:bps-quota-repair-20261111` | `d26cbf7595ecf41ba8cbfd90c956291ac351b471` | `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd` |
| Worker | `mx-oauth-login:phase2-local-20261111` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` |

经典 Linux load 得到的第二 ID 同样固定在新工具 CANDIDATES 中，完整值以本轮 manifest 和工具常量为准。

## 真实生命周期

执行命令，工作目录 `D:/MX-CPAMP-worktrees/bps-access-inspection`：

```powershell
& './docs/evidence/bps-final-release-20261111/tools/run.ps1' -BundleRoot 'C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111'
```

控制器使用缓存 Linux runner `sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48`，Docker CLI/Compose v2.33.0。runner 断网，仅它挂载 Docker socket；四应用使用真实缓存 rc1 镜像、独立内部网络及唯一 Linux 命名卷，配置、密钥、SQLite 均为合成现场，无宿主发布端口。

控制器只替换 `ROOT/PROJECT/STATE/BASELINE` 现场事实常量；调用交付原 main 与全部函数，使用真实 Docker、Compose、文件权限、Linux Shell 和 PTY。

- prepare 实际加载冻结归档、检查来源与密钥、生成候选并做认证探针。原四服务完整 ID、配置、wrapper 和密钥字节保持一致，live BPS状态目录仍不存在。
- PTY 输入 CANCEL 后返回 `confirmation_cancelled`；没有 maintenance，四服务与受保护文件保持一致。
- PTY 输入 DEPLOY 后实际停止三个应用、停态备份、生成新状态目录与三文件 wrapper、启动新候选并做认证。验证 CPA/Manager/Worker 正向状态分别为200/404/200，无效凭据均401；没有模型推理或登录调用。
- 交付 health 检查实际镜像 ID、完整挂载、三文件 Compose labels 与渲染语义，Caddy 完整 ID 保持且运行。实际执行生成 wrapper 的 config 子命令。
- 实际读取停态 tar 中的配置、Manager目录、必要密钥，Worker密钥原字节一致；tar0600、live状态目录0700，完整态再次 apply 拒绝。最后按唯一归属清理并核零残留。

## 证据复用

本地旧工具与 `39eb250ef8407213a4e7ff7dea444cdc007370f1` 的 py/sh 正文逐字一致。新旧32个函数 AST 在第三份 Compose、原子临时文件名和恢复标题归一后完全一致，BASELINE 未变；sources、secrets、confirm_deploy、drift、backup 的实际逻辑原样保留。[旧边界总回执](../../phase2-release-boundaries-20261111/README.zh-CN.md) 继续适用：

| 已有专项 | 本轮处理 |
| --- | --- |
| 无TTY、EOF、完整120.58秒超时、保护源/Compose漂移、secret权限 | 复用 `matrix-shell-dependency.log` 各断言；原整轮exit1保持原口径 |
| maintenance后备份失败、保留停态和阶段、拒重入 | 复用 `maintenance-failure.log`，控制器exit0 |
| CPA Env/Args来源、Manager inline-secret优先级 | 复用 `matrix-merge-first.log` 四项原函数断言；原整轮后续exit1保持原口径 |

现有状态保护继续生效：prepare 与切换前 drift 都真实核有效旧 BPS状态来源不存在；不可读来源按 unknown 拒绝；安装前再次拒绝已有宿主目标目录，随后只建立0700新目录。新入口不复用旧prepared，也不删除maintenance或自动回滚数据。

## 已知边界

本轮仅验证本机合成现场的发布生命周期。HTTPS relay的实际Caddy路由、Codex正向2FA及生产活动unknown仍沿用总交付缺证；本轮未修改Caddy或缩减用户选择范围，`write_release_ready=false`。生产版本、在途任务与现场来源仍须按实际回执核验。

本线无产品改动、产品重建、数据库迁移、真实登录/刷新/模型、推送或生产部署。下一责任人：C独立审查工具与证据，主会话整合本轮可审包和范围决策。
