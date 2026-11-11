# 并发设置绑定展示时CPA连接

日期：2026-11-11。代码输入：`bfc414ba160727cbc4f31cf06c73cd6d3b2c5ced`加本提交。仅修改Manager并发BFF、Web API/页面和专项；A线产品不变，无数据库动作。

## 反例

在增加字段但尚未实施连接校验时，先List(A)，切换Manager至B，两边具有相同auth_id，再用旧页面输入Update。`go test ./internal/service/credentialconcurrency -run TestStaleDisplayedConnectionCannotWriteOtherCPA -count=1`退出1，回执`writesA=0 writesB=1`，见`connection-binding-before.txt`。反例确认旧页面可写入新的CPA。

## 完成行为

Local快照追加随机非秘密`connection_ref`；PATCH必须回传。引用在Manager内存只保留当前连接一份，绑定URL与管理密钥，仅随机引用出现在浏览器。读取目标凭证前及写入前核验，保留原中途setup漂移保护。Home响应形状保持不变；页面轮询切实例时，按连接引用重建同名凭证表单，清除旧编辑值。完整接口见`docs/contracts/credential-concurrency-bff.zh-CN.md`。

## 专项结果

固定Go1.26.10、Node22.23.4路径沿用`concurrency-ui.zh-CN.md`。命令分别在Manager/Web目录执行，stdout/stderr直接Tee保存；日志仅规范化结尾空行。

| 命令 | 退出码/结果 | 输出 |
|---|---|---|
| `go test ./internal/service/credentialconcurrency ./internal/http/controller/credentialconcurrency` | 0，两包通过 | connection-binding-manager.txt |
| `node ../../node_modules/vitest/vitest.mjs run src/features/accounts/CredentialConcurrencyPage.test.tsx` | 0，5项通过 | connection-binding-web.txt |
| `go test ./internal/http/controller/credentialconcurrency -run TestSnapshotReferenceRoundTrip -count=1` | 0，HTTP快照/PATCH引用贯通通过 | connection-binding-http.txt |
| `node ../../node_modules/vitest/vitest.mjs run src/services/api/credentialConcurrency.test.ts` | 0，1项通过 | connection-binding-api.txt |

覆盖A快照切B后双方零写、相同连接引用稳定、缺失/格式错误/未知引用拒绝且不请求上游、Manager重启与管理密钥切换失效、正常设置/恢复默认、配置ETag与写中切换、页面显式传引用和切实例清旧编辑值。测试使用本地假上游，无真实账号、部署或全量构建。
