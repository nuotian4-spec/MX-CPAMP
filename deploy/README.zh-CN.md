# 本地集成模板

`compose.login.example.yaml` 仅供与现有 Compose 人工合并。保留原 CPA/CPAMP 服务、数据卷、网络、域名与 Caddy 配置；不更改 SSH、证书、防火墙或生产代理。未配置或缺失 Worker secret 时 Manager 登录入口明确报配置错误，原 OAuth 入口保持原状。

本地构建：

```powershell
docker build -t mx-oauth-login:local ./components/oauth-login
```

部署前将至少 32 字节随机共享 secret 放入 `deploy/secrets/oauth_login_secret`，CPAMP 与 Worker 只读同一 secret。Worker 镜像以 `node`（UID/GID 1000）运行，Linux 宿主文件必须 `root:1000`、`0440`；不要使用仅 root 可读的 `0600`。Compose 文件型 secret 不依赖 `uid/gid/mode` 重映射。新建文件可在 Linux 上执行：

```sh
sudo install -d -o root -g root -m 0700 deploy/secrets
sudo sh -c 'umask 077; openssl rand -hex 32 > deploy/secrets/oauth_login_secret'
sudo chown root:1000 deploy/secrets/oauth_login_secret
sudo chmod 0440 deploy/secrets/oauth_login_secret
```

仅对本次新文件执行；既有 secret 不覆盖。secret 不进入命令参数、Compose environment 或聊天记录。Worker 只向 Compose 内网提供 18417，不映射宿主机端口。`init: true` 回收终止任务的子孙进程。模板使用既有服务名 `cpa-manager-plus`，不是创建第二个管理服务。

BPS 运行时模型清单由 CPA 配置显式填写，未核验的模型保持空清单，不复制历史默认模型。现有 Caddy 域名与路由保持原位，不为 Worker 增加公网路由。

新组件不挂载 CPA auth、Manager SQLite/data.key 或 Docker socket，不新增 SQL schema。移除本模板新增服务和 Manager 两个环境项即可停用内部 2FA 登录，不删除 CPA 已导入账号。CPA 与 CPAMP 镜像构建和切换由本轮集成负责人统一核证后执行；此模板未在生产执行。
