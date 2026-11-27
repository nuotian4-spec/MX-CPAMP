# SIWC Manager 桥接专项验收

日期：2026-11-27。工作树：`cpa-core-v817-20261127/MX-CPAMP`。基础 HEAD：`b9ea2dc4c6bb377d31143970a6df1084297e4b09`。

Manager 专用桥接已实现；本地专项 **16 根、31 叶子通过，exit=0**。独立审查席已实读代码与原始证据，后端段通过。最终 CPA 与最终 Manager 的禁网合成链路 **9 项通过，exit=0**。

## 产品边界

- `/api/siwc/credential`：复用现有连接引用，精确核对凭证 id/index/provider；仅回传 SIWC 摘要。
- `/api/siwc/bind`：只对本地文件 Codex OAuth 开放；Home、运行时和非文件来源只读；链接生成返回 `wait`。
- `/api/siwc/callback`：完整回调地址原样转发，保留签发的 `client_id` 和 `error`；采用 CPA 的固定 loopback 回调地址。明确输入拒绝允许修正，不确定回执只查询状态。
- `/api/siwc/status`：只查询登记的同一 state；上游完成后重新读取精确目标凭证摘要。
- `/api/siwc/session`：取消成功阻断迟到回调；取消返回 false 时回查同一 state，保留 wait/error/ok 三种结果。
- 五个接口均经过管理员认证和 `Cache-Control: no-store`，不扩展通用 v8 管理代理。
- 会话与生成时的 CPA 连接、凭证身份绑定，进程内有效期 5 分钟。连接切换拒绝转发旧 state；上游会话丢失返回 `410 / siwc_session_expired` 并允许重建。

## 文件范围

`components/cpamp/apps/manager-server/`：

- `internal/model/siwc.go`
- `internal/service/siwc/service.go`
- `internal/service/siwc/service_test.go`
- `internal/http/controller/siwc/handler.go`
- `internal/http/router/siwc_test.go`
- `internal/http/router/router.go`
- `internal/app/context.go`

## 验证证据

命令：

```text
go test -json -count=1 ./internal/service/siwc ./internal/http/router -run ^TestSIWC
```

使用本机 Go 1.26.10，`GOPROXY=off`、`GOTOOLCHAIN=local`。真实 Manager Router 调用本地合成 CPA HTTP 服务，管理员数据仅保存在测试内存 SQLite。未改变产品数据库 schema。

| 验证面 | 结果 |
| --- | --- |
| 无管理员、普通 client key、CPA management key 拒绝 | PASS，未触达上游 |
| 精确身份、非法连接引用、非 Codex、Home/file、runtime/non-file | PASS |
| 重复及并发 bind | PASS，上游仅一个 bind |
| issued client_id 原样传递、error-only 回调 | PASS |
| 错 state、重复参数、错误地址、重复及并发 callback | PASS |
| wait 保持未完成、ok 回读凭证 | PASS |
| cancel true 与迟到 callback | PASS |
| cancel false + wait/error/ok，旧 bound 不冒充完成 | PASS |
| 老内核不支持、CPA 重启会话失效、允许重新 bind | PASS |
| 回调明确输入拒绝允许修正；5xx 后拒绝重复提交 | PASS |
| 会话 TTL、连接切换不转发旧 state | PASS |
| 窄投影、日志秘密隔离、方法/输入大小/路由限制 | PASS |

原始证据：

- [执行命令](command.txt)
- [逐项 JSONL 输出](siwc-manager-tests.jsonl)
- [退出码](exit-code.txt)
- [根数与叶子数](summary.json)

`git diff --check` 对 Context/Router 接线通过。未重跑已通过的 CPA 专项；Web 与嵌入式 Manager 产物由 Web 席统一构建。

## 最终二进制跨产物验收

时间：2026-11-27 07:41:06 UTC。复用以下已有产物，未重新构建：

- CPA：`C:/Users/子羽/.codex/artifacts/cpa-core-v817-20261127/CLIProxyAPI-final`，CPA tree `8501bb9d2095cb9e7bc71b6bc008aa097b6670cc`。
- Manager：`C:/Users/子羽/.codex/artifacts/cpa-core-v817-20261127/cpa-manager-plus-siwc-final`，Web 构建席给出的 CPAMP tree `ba548720f8d46f16835b8d5bed5b2d99aeaf316a`。

使用已有 `python:3.12-bookworm` 镜像，`--pull never --network none`；两个服务仅经同容器 loopback 通信。CPA 使用 `-local-model`，合成 Codex 凭证有效期设为 2099 年，Manager 使用临时空数据目录。没有打开授权链接，回调仅携带 `state/error/error_description`，没有提交授权码、签发客户端或 token 交换。

实际结果：

1. CPA 从隔离文件加载唯一合成凭证。
2. Manager 读取摘要返回 200/no-store，保留精确 id/index，支持绑定且初始未绑定。
3. Manager 调用真实 CPA bind 后返回 wait，state 与真实生成链接一致。
4. 完整 error-only 回调由 Manager 成功转入 CPA，提交回执保持 wait。
5. 同 state 最终返回 `error / siwc_session_failed`，没有伪造完成摘要。
6. 合成凭证原始字节保持不变。
7. 两个启动进程均已停止，临时合成状态已清理，Docker 容器自动移除。

九项断言及运行回执见 [结果](real-chain-result.json)、[Docker 命令](real-chain-command.json)、[退出码](real-chain-exit-code.txt)、[CPA 日志](real-chain-cpa.log)、[Manager 日志](real-chain-manager.log)。运行脚本为 [PowerShell 入口](run-real-chain.ps1) 与 [容器内验收脚本](run-real-chain.py)。

CPA 启动时两个公共版本刷新尝试被 `network=none` 拦截，日志记录 `network is unreachable`；它们不属于 SIWC token 交换。跨产物验收没有外网可达性，没有真实 OAuth、模型或额度调用。

## 验收范围

此处证明 Manager 路由、状态机和真实二进制的 CPA 请求合同在合成环境通过；成功 token 交换分支复用同版 CPA 已验专项，真实 OpenAI SIWC 授权及生产部署另行验收。未调用真实 OAuth、模型或账号接口。

独立审查：`/root/cpa_v817_review` 已核对 Home 只读、会话 404 分类、输入拒绝可修正、失效会话释放和原始命令日志，结论为后端段通过。审查未重复执行专项。

下一责任人：独立审查席核查最终跨产物回执；P9 负责总体验收口径。
