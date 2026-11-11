# BPS 工具失败白名单诊断

## 输入与结论

- 产品基线：`2f51beb7c8f9ed7ecb3c91562fe94824192c6990`。
- 诊断候选：`46aa62cd366ec06889ab3817ae78142bb55924d1`。
- 本目录所有请求与回包均为合成数据，测试上游由进程内 RoundTripper 提供。
- 真实工具首轮的既有 `502 / internal_server_error / server_error / unknown` 回执缺少分支信息，当前证据仍不足以确定其原因。
- 基线离线矩阵证明，多种可达失败会被公开错误压成相同分类；本次只增加四个诊断字段。

## 公开契约

`error.diagnostics` 固定包含 `stage`、`code`、`upstream_http_status`、`inband_status`。前两项使用固定枚举，未知数值为 `null`。实际 HTTP 状态取自 HTTP 响应；带内状态只取桥接层可确定的语义状态。

实际 HTTP 200、带内 403 的普通响应仍返回公开 HTTP 502，同时记录 `upstream_http_status=200`、`inband_status=403`。已经开始输出的 SSE 保持公开 HTTP 200，通过终态错误携带同样字段。上游 HTTP 429、503 仍返回相同公开状态与原有错误代码/类型。

桥接层规范化的通用 `basispoints_upstream_error` 502 可能来自默认分类，缺少进一步来源证据时记录 `upstream_inband / unknown / inband_status=null`。这也可能隐藏显式 502，避免把推导值写成已知状态。

诊断不保留原错误文本、响应正文、URL、工具名、参数、令牌或身份值。固定 HTTP 错误文案仅包含已知状态数字。接受响应后的错误继续使用 `bpsAcceptedResponseError` 原有的 `StatusCode` 和 `IsStreamTruncated` 接口。

非 2xx 错误在 AuthManager 内保持原 `statusErr` 接口和固定文本；`PublicErrorBody()` 只在调度结束后的 `executionErrorMessage` 中用于呈现。此步骤位于已有 terminated、DirectResponse、Headers 处理之后；呈现包装通过 `Unwrap` 保留原错误。未引入调度侧可读取的 `ResponseBody` 接口。

HTTP 404 的外部 `model_not_found / invalid_request_error` 与诊断照常显示，内部 `LastError.Code` 保持原空值。无自定义规则时，原有 HTTP 404 冷却保持不变；精确匹配原固定文本的 `stop` 规则仍生效，不新增冷却。

## 验证

| 证据 | 输入 | 结果 |
| --- | --- | --- |
| `exact-handler-matrix.log` | 基线、原 `toolRequest()` 的合成请求、13 种回包 | exit 0；确认规范包装、直接工具、JSON 成功和多种失败分支；每场景一次模拟上游调用 |
| `diagnostic-contract-before.log` | 基线 + 新公开契约测试 | exit 1；32 组均准确捕获诊断字段缺失 |
| `diagnostic-contract-after.log` | `b8ad2cd` 产品代码 | exit 0；Responses/Chat × 普通/SSE × 8 个分支全部通过 |
| `bps-regression.log` | `b8ad2cd` 产品、测试及格式化输入 | exit 0；helps、executor、api 三包 `TestBPS` 专项，包含增强的未知状态 `null` 断言 |
| `node-diagnostics.log` | 候选 `diagnostics.mjs` 与 `verify-diagnostics.mjs` | exit 0；合法枚举、实际/带内状态分列、未知/越界反例及重复脱敏保留 |
| `http404-before.log` | `087eeeb` + 404 调度对照测试 | exit 1；捕获内部分类变化以及原文本规则失配 |
| `http404-after.log` | `46aa62c` 产品输入 | exit 0；四入口、有无原文本规则、旧/新错误路径，共 16 次模拟 dispatch，公开分类与内部状态对齐 |
| `diagnostic-contract-presentation.log` | `46aa62c` 产品输入 | exit 0；补跑受呈现边界影响的原 32 组公开协议契约 |
| `presentation-boundary.log` | `46aa62c` 产品输入 | exit 0；公开呈现、DirectResponse、terminal auth 优先级及 header/cause 保留 |

32 组公开协议契约验证经过真实 HTTP handler、AuthManager 与 BPS executor，逐项断言原公开 HTTP 状态、错误 `code/type`、四字段结构和每组一次模拟上游调用，并检查私密标记未进入响应。SSE 用合成 created/text delta 确保响应已提交后再触发失败。

测试工具链：Go 1.26.10 windows-amd64，`GOTOOLCHAIN=local`、已有 `GOMODCACHE`、`GOPROXY=off`；Node 使用本机 `D:/New Folder/node.exe`。

在 `components/cpa` 执行：

```powershell
& $go test ./internal/api -run '^TestBPSFailureDiagnosticsPublicProtocols$' -count=1 -v -timeout 90s
& $go test ./internal/runtime/executor/helps ./internal/runtime/executor ./internal/api -run '^TestBPS' -count=1 -timeout 90s
& $go test ./internal/api -run '^TestBPSHTTP404DiagnosticsKeepDispatch$' -count=1 -v -timeout 90s
& $go test ./sdk/api/handlers -run '^(TestPublicExecutionErrorPresentation|TestWriteErrorResponseDirectResponse|TestBuildErrorResponseBodyWithError_TerminalAuthEnforcesContractOnJSONInput)$' -count=1 -v -timeout 90s
```

在工作树根目录执行：

```powershell
& 'D:/New Folder/node.exe' docs/evidence/phase2-tool-image-20261111/verify-diagnostics.mjs
git diff --check
```

Go 修改已执行 gofmt。最终 CPA 生产编译由主线合入后统一 Docker 构建一次；本专项没有单独重复构建。

## 交付边界

无数据库迁移、数据清理、配置或依赖变更；未改上游请求、工具/图片语义、重试、回退、repair 或凭证调度。无真实登录、刷新、模型重试、推送或部署动作。真实工具可用性由后续单独授权的业务验证判断。
