# OVH 四入口候选上线执行包

日期：2026-11-10。项目：MX-CPAMP。责任人：P9；部署材料执行席：P8 Manager/Login。

## 现场基线

用户只读回执确认：Ubuntu x86_64，唯一 Compose 项目 `cpa-stack`，目录 `/opt/cpa-stack`，文件链仅 `compose.yaml`。剩余磁盘约 93GB。三个原服务均运行；Manager healthy，CPA 与 Caddy 未配置容器健康检查。

| 服务 | 现有镜像 | 完整镜像 ID |
|---|---|---|
| CPA | `eceasy/cli-proxy-api:v8.0.63` | `sha256:8377845ba3ec132c3bc41765d348e693703dc88e232d37f7a7eafc2721bff553` |
| Manager | `seakee/cpa-manager-plus:v1.14.10` | `sha256:0e803f21d51aceea7b752686417bf57f40678d8068fe102ba23cb2a3afb414e1` |
| Caddy | `caddy:2.10.2-alpine` | `sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d` |

保留原 Compose、Caddyfile、证书命名卷、Manager `/data`、原管理密钥、CPA 配置/auth/logs/plugins 挂载。仅 Caddy 暴露公网 80/443。原始服务器日志、配置正文与秘密不入仓。

## P8 任务

### WHY

将已验证的本地候选送达既有 VPS，提供可审阅、按阶段执行的准备与切换材料。

### WHAT

准备阶段下载并验证镜像归档、加载镜像、隐藏正文解析现有配置，核实数据及 KV 路径，生成固定持久 override 和双 Compose 管理入口；服务继续运行。切换阶段重新核对漂移，显式确认后短停 CPA/Manager，完成本次一致性备份再启动候选及内部 Worker。Caddy 保持运行。检查只读健康和服务间鉴权，不调用真实登录或模型。

### WHERE

`D:/MX-CPAMP-worktrees/login` 中 `deploy/**`、`docs/deployment*.zh-CN.md`、`docs/evidence/deployment/**`。产品组件保持不变；主分支由 P9 整合。

### HOW MUCH

以已知单项目现场为边界，约 15 分钟给首个可审材料，不构建通用发布框架。镜像复用原构建与专项证据，只验证新增部署边界。

### DONE

Bash 语法、Linux 实际确认/失败处理、真实 Compose 合并与旧卷保留、新 secret 的容器实际读取、归档校验及镜像身份验证通过；记录命令、输入及退出码。独立只读审查后交付固定提交命令。

### DON'T

不改域名、Base URL、API Key、Caddy、SSH、防火墙或旧数据；不安装备份定时任务；不自动回灌旧库或倒退 refresh token；不登录真实账号或调用模型；不应用其他项目的发布脚本。

## 分发与数据影响

候选通过 GitHub prerelease 的 Docker 镜像归档分发，不重新构建产品。版本 `mx-cpamp-20261110-rc1`，归档 `mx-cpamp-images-linux-amd64.tar.gz`，配套 manifest 中的摘要由准备脚本实际消费。

仅本次切换备份 Manager 数据与 CPA 凭据/KV，以及恢复所需配置和密钥；保存于服务器 root 私有目录，不上传仓库。无新增 SQL/schema 迁移。应用运行及后续账号登录会写现有数据目录；生产切换和真实业务验收尚未执行。

当前责任人：P8 完成部署材料；P9 负责镜像分发与核证。下一责任人：独立审查后由用户执行准备阶段并回传。
