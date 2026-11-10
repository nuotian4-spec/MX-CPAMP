# BPS 请求能力错误诊断与修复

日期：2026-11-11。固定 CPA 基线：`95dd9ca2c16d5469c43ce7c910f38ff271af45e3`。修复产品提交：`2ed27c7702e4a58cffc28e8a668273b1267ebffc`。

## 已证事实

原请求为 `POST /v1/responses`，内容是 `{"model":"gpt-5.6-sol","input":"Reply with OK.","max_output_tokens":128,"reasoning":{"effort":"low"},"stream":false}`。模型目录仅注册 BPS 的 `gpt-6-astra`、`gpt-6.1-sol`、`gpt-5.6-sol`，均采用产品目录的 `UserDefined:true` 语义。

固定基线的真实公开处理器在上述目录和请求下返回 HTTP 500，错误消息为 `provider_not_found: no provider supplied`，替身 RoundTripper 调用数为 0。将请求模型分别替换为目录中另两个模型，结果相同。仅移除 `max_output_tokens` 的离线对照全部返回 200、`completed` 和文本 `OK`，替身调用数为 1；最终对外内容由真实 BPS executor、协议处理和翻译器生成。

确定性路径：

1. `sdk/cliproxy/executor/bps_capability.go:60` 将非 null 的 `max_output_tokens` 判为不支持。
2. `sdk/cliproxy/auth/bps_capability.go` 过滤 BPS；仅 BPS 的 provider 列表因此变空。
3. 基线 `sdk/cliproxy/auth/conductor_execution.go:176` 返回未带 HTTP 状态的 `provider_not_found`。
4. `sdk/api/handlers/handlers_execution.go:376` 默认映射成 500。

因此在这个精确本地输入下，BPS executor、thinking、translator、HTTP 传输均尚未执行，密码、TOTP、令牌刷新、代理出口或上游模型返回并非复现该失败所需条件。

## 修复行为

仅当 BPS 能力过滤使原本非空的 provider 列表变空时，返回既有 `request_scoped` 400，消息明确为 `BPS does not support max_output_tokens`。Execute、ExecuteCount、ExecuteStream 三入口一致。公开响应沿用 `invalid_request_error`。

保留现有 BPS 能力限制；没有支持、删除或忽略 `max_output_tokens`。混合池中的兼容 Codex 候选继续执行；真实空 provider 列表和剩余兼容通道无凭证时仍保留原有可用性错误，不统一改成 400。

## 验证

所有容器均使用 `--network none`。测试只使用合成访问材料，未加载真实账号、未访问生产、未作真实模型重试。构建环境镜像为 `mx-cpa-buildenv:phase2-local-20261111`，ID 为 `sha256:c2c0afa322d878c8f1070c76e06047b50b045507b2f7184cb1690527136600d7`。

| 证据 | 命令 | 结果 |
| --- | --- | --- |
| `exact-public-request-final.log` | 在原构建环境仅挂载 `bps_model_diagnostic_test.go` 到 `/app/internal/api/zz_bps_model_diagnostic_test.go`；`go test ./internal/api -run '^TestPhase2BPSExactPublicRequestDiagnostic$' -count=1 -v -timeout 90s` | exit 0；三个模型分别验证原输入 500/调用 0 与离线对照 200/调用 1，共 6 项 |
| `fixed-targeted-tests.log` | 将修复后的 `components/cpa` 只读挂载到 `/app`；`go test ./sdk/cliproxy/auth ./internal/api -run '^(TestBPS\|TestManagementV8IndependentContract)' -count=1 -v -timeout 90s` | exit 0；三模型公开处理器 6 项、三入口乘四种候选条件 12 项及既有 BPS 专项通过 |
| `fixed-build.log` | 同一修复源码只读挂载；`go build -buildvcs=false -o /tmp/CLIProxyAPI ./cmd/server` | exit 0 |
| Git 工具回执 | `git diff --check 95dd9ca2c16d5469c43ce7c910f38ff271af45e3..2ed27c7702e4a58cffc28e8a668273b1267ebffc` | exit 0 |

`exact-public-request.log` 保留首次离线夹具运行的 exit 1 记录；最终证据为 `exact-public-request-final.log`。两个文件均未作真实联网。最终夹具通过 Manager 的既有 RoundTripperProvider 注入替身，覆盖公开 handler 创建的执行上下文。

## 未知与边界

真实那次只保存 HTTP 500，没有保存错误正文或类别，历史单次错误码仍未知；本次证明的是同版本、同模型目录、同请求体下确定可达的本地失败路径。去字段成功仅来自合成上游，未证明真实模型推理成功，也不增加真实调用预算。

无 SQL/schema 迁移，无数据库写入，无 Manager/Worker 改动，无生产部署或推送。下一责任人：独立审查席核产品提交，主会话统一整合并仅重建 CPA。
