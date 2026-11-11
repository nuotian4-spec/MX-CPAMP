# 上线前只读预检证据

日期：2026-11-10。基线 `8030a95d65c7a0c51e4fd4c0bcb36a1b82eebd32`，工作区起始干净。新增 `deploy/readonly-preflight.sh`、使用说明及本项证据；产品、构建和已有部署模板未变。

脚本按服务标签或同名容器发现 CPA/Manager，列出所属项目及同项目 Caddy；one-off 标签单列，多项目不自动选择。输出白名单身份、健康、端口、Compose 路径链及 Mount Type/Name/Source/Target/RW，路径只读 stat/du；配置只取数字版本。可选 Labels 缺失时使用安全 index，缺 revision 明确 `not_declared`，保留镜像ID与架构。

## Linux 端到端

复用本机缓存 Debian CPA 镜像 `mx-cpa:integration`，ID `sha256:58d0177203d8f4e4968cebc60c59bdb0e236404a05c27656a38351dc1022ab93`，无网络、只读根目录；测试临时目录可执行，以运行 Docker 元数据替身：

```powershell
docker run --rm --network none --read-only --tmpfs /tmp:exec,mode=1777,size=32m --mount 'type=bind,source=D:/MX-CPAMP-worktrees/login/deploy,target=/checks,readonly' --mount 'type=bind,source=D:/MX-CPAMP-worktrees/login/docs/evidence/manager-login,target=/fixtures,readonly' --entrypoint bash mx-cpa:integration /fixtures/preflight-fixture.sh
```

退出 `0`，`preflight-linux-fixture.log`。包含真实 Linux `bash -n`，以及直接执行交付脚本的无服务、单项目、双项目、独立无Labels容器/镜像四场景。单项目含常驻、one-off、同项目Caddy、完整双Compose路径链、bind空Name及命名卷Name。Env、配置、账号正文的三个合成 secret 哨兵均未出现在脚本 stdout；替身拒绝读命令以外的 Docker 操作。

## 实际 Docker 模板边界

```powershell
& docs/evidence/manager-login/preflight-docker-smoke.ps1
```

退出 `0`，`preflight-docker-smoke.log`。从交付脚本直接抽取实际 `--format` 字符串，通过真实 Docker CLI 核验。缓存原镜像 `eceasy/cli-proxy-api:v8.0.63`，ID `sha256:8377845ba3ec132c3bc41765d348e693703dc88e232d37f7a7eafc2721bff553`，确无Labels/revision。测试仅新建本次随机前缀的停止态容器及空命名卷，容器含假Env哨兵及只读bind；未启动容器、未连接Docker socket进测试容器、未拉镜像。

实际模板全部退出 `0`，镜像仍输出完整ID、linux/amd64与 `revision=not_declared`，无Labels查询为空，bind Name为 `-`，命名卷Name正确保留。Env哨兵未输出。测试容器及卷已定向删除，分别inspect确认不存在，`actual_docker_fixture_cleanup=verified`。

本轮未读取真实 `.env`、密钥或账号文件，未调用服务器，未运行产品测试/构建，未push、发布镜像或部署。下一责任人：P9整合脚本、核固定提交并向用户交付服务器只读调用；收到真实现况回执后再准备发布命令。
