# 本站更新宿主桥首次接入

宿主桥把既有站点接入网页核心更新。首次接入由 Linux root 在明确维护窗口执行；日后的固定版本任务由独立 systemd worker 执行，不依赖 Manager 进程存活。

## 独立 bootstrap 材料

在本地仓库运行打包脚本，输出目录须已存在，目标归档须是新路径：

```sh
python3 /path/to/MX-CPAMP/deploy/package-update-host.py \
  --output /path/to/output/mx-update-bootstrap.tar.gz
```

脚本只读取同目录本地材料并生成 tar.gz，不下载、发布或提交代码。归档根目录包含六个普通文件，所有者元数据为 root:root：

| 文件 | 归档权限 |
| --- | --- |
| `mx-update.py` | 0700 |
| `mx-update-host.py` | 0700 |
| `mx-update-host-install.py` | 0700 |
| `mx-update-host.service` | 0644 |
| `mx-update-operation@.service` | 0644 |
| `README.update-host.zh-CN.md` | 0644 |

bootstrap 是独立材料包，不属于核心发行 `release.json` 的 ASSETS 集合。既有固定版本发行合同保持不变；本包不包含镜像、站点配置、账号、数据库或 `release.json`。

## 接入前提

- Linux root、Python 3.9 以上、systemd、Docker Compose v2 和 curl 已可用。
- 使用已登记的真实站点文件，填写现有项目名、站点 root、基础 Compose 链、CPA 配置、状态映射、固定发行来源和入口健康 URL/预期状态。站点文件为 root 所有、0600；示例路径须换成实际站点。
- `cli-proxy-api`、`cpa-manager-plus`、`oauth-login` 三应用当前使用同一完整 Compose 链，保留基础链与全部活动 override；四服务处于可核验的运行状态。
- CPA 以 root 运行。宿主和 CPA 共用同一个 root:root、0600 的 `.mx-update.lock` 文件 inode，通过 bind mount 与 `flock` 协调。保留原锁文件，不用删除再创建或复制替换锁的方式清理任务。
- `.mx-update-state` 目录由本站独占，权限 0700；Unix socket 位于 `/run/mx-update/<项目名>.sock`，权限 0600，目录权限 0700。CPA 只读绑定该 socket 目录，携带固定站点 ID 调用，不开放任意目标 URL。

当前宿主注册表固定为 `/etc/mx-update/site.json`，只登记一个站点；已有另一 root 或项目名的登记会拒绝。既有站点文件本身就在 `/etc/mx-update/site.json` 时受支持：保留该 0600 普通文件，不建立指向自己的符号链接。

## 首次安装

将经审查的 bootstrap 归档置于服务器本地。用实际归档路径、解压目录和站点路径替换示例；在 Linux root shell 执行：

```sh
ARCHIVE=/root/mx-update-bootstrap.tar.gz
BOOTSTRAP=/opt/mx-update-bootstrap
SITE=/opt/cpa-stack/site.json

install -d -m 0700 "$BOOTSTRAP"
tar -xzf "$ARCHIVE" -C "$BOOTSTRAP"
chown root:root "$SITE"
chmod 0600 "$SITE"

python3 "$BOOTSTRAP/mx-update-host-install.py" \
  --site "$SITE" \
  --confirm-maintenance
```

`--confirm-maintenance` 是首次完整维护授权：停止原 Caddy 和三个 APPS 服务，保留配置、站点文件与原活动指针备份，增加桥接 Compose 文件，使用完整应用链一次重建三个 APPS，再启动本轮原 Caddy 容器。它不是单独重启 CPA，也不升级应用镜像。重建使用 `--no-build --pull never --no-deps --force-recreate`；最终核对原镜像、完整链、原 Caddy ID、挂载与入口健康。

安装把脚本放入 `/usr/local/lib/mx-update/`，单位文件放入 `/etc/systemd/system/`，登记固定站点并启动 `mx-update-host.service`。原系统材料存在时一并保留到本轮备份。worker 单位通过 `ExecStopPost` 调用 `--interrupted`；维护中断时持久保留所有者，并核查四服务停态。

现场核对命令：

```sh
systemctl status mx-update-host.service --no-pager
systemctl cat mx-update-operation@.service
```

成功回执位于 `<站点 root>/.mx-update-bootstrap/<本轮时间>/completed.json`，任务记录为 completed，active 所有者由安装程序正常清除。保留本轮备份与日志。

## 首次接入失败后的恢复

保留 `.mx-update-bootstrap/<本轮时间>/`、`.mx-update-state/`、桥接文件和实际运行材料。停态后失败会保留 recovery_required 与 active 所有者，并尝试停止四服务；停止动作失败时现场停态仍须核实。首次接入不自动回灌数据库，也不自动重试安装。

先读取本轮记录：

```sh
BACKUP=/opt/cpa-stack/.mx-update-bootstrap/本轮时间
python3 -m json.tool "$BACKUP/before.json"
```

按该轮材料恢复，而不是凭当前基础 Compose 文件猜测原状态：

1. 依据 `before.json.site_path` 和备份 `site.json`，恢复原站点登记；把备份 `config.yaml` 恢复到原 `cpa_config`，保持 root 所有、0600。
2. 原现场有活动指针时，从 `active-pointer.json` 恢复原 `active_release_pointer`；原现场无指针时，恢复其原本不存在的状态。该判断以 `before.json.runtime.pointer` 为准。
3. 取 `before.json.original_chain` 的每个文件及原顺序，恢复全部 Compose 链与对应原镜像。项目 root、项目名取原站点登记；镜像 ID 以 `before.json.runtime.services` 为准，保留已有 override，不追加另一条链冒充原链。
4. 用核实后的原完整链重建三个 APPS，保留原镜像、不 build、不 pull；再启动 `before.json.runtime.services.caddy.id` 指向的原 Caddy，并确认入口恢复。

实际现场恢复后再执行收口确认：

```sh
python3 /usr/local/lib/mx-update/mx-update-host-install.py \
  --site "$SITE" \
  --recover-bootstrap "$BACKUP"
```

`--recover-bootstrap` 只核验实际容器、原镜像与完整链、原活动指针和登记入口健康，不替操作者执行上述恢复。全部通过后，将本轮失败任务记为 failed/recovered，并清除它自己的 active 所有者；不把恢复原版本记为安装成功。不要直接删除 `active.json` 解锁，也不要删除备份后重跑首次安装。

## 日后核心更新任务的恢复

日后核心任务使用不同的恢复入口。先依据该任务的候选基线、`.mx-updates/<版本>-<任务编号>/plan.json`、完成记录与活动指针，恢复或完成实际现场；保留任务、备份及停态记录。

```sh
OPERATION=core-实际任务编号
python3 /usr/local/lib/mx-update/mx-update-host.py \
  --site /etc/mx-update/site.json \
  --recover "$OPERATION"
```

该命令仅核验当前运行容器、镜像、完整 Compose 链和活动指针，以及入口健康。恢复原基线时收口为失败；确认新版本时还要求 plan 镜像、同轮 completed 记录、原 Caddy 身份与活动指针相互匹配。核验通过后才清除本任务所有者，不自动回滚、不回灌数据库，不以删除 active 文件代替恢复。

## 验收边界

离线 contract 测试证明控制流程、文件锁、材料与合成系统边界；它不是生产实测。归档生成、systemd 单位静态检查和合成健康回执，都不代表真实站点维护、真实 systemd 生命周期、许可服务或业务推理已经通过。真实服务器接入和业务验收按站点维护授权另行记录。
