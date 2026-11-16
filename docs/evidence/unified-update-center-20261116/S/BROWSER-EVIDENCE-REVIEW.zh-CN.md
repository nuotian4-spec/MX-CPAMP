# 浏览器与 Manager 重启证据核对

## 结论

本地验收证据一致，未发现阻断项。本记录固化已经完成的只读核对，不新增调查、测试、构建或容器重启；P12 产品源码保持冻结。

## ZIP 安装

原任务 `plugin-d86bbdac-622e-473b-ade1-a9434c9fef8b` 的浏览器单次安装请求返回 202，后端记录为 `completed`、活动版本 `1.0.0`、`registered=true`，与页面和 CPA 请求日志一致。取消分支删除暂存，没有发出安装请求。插件为 gcc 编译的合成本地插件，不代表供应商许可或模型能力验收。

## Manager 重启与 worker 连续性

Manager 实际进程 PID 从 `4130` 变为 `6592`。重启前后独立 worker PID 均为 `4663`，重启脚本在两侧执行进程存活检查。宿主 `updater-proof.json` 记录的 worker 同为 `4663`，阶段为 `completed`。

## 同任务只读恢复

原核心任务为 `core-0e09ec67-0b0c-4ec5-8fd0-fcf6776e91f3`，对应候选 `0e09ec67-0b0c-4ec5-8fd0-fcf6776e91f3`。

首次核心浏览器脚本因未记住凭据，页面重载后回到登录页，等待成功终态超时并以 exit 1 结束；该结果保留。后续脚本仅重新认证并读取原任务，没有再次执行更新，真实 exit 0。恢复阶段五个 v8 请求全部为 GET 200；页面显示原任务 `completed`，目标版本与活动版本均为 `v1.2.3`。页面、后端 operation、宿主 `completed.json` 与活动指针一致指向该版本和同一任务目录。

本证据证明 Manager 重启不终止该 worker，重新登录后能够只读恢复原任务；未验证无需重新登录的自动恢复。

## 已核证据与归档

本次已核原始证据位于 `C:/Users/子羽/.codex/tmp/unified-update-20261116/preview/browser/`：`zip-result.json`、`core-confirmed.json`、`manager-restarted.json`、`core-result.json`、`core-recovery-result.json`，04/05/06 日志、`core_restart_watch.ps1` 和 `recover_core_ui.py`；并核对相关 CPA 日志。

宿主记录按 `preview/host-ready.json` 指向的准确 fixture root `/tmp/mx-update-fixture-jim8ksgn` 只读核对，包括上述两个原任务 operation、核心任务完成记录、活动指针、worker 记录和 updater proof。

P9 已将 18 项原始材料归档至同候选 `docs/evidence/unified-update-center-20261116/P9/browser/`，宿主记录归档至 `docs/evidence/unified-update-center-20261116/P9/LOCAL-HOST-OPERATION-SNAPSHOT.json`。本次落盘不重新核验归档副本。

## 验收边界

Manager 重启、浏览器操作、worker 生命周期与文件系统记录为本地实际执行；站点 Docker 和发行传输为夹具。结论不扩展到真实站点 Compose、systemd、供应商许可、真实模型请求或生产现场。

## 下一责任人

P9：归档与最终本地验收。
