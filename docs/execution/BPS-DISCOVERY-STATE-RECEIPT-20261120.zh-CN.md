# BPS 模型发现状态与插件维护归属执行回执

日期：2026-11-20。

基线：`05bea8d5b24cc001ab888e87418ce7d96754f3b9`。

工作树：`C:/Users/子羽/.codex/worktrees/bps-discovery-state-20261120/MX-CPAMP`。

本地候选完成，主会话独立审查通过。未提交、推送、发布或部署。

## 结果与合同

新凭证模型发现返回 `bps_model_discovery_rejected` 时，管理页显示发现失败和受控错误码，不再把它等同为成功的空目录。成功空目录、成功有目录、失败、停用、无本轮记录可以区分；发现失败仍保留已有模型目录。

CPA 的 `GET /v8/management/credentials/models?name=...` 保留 `models`，增加 `discovery`：

```json
{
  "models": [],
  "discovery": {
    "status": "failed",
    "checked_at": "2026-11-20T00:00:00Z",
    "plugin_id": "cpa-bps",
    "error_code": "bps_model_discovery_rejected"
  }
}
```

`status` 为 `unknown|succeeded|failed|disabled`；其余字段可省略。没有真实上游状态来源，因此不生成 `upstream_status`。允许的具体错误码为 `bps_model_discovery_rejected`、`bps_model_discovery_failed`、`bps_invalid_model_catalog`、`bps_reauthorization_required`、`bps_proxy_invalid`，其他错误统一为 `plugin_model_discovery_failed`；错误正文和凭证材料不进入结果。

结果保存在当前 CPA 进程内，绑定凭证注册代次、凭证版本及发现输入。重启后无记录为 `unknown`；停用即时返回 `disabled`，更新和删除清理结果。挂起的旧请求、停用再启用前的请求、较旧发现尝试不覆盖当前状态。插件返回的受控凭证更新在同一凭证 mutation 锁内承接发现尝试，成功目录与成功状态保持一致。

Manager 仅精确转发该 v8 GET 路由，复用保存的 CPA 连接、管理密钥和既有管理鉴权。系统 BPS 面板与凭证详情模型标签均接入。旧服务返回 404 时回退旧模型读取并显示未知状态；401、502 不作为回退依据。Demo 路径使用本地模型夹具。

插件维护记录增加可选 `action`。ZIP 和商店安装记录 `install`，配置记录 `configure`，移除记录 `remove`；商店安装在下载前失败也保留安装归属。管理页明确显示 `plugin_id`，旧任务缺失动作使用“插件维护”，不补写历史记录。维护结果和当前注册、商业许可状态分开呈现。

## 验证

日志目录：`C:/Users/子羽/.codex/artifacts/bps-model-investigation-20261120/`。

以下命令均在本工作树对应组件目录执行。各测试组存在重叠，不相加作为唯一测试数量。

### CPA

工作目录：`components/cpa`。

最终定向验证：

```powershell
go test ./sdk/cliproxy/auth ./sdk/cliproxy ./internal/api/handlers/management -run '^(TestModelDiscoveryLifecycleAndAttemptOrdering|TestModelDiscoveryDisableEnableRejectsPendingAttempt|TestPluginModelDiscoveryResults|TestModelDiscoveryNewerFailureAfterOlderPublication|TestStoreInstallEarlyFailureKeepsAction|TestPluginModelRegistrationInvalidAuthUpdateDoesNotRetry_Issue6718|TestModelRegistrationPluginExclusionSources)$' -count=1
```

退出码 0，三个包均通过。覆盖失败/空目录/已有目录、错误脱敏、凭证隔离、停用及旧请求、插件真实 `AuthUpdate` 分支、较新失败覆盖较旧发布、商店安装早期失败的动作回读，并复用既有插件更新与排除规则测试。日志：`cpa-final-targeted.log`。

前序已通过并复用的合同验证：

```powershell
go test ./sdk/cliproxy/auth ./sdk/cliproxy ./internal/api/handlers/management ./internal/updatestate -run 'Test(ModelDiscoveryLifecycleAndAttemptOrdering|PluginModelDiscoveryResults|AuthFileModelsV8DiscoveryAndLegacyShape|OperationActionRoundTrip|ModelRegistrationPluginExclusionSources)$' -count=1
```

该轮 `auth`、`management`、`updatestate` 三包退出成功，证据为前序会话工具回执；v8/旧模型响应形状与 action 保存读取通过。`cliproxy` 的最终状态以本节最终定向日志为准，不把整个前序命令记作全通过。

最终编译：

```powershell
go build -o C:/Users/子羽/.codex/artifacts/bps-model-investigation-20261120/cpa-discovery-candidate.exe ./cmd/server
```

退出码 0；日志：`cpa-final-build.log`。

### Manager 后端

工作目录：`components/cpamp/apps/manager-server`。

```powershell
go test ./internal/httpapi -run '^TestServerCompatV8CredentialModelsProxy$' -count=1
go build -o C:/Users/子羽/.codex/artifacts/bps-model-investigation-20261120/manager-discovery-candidate.exe ./cmd/cpa-manager-plus
```

测试退出码 0：精确路径、参数、CPA 管理密钥替换、缺少/错误管理权限、POST 不转发均通过。测试证据为前序会话实际工具回执，未单独保存日志，本轮复用。编译退出码 0，日志：`manager-final-build.log`。

### Manager 前端

工作目录：`components/cpamp/apps/web`。调用本机 `D:/New Folder/node.exe`，使用已有依赖，不安装或升级依赖。

前序模型状态入口验证：

```powershell
& 'D:/New Folder/node.exe' node_modules/vitest/vitest.mjs run src/features/system/components/CpaBpsModelsPanel.test.tsx src/features/system/components/BPSSettingsRouting.test.tsx src/features/authFiles/hooks/useAuthFilesModels.test.tsx src/features/plugins/pluginMaintenance.test.ts src/services/api/authFiles.discovery.test.ts
```

5 个文件、32 个测试通过，退出码 0，证据为前序会话工具回执。覆盖系统页状态和 API 缺字段/404兼容、502 非上游状态等。

前序维护归属与详情/Demo 验证：

```powershell
& 'D:/New Folder/node.exe' node_modules/vitest/vitest.mjs run src/features/plugins/pluginMaintenance.test.ts src/features/plugins/PluginsPage.bps.test.tsx src/features/accounts/components/accountDetail/AccountModelsTab.test.tsx src/features/demo/demoApi.authFiles.test.ts
```

4 个文件、57 个测试通过，退出码 0，证据为前序会话工具回执。包括 `cpa-codex-candy-eval` 维护归属与 Demo 兼容。

最终边界验证：

```powershell
& 'D:/New Folder/node.exe' node_modules/vitest/vitest.mjs run src/features/authFiles/hooks/useAuthFilesModels.test.tsx src/features/accounts/components/accountDetail/AccountModelsTab.test.tsx src/features/plugins/pluginMaintenance.test.ts
& 'D:/New Folder/node.exe' node_modules/vitest/vitest.mjs run src/features/accounts/AccountsPage.test.tsx -t 'BPS 状态变更会清除模型发现缓存|keeps a single row status toggle scoped|keeps the full reload for a multi-row status action'
& 'D:/New Folder/node.exe' node_modules/vitest/vitest.mjs run src/features/authFiles/hooks/useAuthFilesModels.test.tsx -t '停用更新会清除缓存并阻止挂起的发现结果回写'
```

依次为 3 文件/57 测试通过、1 文件/5 测试通过/383 跳过、1 文件/1 测试通过/15 跳过，退出码均为 0。日志依次为 `frontend-final-targeted.log`、`accounts-status-targeted.log`、`frontend-cache-final.log`。最后一项验证缓存失效时解除 loading 状态的最终代码。

构建：

```powershell
npm.cmd run build
```

退出码 0，`tsc && vite build`，1744 个模块，产物 `dist/index.html`。日志：`frontend-final-build.log`。

在仓库根目录运行定向 lint：

```powershell
$changed = @(git -c core.safecrlf=false diff --name-only -- '*.ts' '*.tsx') + @(git ls-files --others --exclude-standard -- '*.ts' '*.tsx')
$webFiles = $changed | ForEach-Object { $_ -replace '^components/cpamp/apps/web/', '' }
Push-Location components/cpamp/apps/web
& 'D:/New Folder/node.exe' node_modules/eslint/bin/eslint.js $webFiles --report-unused-disable-directives
Pop-Location
```

退出码 0，0 错误、5 条既有警告。静态对照 `git show HEAD:<path>` 确认五处上下文均存在于基线：`AccountsPage.test.tsx` 的三处 `no-explicit-any` 和 `AccountsPage.tsx` 的两处 `showNotification` 依赖警告。日志：`frontend-final-lint.log`。

Go 文件已执行 `gofmt`。仓库根目录 `git -c core.safecrlf=false diff --check` 退出码 0。

## 范围与限制

本轮修复失败结果的记录和呈现，以及插件维护归属。插件 `bps_model_discovery_rejected` 丢失的实际上游 HTTP 状态和响应原因仍需插件维护者补充；本候选不代表模型目录已恢复。

没有调用真实账号、授权、模型或上游发现，没有改生产、数据库、依赖锁文件、CI、发布合同或其他工作树。原观察工作树的三份文档保持原样。后续发布合同和生产动作由主会话单独收口。

下一责任人：主会话负责后续候选入仓与固定版本发布准备；插件维护者负责补充实际模型发现 HTTP 状态和业务原因。生产模型恢复尚未验收。

## 主会话审查与验收

结论：通过本地候选验收。

主会话复核了状态记录及凭证更新锁、发现尝试顺序、停启失效、插件返回 Auth 更新、v8 精确只读转发、页面缓存、两个模型入口及维护动作的保存读取。发现错误只对允许的错误码进行展示；未知的原始上游状态保持缺省。旧管理模型响应保持原状。

审查后的最终代码包含：较新失败保留、停用再启用拒绝旧发现、当前插件 Auth 转换保留成功结果、前端状态变更清缓存、商店安装早期失败动作归属及非安装任务标签。相关专项证据见上文，不重复执行未变化的门禁。

主会话读取了最终 CPA 定向日志、前端 57 项状态组、5 项账号开关组、最后 1 项缓存回归、三项构建与 lint 日志，并执行了 `git diff --check` 和路径范围核对。新增源码无行尾空白；无依赖清单、数据库路径或允许范围外变更。lint 的五处既有警告保留。最终加载态调整使用最后一项定向回归验收，未重复前端构建。

已识别的发布前置：本候选基线自带的生产接线合同与基线组件树已有 13 项原生自动授权相关差异；这不是本轮新增差异。本回执不替代那 13 项的既有审查，发布前应引用原回执并补齐本轮源码映射。详见仓外 `C:/Users/子羽/.codex/artifacts/bps-model-investigation-20261120/RELEASE-BOUNDARY.zh-CN.md`。

## 文件清单

本候选共 43 个路径：42 个产品/测试路径及本回执。

```text
components/cpa/internal/api/handlers/management/auth_file_models_v8.go
components/cpa/internal/api/handlers/management/auth_file_models_v8_test.go
components/cpa/internal/api/handlers/management/plugin_imports.go
components/cpa/internal/api/handlers/management/update_operation_action_test.go
components/cpa/internal/api/handlers/management/update_operations.go
components/cpa/internal/api/server_management_v8.go
components/cpa/internal/pluginhost/adapters.go
components/cpa/internal/updatestate/store.go
components/cpa/internal/updatestate/store_test.go
components/cpa/sdk/cliproxy/auth/conductor.go
components/cpa/sdk/cliproxy/auth/conductor_lifecycle.go
components/cpa/sdk/cliproxy/auth/conductor_selection.go
components/cpa/sdk/cliproxy/auth/kilo_accounts.go
components/cpa/sdk/cliproxy/auth/model_discovery.go
components/cpa/sdk/cliproxy/auth/model_discovery_test.go
components/cpa/sdk/cliproxy/model_discovery.go
components/cpa/sdk/cliproxy/model_discovery_test.go
components/cpa/sdk/cliproxy/service_executors.go
components/cpamp/apps/manager-server/internal/http/controller/proxy/handler.go
components/cpamp/apps/manager-server/internal/http/router/router.go
components/cpamp/apps/manager-server/internal/httpapi/server_compat_test.go
components/cpamp/apps/manager-server/internal/service/proxy/service.go
components/cpamp/apps/web/src/features/accounts/AccountsPage.test.tsx
components/cpamp/apps/web/src/features/accounts/AccountsPage.tsx
components/cpamp/apps/web/src/features/accounts/components/accountDetail/AccountModelsTab.test.tsx
components/cpamp/apps/web/src/features/accounts/components/accountDetail/AccountModelsTab.tsx
components/cpamp/apps/web/src/features/authFiles/components/ModelDiscoveryNotice.tsx
components/cpamp/apps/web/src/features/authFiles/hooks/useAuthFilesModels.test.tsx
components/cpamp/apps/web/src/features/authFiles/hooks/useAuthFilesModels.ts
components/cpamp/apps/web/src/features/demo/demoApi.authFiles.test.ts
components/cpamp/apps/web/src/features/demo/demoApi.ts
components/cpamp/apps/web/src/features/plugins/PluginsPage.bps.test.tsx
components/cpamp/apps/web/src/features/plugins/PluginsPage.tsx
components/cpamp/apps/web/src/features/plugins/pluginMaintenance.test.ts
components/cpamp/apps/web/src/features/plugins/pluginMaintenance.ts
components/cpamp/apps/web/src/features/system/components/BPSSettingsRouting.test.tsx
components/cpamp/apps/web/src/features/system/components/CpaBpsModelsPanel.test.tsx
components/cpamp/apps/web/src/features/system/components/CpaBpsModelsPanel.tsx
components/cpamp/apps/web/src/services/api/authFiles.discovery.test.ts
components/cpamp/apps/web/src/services/api/authFiles.ts
components/cpamp/apps/web/src/services/api/client.ts
components/cpamp/apps/web/src/types/pluginImport.ts
docs/execution/BPS-DISCOVERY-STATE-RECEIPT-20261120.zh-CN.md
```
