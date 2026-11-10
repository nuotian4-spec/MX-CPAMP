# cpa-stack 本轮上线

仅适用于已核现场：项目 `cpa-stack`、目录 `/opt/cpa-stack`、原入口仅 `compose.yaml`，CPA/Manager/Caddy 镜像和挂载与预检一致。prepare 不停服务；apply 需要独立执行并在终端输入 `DEPLOY`。只切 CPA/Manager，新增内网 oauth-login，Caddy、域名、原 API Key、原 Compose、`.env` 与其 env_file 来源保持原位。

## 下载与准备

候选资产已发布为 [mx-cpamp-20261110-rc1](https://github.com/nuotian4-spec/MX-CPAMP/releases/tag/mx-cpamp-20261110-rc1)，固定已审提交 `3baeb518da2dbbff14befe386701a9d2815c9a9c`。三个文件必须下载到同一目录，使用服务器既有代理环境，不把 Windows 本机代理地址写入服务器。

```bash
sudo bash <<'BOOTSTRAP'
set -euo pipefail
SOURCE_COMMIT='3baeb518da2dbbff14befe386701a9d2815c9a9c'
RELEASE_BASE='https://github.com/nuotian4-spec/MX-CPAMP/releases/download/mx-cpamp-20261110-rc1'
TOOLS='/opt/cpa-stack/mx-release-20261110-rc1'
install -d -o root -g root -m 0700 "$TOOLS"
for name in cpa-stack-release.sh cpa-stack-release.py cpa-stack-compose.sh; do
  curl -fLsS --proto '=https' \
    "https://raw.githubusercontent.com/nuotian4-spec/MX-CPAMP/$SOURCE_COMMIT/deploy/$name" \
    -o "$TOOLS/$name"
done
chmod 0600 "$TOOLS/"*
bash "$TOOLS/cpa-stack-release.sh" prepare --release-url "$RELEASE_BASE"
BOOTSTRAP
```

也可预先下载 `mx-cpamp-images.manifest.json` 和 `mx-cpamp-images-linux-amd64.tar.gz` 到同一目录，改用：

```bash
sudo bash /opt/cpa-stack/mx-release-20261110-rc1/cpa-stack-release.sh prepare --bundle-dir /实际bundle目录
```

回传 `prepared=... maintenance_started=no` 与所有阶段行。准备阶段实际消费 manifest 中的 SHA256，分别核 gzip 与 docker load 退出码，检查三镜像接受的完整ID、revision、linux/amd64。Docker Desktop manifest ID 和 Linux config ID 的已核接受集合由本次 manifest 固定。

prepare 将 Compose JSON、CPA 配置解析结果、容器元数据、镜像装载输出保存在 `/opt/cpa-stack/.mx-upgrade-20261110-rc1`，目录 `0700`、文件 `0600`；正文含私密配置，不贴聊天。只输出阶段、错误类别和材料位置。现有 auth-dir 必须映射既有 auth 挂载；自定义 kv-store.path 只接受落在已知持久挂载内的路径，默认 `<auth-dir>/kv.db`。Manager 按实际所选 `/data` 内 JSON 检查持久路径并记录文件漂移；未指定 JSON 时，核镜像内默认配置文件确实不存在且 USAGE 路径在 `/data`，才接受纯 ENV 模式。未知路径明确停止。

既有 `secrets/oauth_login_secret` 不覆盖；仅缺失时生成，要求 `root:1000`、`0440`。Compose 使用单文件 secret 挂载，Worker不挂载整个 `0700` secret 目录，不依赖 Compose uid/gid 映射。持久 override 先在本轮私有目录合并核验，再发布为 `compose.mx-cpamp.yaml`；仅识别同 inode 的本轮材料恢复发布，未知 override 拒绝覆盖。

维护前用原 CPA API Key 做只读模型目录请求；旧 Manager 原管理文件 key 必须得到 config 200，无效 key 必须401。未setup匿名放行或原 bootstrap key 已失效均停止准备，不重置用户密钥、不要求贴出密钥。

## 确认与切换

仅在 P9 核 prepared 回执、独审放行后执行：

```bash
sudo bash /opt/cpa-stack/mx-release-20261110-rc1/cpa-stack-release.sh apply
```

必须有 TTY，逐字输入 `DEPLOY`；取消、EOF、无 TTY 都不进入维护。切换前重核现场、环境来源、所选 Manager JSON、配置和镜像。随后只停 CPA/Manager，确认两者停止且原 Caddy 仍运行，再备份整个 `manager-data`、CPA auth、独立自定义 bbolt、必要配置/secret及环境来源。备份位于本轮 state 的 `backup`，目录0700、归档0600，覆盖 SQLite、WAL、data.key；不安装定时备份。

以固定双文件入口启动三个候选服务，禁止 pull/build，不碰 Caddy。收敛检查为 CPA 认证目录200、候选 Manager 严格鉴权的不存在任务404、Worker认证health200，以及原 Caddy 容器仍运行；不触发登录、任务创建或模型推理。CPA/Caddy 原无 Docker health，不将它们写成 healthy。

后续日常管理统一使用：

```bash
sudo /opt/cpa-stack/cpa-compose ps
```

入口固定带 `-p cpa-stack -f compose.yaml -f compose.mx-cpamp.yaml`，避免只用原文件丢失新环境与 Worker。

## 失败与代码回退

失败保留真实阶段及原日志，不自动回灌 SQLite、bbolt 或账号 refresh token。维护已开始时，脚本拒绝从 apply 盲重跑；先由部署负责人核 `stage`、`stop.log`、`up.log`、`health.log`、当前容器与备份。不要改 state、删锁或强删未知 override 绕过门槛。已完成发布再次执行会被拒绝。

需要代码回退时由 P9 核实现场后，仅用原 Compose 与原镜像切回 CPA/Manager，Worker停止，Caddy保持运行；既有代码和环境来源不覆盖：

```bash
cd /opt/cpa-stack
sudo ./cpa-compose stop oauth-login
sudo docker compose --project-directory /opt/cpa-stack -p cpa-stack -f compose.yaml \
  up -d --no-deps --no-build --pull never cli-proxy-api cpa-manager-plus
```

这两条是待现场核准的代码恢复口径，不恢复旧库；仍须核原镜像本地存在及运行身份。回退镜像不撤销切换期间已经更新的账号凭据或业务数据，备份只作为受控数据恢复材料。
