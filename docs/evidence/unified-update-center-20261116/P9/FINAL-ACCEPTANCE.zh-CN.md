# 插件 ZIP 与网页内核更新本地验收

日期：2026-11-16（CST）。责任人：CPA P9。

## 结论

**本地候选验收通过；入仓、发布和首次现场接入待授权。** 正式网页、Manager 管理代理、CPA 安装服务、插件宿主与独立更新任务已贯通。本记录不作为供应商业务或生产维护回执，整体 Goal 保持未完成。

## 代码输入

- 唯一候选：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`。
- HEAD：`9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`，另有未提交增量。
- 产品及部署候选：53 路径，19 修改、34 新增；逐路径状态、Git blob 和字节数见 `FINAL-CANDIDATE-FILES.json`。文档及证据在此范围外单独归档。
- component 输入：`../A/P12-BUILD-INPUT.json`，4638 文件。P9 核当前候选 component 增量均匹配 P12，差异数 0；S 已独立核完整集合与 contract 对应关系。
- P07→P12 恰九文件变化，均属 Manager 精确代理与前端连接引用；CPA 无变化，继续使用 P07 构建。最终 Manager/web 使用 P12。
- S 审查：`../S/FINAL-LOCAL-REVIEW.json`、`../S/MANAGER-CONNECTION-REVIEW.json`，均 PASS。`deploy/production-wiring.contract.json` 已指向后者。
- 本轮未修改 schema、migration 或 SQL；未操作生产数据库。新增更新操作记录、备份及宿主登记由已有文件持久化机制承载。

## 实际浏览器验收

环境为本机隔离 internal Docker 网络，真实 Chrome/patchright、最终嵌入 web 的 Manager、正式 CPA、正式插件宿主与真实 Unix 宿主任务。账号和管理凭据均为专用合成数据；站点发行传输、Docker 生命周期及健康外边界为夹具。

| 范围 | 实际结果 | 证据 |
| --- | --- | --- |
| 插件 ZIP 预览及取消 | 正式上传预览；取消仅 DELETE 暂存，无安装请求 | `browser/zip-result.json`，7 项通过 |
| 插件 ZIP 确认安装 | 仅一次 install POST；实际库注册，活动版本 1.0.0 | 同上及 `LOCAL-HOST-OPERATION-SNAPSHOT.json` |
| 固定核心候选 | 未确认维护时执行禁用；站点 fixture、目标 v1.2.3；确认产生真实操作 ID | `browser/core-result.json` |
| Manager 真正重启 | Docker 宿主 PID 4130→6592；worker 前后均 PID 4663，存活检查通过 | `browser/manager-restarted.json`、`browser/core_restart_watch.ps1` |
| 原任务恢复 | 重新认证后五个 v8 请求均 GET 200；原 ID、目标/活动版本一致，无重复安装 | `browser/core-recovery-result.json` |
| 宿主完成记录 | 原操作 completed，活动指针、完成记录均 v1.2.3；worker proof 为 completed | `LOCAL-HOST-OPERATION-SNAPSHOT.json` |
| 视觉核对 | 原插件页预览弹窗、原系统页紧凑更新区域、成功状态与任务编号可见 | `browser/zip-preview.png`、`browser/core-completed-visible.png` |

任务编号：

- 插件：`plugin-d86bbdac-622e-473b-ade1-a9434c9fef8b`。
- 核心：`core-0e09ec67-0b0c-4ec5-8fd0-fcf6776e91f3`。

### 命令与退出码

- ZIP：隔离浏览器运行 `accept_ui.py` 的 zip 分支，工具 `1469c5`，`ZIP_BROWSER_RC=0`；原始输出 `browser/04-zip-acceptance.log`。
- 核心：`accept_ui.py` 的 core 分支及 `core_restart_watch.ps1`，工具 `ef5cf7` / `06783e`；独立 Manager 重启 rc=0。浏览器初段因重载后回到登录页而等待超时，退出 1，保留 `browser/05-core-restart-acceptance.log` 及对应截图。
- 只读恢复：`docker run --rm --pull never --network unified-update-preview-20261116 ... python3 -B /browser/recover_core_ui.py`，工具 `c12fa3`，`CORE_RECOVERY_RC=0`。仅重新认证、读取原操作，不重复维护。
- 可视截图：对原任务只读取景，工具 `7b25ba`，`SCREENSHOT_RC=0`。
- 输入及最终静态检查：`FINAL-CANDIDATE-FILES.json`、`FINAL-CHECK.json`。候选不匹配时停止交付；未以 HEAD 单独代表全部输入。

## 复用的有效证据

- A/P01–P09：ZIP 正负解析、正式管理路由及实际 C ABI 宿主、既有安装/启停语义、共享维护锁、宿主 29 项、首次接入 18 项、Go→Unix HTTP→独立 worker→实际 updater、bootstrap 3/3。
- A/P12：Manager 五项顶层专项、前端 API 与页面 20/20、TypeScript/ESLint、最终 web 和 Manager 构建均退出 0；真实 Manager→CPA 与直接 CPA 读取通过。
- CPA 继续引用 P07 构建。P9 已回读 P12 最终构建日志及 S 精确输入结论，没有重跑输入未变的专项、全量测试或构建。
- 原 S 席另以只读方式核对本次浏览器结果、原任务、完成记录、活动指针及 worker proof，结论一致，无本地验收阻断项。独立记录为 `../S/BROWSER-EVIDENCE-REVIEW.zh-CN.md`，未重复产品测试。

## 最终产物

- CPA：`C:/Users/子羽/.codex/tmp/unified-update-20261116/CLIProxyAPI`。
- Manager：`C:/Users/子羽/.codex/tmp/unified-update-20261116/cpa-manager-plus-bound`。
- 嵌入 web：`C:/Users/子羽/.codex/tmp/unified-update-20261116/web-seam/index.html`。
- 宿主 bootstrap：`C:/Users/子羽/.codex/tmp/unified-update-20261116/preview/mx-update-bootstrap.tar.gz`，17806 字节、六成员，输入未变，复用 P09。
- 使用说明：`docs/execution/UNIFIED-UPDATE-USER-GUIDE.zh-CN.md`；首次接入及恢复：`deploy/README.update-host.zh-CN.md`。

## 已知限制与下一责任人

- Manager 重启后连接引用失效；未记住凭据时需重新登录，再只读恢复原任务。本地验收不主张免登录自动恢复。
- 官方版本和本站发行在隔离环境按固定样本验证，外网发行下载及真实 Linux systemd/Compose 首次接入留待现场验收。
- 真实 BPS 0.5.58 目前仅静态包检查；供应商许可、OAuth 和真实推理未执行。同版同路径异字节更新保持写前拒绝，不宣称热替换可用。
- 本轮未回迁主仓、未提交、未推送、未部署、未操作线上账号或数据库；未将本机测试包作为正式发行。
- 2026-11-16 06:43 CST 已停止本轮独立 Manager 与组合预览容器，保留容器、合成数据和证据，没有停止任何线上服务。实际回执 `PREVIEW-STOP.json`。
- 下一责任人 P9：按授权组织 53 路径候选及配套文档的受控入仓/发布，再单独执行首次现场接入及维护。没有新增实现选择需要用户重复裁定；只需在实际写侧节点确认范围。
