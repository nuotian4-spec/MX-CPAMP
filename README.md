# MX-CPAMP

面向个人部署的 CPA / CPA-Manager-Plus 定制项目。

## 当前状态

已实现四入口、独立 BPS 运行时与内部登录容器，通过本地专项、Linux 构建、三容器集成和独立审查。真实业务验收待安排，尚未更新生产服务器。

[本地候选与验收记录](docs/local-candidate.zh-CN.md) · [上线准备与切换](docs/deployment-release.zh-CN.md) · [上线前只读预检](docs/deployment-preflight.zh-CN.md) · [方案与架构](docs/architecture.zh-CN.md) · [上游来源与基线](docs/UPSTREAMS.md)

## 目标

- Codex OAuth、Codex 2FA、BPS OAuth、BPS 2FA 四个入口。
- 账号按授权类型固定通道，同名模型合并展示与调度。
- 2FA 登录器在内部容器运行；密码与 TOTP 种子仅当次使用。
- CPA 统一保存令牌、自动刷新和转发请求；刷新失效后人工补登录。

## 仓库布局

采用普通目录单仓库：

```text
components/
  cpa/             CPA 定制源码与原许可
  cpamp/           CPA-Manager-Plus 定制源码与原许可
  oauth-login/     登录执行器、双 profile Adapter 与独立构建
deploy/            无真实凭据的 Compose、Caddy 和配置模板
docs/              设计、来源、验证与发布记录
```

三份镜像分别使用组件目录作为构建上下文，不复制上游 `.git`，不使用 submodule。保留各组件的上游许可与来源，不将全部文件统一声明为一种许可。

## 使用边界

- 2FA 入口只取得所选通道授权；CPA 负责保存和刷新令牌。刷新失效后在对应入口补登录。
- BPS 首期为文本 Responses、Chat Completions 及 SSE；同名模型沿用现有账号调度。BPS 模型在 `oauth.providers.bps.models` 显式配置，默认空清单。
- BPS 不接原生 WebSocket、工具、媒体、compaction、background 或 `previous_response_id`。已有 Codex 能力沿用原路径。
- 本地验证采用假登录上游。四入口真实登录、令牌续期和真实模型调用待业务验收；BPS 手工 OAuth 的官方回调授权码可兑换性仍待确认。

[内部登录部署模板](deploy/README.zh-CN.md) · [CPA 管理契约](docs/contracts/cpa-login-v8.md) · [Worker 契约](docs/contracts/login-worker.md)

## 数据与发布

本仓库是公开仓库，只保存源码和脱敏模板。实际账号、令牌、密码、TOTP、数据库、备份与运行日志不入仓，也不进入镜像。

现有域名、API Key 和生产数据目录保持不变。开发与审查在 `codex/` 分支进行；本地验证、仓库推送、镜像发布、生产部署分别记录真实结果。
