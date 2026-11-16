# 入仓只读预检

日期：2026-11-16（CST）。责任人：A。

## 结论与当前边界

预检通过，待用户明确授权入仓。实际远端 main 与候选 HEAD 一致，53 个产品及部署路径相对 P9 最终清单零漂移，没有新增远端提交导致的公共文件合并冲突。当前索引为空。本轮唯一写入是本预检记录；没有暂存、提交、推送、分支切换、fetch、tag、工作流触发、生产维护或产品修改。

唯一候选：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`。

原验收仍以 `docs/evidence/unified-update-center-20261116/P9/FINAL-ACCEPTANCE.zh-CN.md`、`P9/FINAL-CHECK.json` 与 S 原审查回执为准。本记录是后续入仓预检，不替代或改写原验收。

## 真实远端与漂移证据

- 网络使用已确认代理 `http://127.0.0.1:7890`，仅 HTTPS；命令为 `git -c http.proxy=http://127.0.0.1:7890 ls-remote --symref https://github.com/nuotian4-spec/MX-CPAMP.git HEAD refs/heads/main`，exit 0，工具回执 `661ee4`。
- 真实远端 HEAD 指向 `refs/heads/main`，两者 SHA 均为 `9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`，等于候选 HEAD。
- `P9/FINAL-CANDIDATE-FILES.json` 的 53 路径逐项比较 Git blob、字节数、状态与路径集合：19 修改、34 新增；不匹配、额外、缺失均为 0；全部 `git hash-object` exit 0，工具回执 `b5b586`。
- 真实远端 SHA 与候选 HEAD 的提交差为 `0 / 0`，基线差异命令 exit 0；实际远端没有新的公共文件差异需要合并。
- 本地 `main` 与 `origin/main` 都仍为 `cae2b599e864153e95c241b3070a7336dcddfad0`，相对候选为 `0 / 181`；这些旧引用未刷新，不作为本轮远端事实依据，不回迁或触碰主仓。
- 原 P01–P12 测试、构建及 P9 最终验收证据保持不变，本轮没有重跑。

## GitHub 工作流触发边界

当前根 `.github/workflows/` 只有 `release.yml`，且候选对此无改动。第 3–13 行仅声明 `workflow_dispatch`，没有 push、pull_request、schedule 或 workflow_run 触发。对 main 或 codex 分支的普通 push 不会启动该仓库工作流的测试、构建、tag、Release 或生产部署。

手动 dispatch 要求固定版本及完整提交；第 24 行限定 main，第 60 行起的 publish 会构建三镜像、创建 tag 和 GitHub Release。第 128 行明确 Release 不执行服务器部署。后续入仓不触发 dispatch、不创建 tag、不调用部署入口。核对回执：`9cb921`。此结论覆盖仓库内工作流文件；没有将外部 Webhook 或组织级服务声明为已核验。

## 精确纳入与排除

产品及部署纳入 53 路径，沿用 P9 最终清单。文档纳入原 99 份，加 S 本轮材料审查和本记录，共 101 份；合计 154 路径。S 的 `RELEASE-MATERIALS-REVIEW.zh-CN.md` 已给出 PASS，范围内没有必须排除项；P9 已确认保留全部本轮材料。历史冻结源码作为明确标识的审查输入保留，不替代产品源码，也不向外部历史递归扩展。

原 99 份材料内排除项：无。范围外继续排除本机预览运行目录、容器数据、编译产物、浏览器 storage/cookie/profile、真实账号和站点私有配置、数据库和实际备份内容、供应商 ZIP/SO。文件内的外部路径引用不是复制该材料的授权。

所有清单项均为相对于上述唯一候选根的精确 Git 路径；禁止目录级盲目暂存或 `git add -A`。

## 获准后的最小执行路线

1. P9 转达本轮用户的明确入仓授权和目标远端分支；在此之前保持零暂存、提交和推送。
2. 仅在当前候选创建未占用的 `codex/unified-update-center-20261116` 工作分支，以已经核实的候选 HEAD 为基线；不切换到陈旧本地 main，不回迁主仓。
3. 按本记录的精确路径逐项暂存，预期 154 路径，即 19 修改、135 新增。只核暂存路径集合、`git diff --cached --check` 和本轮数据库路径命中，不重复原测试或构建。
4. 提交后，推送前重新通过 7890 代理执行目标远端 `ls-remote`；远端 main 若变化则暂停写侧，先报告差异。只向用户授权的目标 ref 做正常快进推送，不强推、不发布 tag、不手动触发工作流。
5. 推送后用真实 `ls-remote` 核目标 SHA，并由 P9 收口实际入仓结果；发布及首次现场接入另行授权。

下一责任人：P9 确认用户授权与目标 ref；A 在收到明确转达后按精确写域执行。

## 产品及部署：53 路径
```text
A components/cpa/internal/api/handlers/management/core_updates.go
M components/cpa/internal/api/handlers/management/handler.go
A components/cpa/internal/api/handlers/management/plugin_imports_test.go
A components/cpa/internal/api/handlers/management/plugin_imports.go
M components/cpa/internal/api/handlers/management/plugin_store_release_test.go
M components/cpa/internal/api/handlers/management/plugin_store_test.go
M components/cpa/internal/api/handlers/management/plugin_store.go
A components/cpa/internal/api/handlers/management/plugin_update_backup.go
A components/cpa/internal/api/handlers/management/update_operations.go
M components/cpa/internal/api/server_management_v8.go
A components/cpa/internal/api/server_update_integration_test.go
M components/cpa/internal/pluginhost/snapshot.go
A components/cpa/internal/pluginstore/import_test.go
A components/cpa/internal/pluginstore/import.go
M components/cpa/internal/pluginstore/install_test.go
M components/cpa/internal/pluginstore/install.go
M components/cpa/internal/pluginstore/manifest.go
A components/cpa/internal/updatestate/lock_unix.go
A components/cpa/internal/updatestate/lock_windows.go
A components/cpa/internal/updatestate/store_test.go
A components/cpa/internal/updatestate/store.go
M components/cpamp/apps/manager-server/internal/http/controller/proxy/handler.go
M components/cpamp/apps/manager-server/internal/http/middleware/cors.go
M components/cpamp/apps/manager-server/internal/http/router/router.go
A components/cpamp/apps/manager-server/internal/httpapi/update_proxy_test.go
M components/cpamp/apps/manager-server/internal/service/credentialconcurrency/service.go
M components/cpamp/apps/manager-server/internal/service/proxy/service.go
A components/cpamp/apps/manager-server/internal/service/proxy/v8_updates.go
M components/cpamp/apps/web/src/features/pageRefresh.test.tsx
M components/cpamp/apps/web/src/features/plugins/PluginsPage.tsx
A components/cpamp/apps/web/src/features/plugins/PluginZipImportModal.module.scss
A components/cpamp/apps/web/src/features/plugins/PluginZipImportModal.test.tsx
A components/cpamp/apps/web/src/features/plugins/PluginZipImportModal.tsx
A components/cpamp/apps/web/src/features/settings/SystemUpdateSection.module.scss
A components/cpamp/apps/web/src/features/settings/SystemUpdateSection.test.tsx
A components/cpamp/apps/web/src/features/settings/SystemUpdateSection.tsx
M components/cpamp/apps/web/src/features/system/SystemPage.tsx
A components/cpamp/apps/web/src/features/updatePages.integration.test.tsx
A components/cpamp/apps/web/src/services/api/pluginImports.test.ts
A components/cpamp/apps/web/src/services/api/pluginImports.ts
A components/cpamp/apps/web/src/types/pluginImport.ts
A deploy/mx-update-host-install.py
A deploy/mx-update-host.py
A deploy/mx-update-host.service
A deploy/mx-update-operation@.service
M deploy/mx-update.py
A deploy/package-update-host.py
M deploy/production-wiring.contract.json
A deploy/README.update-host.zh-CN.md
A deploy/test_mx_update_host_install.py
A deploy/test_mx_update_host.py
A deploy/test_package_update_host.py
A deploy/testdata/update-host-fixture.py
```

## 文档及证据：101 路径

```text
docs/evidence/unified-update-center-20261116/A/INGEST-PREFLIGHT-20261116.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P01-PARSER-NEGATIVE.log
docs/evidence/unified-update-center-20261116/A/P01-PARSER-RECEIPT.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P02-PLUGIN-HTTP-TEST.log
docs/evidence/unified-update-center-20261116/A/P03-FRONTEND-TOOL-RECEIPTS.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P04-V8-SYNTHETIC-ROUTER.log
docs/evidence/unified-update-center-20261116/A/P05-HOST-TEST-OUTPUT.txt
docs/evidence/unified-update-center-20261116/A/P05-HOST-UNITTEST.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P06-INSTALL-TEST-OUTPUT.txt
docs/evidence/unified-update-center-20261116/A/P06-INSTALL-UNITTEST.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P07-BUILD-INPUT.json
docs/evidence/unified-update-center-20261116/A/P07-CPA-MANAGER-BUILD.log
docs/evidence/unified-update-center-20261116/A/P07-FINAL-BUILD.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P07-MANAGER-LOCAL-CACHE-BUILD.log
docs/evidence/unified-update-center-20261116/A/P07-WEB-BUILD.log
docs/evidence/unified-update-center-20261116/A/P08-CORE-JOINT.log
docs/evidence/unified-update-center-20261116/A/P09-BOOTSTRAP.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P10-LOCAL-PREVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P11-BUILD-INPUT.json
docs/evidence/unified-update-center-20261116/A/P11-MANAGER-SEAM-BUILD.log
docs/evidence/unified-update-center-20261116/A/P11-MANAGER-SEAM-GREEN.log
docs/evidence/unified-update-center-20261116/A/P11-MANAGER-SEAM-RED.log
docs/evidence/unified-update-center-20261116/A/P12-BUILD-INPUT.json
docs/evidence/unified-update-center-20261116/A/P12-CONNECTION-REF-RED.log
docs/evidence/unified-update-center-20261116/A/P12-LIVE-MANAGER-CPA.log
docs/evidence/unified-update-center-20261116/A/P12-MANAGER-CONNECTION-GREEN.log
docs/evidence/unified-update-center-20261116/A/P12-MANAGER-FINAL-BUILD.log
docs/evidence/unified-update-center-20261116/A/P12-MANAGER-SEAM.zh-CN.md
docs/evidence/unified-update-center-20261116/A/P12-WEB-CONNECTION-TEST-BUILD.log
docs/evidence/unified-update-center-20261116/P9/browser/04-zip-acceptance.log
docs/evidence/unified-update-center-20261116/P9/browser/05-core-restart-acceptance.log
docs/evidence/unified-update-center-20261116/P9/browser/06-core-readonly-recovery.log
docs/evidence/unified-update-center-20261116/P9/browser/07-core-detail-screenshot.log
docs/evidence/unified-update-center-20261116/P9/browser/accept_ui.py
docs/evidence/unified-update-center-20261116/P9/browser/core_restart_watch.ps1
docs/evidence/unified-update-center-20261116/P9/browser/core-completed-detail.png
docs/evidence/unified-update-center-20261116/P9/browser/core-completed-visible.png
docs/evidence/unified-update-center-20261116/P9/browser/core-confirmation.png
docs/evidence/unified-update-center-20261116/P9/browser/core-confirmed.json
docs/evidence/unified-update-center-20261116/P9/browser/core-failure.png
docs/evidence/unified-update-center-20261116/P9/browser/core-recovery-result.json
docs/evidence/unified-update-center-20261116/P9/browser/core-result.json
docs/evidence/unified-update-center-20261116/P9/browser/manager-restarted.json
docs/evidence/unified-update-center-20261116/P9/browser/recover_core_ui.py
docs/evidence/unified-update-center-20261116/P9/browser/zip-installed.png
docs/evidence/unified-update-center-20261116/P9/browser/zip-preview.png
docs/evidence/unified-update-center-20261116/P9/browser/zip-result.json
docs/evidence/unified-update-center-20261116/P9/FINAL-ACCEPTANCE.zh-CN.md
docs/evidence/unified-update-center-20261116/P9/FINAL-CANDIDATE-FILES.json
docs/evidence/unified-update-center-20261116/P9/FINAL-CHECK.json
docs/evidence/unified-update-center-20261116/P9/FINAL-DIFF-CHECK.log
docs/evidence/unified-update-center-20261116/P9/LOCAL-HOST-OPERATION-SNAPSHOT.json
docs/evidence/unified-update-center-20261116/P9/PREVIEW-STOP.json
docs/evidence/unified-update-center-20261116/S/BROWSER-EVIDENCE-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/api/handlers/management/core_updates.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/api/handlers/management/handler.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/api/handlers/management/plugin_imports.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/api/handlers/management/plugin_store.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/api/handlers/management/plugin_update_backup.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/api/handlers/management/update_operations.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/api/server_management_v8.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/pluginstore/install.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/updatestate/lock_unix.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpa/internal/updatestate/store.go
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpamp/apps/web/src/features/system/SystemPage.tsx
docs/evidence/unified-update-center-20261116/S/core-host-input/components/cpamp/apps/web/src/services/api/pluginImports.ts
docs/evidence/unified-update-center-20261116/S/core-host-input/deploy/mx-update-host.py
docs/evidence/unified-update-center-20261116/S/core-host-input/deploy/mx-update.py
docs/evidence/unified-update-center-20261116/S/core-host-input/INPUT.json
docs/evidence/unified-update-center-20261116/S/CORE-HOST-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/FINAL-LOCAL-REVIEW.json
docs/evidence/unified-update-center-20261116/S/FINAL-LOCAL-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/host-registration-input/INPUT.json
docs/evidence/unified-update-center-20261116/S/host-registration-input/mx-update-host-install.py
docs/evidence/unified-update-center-20261116/S/host-registration-input/mx-update-host.py
docs/evidence/unified-update-center-20261116/S/host-registration-input/mx-update-host.service
docs/evidence/unified-update-center-20261116/S/host-registration-input/mx-update-operation@.service
docs/evidence/unified-update-center-20261116/S/host-registration-input/test_mx_update_host.py
docs/evidence/unified-update-center-20261116/S/HOST-REGISTRATION-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/INSTALL-LIFECYCLE-FIX-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/INSTALL-LIFECYCLE-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/lifecycle-fix-input/host.go
docs/evidence/unified-update-center-20261116/S/lifecycle-fix-input/INPUT.json
docs/evidence/unified-update-center-20261116/S/lifecycle-fix-input/install.go
docs/evidence/unified-update-center-20261116/S/lifecycle-fix-input/plugin_imports.go
docs/evidence/unified-update-center-20261116/S/lifecycle-fix-input/plugin_store.go
docs/evidence/unified-update-center-20261116/S/lifecycle-fix-input/plugin_update_backup.go
docs/evidence/unified-update-center-20261116/S/lifecycle-fix-input/update_operations.go
docs/evidence/unified-update-center-20261116/S/MANAGER-CONNECTION-REVIEW.json
docs/evidence/unified-update-center-20261116/S/MANAGER-CONNECTION-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/RELEASE-MATERIALS-REVIEW.zh-CN.md
docs/evidence/unified-update-center-20261116/S/zip-static-input/import_test.go
docs/evidence/unified-update-center-20261116/S/zip-static-input/import.go
docs/evidence/unified-update-center-20261116/S/zip-static-input/pluginImport.ts
docs/evidence/unified-update-center-20261116/S/ZIP-STATIC-REVIEW.zh-CN.md
docs/execution/BPS-PLUGIN-UPDATE-20261116.zh-CN.md
docs/execution/CPA-UPSTREAM-V8.0.73-20261116.zh-CN.md
docs/execution/UNIFIED-UPDATE-CENTER-20261116.zh-CN.md
docs/execution/UNIFIED-UPDATE-IMPLEMENTATION-20261116.zh-CN.md
docs/execution/UNIFIED-UPDATE-PACKAGE-MAP-20261116.zh-CN.md
docs/execution/UNIFIED-UPDATE-USER-GUIDE.zh-CN.md
```
