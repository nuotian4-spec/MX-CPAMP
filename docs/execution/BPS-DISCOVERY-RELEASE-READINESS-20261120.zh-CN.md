# BPS 模型发现候选发布准备

日期：2026-11-20。仅本地准备，尚未提交、推送、创建版本或触发发布。

基线：`05bea8d5b24cc001ab888e87418ce7d96754f3b9`。主会话已核实远端 main 与此一致，最新 Release 为 `v2026.11.20-rc.1`，精确查询 `v2026.11.20-rc.2` 的 tag 为空、Release 返回 404；建议后续使用 `v2026.11.20-rc.2`，执行发布前仍核对版本占用。

## 来源和审查

原合同组件映射对应 `31e0fbcf28d895e13e8eb247e36d4596c655e74c`，由 `5ac9110` 接线提交保存。它与当前基线已有 13 个组件路径差异：`84a260d236b3e42581ab666b4cba72af3ee5ab96` 引入 12 个，`05bea8d5b24cc001ab888e87418ce7d96754f3b9` 引入本地测试窗口 1 个。

这 13 项属于既有 BPS 自动授权，与本轮发现状态的 42 个组件路径无重叠。合同分两条 lineage 记录；当前 source_files 同时覆盖两组，合计更新 55 个映射条目。

**既有自动授权审查：PASS，审查人：主会话 P9。** 本次独立静态读取自动授权 API、队列、状态和保存确认、`bpsManualMenu` 绑定及 `PluginResourcePage` 接入差异，并核对四语文案、测试和本地窗口。账号仅经固定连接 scope 的插件 POST；state 绑定、提交不重放、保存成功三元组、页面断开及停止、输入清理、sandbox 不执行供应商脚本均保持。两个本地窗口使用本机模拟，没有改生产启动、Docker 或后端。未发现阻断此次源码映射补齐的问题。

运行证据引用 [自动授权原执行记录](BPS-AUTO-LOGIN-RESTORE-PLAN.zh-CN.md)：插件目录 76 项测试、4 个手动和5 个自动模拟浏览器场景、类型检查、lint 及构建通过。本次没有重跑这些产品验证，也未将它们冒记为本轮模型发现的独立测试。原执行记录同时载明全量测试 13 项失败，其中11项在干净基线复现，`updatePages.integration.test.tsx` 存在 `DEMO_API_BASE` mock 导出缺失；本次合同补齐不处理这些记录。

**本轮发现状态审查：PASS，审查人：主会话 P9。** 42 个组件路径及执行回执已独立验收；引用 [模型发现状态执行回执](BPS-DISCOVERY-STATE-RECEIPT-20261120.zh-CN.md) 的定向测试、CPA/Manager 编译、前端构建及 lint 证据。本次追加文件仅为本回执和 `deploy/production-wiring.contract.json`，原43路径候选保持冻结。

## 合同验证

使用仓库现有 `deploy/capability_probe.py` 的 `validate_source_mapping`、`validate_test_run` 和 `validate_wiring_contract`，不更改校验规则。

在临时目录设置 `GIT_INDEX_FILE`，执行 `git read-tree 05bea8d5b24cc001ab888e87418ce7d96754f3b9`，只将当前候选路径加入临时 index，再执行 `git write-tree`。源码映射直接取 `git ls-tree -r <临时tree> -- components/cpa components/cpamp`；没有创建 commit，真实 index 字节保持一致。

本次验证结果：

| 检查 | 结果 |
| --- | --- |
| 基线与原合同的差异 | 精确13项，来源为上述两个已合入提交 |
| 工作树与基线的组件差异 | 精确42项，与13项不重叠 |
| 完整 source_files | 4694项，`validate_source_mapping` 通过 |
| 组件 tree | `2d3e50a5d1681c81c0e188fc8f2dccee153cde65` |
| 五组继承日志校验 | `m10-scope`、`m10-joint`、`bps-native`、`bps-stream`、`bps-legacy` 均通过 |
| 合同结构与审查字段 | `validate_wiring_contract` 通过 |
| 真实 index | 未改变 |
| Git diff 空白检查 | 通过 |

五组继承日志由校验函数读取并核对原有终态和关键输出，没有重跑这些测试。`production-wiring.evidence.json`、历史 lineage、test_runs 要求均保持原样。

## 尚未成立的结果

真实 BPS 自动授权、生产辅助服务就绪和新凭证模型目录恢复仍未验证。本次合同验证只证明候选源码与现有证据合同完整绑定，不证明这些线上业务已成功。

后续责任人：主会话。独立核对两项追加文件后，在获得入仓发布授权的范围内继续固定提交发布流程。

## 基线13项清单

```text
components/cpamp/apps/docs/manual/plugins.md
components/cpamp/apps/web/joint-tests/bpsNativeManual.browser.mjs
components/cpamp/apps/web/joint-tests/bpsNativeMenuWindow.mjs
components/cpamp/apps/web/src/features/plugins/PluginResourcePage.test.tsx
components/cpamp/apps/web/src/features/plugins/PluginResourcePage.tsx
components/cpamp/apps/web/src/features/plugins/bpsAutoLoginMenu.test.ts
components/cpamp/apps/web/src/features/plugins/bpsAutoLoginMenu.ts
components/cpamp/apps/web/src/features/plugins/bpsManualMenu.ts
components/cpamp/apps/web/src/i18n/locales/en.json
components/cpamp/apps/web/src/i18n/locales/ru.json
components/cpamp/apps/web/src/i18n/locales/zh-CN.json
components/cpamp/apps/web/src/i18n/locales/zh-TW.json
components/cpamp/apps/web/src/services/api/bpsAutoLogin.ts
```
