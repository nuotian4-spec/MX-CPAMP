# 四入口与 BPS 本地候选

日期：2026-11-10。范围：本地二改、验证与仓库交付。状态：本地候选通过，真实业务与生产验收待安排。

## 仓库交付状态

开发分支 `codex/openai-bps-login` 已推送，已创建 [草稿 PR #1](https://github.com/nuotian4-spec/MX-CPAMP/pull/1)。首批交付提交为 `3bc483f1a9e4319c9568dc216d7b68de6c8a1345`；后续文档与只读预检材料不改变下列产品与镜像输入。

CPA 上游自带的两项 Google OAuth 公开客户端常量，按用户授权完成本次推送放行，保留原有源码与 Google 登录行为。已通过 GitHub API 核实仓库 `secret_scanning`、`secret_scanning_push_protection` 均为 `enabled`。三镜像归档已作为 [mx-cpamp-20261110-rc1](https://github.com/nuotian4-spec/MX-CPAMP/releases/tag/mx-cpamp-20261110-rc1) 分发；生产服务尚未切换，真实业务验收完成前保持 PR 草稿。

用户已回传 [服务器只读预检](deployment-preflight.zh-CN.md)，确认既有 cpa-stack、完整镜像和数据挂载。匹配现场的 [准备与切换材料](deployment-release.zh-CN.md) 已通过 Linux 专项及 [独立审查](evidence/deployment-review.zh-CN.md)。当前等待用户执行准备阶段，回传后再进入一次性备份与受控切换。

## 实现

- 保留 Codex OAuth，增加 Codex 2FA、BPS OAuth、BPS 2FA。每次仅申请所选通道授权，账号固定使用自己的通道与 client。
- Manager 创建内存登录任务，绑定当前 CPA 实例；浏览器预先保存任务编号，创建回执丢失仍可查询和取消。明确拒绝的 OAuth 回调允许纠正重提，提交结果未知时继续持锁查询。
- 内部 Worker 通过 stdin/fd3 传递当次凭据与授权结果，取消、超时清理任务进程组。密码与 TOTP 种子不长期保存；浏览器不接收 token。
- CPA 独立保存和刷新 Codex/BPS 凭据，保存及热注册完成后才报告成功；补登录与刷新沿用凭据版本协调。
- BPS 接入文本 Responses、Chat Completions 和 SSE，按显式模型清单注册；同名模型去重并沿用账号槽位调度。

## 验证输入与证据

| 组件 | 最终产品提交 | 验证 |
|---|---|---|
| CPA | `5c90a92eef27f450c7080bc8735c88d873fc8915` | 首轮八包专项、OAuth/import 及 client/storage；修复后 helps/executor/service 三包、首 delta 路径两包定向回归 |
| Manager | `07572357832b69a30e3ee6c45f9f0cc2c3350e0c` | 首轮 Manager 五包、Web 67 项；修复后 Go 两包、Web 7 项、lint、实际 tsc/vite 和 Linux 构建 |
| Worker | `42b04ac71df347e9c22fa35bad484c437da52d4b` | Linux 7 项、stdin/fd3、鉴权、取消/超时和 secret 权限实际边界 |

整合分支通过组件逐目录 diff 确认与这些产品输入一致；后续证据与文档提交不改变产品构建输入。专项通过不等于全上游测试或真实业务通过。

- [CPA 首轮回执](evidence/cpa-runtime/README.zh-CN.md)、[CPA 修复回执](evidence/cpa-runtime/review-p2.zh-CN.md)、[首 delta 事件生命周期](evidence/cpa-runtime/review-delta.zh-CN.md)。
- [Manager/Worker 回执](evidence/manager-login/RECEIPT.zh-CN.md)、[整改与预集成回执](evidence/manager-login/REVIEW-AND-CROSSLINE.zh-CN.md)。
- [整合分支输入与范围核证](evidence/local-candidate-checks.log)。

独立审查已关闭 Manager 两项 P2、CPA 三项 P2（含首 delta 建立事件的补充复核），无剩余审查阻断。见 [独立审查记录](evidence/independent-review.zh-CN.md)。

最终 CPA Linux 构建与真实三容器集成均退出 0。Manager→Worker 的 stdin/fd3 授权管道→CPA v8 保存/热注册全链路使用真实组件，仅登录上游采用合成授权进程。覆盖两通道凭据及 refresh token、同名模型去重、未认证 401、回执脱敏，以及创建回执丢失后取消零导入；本次临时容器和卷已核验清理。

| 本地镜像 | 实际完整镜像 ID |
|---|---|
| `mx-cpa:integration` | `sha256:58d0177203d8f4e4968cebc60c59bdb0e236404a05c27656a38351dc1022ab93` |
| `mx-cpamp:integration` | `sha256:85a8f76852c08de220ba132490cbdf234b87f1bdeba91a6e0d9bfa905769a25c` |
| `mx-oauth-login:candidate` | `sha256:64168881ce990f58d24736d1f8488549457ed2c775a072c1d0572bd524b7926b` |

三者实际 revision 分别对应上表产品提交，见 [身份核证](evidence/manager-login/final-image-identities.log)、[最终跨线日志](evidence/manager-login/crossline-final.log)。Manager/Worker 产品输入未变，复用原镜像和专项；仅受影响的 CPA 重新构建。

## 本地构建

在已核干净的仓库根目录执行。三份构建上下文只包含对应组件；这些命令不切换服务器服务。本地联网沿用 7890 代理及 Docker Desktop 既有代理配置，不将本机地址写入服务器。

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
$sourceCommit = (git rev-parse HEAD).Trim()
docker build -t mx-cpa:local --label "org.opencontainers.image.revision=$sourceCommit" --build-arg "COMMIT=$sourceCommit" --build-arg VERSION=local-bps -f components/cpa/Dockerfile components/cpa
docker build -t mx-cpamp:local --build-arg "SOURCE_COMMIT=$sourceCommit" --build-arg VERSION=local-four-entry -f components/cpamp/Dockerfile.manager-server components/cpamp
docker build -t mx-oauth-login:local --build-arg "SOURCE_COMMIT=$sourceCommit" -f components/oauth-login/Dockerfile components/oauth-login
```

本轮已验证的镜像及其真实 ID 见组件回执，无需为重复本地验收再构建相同输入。内部服务接入方式见 [部署模板](../deploy/README.zh-CN.md)；该模板用于人工合并现有 Compose，不直接覆盖生产配置。

## 数据与业务验收

本次没有 SQL schema/migration 或线上数据库动作。测试假账号仅写入隔离临时卷；未来真实登录会新增或更新 CPA 凭据文件。现有域名、Base URL、API Key、数据目录与定时任务均未在服务器修改。

已知限制：

- 四入口真实登录、所属 client 的令牌刷新、真实模型与 SSE 调用仍待业务验收。BPS 手工 OAuth 的官方回调授权码是否已被页面消费尤其需要真实回执。
- BPS 模型清单默认空；只填写已确认可用的模型，不以历史默认列表代替当前能力。
- 首期 BPS 不接原生 WebSocket、工具、媒体、opaque/compaction、background、服务端 `previous_response_id` 或 token counting。
- 自动续期由 CPA 执行；刷新失效后人工补登录，不保存密码/TOTP 作无人值守重登。
- 候选镜像已发布；生产准备、切换与本次必要备份尚未执行。分发身份见 [归档核证](evidence/deployment/archive-verification.zh-CN.md)。
