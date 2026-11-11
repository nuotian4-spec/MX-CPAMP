# BPS 官方账号用量 Manager / Web 专项回执

- 基线：`f4dd43e4c217ca6eb7762ca4d4f747665280657c`
- 产品提交：`28258fd65ab0dff4a68dc470b8193e22ade6d076`
- 分支：`codex/bps-account-usage-manager`
- 实施范围：`components/cpamp/apps/manager-server/**`、`components/cpamp/apps/web/**`
- 数据库动作：无 schema 或 migration 变更；沿已有 SQLite 快照写查路径保存 BPS 用量，首次接入采用独立 BPS 账户身份键。
- 下一责任人：独立审查席核查稳定产品提交；主会话统一 Manager 构建和断网组合验收。

## 产品行为

`GET /api/bps/usage?auth_index=...&connection_ref=...` 经过管理员认证和连接引用检查后转发 CPA 的专用 usage GET，可选 `auth_id`，保持顶层冻结 DTO 和 `no-store`。请求不跟随重定向，不执行推理、登录或刷新。

账号列表、卡片、详情、监控额度和巡检结果接入独立 BPS 用量状态。来源标签为“官方账号用量”，显示套餐、实际窗口、重置时间和巡检用量观察时间。保留合法 0、reset-only 和绝对 reset 优先；失败展示未知或错误，不生成 0 用量。模型范围为 `feature / bps_account_usage / complete=false`，不声称 BPS 独占、共桶或所有模型共享。

BPS 权限判定和用量查询分开：用量失败保留权限结论，权限失败仍可展示独立成功用量。巡检固定 `keep`、`autoRecoverEligible=false`、`isQuota=false`，不触发自动管理动作。

身份隔离包括：

- 主动 store / 页面快照缓存：BPS 独立键绑定物理文件、provider、authIndex、account_id、accountSnapshot 和当前文件更新时间。
- 巡检记录：要求原 provider / authIndex / account_id / accountSnapshot 与当前凭证完全相同，至少一项账户身份存在，记录晚于文件更新。
- 持久快照：BPS 专用 `bps-quota-account:v1:` 元组键绑定物理文件、provider、authIndex、account_id 和 accountSnapshot；写入、查询和巡检入库统一使用。Web 查询携带当前账户真实身份。其他 provider 仍使用原历史身份算法。

## 验证

Node：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`。

Go：`C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe`，`GOTOOLCHAIN=local`、`GOPROXY=off`。使用现有依赖，无安装或 lock 变更。

| 验证 | 结果 | 输入与证据 |
| --- | --- | --- |
| Go 6 包 BPS 专项 | exit 0 | `prior-tool-receipts.json` 原工具回执摘录；最后快照身份变更另补全包验证，其余 Go 产品输入未变 |
| Web 9 文件专项 | 295 例，exit 0 | `prior-tool-receipts.json`，18:26:35 原工具回执摘录；最后展示和快照身份变更另补相关文件，其余输入等价复用 |
| 结果卡展示专项 | 1 文件、11 例，exit 0 | `results-panel-vitest.log`，18:31:19；BPS 标识、套餐、观察时间、额度失败与权限成功分离 |
| 快照身份专项 | Go quotasnapshot 全包，exit 0，10.299 秒 | `bps-snapshot-identity-go.log`；真实 SQLite 往返，同 name / authIndex 仅换 account_id 或 accountSnapshot 均读不到旧窗口，原账户仍可读取 |
| Web 快照身份专项 | 2 文件、11 例，exit 0 | `bps-snapshot-identity-vitest.log`，18:35:28；BPS 账户字段随查询发送，换账号不命中页面快照缓存，已有 history target 用例保持通过 |
| ESLint | 受影响 TS / TSX exit 0 | `prior-tool-receipts.json`，本席 18:26 全部受影响文件原回执摘录；最终 5 个展示文件及 3 个快照相关文件补跑 `--max-warnings 0` 均 exit 0 |
| 格式 / diff | exit 0 | 全部受影响 Go 文件 `gofmt -l` 无输出；最终结果卡两文件 Prettier 检查通过，见 `final-affected-prettier.log`；提交前 `git diff --cached --check` 通过 |

Go 6 包命令，工作目录 `components/cpamp/apps/manager-server`：

```powershell
go test ./internal/model ./internal/service/cpa ./internal/service/credentialconcurrency ./internal/http/controller/bpssettings ./internal/service/codexinspection ./internal/service/quotasnapshot -run 'BPS' -count=1
```

原命令完成输出摘要，进程 exit 0：

```text
ok internal/model 0.071s
ok internal/service/cpa 0.096s
ok internal/service/credentialconcurrency 0.099s
ok internal/http/controller/bpssettings 0.093s
ok internal/service/codexinspection 0.158s
ok internal/service/quotasnapshot 0.126s
```

Web 原命令完成输出为 `Test Files 9 passed (9)`、`Tests 295 passed (295)`，进程 exit 0。JSON 摘录保留原工具 call_id、原始包路径、标准输出及进程退出码；未重新运行这批命令。

Web 9 文件命令，工作目录 `components/cpamp/apps/web`，由上述 Node 执行：

```text
../../node_modules/vitest/vitest.mjs run
src/services/api/bpsUsage.test.ts
src/features/accounts/model/bpsQuota.test.ts
src/features/monitoring/model/bpsInspectionProbe.test.ts
src/features/monitoring/components/CodexInspectionQuotaWindows.test.tsx
src/features/accounts/model/accountQuotaSnapshots.test.ts
src/features/monitoring/model/monitoringCenterPageModel.test.ts
src/features/accounts/model/accountRows.test.ts
src/features/accounts/model/accountsPagePresentation.test.ts
src/features/monitoring/accountOverviewQuotaTargets.test.ts
```

最后补充命令：

```powershell
go test ./internal/service/quotasnapshot -count=1
```

```text
../../node_modules/vitest/vitest.mjs run src/features/monitoring/components/CodexInspectionResultsPanel.test.tsx
../../node_modules/vitest/vitest.mjs run src/features/accounts/model/bpsQuota.test.ts src/features/accounts/model/accountHistoryRows.test.ts
```

## 待主会话完成

统一 Manager 构建包含最终类型检查，本席未重复构建。真实 API、登录、刷新、推理、现有桌面窗口操作、push、deploy 和页面人工验收均未执行；本回执只覆盖本地专项。Web 测试的既有 Vite 配置警告不影响上述实际退出码。
