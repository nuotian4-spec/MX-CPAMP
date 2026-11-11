# 二期只读现场盘点

基线：`9eb2ec9e7f45876304f91ca3cb7f8fce2423d706`。仅新增盘点脚本与本目录材料，不修改 rc1 发布工具或产品代码。

## 用户一次复制入口

`short-preflight.sh` 是完整可复制命令，43行；原样粘贴到 VPS 的 SSH 终端执行。默认选择 `cpa-stack`；`CPA_PREFLIGHT_PROJECT` 仅供显式选择其他项目和本地隔离测试。命令不下载文件、不创建脚本或报告，不读取秘密正文。

输出四服务实际容器/镜像ID、revision、健康、网络模式、网络名、实际发布端口、挂载及各自完整 Compose 标签链；还包含磁盘和原 rc1 完成标记、备份、拟 BPS 目录、管理 wrapper 的存在/权限/大小。Caddy 可保持原单文件链，三应用可使用原文件加持久 override，逐服务如实输出。未知或缺少镜像 revision 不补造。

原 rc1 完成标记由 `deploy/cpa-stack-release.sh:125` 创建为 `.mx-upgrade-20261110-rc1/complete`；wrapper 按该脚本第99行安装到 `cpa-compose`。盘点不读取其内容或执行 wrapper。

短入口不执行 HTTP 请求，不验证认证或 Caddy 路由；登录和模型在途均明确 `unknown`，最终 `write_release_ready=false`。输出是现场事实，不是发布放行。

## 完整本地保留入口

`deploy/phase2-readonly-preflight.sh [PROJECT]` 默认项目为 `cpa-stack`。Bash/Python3、Docker/Compose 和容器内既有 Caddy 是依赖；不安装依赖。全部子进程 stdout/stderr 先在内存捕获，只有白名单 JSONL 回传。错误仅固定代码。

命令范围：`docker ps/inspect/image inspect/compose config/version`、容器内 `caddy adapt`、本机 stat/disk usage/必要文件读取，以及选中内部 Docker IP 的固定 GET。无 up/start/create、拉取、登录、刷新、模型推理、状态文件写入或数据修改命令；不跟随 HTTP 重定向，不使用外部代理。

完整 Compose 解析和 CPA YAML 内容、env、凭据及密钥只在服务器进程内使用，不进入 argv/env 或输出。配置字段使用当前顶层 `bps-protocol`；显式状态路径保留。只对 GET 端点读取认证状态及白名单计数，不输出正文、账户列表、URL、查询、站点域名或 Caddy 配置。Caddy 适配只报告中转路径/方法/目标分类，实际顺序仍需审阅。

Manager 单任务与 Worker 都没有可用的只读活动任务列表，始终记录未知。旧 rc1 的批次、凭证并发接口缺失返回404时记录未知。并发只接受 `mode=local` 且各计数均非负整数；Home、缺字段、非法值、认证失败均不是零。此入口同样固定 `write_release_ready=false`。

## 本地边界验证

执行：在工作树根目录运行 `docs/evidence/phase2-release-preflight/run.ps1`，结果 `fixture.log`，exit 0。

测试使用缓存 `cpa-backup-test:20261110` 的 Linux/Python/Docker CLI/Compose v2.33.0、缓存 Python 与 Caddy 镜像，以及本机 Docker Engine。该 Compose 与生产版本不相同；验证覆盖真实多文件合并、标签、bind、端口与 inspect 边界，不声称验证了生产版本所有行为。没有下载或重新构建镜像。

fixture 创建随机项目、四个容器、专用网络/卷；Docker socket 仅提供给本地测试容器。Caddy先用原文件创建，CPA/Manager/Worker再用双文件创建，保持 Caddy 原链。HTTP应用使用合成服务器，结构按真实 Manager handler 的根对象/null批次、并发快照契约；不是生产认证或业务验收。

覆盖：

- 四服务/原加override/Caddy原链、显式顶层BPS状态路径。
- inspect 的 env/Args旁路、Compose环境、状态/备份文件、API正文含秘密哨兵；脚本输出与stderr无哨兵。
- 运行前后文件字节、容器完整ID/镜像ID/启动时间一致。
- 三个认证探针、可读local计数、旧404/Home/非法pool/错误密钥全部按预期分类。
- 短入口按文件原字节运行；测试容器已为root，仅使用本地sudo透传桩和随机项目环境项，不替换任何 Docker/Compose/文件盘点逻辑。真实发布端口、网络模式、wrapper元信息及未知在途断言通过。
- fixture 容器、网络和卷定向清理，`fixture_cleanup=verified`。

不重复产品测试，不执行生产操作。完整脚本与短入口的适用范围分别如上，不把 fixture 认证/计数结果归到旧 rc1 现场。
