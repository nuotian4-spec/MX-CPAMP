# Kilo 延迟观测第一阶段执行回执

日期：2026-11-17。状态：本地观测候选与发布接线审查通过，发布准备完成，用户已批准入仓与固定版本发布；P8实现与分层专项、独立只读审查、P9集成与构建验收均已完成，产品与测试文件保持冻结。

- 工作树：`C:/Users/子羽/.codex/worktrees/kilo-latency-observation-20261117/MX-CPAMP`。
- 分支：`codex/kilo-latency-observation-20261117`。
- HEAD：`f9ef5c16a027b7e9a7c01e85c4c65e079018f691`，本轮改动为未提交本地候选。
- 数据库动作：无。未提交、推送、发布、部署、重启、访问生产或调用真实模型。
- 当前责任人：P9，按用户授权将28路径提交推送main，并通过既有Actions发布`v2026.11.17-rc.2`。本回执为发布前冻结输入；实际发布结果以GitHub固定标签和工作流为准。下一责任人：P9，发布核验后提供生产维护入口；生产维护另行确认，实际生产采样及性能收益仍未验收。

## 实现范围

| 层 | 文件 | 实际行为 |
| --- | --- | --- |
| 共享状态 | `components/cpa/internal/kilolatency/{latency.go,content.go}` | context共享单调时间点、累计等待与计数；缺失挂点不输出假0；最终一次汇总 |
| HTTP入口与交付 | `sdk/api/handlers/kilo_latency.go`、`handlers.go`、`openai/openai_handlers.go`、`openai/openai_responses_handlers.go` | Responses/Chat入口创建状态；读取实际写出的完整SSE正文；成功Flush后记录首正文；在handler返回时结束 |
| 准入 | `sdk/cliproxy/auth/{credential_concurrency.go,kilo_binding.go}` | 分离会话gate与容量等待；测量选择mutex等待、选择次数和首次入队负载；不改选择、唤醒或额度 |
| 绑定 | `internal/kilobinding/store.go` | 仅测量现有锁等待、read、write/Sync/原子替换区间；原有持久化与公开顺序保持 |
| 上游与用量 | `internal/runtime/executor/kilo_executor.go`、`helps/{kilo_latency.go,usage_helpers.go}` | KiloDo前记录出站、返回记录响应头；正文识别先于ObserveKiloBinding；在既有Publish边界冻结TTFT可用性 |
| 私有队列事件 | `internal/redisqueue/{plugin.go,kilo_latency.go}` | Kilo未知TTFT省略`ttft_ms`；已观测的亚毫秒值仍是可用的0ms；其他provider保留既有数值输出 |

表内除共享状态外的简写路径均以`components/cpa/`为根。导出SDK `usage.Record`、插件接口定义、cpamp及依赖声明均未修改；既有用量发布时间和次数保持。二进制插件实际加载兼容性不在本轮本地证据内。

## 指标与分类

- `t0`是Responses/Chat HTTP handler第一行，位于读取body之前；包含随后解析、绑定与准入，但不包含此前Gin中间件或Caddy耗时。
- `upstream_headers_ms`与`upstream_first_text_ms`都从KiloDo调用前起算，包含连接等待，不单独归为模型计算。
- `cpa_first_text_ms`只在实际写出的当前SSE正文被完整识别且Flush成功后存在；reasoning、工具参数、心跳、created、完成/错误终态均不冒充正文。非流式保留`mode=nonstream`，不伪造正文Flush首字。
- `first_text_forward_ms`覆盖正文识别到下游Flush的区间；`server_total_ms`在handler返回时结束。旧用量Latency没有被替换。
- `session_wait_ms`只在gate发生等待时产生；容量等待不重复算入该区间。`selection_attempts`含首次选择，重选次数可取其减1。
- `queue_waiting_at_entry`与`queue_active_at_entry`取管理器级首次入队负载，不是仅Kilo账号负载；每请求只计算一次，不为后续唤醒反复扫描。
- `binding_lock_wait_ms`、`binding_read_ms`、`binding_commit_ms`是局部累计区间，不能与端到端指标直接相加。
- 结果为`success_text`、`success_tool`、`success_empty`、`failed`、`canceled`、`rejected`。HTTP200心跳后出现429准入错误仍归`rejected`；503存储/执行故障归`failed`；正文前后取消单独分类；正常结束的执行context清理不误报取消。
- 只有原生Kilo路由已确认后启用摘要；尚未确认路由的鉴权/无效请求不构成本摘要样本。成功正文P95只使用`success_text`且相应指标存在的样本。
- 早期service-tier事件触发原有用量Publish时若尚未见正文，usage的TTFT保持缺失；后续正文仍进入最终诊断，但不回写旧用量、不追加第二次Publish。

## 日志启用与关联

使用既有配置与日志出口，不新增控制面或独立文件写入：

```yaml
observability:
  logs:
    debug: true
```

启用后在既有应用stdout或已配置的轮转日志中查收`Kilo request latency `前缀。同一条Debug消息带脱敏JSON快照；实际`logging.LogFormatter`可见性已由专项验证，结构字段也保留给既有hook。Debug关闭时不序列化JSON且不输出摘要，不能据此宣称已经采样。`request-log: true`不是此摘要的前提。

JSON的完整`request_id`复用入站关联ID，与既有usage的父请求`TraceID/trace_id`对应；usage的执行`RequestID`仍为各次执行ID。日志前缀可能只显示短ID，关联使用JSON内的完整值。不记录正文、token、原始客户端Key、代理凭据、会话内容或新增身份指纹。

## P8已执行证据

环境：Windows/amd64，本地缓存Go1.26.10；`GOPROXY=off`、`GOTOOLCHAIN=local`。可执行文件为：

```text
C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe
```

命令工作目录统一为本工作树`components/cpa`，日志根目录为：

```text
C:/Users/子羽/AppData/Local/Temp/kilo-latency-phase1-20261117/
```

| 精确命令 | 结果 | 输出文件 |
| --- | --- | --- |
| `go test ./internal/kilolatency ./sdk/api/handlers ./sdk/cliproxy/auth ./internal/kilobinding ./internal/runtime/executor/helps ./internal/redisqueue -run '^TestKiloLatency' -count=1 -v` | 六包PASS，退出0 | `unit-20261117.log`、`unit-20261117.exit.txt` |
| `go test ./internal/kilolatency ./sdk/api/handlers ./sdk/cliproxy/auth ./internal/runtime/executor/helps -run '^TestKiloLatency' -count=1 -v` | 正文判定、503分类与用量专项四包PASS，退出0 | `unit-final-content-outcome-20261117.log`、同名`.exit.txt` |
| `go test ./sdk/api/handlers -run '^TestKiloLatencyApplicationLogVisibility$' -count=1 -v` | 实际应用formatter可见性、单次摘要与正文隐私PASS，退出0 | `unit-log-visibility-20261117.log`、同名`.exit.txt` |
| `git -c core.whitespace=cr-at-eol diff --check` | 退出0 | `diff-check-20261117.log` |

仅对本轮修改/新增Go文件执行gofmt。单元专项使用假时钟、显式事件或取消信号，不用Sleep建立时间顺序。已覆盖精确分段、未知与亚毫秒TTFT、reasoning/工具/终态排除、真实Write/Flush失败、HTTP200后准入拒绝、取消、绑定操作计数及早期usage冻结后不重复发布。

## P9独立审查与统一验收

独立只读审查覆盖入口、实际正文Flush、错误分类、日志隐私、用量发布和非Kilo语义，最终无阻塞项。P9复核最终日志序列化及真实应用formatter证据；未机械重跑已通过且不变的分层专项。

命令工作目录与日志根目录同上。下表各次执行退出码均为0：

| 精确命令或构建 | 结果 | 输出文件 |
| --- | --- | --- |
| `go test ./sdk/cliproxy -run '^TestKiloLatency' -count=1 -v -timeout 90s` | 真实Router/Executor十个场景PASS | `p9-integration.log` |
| `go test ./sdk/cliproxy -run '^TestKiloLatencyNativeText$/^responses$' -count=1 -v -timeout 45s` | 最终日志接线版本单个Responses场景PASS；其余不变场景复用上一项 | `p9-integration-final-log.log` |
| `go test ./sdk/api/handlers -run '^$' -bench '^BenchmarkKiloLatencyObservation$' -benchmem -benchtime=500ms -count=1` | 最终三分支观测开销测量完成 | `p9-benchmark-final.log` |
| `go build -mod=readonly -buildvcs=false -o <日志根>/CLIProxyAPI-windows-amd64.exe ./cmd/server` | Windows/amd64，Go1.26.10，CGO=0，产物108893696字节 | `p9-build-windows.log` |
| `go build -mod=readonly -buildvcs=false -o /out/CLIProxyAPI-linux-amd64 ./cmd/server` | 缓存Linux容器内amd64，Go1.26.11，CGO=1，产物105444240字节 | `p9-build-linux.log` |

Linux构建使用现有本地`golang:1.26-bookworm`，`--network none --pull never`，源码和模块缓存只读挂载；两平台构建均禁用依赖网络下载。Linux本地缓存编译器为1.26.11，生产Dockerfile固定1.26.10，因此该结果是Linux/CGO服务端编译证据，不等同于正式生产镜像或现存插件加载验收。两平台各执行一次最终服务端构建，产物均在仓库外。

### 最终局部开销

Windows/amd64、Go1.26.10，同一四事件内存SSE序列，每分支测量500ms：

| 场景 | 微秒/请求 | 字节/请求 | 分配次数/请求 |
| --- | ---: | ---: | ---: |
| 无观测基线 | 2.873 | 7635 | 29 |
| 观测启用，Debug关闭 | 7.509 | 9773 | 57 |
| 观测及Debug摘要启用 | 14.265 | 15490 | 100 |

相对于同组基线，启用观测增加4.636微秒，启用摘要格式化增加11.392微秒。该测量仅描述固定局部夹具的CPU与分配成本，不作为生产首字收益或高并发退化已通过的证据。

- `kilo_latency_observation_test.go`复用真实Router、Executor、Transport与本地CONNECT/TLS夹具；外网目标重定向到loopback；持续排空requests通道。
- 集成覆盖Responses与Chat流式、正文先于终态交付、纯工具、终态-only缺失、非流式、早期usage、正文前后失败/取消、一次摘要、一次usage与单POST。usage检查使用队列barrier，不停止全局dispatcher。
- 三分支benchmark为`baseline`、`observed`、`observed_debug`；固定相同内存SSE序列。Debug分支使用实际LogFormatter与`io.Discard`，含安全JSON序列化和格式化CPU/分配成本，不测磁盘、网络日志sink或生产并发。
- Windows与Linux/amd64服务端最终build由P9统一完成；编译器及生产镜像/plugin ABI证据边界见上表。

## 交付结论

结论：本地观测候选通过。未改变并发、锁粒度、唤醒、连接复用、NoReplay、请求载荷、绑定存储策略、计费次数、BPS或既有工具续接缺口；无数据库动作。没有生产首字或总耗时改善结论；20%/10%目标仍属于后续有可比基线的性能验收。下一步是经授权发布观测版本、采集同条件正常流量，再选一至两个实测主要热点实施优化。

## 发布接线审查

2026-11-17，P9独立核对发布输入，发布入口只读审查无阻塞；既有Actions可复用。最终候选为28路径：22个CPA Go文件、4份说明文档、2份发布材料。临时组件树`4d0a4667fd0a1e9ff0f686884c7049e2116a4ec0`只纳入22个Go增量，不代表最终发布提交。

- 完整组件映射4655→4668项，精确13新增、9修改；其余4646项一致。当前22个Go文件的Git blob均与输入记录一致，真实索引未改。
- 历史接线evidence、test_runs、coverage及原lineage保留；普通启动入口、Manager、依赖声明与数据库路径未改。既有本地产品证据继续有效，本次未重跑产品测试或构建。
- 统一调用既有`validate_wiring_contract`与`validate_source_mapping`各一次，均PASS；映射校验对象为上述临时组件树。结果保存于`C:/Users/子羽/AppData/Local/Temp/kilo-latency-phase1-20261117/p9-release-wiring-20261117.json`。
- 合同、lineage与发布输入审查状态均为PASS。拟发布`v2026.11.17-rc.2`，工作流必须使用授权入仓后的完整提交SHA，执行既有三镜像构建及发布消费验证。
- 用户已于2026-11-17批准28路径提交、推送及固定版本发布；授权不包括部署或生产重启。本节记录发布前验收，正式镜像结果以工作流回执为准；生产插件加载、正常流量采样及性能收益不在本地接线结论内。
