# 独审整改与真实跨线集成

日期：2026-11-10。B 原产品 `42b04ac71df347e9c22fa35bad484c437da52d4b`；本次整改产品 `07572357832b69a30e3ee6c45f9f0cc2c3350e0c`。A 最终产品 `5c90a92eef27f450c7080bc8735c88d873fc8915` 的 Linux 镜像构建与真实三容器跨线集成均退出 `0`。预集成及 `4bca838` 中间记录保留，最终结论与镜像身份见末节。

## B 两项整改

1. Manager 保留类型化上游 HTTP status 和白名单错误码。CPA 返回明确400验证拒绝时释放账号写锁并允许纠正回调；网络超时、5xx、409或未知错误保留提交状态与写锁，等待查询或取消，不盲重试。
2. 浏览器创建前生成 UUID `task_id`，贯穿 Manager/Worker；创建响应丢失仍按该 ID 查询与取消。Manager 同 ID 创建幂等；离页取消先于迟到 POST 时，内存取消记录阻止后续启动。密码/TOTP 清理与原先相同。

产品整改命令与退出证据：

- 固定 Go1.26.10，Manager目录 `go test -count=1 ./internal/service/oauthlogin ./internal/http/controller/oauthlogin`：exit0，`review-fixes-go.log`。新增400→纠正→成功、timeout保留锁、预知ID取消零导入、取消先到阻止迟到POST。
- 固定 Node22.23.4，CPAMP目录 `npm --workspace apps/web run test -- src/features/oauth/InternalLoginCards.test.tsx src/services/api/loginTasks.test.ts`：7/7、exit0，`review-fixes-web.log`。覆盖创建已接受但回执丢失→离页按已知ID取消、无成功事件与无重复提交。
- `npx eslint apps/web/src/features/oauth/InternalLoginCards.tsx apps/web/src/services/api/loginTasks.ts --max-warnings=0`：exit0，`review-fixes-lint.log`。
- `docker build -t mx-cpamp:integration --build-arg SOURCE_COMMIT=07572357832b69a30e3ee6c45f9f0cc2c3350e0c --build-arg VERSION=local-four-entry -f components/cpamp/Dockerfile.manager-server components/cpamp`：exit0，`review-fixes-cpamp-build.log`；前端输入确有变化，故补一次本次输入的 tsc/vite 构建和 Go 层。旧67项与 Worker7项不机械复跑。

## 真实三容器预集成

CPA 从 A worktree 固定85de32f源码构建：

```powershell
docker build -t mx-cpa:integration --build-arg COMMIT=85de32f9d327d968cce7ea9178d6f557a850a93e --build-arg VERSION=local-bps -f components/cpa/Dockerfile components/cpa
```

exit0，`cpa-linux-build.log`。上游 Dockerfile未提供revision标签，以完整镜像ID和上述构建提交绑定，不伪造标签。

根目录运行 `node docs/evidence/manager-login/crossline/run.mjs`：exit0，`crossline-preintegration.log`。当轮脚本已穿过真实 Manager →真实 Worker HTTP/Tasks →stdin/fd3合成授权 →真实 CPA v8 import →filestore/热注册，不使用假CPA响应。

| 对象 | 完整镜像ID | 产品输入 |
|---|---|---|
| CPA | `sha256:43ccae3ac3033b513131cf64f625cc6b9658c57a67a384c7fccd95d0afff64c3` | A85de32f，预集成 |
| Manager | `sha256:85a8f76852c08de220ba132490cbdf234b87f1bdeba91a6e0d9bfa905769a25c` | B0757235，revision一致 |
| Worker | `sha256:64168881ce990f58d24736d1f8488549457ed2c775a072c1d0572bd524b7926b` | B42b04ac，业务输入未变 |

当轮通过：Codex/BPS各一个假账号成功且独立通道；CPA账号文件type、refresh_token、login_method正确；热注册Codex18个模型、BPS1个模型；以本次Codex目录选出共享测试模型，BPS显式重复配置和公开列表都去重；v8列表来源及脱敏正确；Manager health、CPA模型目录健康、管理未认证401。

三个服务共享 `network none` 命名空间，未新增Docker网络、未访问外部上游。假服务密钥在随机命名的本地临时卷，登录密码/TOTP只在内存/pipe。完成后脚本清理其创建的容器与卷。当前脚本进一步包含“真实POST已接受但响应体丢弃→预知ID取消→无第三份账号”以及清理结果核验，留待最终 A 镜像以同脚本验证，不倒填到原预集成日志。

## 4bca838 中间镜像与跨线集成

A worktree `D:/MX-CPAMP-worktrees/runtime` 构建前 HEAD 为产品 `4bca838c3e65be79dc5743d417571b99f6f5f55e`；构建后 HEAD 为仅证据提交 `e9fb547f238a687e332ee1bedd81b937ab26c489`，`git diff 4bca838c3e65be79dc5743d417571b99f6f5f55e -- components/cpa` 为空。CPA 构建上下文保持相同产品输入。

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
docker build -t mx-cpa:integration --label org.opencontainers.image.revision=4bca838c3e65be79dc5743d417571b99f6f5f55e --build-arg COMMIT=4bca838c3e65be79dc5743d417571b99f6f5f55e --build-arg VERSION=local-bps -f components/cpa/Dockerfile components/cpa
```

退出码 `0`，日志 `cpa-linux-build-4bca838.log`。revision 标签通过构建 CLI 设置，CPA Dockerfile 未改。

在本线 `D:/MX-CPAMP-worktrees/login` 执行：

```powershell
docker image inspect mx-cpa:integration mx-cpamp:integration mx-oauth-login:candidate --format '{{index .RepoTags 0}} {{.Id}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
$env:PATH='C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64;'+$env:PATH
node docs/evidence/manager-login/crossline/run.mjs mx-cpa:integration mx-cpamp:integration mx-oauth-login:candidate
```

两条命令退出码均 `0`，分别见 `image-identities-4bca838.log`、`crossline-4bca838.log`。

| 镜像 | 实际完整镜像ID | 实际 revision |
|---|---|---|
| `mx-cpa:integration` | `sha256:777d933d7ea6f65d722acb4427147d5e7c0562d5bf30ff82779e3f4b55509623` | `4bca838c3e65be79dc5743d417571b99f6f5f55e` |
| `mx-cpamp:integration` | `sha256:85a8f76852c08de220ba132490cbdf234b87f1bdeba91a6e0d9bfa905769a25c` | `07572357832b69a30e3ee6c45f9f0cc2c3350e0c` |
| `mx-oauth-login:candidate` | `sha256:64168881ce990f58d24736d1f8488549457ed2c775a072c1d0572bd524b7926b` | `42b04ac71df347e9c22fa35bad484c437da52d4b` |

CPAMP/Worker 镜像 ID 与既有构建记录一致，产品代码、依赖和构建输入未变；本线相对 `07572357832b69a30e3ee6c45f9f0cc2c3350e0c` 的 `components/cpamp`、`components/oauth-login`、`deploy`、`docs/contracts/login-worker.md` diff 为空，复用原构建及对应专项证据。未重复旧 Web67 项或 Worker7 项。

本次脚本通过 Codex/BPS 两条真实 HTTP 导入链：两份账号文件分别持久化 refresh_token 与 login_method，Codex 注册18个模型、BPS注册1个模型；BPS配置重复模型与公开同名目录均去重；Manager health、CPA模型目录、两管理入口未认证401、浏览器回执与管理列表脱敏均通过。真实 POST 已接受且响应体丢失后，按预知 task_id 取消，CPA文件与注册账号仍仅两份，取消任务零导入。脚本末尾定向查询本次随机前缀容器及卷列表为空，`isolated_resources_cleanup=verified`。

三个服务仅共享 `network none` 的命名空间，登录上游使用合成授权进程。此结果覆盖本地隔离集成；真实 OAuth/2FA、BPS手工回调兑换、真实刷新及模型/SSE业务仍待另行授权验收。

本次无 SQL schema/migration 或线上数据库动作。本地假账号与配置仅在已清理的临时卷；未 push、发布镜像或部署。此镜像为中间产物，最终以如下 `5c90a92` 记录为准。

## 最终 5c90a92 镜像与跨线集成

A worktree `D:/MX-CPAMP-worktrees/runtime` 的 HEAD 为仅证据提交 `7f2b8069c8245c47a9f0c8038c58bc3672cab289`。构建前后 `components/cpa` 工作区均干净，`git diff 5c90a92eef27f450c7080bc8735c88d873fc8915 -- components/cpa` 均为空，构建输入等同固定产品提交。

在 A worktree 执行一次受影响 CPA 构建：

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
docker build -t mx-cpa:integration --label org.opencontainers.image.revision=5c90a92eef27f450c7080bc8735c88d873fc8915 --build-arg COMMIT=5c90a92eef27f450c7080bc8735c88d873fc8915 --build-arg VERSION=local-bps -f components/cpa/Dockerfile components/cpa
```

退出码 `0`，原始构建输出为 `cpa-linux-build-final.log`。revision 来自 CLI label，未修改 CPA Dockerfile。

在 B worktree `D:/MX-CPAMP-worktrees/login` 执行身份核证与一次同脚本集成：

```powershell
docker image inspect mx-cpa:integration mx-cpamp:integration mx-oauth-login:candidate --format '{{index .RepoTags 0}} {{.Id}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
$env:PATH='C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64;'+$env:PATH
node docs/evidence/manager-login/crossline/run.mjs mx-cpa:integration mx-cpamp:integration mx-oauth-login:candidate
```

两条命令退出码均 `0`；对应 `final-image-identities.log`、`crossline-final.log`。实际身份：

| 镜像 | 实际完整镜像ID | 实际 revision |
|---|---|---|
| `mx-cpa:integration` | `sha256:58d0177203d8f4e4968cebc60c59bdb0e236404a05c27656a38351dc1022ab93` | `5c90a92eef27f450c7080bc8735c88d873fc8915` |
| `mx-cpamp:integration` | `sha256:85a8f76852c08de220ba132490cbdf234b87f1bdeba91a6e0d9bfa905769a25c` | `07572357832b69a30e3ee6c45f9f0cc2c3350e0c` |
| `mx-oauth-login:candidate` | `sha256:64168881ce990f58d24736d1f8488549457ed2c775a072c1d0572bd524b7926b` | `42b04ac71df347e9c22fa35bad484c437da52d4b` |

CPAMP/Worker 的产品、依赖与构建输入未变，inspect 的完整镜像ID及revision与原回执一致；本轮仅新增证据文档，复用原镜像、构建和对应专项，不重复 Web67 项、Worker7 项或其他包。

最终跨线结果：Codex/BPS各一份账号持久化正确通道、refresh_token与login_method，分别热注册18/1个模型；BPS重复配置及公开同名模型去重通过；Manager health、CPA目录、两管理入口未认证401与回执/列表脱敏通过。真实POST被接受但响应体丢失后，按预知task_id成功取消，CPA账号文件仍仅两份，取消任务零导入。脚本定向查询本次随机前缀容器与卷为空，`isolated_resources_cleanup=verified`。

本次为断网容器中的真实 Manager→Worker→CPA 本地集成，登录上游为合成授权进程，未执行真实登录、模型请求、线上数据库动作、镜像发布、push或生产部署。无SQL schema/migration变化。真实OAuth/2FA、BPS手工回调兑换、真实刷新及模型/SSE业务仍待另行授权验收。

下一责任人：P9核验独审并整合最终证据，维护根文档。
