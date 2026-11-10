# 批次摘要使用既有凭证索引

日期：2026-11-11。代码输入：`8da013158ede9837ccf05ebaa3003845d327749b`加本提交。文件域仅Manager oauthlogin、批次Web与契约/专项；CPA、Worker和单任务公开契约未改，无SQL/schema迁移。

## 反例与定位

假CPA改用真实格式`codex-user@example.invalid-plan.json`作为导入返回ID。修复前执行`go test ./internal/service/oauthlogin -run TestBatchSummariesDoNotExposeEmailInCredentialFilename -count=1`，exit1，POST同编号重试摘要包含原邮箱。直接保存的输出为`batch-auth-index-before.txt`。

原批次嵌入整个Task，连同Task.auth_id一并JSON编码。现在批次持有独立白名单DTO，成功仅公开CPA现有auth_index；内部Task仍保存真实ID。固定credentials GET通过name筛选，并核id+provider唯一对应；查询前后复核冻结setup。路由支持依据：CPA `server_management_v8.go`的GET /credentials及`auth_files.go`的nameFilter/matchesAuthFileLookup。

新Manager错误码`credential_mapping_unconfirmed`表示已确认保存注册、凭证索引尚待核实；中断后续条目，页面明确“已保存，凭证索引待核实”，不自动重导入。Worker错误白名单不变。

## 验证

固定Go1.26.10、Node22.23.4路径沿用`concurrency-ui.zh-CN.md`。以下命令在对应Manager/Web目录执行，stdout/stderr直接Tee保存；仅规范化日志末尾空行。

| 命令 | 结果 | 日志 |
|---|---|---|
| `go test ./internal/service/oauthlogin ./internal/http/controller/oauthlogin` | exit0，两包通过 | batch-auth-index-manager.txt |
| `node ../../node_modules/vitest/vitest.mjs run src/services/api/loginBatches.test.ts src/features/oauth/InternalLoginCards.test.tsx` | exit0，2文件13项通过 | batch-auth-index-web.txt |

覆盖含邮箱文件名的POST/GET/latest/DELETE摘要脱敏、成功索引与真实CPA条目一致、内部ID保留、取消时等待导入结果、终态摘要恢复；索引缺失、重复、错provider、非法索引、查询失败及查询前后连接漂移均停止后续条目。页面仅显示凭证索引，已保存但索引未知提示明确且不重放。既有单任务测试仍通过，其CPA假服务只接受原POST导入，证明未给单任务增加索引查询。

未进行真实登录或生产操作，未重复全量构建。整合后只需重建Manager（含Web产物），CPA/Worker产品输入等价，可复用原镜像。
