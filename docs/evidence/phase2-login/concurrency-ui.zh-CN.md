# 凭证并发管理与构建专项

代码输入：`220a1d569f5b9eca060f71f9f29aeeb8f82873ef`加本提交的Manager并发BFF、页面、导航与测试。工作树 `D:/MX-CPAMP-worktrees/phase2-login`。日期：2026-11-11。

窄接口为 `GET/PATCH /api/credential-concurrency`，Admin鉴权、禁缓存。PATCH只接收`auth_id`与`concurrency_limit`（正整数1至2147483647或null）。OAuth按auth_id调用fields；配置Key按auth_index精确找到keys节点，携带ETag/If-Match条件写入，仅改变该节点的concurrency-limit。配置内容和密钥不返浏览器；映射歧义、版本冲突与连接变更停止写入。Home、runtime/plugin/keyless均只读。

页面入口：侧栏“凭证并发”、路由`/credential-concurrency`。公共池等待单独展示；每份凭证显示active/effective_limit/pinned_waiting与独立上限。空值恢复默认，降低上限不终止现有请求。

## 验证

固定Node：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64`。
固定Go：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/gomodcache/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin`，`GOTOOLCHAIN=local`。

| cwd | 命令 | 回执 |
|---|---|---|
| components/cpamp/apps/manager-server | `go test ./internal/service/credentialconcurrency ./internal/http/controller/credentialconcurrency ./internal/http/router` | exit0；两个专项包通过，router编译通过 |
| components/cpamp/apps/web | `node ../../node_modules/vitest/vitest.mjs run src/features/accounts/CredentialConcurrencyPage.test.tsx` | exit0，4项通过 |
| components/cpamp/apps/manager-server | `go build -o "$env:TEMP/mx-phase2-manager.exe" ./cmd/cpa-manager-plus` | exit0；Manager编译通过 |
| components/cpamp/apps/web | `npm.cmd run build`（固定Node置于PATH） | exit0，含tsc和Vite；dist/index.html 6,505,905字节 |
| components/cpamp/apps/manager-server | `go test ./internal/service/oauthlogin ./internal/http/controller/oauthlogin` | exit0；新增导入中取消仍保存成功结果、批次Admin鉴权和1MiB边界测试后通过 |

已有批次12项页面专项复用；之后该测试文件仅补ReactNode属性的类型收窄，不改变测试或产品行为。BPS生成/打开/复制授权链接、回调ACK不等同saved、额外交互、离页不取消及活动批次续查均在该专项中。

工具stdout逐字摘录转存于`validation-tool-output.txt`，不是重新执行生成的日志。Manager构建仅验证程序编译，内嵌页面仍使用仓库已有输入；Web单独完成候选构建，最终镜像整合由主线绑定最终提交完成。本线无真实登录、模型调用、推送、生产修改或SQL/schema迁移。
