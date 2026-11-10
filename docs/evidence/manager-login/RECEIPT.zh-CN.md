# 管理面板与内部登录器本地候选

日期：2026-11-10。输入基线：`22d0f16cfcd741a95dd34810aca61ea688390f10`。产品提交：`42b04ac71df347e9c22fa35bad484c437da52d4b`，分支 `codex/manager-login`。后续证据提交只包含本目录与组件证据文档；产品代码、锁文件及镜像构建输入等价。

## 交付范围

- 原 Codex OAuth 保留；新增 Codex 2FA、BPS OAuth、BPS 2FA。账号 BPS 标签与补登录跳转独立，BPS OAuth 明示真实可兑换性待验。
- Manager 内存任务、管理员鉴权、同一时刻一项任务、固定 provider/client、仅精确 v8 auth-url/callback/status/session 与后台 import。任务及 OAuth 会话绑定创建时 CPA URL/管理密钥；切目标后拒绝导入与回调。
- 导入通过既有 MutationCoordinator；只有 CPA 返回 `saved=true`、`registered=true`、正确 provider/auth_id 才报告成功。导入开始后不宣称取消。OAuth 回调接受不等于成功，写锁保持至状态终态或会话过期；不自动重试回调。
- Worker 单 profile 授权、stdin 输入、fd3 返回、固定枚举错误、额外验证 needs_user_action；取消/超时终止进程组。Manager 首次新建任务先清理旧进程遗留任务；终态结果主动清理。
- 登录依赖与 CPA 推理进程隔离。密码/TOTP 不进入 argv/env/文件/任务日志/SQLite，不用于无人值守重登。CPA 为唯一 token 保存和刷新者。
- 许可保留 toSub2 MIT 与 Sub2API LGPL-3.0。部署为人工合并模板，既有服务名 `cpa-manager-plus`；无 Worker 公网端口、Docker socket 或业务卷。BPS 模型清单显式为空，待填已核模型。

## 已通过证据

下列命令均 exit 0。专项测试在产品提交前相同源码树运行；提交只固定代码身份，不改变测试输入。日志均为本地假凭据与假上游，不含真实账号材料。

| 验证 | 命令与输入 | 证据 |
|---|---|---|
| 固定依赖安装 | Node22.23.4/npm10.9.9；CPAMP `npm ci --ignore-scripts --no-audit --no-fund` | `npm-install.log` |
| Manager 相关包和真实 Worker 整合 | 固定 Go1.26.10；Manager目录 `go test -count=1 ./internal/service/oauthlogin ./internal/http/controller/oauthlogin ./internal/http/router ./internal/config ./internal/service/proxy`，`OAUTH_LOGIN_TEST_NODE` 指固定22.23.4，`OAUTH_LOGIN_TEST_COMPONENT` 指本组件 | `go-tests-final.log` |
| 前端专项与原入口回归 | CPAMP目录 `npm --workspace apps/web run test -- src/features/oauth/InternalLoginCards.test.tsx src/services/api/loginTasks.test.ts src/features/oauth/OAuthPage.test.tsx src/features/accounts/model/accountReauth.test.ts src/features/accounts/model/accountsPagePresentation.test.ts`；67/67 | `web-tests-final.log` |
| 新前端代码 lint | `npx eslint apps/web/src/features/oauth/InternalLoginCards.tsx apps/web/src/services/api/loginTasks.ts --max-warnings=0` | `web-lint-final.log` |
| Worker Linux 容器专项 | `docker run --rm --init --network none --mount type=bind,source=D:/MX-CPAMP-worktrees/login/components/oauth-login/test,target=/app/test,readonly -w /app --entrypoint node mx-oauth-login:candidate --test test/worker.test.mjs test/profiles.test.mjs`；7/7 | `worker-container-tests.log` |
| Worker 镜像构建 | 根目录 `docker build -t mx-oauth-login:candidate --build-arg SOURCE_COMMIT=42b04ac71df347e9c22fa35bad484c437da52d4b -f components/oauth-login/Dockerfile components/oauth-login` | `worker-docker-build-bound.log` |
| Manager Linux 镜像与最终 Web 构建 | 根目录 `docker build -t mx-cpamp:candidate --build-arg SOURCE_COMMIT=42b04ac71df347e9c22fa35bad484c437da52d4b --build-arg VERSION=local-four-entry -f components/cpamp/Dockerfile.manager-server components/cpamp`；同 Web 输入复用成功的 tsc/vite 层，最终重建绑定真实 revision 的 Go 层 | `cpamp-docker-build-final.log`、`cpamp-docker-build-bound.log` |
| Linux secret 权限真边界 | 新镜像默认 USER node，Linux 原生临时卷 root:1000/0440 可读32字节；root:root/0600 返回 EACCES；临时卷随后清理 | `worker-secret-permissions.log` |
| 最终 Manager 镜像健康与鉴权 | `docker run --rm --network none --read-only --tmpfs /data` 启动最终镜像；容器内 `/health` 返回 ok，未认证新任务入口401且no-store；容器已销毁 | `cpamp-container-health.log` |
| 镜像代码身份 | `docker image inspect mx-cpamp:candidate mx-oauth-login:candidate`；两者 revision 均完整产品提交42b04ac | `image-identities.log` |

Manager 整合穿过实际 HTTP Worker、实际 stdin/fd3 与假 CPA v8：Codex/BPS 成功各一次；额外验证、失败、取消不导入；浏览器结果无 token/password/TOTP。另覆盖超时、单任务忙、错误 client、CPA 未热注册、导入取消竞态、目标漂移、未知 v8 action、未认证401/no-store，以及 OAuth 保存状态前写锁保持。前端覆盖离页/切实例取消旧任务和正常轮询不取消。

Worker 容器测试覆盖两通道假上游认证、TOTP/workspace/PKCE、独立 token endpoint、错误 client、额外验证无重试、服务鉴权、结果清理、子孙进程取消/超时。原 Windows及缓存Linux镜像专项见 `components/oauth-login/EVIDENCE.zh-CN.md`；最终新镜像使用相同业务源码和依赖，revision标签不改变登录输入。

先前失败和中间日志保留为诊断证据，最终结论仅引用上述文件。`cpamp-docker-build.log` 为类型检查失败记录，不能当作构建通过；`web-typecheck.log` 不替代成功镜像内的 tsc。

## 数据与真实业务边界

无 SQL schema、migration 或线上数据库动作；本地健康测试仅用临时容器数据。未 push、未发布镜像、未部署、未读真实 secrets、未登录真实账号、未调用收费模型、未启用巡检或自动动作。

本线完成 Manager/Worker 与假 CPA 契约验证。A 线真实 CPA v8 导入/保存/刷新及 BPS Executor 的跨线整合由 P9 接续；本线假 CPA 通过不等于真 CPA 全链通过。BPS 手工官方回调可兑换性、四入口真实登录与刷新、模型/SSE真实业务仍待另行用户授权验收。

下一责任人：P9 独立审查并整合 A/B 两线，复用同输入证据；不据此执行生产切换。

## 后续整改与跨线证据

B独审两项整改产品为 `07572357832b69a30e3ee6c45f9f0cc2c3350e0c`，已补受影响专项与构建。真实CPA85de32f/Manager0757235/Worker42b04ac预集成已通过；A后续SSE整改的最终镜像仍待接续。详情见同目录 `REVIEW-AND-CROSSLINE.zh-CN.md`，不把预集成写成最终A验收。
