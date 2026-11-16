# 前端定向工具回执

候选：`unified-update-20261116/MX-CPAMP`。本记录转存 UX1 返回的真实工具摘要；不是原始日志。测试使用无网 Docker Node v22.23.4，未调用真实许可、账号或模型服务。

| 范围 | 工具 Chunk | 结果 | 工具原始时间 |
|---|---|---|---|
| ZIP 弹窗原专项 | `a99453` | 20/20 PASS；`component_vitest_exit=0`；`prettier_exit=0` | 20:11:41，917 ms |
| 核心更新组件 | `80f2a5` | 22/22 PASS；`core_vitest_exit=0` | 20:41:18，1.32 s |
| unknown 许可增量 | `80f2a5` | 4 PASS / 20 skipped；`modal_unknown_license_vitest_exit=0`；`component_docker_exit=0`；`prettier_exit=0` | 20:41:20，1.07 s |
| API 固定连接与页面接线 | `7688ab` | API 6 + 原页面 React 接线 11，共 17/17 PASS；`update_wiring_vitest_exit=0` | 未返回 |
| 原页面受影响回归 | `6c0dc6` | 14 PASS / 40 skipped；`scoped_page_refresh_exit=0`；该文件 ESLint=0 | 未返回 |
| 最终 TypeScript、diff | `374770` | `tsc --noEmit --pretty false -p tsconfig.json`、`git diff --check` 均 exit 0 | 未返回 |
| 接线与组件定向 ESLint | `aafee5` | 10 文件 exit 0；原页新增 mock 单独 ESLint 见 `6c0dc6` | 未返回 |
| 定向格式化 | `2dc6a0` | Prettier exit 0 | 未返回 |

时间按工具原文保留，未换算时区。未返回 session_id 的执行不构造日志路径。上述证据为 mock API 组件与 React 页面接线专项；不代替正式 v8 Router、真实合成库注册或实际浏览器页面验收。

页面入口为 `/plugins` 与 `/system`；保留原 `/system/updates`。旧页面回归只新增 21 行 API mock，未替换页面实现。

```text
node /app/node_modules/vitest/vitest.mjs run src/services/api/pluginImports.test.ts src/features/updatePages.integration.test.tsx --maxWorkers=1 --no-file-parallelism --reporter=verbose
node /app/node_modules/vitest/vitest.mjs run src/features/pageRefresh.test.tsx -t "installed|system|plugin mutation|cached model count" --maxWorkers=1 --no-file-parallelism --reporter=verbose
node /app/node_modules/typescript/bin/tsc --noEmit --pretty false -p tsconfig.json
node /app/node_modules/eslint/bin/eslint.js src/services/api/pluginImports.ts src/services/api/pluginImports.test.ts src/types/pluginImport.ts src/features/plugins/PluginsPage.tsx src/features/system/SystemPage.tsx src/features/updatePages.integration.test.tsx src/features/plugins/PluginZipImportModal.tsx src/features/plugins/PluginZipImportModal.test.tsx src/features/settings/SystemUpdateSection.tsx src/features/settings/SystemUpdateSection.test.tsx
node /app/node_modules/eslint/bin/eslint.js src/features/pageRefresh.test.tsx
```

每项均在 `mx-cpamp-web-tests:bps-tools-images-20261111` 镜像中以 `--pull never --network none` 执行；候选 `web/src`、`vite.config.ts`、`tsconfig.json` 只读挂载，静态检查另挂 `tsconfig.node.json`，ESLint 另挂仓库 `eslint.config.js`。保留镜像内 `/app/node_modules`。
