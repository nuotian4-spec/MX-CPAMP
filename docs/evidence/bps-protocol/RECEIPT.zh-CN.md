# BPS协议、工具绑定验证

代码输入：供体核心`9b93c3b`，图片宿主`e602335`、路径解析`7ab7ff3`，加本记录同提交的协议与绑定改动。验证使用固定Go1.26.10、共享GOMODCACHE；测试时`GOPROXY=off`。

本轮接入Responses/Chat Completions普通与SSE工具往返。输出工具前保存7天最小身份绑定，工具结果先锁定原BPS凭证，再恢复native call ID并转换协议。记录只含调用ID、凭证引用、账号ID、subject、调用方scope、到期时间；没有正文、参数、图片或令牌。正常刷新不改身份，身份替换或记录过期明确失败。未执行真实登录、真实模型调用或部署，没有SQL/schema迁移；运行时新增`BPSStateDirectory/bindings`私有文件。

| 检查 | 命令及结果 | 回执 |
|---|---|---|
| 公开HTTP入口 | `go test ./internal/api -run '^TestBPSToolsPublicHTTPRoundTrip$' -count=1 -v -timeout 120s`，exit0 | `public-http-tools.log` |
| 最终宿主专项 | `go test ./internal/bpsbinding ./sdk/cliproxy/auth ./sdk/cliproxy/executor ./internal/runtime/executor ./internal/runtime/executor/helps -run '^(TestBinding\|TestBPS)' -count=1 -timeout 120s`，exit0 | `host-final-tests.log` |
| 供体全包 | `go test ./internal/basispoints ./internal/basispoints/urlvalidator -count=1 -timeout 120s`，exit1，仅下述三项失败 | `core-tests.log` |
| 原始核心对照 | 以Go overlay把`internal/basispoints/stream.go`替换为`git show 9b93c3b:components/cpa/internal/basispoints/stream.go`内容，仅运行下述三项，exit1，同样失败 | `core-baseline-three.log` |

公开HTTP测试参数化覆盖`/v1/responses`、`/v1/chat/completions`各普通/SSE，共4组8次客户端请求。每组首轮工具调用、第二轮结果回传均HTTP200；通过双BPS凭证池实际调度，测试HTTP transport验证两次上游使用同一账号，回传恢复native call ID。上游为本地替身，该结果不表示BPS真实模型权限或生产调用已经通过。

宿主专项还覆盖重启读取、七天到期、调用方隔离、刷新令牌后保持绑定、上游身份替换拒绝、拒绝切换至Codex、无效工具零发布且只发一次模型请求、payload规则重新注入图片时关图策略仍拒绝。最终构建由主线统一执行。

## 供体已有失败

1. `TestCatalogContractLongHistoryMismatchFailsClosed`：传入非nil自动修复回调时，回调次数为1而断言为0。宿主`ReadBPSProtocolEvents`固定调用`StreamWithRepairs(ctx, upstream, nil, nil)`；`TestBPSFailedToolDoesNotRepairOrPublish`验证无效调用不触发第二次模型请求，也不输出工具或创建绑定。
2. `TestFunctionCodeBranchCatalogAndReplay`：通过`input[len(input)-2]`定位原生调用；既有`request.go`在带工具历史的输入末尾追加developer约束，使该位置指向工具结果。
3. `TestCodexThreadDelegationPreservesPairedTools`：通过`len(wantItems)-len(history)+1`推算handoff位置；同一既有追加约束改变该偏移。

对照测试完整表达式为`^(TestCatalogContractLongHistoryMismatchFailsClosed|TestFunctionCodeBranchCatalogAndReplay|TestCodexThreadDelegationPreservesPairedTools)$`。本轮核心产品改动仅为缓冲终态产生的公开工具事件补`response_id`，不改变上述Prepare/repair分支。全包保持失败记录，三项影响交独立审查判断。

`targeted-tests.log`记录较早的组合运行：供体包因离线缓存缺少testify传递依赖而setup失败，auth、executor SDK、runtime executor、helps包分别通过全包测试；该组合退出1，不作为全包通过依据。
