# Kilo 公开错误执行回执

日期：2026-11-17。执行者：A（本轮唯一产品代码写入者）。
基线：`67445a35ba450468948447492b560c204afa7321`。
工作树：`C:/Users/子羽/.codex/worktrees/kilo-public-errors-20261117/MX-CPAMP`。

## 结论

本轮公开错误呈现及新增边界的定向证据齐备，产品源码冻结。Windows amd64及Linux amd64各一次编译成功。首次宽匹配专项仍保留原始RC1，不宣称整套历史测试全绿；custom工具续接10个叶子在原基线同样失败，独立列为已知功能缺口。

没有数据库动作；没有提交、推送、部署、服务重启、真实账号或模型请求。未修改BPS、更新器、插件管理、UI、账号身份/代理分配或重试恢复策略。

## 输入与证据复用

`frozen-inputs.json`登记18个CPA路径：13修改、5新增，含逐项字节数和SHA256。S/V使用该清单直接核对输入是否变化；不变则复用本回执对应证据。未把父代理维护的GLOSSARY/方案或S/D材料计入A代码清单。

首轮helpers、handlers、openai、auth包均通过，cliproxy包4个顶层失败。之后没有重跑全套；只运行失败修正、缺失真实出口、变化的request_scoped分类分支及新增不透明SSE用例。最终helper仅把request_scoped的invalid_request限制在4xx，新增单元涵盖400/409/501/503；501/503实际出口复验，未变的上游分类分支复用首轮证据。

## 原始失败与处置

| 失败 | 实际事实 | 本轮处置/最终证据 |
| --- | --- | --- |
| ToolImageContinuations custom_tool_call 8叶子 | 续接HTTP409，原内部cause为 `Kilo continuation no longer matches its original account or session` | S固定原提交overlay同8项失败；保持成功断言，不扩大修复 |
| LongCallID custom/chat 2叶子 | 同409，随后等待第二条结果失败；Active为0，不能据此判资源泄漏 | S固定原提交overlay同2项失败；保持成功断言 |
| StartedStreamErrors 6叶子 | Windows测试名含冒号造成凭证落盘失败，POST0、503，未触达上游 | 改测试case名称，不改产品持久逻辑；`incremental.log`六叶子通过 |
| BusyAndCancellation 1叶子 | 公开实际为request_preparation_busy，旧断言仍期待kilo_image_preparation_busy | 只更新公开断言；增量503、取消释放、POST0及随后成功通过 |
| 增量admission 2叶子 | 本轮presenter把request_scoped 501/503误分invalid_request | 限定4xx才映射invalid_request；`admission.log`helper和无出口503/compact501通过 |

首轮4顶层含17个失败叶子（8+2+6+1）；日志共有20个FAIL行，因为Busy顶层本身即叶子。增量原RC1为1顶层、2叶子。基线对照以 `../S/BASELINE-CUSTOM-TOOL-CONTROL-20261117.zh-CN.md` 及 `../S/baseline-overlay/` 原始材料为准，A未重复运行。

## 必要出口证据

- HTTP/首帧前：Responses与chat、stream真假组合覆盖真实429/402/403/401和精确quota/auth/restricted机器事实；内部状态及限制触发保持原规则。
- 已开始流：原6项分类/单次POST与释放通过；用户完整第三方SSE又做Responses/chat两项（`opaque-stream.log`），未知保持request_failed、一个错误对象、无completed、POST1；Responses序号2对应前两帧，不是重试次数。
- 本地413/503：保留内部kilo机器码、状态、作用域、NoReplay与预算数字；公开码去品牌。Busy容量释放和取消路径通过。
- 连接/协议：真实本地TLS+CONNECT fixture覆盖断开、坏JSON、坏encoding、无终态、已开始坏帧；错误头无透传、单次POST、无假完成。
- 无账号503：`incremental.log`；无可用出口503和有效模型compact501：`admission.log`；均公开request_failed、POST0/CONNECT0并含request_id。
- WebSocket：仍升级前501，实际拒绝正文unsupported_transport且不含品牌、无Upgrade；`incremental.log`，未增加WS支持。
- Alpha：9场景含执行失败/结果缺失/中途失败/参数规则冲突；原status、POST、结果数、completed_queries保留并增加清理与request_id断言；`incremental.log`。
- 关联：真实普通HTTP请求与已开始流均按公开request_id匹配一条新受控诊断；保留真实status/upstream_status/stream_started，仅允许机器字段，不将未知动态码、凭据或提示词写入新诊断。
- 非Kilo：首轮handlers合成Codex/BPS/空scope对照保持旧输出，成功模型正文未作全局替换。

## 实际命令

工作目录统一为上列工作树的 `components/cpa`；Go完整路径为 `C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe`；测试环境为GOTOOLCHAIN=local、GOPROXY=off、GOSUMDB=off、CGO_ENABLED=0。

### 首轮专项

`TARGETED_RC=1`；原始输出 `targeted.log`，退出码 `targeted-result.txt`。

```text
go test -count=1 -v -timeout=180s -run '^Test(KiloPublic|KiloM7|KiloPayloadBudgetBusyAndRuleBoundary|KiloPayloadHTTP|PublicExecutionErrorPresentation|KiloScopeRejectsWebsocketBeforeUpgrade|KiloM6ManualFenceAndRestrictedPrecedence)' ./internal/runtime/executor/helps ./sdk/api/handlers ./sdk/api/handlers/openai ./sdk/cliproxy ./sdk/cliproxy/auth
```

### 增量出口

`INCREMENTAL_RC=1`；原始输出 `incremental.log`，退出码 `incremental-result.txt`。

```text
go test -count=1 -v -timeout=120s -run '^(TestKiloM7StartedStreamErrors|TestKiloPayloadHTTPBusyAndCancellationBeforeAdmission|TestKiloPublicNativeStartedRequestIDCorrelation|TestKiloPublicNativeAdmissionAndCompact|TestKiloSearchA2NativeContractScenarios|TestKiloScopeRejectsWebsocketBeforeUpgrade)$|^TestKiloM7ErrorShapes$/^third-party-opaque' ./sdk/cliproxy ./sdk/api/handlers/openai
```

### 受影响分类分支

`ADMISSION_RC=0`；原始输出 `admission.log`，退出码 `admission-result.txt`。

```text
go test -count=1 -v -timeout=90s -run '^TestKiloPublicRequestScopePreservesServerFailure$|^TestKiloPublicNativeAdmissionAndCompact$/(unusable-egress|compact)$' ./internal/runtime/executor/helps ./sdk/cliproxy
```

### 完整不透明SSE已开始流

`OPAQUE_STREAM_RC=0`；原始输出 `opaque-stream.log`，退出码 `opaque-stream-result.txt`。

```text
go test -count=1 -v -timeout=60s -run '^TestKiloM7StartedStreamErrors$/^(responses|chat)$/^(case-3|completions)$/^case-3$' ./sdk/cliproxy
```

### Windows编译

`NATIVE_BUILD_RC=0`；原始输出 `native-build.log`，退出码 `native-build-result.txt`。

```text
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o C:\Users\子羽\AppData\Local\Temp\mx-kilo-public-errors-build-20261117-054341/cli-proxy-api.exe ./cmd/server
```

### Linux编译

`LINUX_BUILD_RC=0`；原始输出 `linux-build.log`，退出码 `linux-build-result.txt`。

```text
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o C:\Users\子羽\AppData\Local\Temp\mx-kilo-public-errors-build-20261117-054341/CLIProxyAPI-linux-amd64 ./cmd/server
```

## 冻结路径

- `components/cpa/internal/api/server_kilo_alpha_search.go`（modified）
- `components/cpa/internal/runtime/executor/helps/kilo_payload.go`（modified）
- `components/cpa/internal/runtime/executor/helps/kilo_public_error.go`（added）
- `components/cpa/internal/runtime/executor/helps/kilo_public_error_test.go`（added）
- `components/cpa/internal/runtime/executor/helps/kilo_response.go`（modified）
- `components/cpa/internal/runtime/executor/kilo_executor.go`（modified）
- `components/cpa/sdk/api/handlers/handlers.go`（modified）
- `components/cpa/sdk/api/handlers/handlers_execution.go`（modified）
- `components/cpa/sdk/api/handlers/handlers_stream.go`（modified）
- `components/cpa/sdk/api/handlers/kilo_public_error.go`（added）
- `components/cpa/sdk/api/handlers/kilo_public_error_test.go`（added）
- `components/cpa/sdk/api/handlers/openai/kilo_scope_test.go`（modified）
- `components/cpa/sdk/api/handlers/openai/openai_responses_websocket.go`（modified）
- `components/cpa/sdk/cliproxy/kilo_native_a2_contract_test.go`（modified）
- `components/cpa/sdk/cliproxy/kilo_native_fidelity_test.go`（modified）
- `components/cpa/sdk/cliproxy/kilo_native_payload_test.go`（modified）
- `components/cpa/sdk/cliproxy/kilo_native_public_error_test.go`（added）
- `components/cpa/sdk/cliproxy/kilo_payload_supplement_test.go`（modified）

## 编译与静态检查

两编译原始命令/结果见上；产物仅保存于各result.txt所列Temp路径，没有执行或上传。`diff-check-result.txt`为DIFF_CHECK_RC=0；改动Go文件已gofmt。产物编译通过不等于生产业务验收。

下一责任人：S核最终差异及输入绑定并给独立结论；P9综合A/S/D作V验收。已知custom工具续接基线缺口另行排期。
