# BPS 平铺身份列表

CPA 管理凭证列表的公共 builder 现为 BPS 从平铺 metadata.account_id 发布 `account_id`，同时覆盖 v0/v8。仅接受非空字符串，缺失或错误类型不从 sub、label、email 替代；token、refresh token 和原始元数据不加入列表。Codex 身份提取不变。

上下游固定映射：CPA `account_id` → Manager `cpaauthfiles.FromMap().AccountID` → 巡检 `accountId` → 快照 `auth_account_id_snapshot`。`name/auth_index` 分别映射 `fileName/authIndex`。组合保留精确身份断言，仅增加字段是否存在及是否匹配的布尔诊断，不输出身份原文。

## 验证

在 `components/cpa` 使用本机已缓存 go1.26.10，设置 `GOPROXY=off`、`GOTOOLCHAIN=local`：

```powershell
& 'C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe' test ./internal/api ./internal/api/handlers/management -run 'TestBPS(AccountUsageFlattenedCredentialListing|FlattenedAccountIdentityListing)$' -count=1 -v
```

exit 0，详 `identity-listing-go-test.log/exit`。专项读取实际临时平铺 JSON 文件，穿过 v8 管理公开列表；同一公共 builder 核 v0/v8 的有效身份、空值、缺失、坏类型和其他 provider；确认身份匹配、无凭证回显、原存储零写入。

组合 `node --check run.mjs/check.mjs`、`git diff --check` 通过。未重跑组合、构建或官方请求；原失败与清理回执由主会话保留。无 SQL/schema/迁移或线上动作。
