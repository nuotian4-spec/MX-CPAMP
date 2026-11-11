# OAuth 回调诊断与 2FA 配置门控

- 基线：`33297e872b1974a19035fd81531eab80e8b22721`。
- 产品及测试提交：`aff9fe08b94158521e936ebf707bd57b517258b7`。
- 范围：Manager OAuth 登录 Service/Handler、Web 登录卡及其定向测试；CPA、Worker、数据库及部署配置未变。

## 已核结果

真实 Service/Handler 的离线 HTTP 回归确认：CPA 的九种白名单回调校验拒绝原先被映射为 502；现返回 400 和稳定白名单 `code`，响应不含上游正文、地址、授权码或密钥。未知 400、5xx、传输失败、成功响应解码失败均保持原有未知结果分类与提交锁。

错误体仅在 4096 字节以内、完整单个 JSON 解析成功且后续为 EOF 时采纳白名单码。部分解码、截断、坏尾巴、尾随对象和超长错误体保留未知结果；回归同时断言禁止重复转发授权码。

认证只读 `GET /api/oauth-login/capabilities` 仅返回 `password_login_configured`，只判断 Worker URL 和 secret 是否配置，不探活、不启动登录。Web 两处 2FA 表单仅严格 `true` 时开放；未知、加载、失败与实例切换时禁用输入、预览及提交。已有批次查询、结果、取消和手工 BPS 授权独立保留。回调输入旁区分官方回调页面与授权链接。

## 验证

执行目录：`D:/MX-CPAMP-worktrees/oauth-input-diagnostics/components/cpamp`。

固定 Go：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/gomodcache/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe`。

固定 Node：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`。

Go 环境：`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`GOMODCACHE=C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/gomodcache`；清空 `OAUTH_LOGIN_TEST_NODE` 和 `OAUTH_LOGIN_TEST_COMPONENT`，真实 Worker 集成测试按原约定跳过。

Web 依赖复用 `D:/MX-CPAMP-worktrees/phase2-login/components/cpamp/node_modules`，复用前直接逐字符比较当前和来源的 `package-lock.json`、`apps/web/package.json`，均相等；仅在本工作树建立 junction，未安装或修改依赖。

| 检查 | 输入与命令 | 真实退出码及结果 |
| --- | --- | --- |
| 首轮红测 | 基线产品加新增回归；在 `apps/manager-server` 执行 `go test ./internal/http/controller/oauthlogin -run 'TestCallbackValidationThroughServiceAndHandler\|TestPasswordLoginCapabilities' -count=1` | 1；九类明确拒绝均得到 502 而非 400；配置查询得到 404 |
| JSON 分类红测 | 首版分类修复加反例；`go test ./internal/http/controller/oauthlogin -run 'TestCallbackValidationThroughServiceAndHandler/(partial_decode_failure\|trailing_json_failure\|trailing_second_object\|truncated_json\|oversized_body)$' -count=1` | 1；部分解码、坏尾巴、尾随对象、超长体误判为 400 并释放锁，截断体维持未知 |
| 最终 Go 定向包 | 产品及测试提交；在 `apps/manager-server` 执行 `go test ./internal/service/oauthlogin ./internal/http/controller/oauthlogin -count=1` | 0；两包通过 |
| 最终 Web 定向测试 | 同提交；在 `apps/web` 执行 `node ../../node_modules/vitest/vitest.mjs run src/features/oauth/InternalLoginCards.test.tsx src/services/api/loginTasks.test.ts src/services/api/client.test.ts` | 0；3 文件、30 项通过 |
| Web 格式与 lint | 同提交；Prettier 和 ESLint 检查本轮 6 个变更 Web 文件，ESLint 带 `--report-unused-disable-directives` | 0 |
| 差异空白检查 | `git diff --cached --check` | 0 |

Web 测试涵盖 Axios 错误拦截器保留 `ApiError.status`、页面 400/502 差异提示、配置缺失/失败/格式未知/加载、旧实例迟到响应、切换当帧门控、返回原实例重新确认、已有批次查询取消，以及手工 BPS 独立入口。

## 验收边界

本轮仅使用合成材料和本机 `httptest`；未联网、未使用真实凭据、未运行真实 OAuth 或 Worker、未调用模型或刷新。未执行产品构建、push 或部署，最终 Manager 构建由主会话统一安排。

用户本次具体回调内容未采集；离线缺陷验证不等同于确认本次用户失败源于 CPA 400。真实账号登录、保存注册及浏览器手工验收仍待后续明确授权与实证。

下一责任人：独立审查席核定本提交；主会话统一构建与交付。
