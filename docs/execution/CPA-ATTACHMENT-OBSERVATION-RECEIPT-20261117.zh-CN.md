# CPA 附件与公共调度观测候选

日期：2026-11-17。状态：本地观测候选完成；专项、独立静态审查及服务器构建通过。用户已批准本轮 20 路径入仓与固定版本发布；生产部署未授权。

## 范围与基线

- HEAD：e9196e142dd685eb8534a6f851b3564d70700b12。
- 工作树：C:/Users/子羽/.codex/worktrees/kilo-latency-observation-20261117/MX-CPAMP。
- 仅补本地 Debug 可观测边界；不修改调度、并发限额、上传并行度、超时、payload、插件 ABI、数据库或计费。
- 产品输入为 9 个已跟踪 Go 文件与 2 个新产品 Go 文件；新增 6 个测试文件。已跟踪产品 diff 为 58 行新增、8 行删除；imports 分组已落地。
- 原有 KILO-LATENCY-PHASE1-RECEIPT-20261117.zh-CN.md 与 KILO-LATENCY-RELEASE-INPUTS-20261117.json 的修改保留，排除本轮产品清单。
- 用户当前确认生产 cpa-bps“启用代理池”关闭；来源为用户确认，非独立读取生产，不列为本轮优先排队嫌疑。

## 责任与结论

| 环节 | 责任人 | 范围 | 状态 |
| --- | --- | --- | --- |
| 产品代码与专项测试 | central_admission_audit | 唯一产品写入者；接线、阶段语义、流式生命周期及脱敏 | 通过 |
| 独立审查 | attachment_path_audit | 限定 diff 及直接依赖；合同、流式行为、默认开销和数据边界 | 通过，无剩余阻塞项 |
| 编译与收口 | 主会话 | 唯一服务器构建、diff 范围与文档核验 | 通过，本地候选完成 |

## 准确输入清单

下列路径以 components/cpa/ 为根，共 11 个产品文件：

1. internal/pluginhost/adapters_executors.go
2. internal/pluginhost/rpc_client.go
3. internal/requestlatency/latency.go（新增）
4. internal/runtime/executor/bps_executor.go
5. internal/runtime/executor/helps/bps_images.go
6. sdk/api/handlers/handlers.go
7. sdk/api/handlers/handlers_execution.go
8. sdk/api/handlers/handlers_stream.go
9. sdk/api/handlers/kilo_latency.go
10. sdk/api/handlers/request_latency.go（新增）
11. sdk/cliproxy/auth/credential_concurrency.go

新增 6 个测试文件：

1. internal/pluginhost/request_latency_test.go
2. internal/requestlatency/latency_test.go
3. internal/runtime/executor/helps/request_latency_test.go
4. internal/runtime/executor/request_latency_test.go
5. sdk/api/handlers/request_latency_test.go
6. sdk/cliproxy/auth/request_latency_test.go

## 权威验证证据

命令工作目录为 components/cpa；使用 Windows Go 1.26.10 绝对 runtime，环境 GOTOOLCHAIN=local、GOPROXY=off、GOSUMDB=off。本节引用执行者和主会话已有真实回执，文档收口不复跑。

~~~powershell
$env:GOTOOLCHAIN='local'; $env:GOPROXY='off'; $env:GOSUMDB='off'
$go='C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe'
& $go test ./internal/requestlatency ./sdk/api/handlers ./internal/pluginhost ./sdk/cliproxy/auth ./internal/runtime/executor ./internal/runtime/executor/helps -run 'TestRequestLatency|TestKiloLatencyApplicationLogVisibility|TestKiloLatencyWriterRequiresBodyAndSuccessfulFlush' -count=1 -timeout=90s
~~~

- 首组四包 PASS 耗时分别为 0.041 / 0.217 / 0.247 / 0.349 秒，RPC 实际接线首组通过。
- 最终通过范围：新增 11 个测试函数、23 个叶场景，以及直接受影响 Kilo 2 个函数、7 个叶场景。
- 定向命令仅覆盖受影响专项；没有产品改动导致复跑，其余已通过项未重复。

~~~powershell
& $go test ./internal/pluginhost ./internal/runtime/executor/helps -run 'TestRequestLatencyActualPluginStream|TestRequestLatencyActualBPSUpload' -count=1 -timeout=90s
~~~

- 定向结果：pluginhost 0.215 秒、helps 0.251 秒，exit 0；执行会话 32337。
- 主会话唯一实际服务器构建使用同一 runtime 与环境：

~~~powershell
& $go build -o NUL ./cmd/server
~~~

- 构建会话 72127，SERVER_BUILD_RC=0。
- 已有格式核验：tracked diff-check 返回 0，新增 8 个 Go/测试文件空白错误为 0。
- 独立静态审查见 CPA-ATTACHMENT-OBSERVATION-REVIEW-20261117.zh-CN.md；调查与后续拟议性能方案见 CPA-ATTACHMENT-QUEUE-INVESTIGATION-20261117.zh-CN.md。

## 最小使用说明与指标

- 候选合入、固定版本发布并部署后才可使用；本轮不改变任何线上开关。
- 现有 Debug 开启时，受覆盖的非 Kilo Chat/Responses 外部请求结束输出一条 CPA request stages 摘要；嵌套调用复用状态，不额外收口；原 Kilo 采样不恢复。
- 摘要仅含固定阶段、类别、耗时、计数、结果及既有请求关联号；不记录正文、文件名、凭证、URL、代理地址或原始错误文本。
- route 区分 direct_plugin 与 credential_executor；嵌套及 Home/local/mixed 单独标识。直接插件不等于经过当前层 AuthManager。
- selection_lock_wait 与 credential_wait 分开；内置 BPS 的准备/上传和闭源 cpa-bps 插件调用分开。
- execution_input_bytes 为执行入口 payload 长度，不是公网原始请求字节数；未确认时省略。
- server_total_ms 从观测上下文创建到 handler 收口；不是从公网接收开始的端到端耗时。
- host_first_output_ms 是宿主完整成功 Write 中的可识别输出，不代表成功 Flush 或客户端收到。
- plugin_first_output 是宿主消费者/翻译边界识别到首个 text/tool，不是插件内部首网络字节；plugin_call_return 单独表示函数返回。
- 重叠或累计阶段不能简单求和；未知阶段省略，不记为 0。

## 已知边界与下一步

- 输出观察上限 1 MiB；超大 SSE 批次或未支持内容形态可能缺少首输出字段。
- 原生 Claude、Gemini、WebSocket、multipart 入口不在本轮覆盖内；闭源插件内部上传/排队及 Home 远端阶段未观测。
- HTTP bridge 没有上传/推理阶段合同；不根据 URL 或正文猜测，宿主调用耗时不等于插件上传耗时。
- 本地验收证明观测候选符合合同，不证明截图根因、生产阶段分布或延迟优化完成；本轮没有性能行为修复。
- 下一责任人：发布执行者按本轮授权完成受控入仓与固定版本发布，主会话核验发布结果；生产部署另行确认。
