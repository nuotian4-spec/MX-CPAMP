# 独审整改与真实跨线预集成

日期：2026-11-10。B 原产品 `42b04ac71df347e9c22fa35bad484c437da52d4b`；本次整改产品 `07572357832b69a30e3ee6c45f9f0cc2c3350e0c`。A 预集成产品 `85de32f9d327d968cce7ea9178d6f557a850a93e`；A 后续 SSE 整改尚待最终镜像核验，不将此预集成认作最终 A 验收。

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

## 待接续

等 A SSE 修复最终提交后，按真实完整产品 SHA 构建 CPA 镜像，只重跑同一跨线脚本并记录完整镜像ID；CPAMP/Worker输入未变则直接复用。不运行真实登录、收费模型、生产部署或线上数据库动作。

本次无 SQL schema/migration 变化。本地假账号文件与配置修改仅在本次临时卷。下一责任人：B执行最终跨线脚本，P9核验独审与整合；根文档由P9维护。
