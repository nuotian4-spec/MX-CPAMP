# CPA v8.1.7 升级基线核验

日期：2026-11-27（Asia/Shanghai）。范围：本地只读基线调查；未运行测试、构建、模型或账号请求，未联网拉取，未改产品。

## 建议起点

使用已整合 CPA v8.0.80 且包含并发持久化修复的本站提交 `b9ea2dc4c6bb377d31143970a6df1084297e4b09` 作为最低升级起点，主协调席核远端 main 后若存在更新则采用更新的已确认提交。从该基线新建候选，将官方 v8.0.80→v8.1.7 增量与本站定制三方整合。当前旧管理工作树及 D:/MX-CPAMP 的大量 dirty 保持原状。

本地 `origin/main` 也指向 b9ea2dc，但这是 tracking ref，本席未联网核远端当前值。不能以此断言 11 月 27 日远端没有新提交。

## 三份本地事实

| 位置 | 当前事实 | 升级用途 |
|---|---|---|
| C:/Users/子羽/.codex/worktrees/bps-management-20261115/MX-CPAMP | detached HEAD `c4f693a9b97ab7b5643d6178bec03f9ad5b95094`，大量候选改动；UPSTREAMS仍只记v8.0.63；CPA go.mod使用moego alpha.1 | 保留旧任务现场，不以此旧HEAD或dirty目录覆盖后续发行 |
| D:/MX-CPAMP | `codex/openai-bps-login`，同一旧HEAD及大量dirty；UPSTREAMS仍记v8.0.63 | 历史事实/证据目录，不是本轮最新CPA整合源 |
| C:/Users/子羽/.codex/worktrees/cpa-core-8080-20261121/MX-CPAMP | 干净，分支`codex/bps-concurrency-20261121`，HEAD b9ea2dc；父提交`58cdb174d37ee2e80fd976a0e270de869ef2e1fa`完成v8.0.80整合；go.mod使用moego alpha.4 | 可复用的最新已核本地提交基线 |

当前目录中存在SIWC文件，不等于当前目录已包含v8.1.7或最新本站修复。内核来源以整合记录、固定上游输入和源码差异共同判断。

## 已有上游与验证缓存

- 官方 v8.0.80：`f5c5681a8361c9da53e93c60b6507816eee7468b`，tree `36bef95f17cb09c90b1e48174a32832e6fcc19c7`。
- 原始源码缓存：`C:/Users/子羽/.codex/artifacts/cpa-core-8080-20261121/upstream-v8.0.80/router-for-me-CLIProxyAPI-f5c5681`，普通导出目录，不带`.git`。
- 同级已有 `upstream-v8.0.80.tar.gz`、`upstream-v8.0.80-tree.json`、`THREE-WAY-FACTS.json`；v8.0.80无需重下载。
- 上次有效验证：该artifacts目录的 `VALIDATION.md`、`VALIDATION.json`、`REVIEW.md`；发布回执为 `PUBLICATION.json`，绑定本站 `v2026.11.21-rc.1` / `58cdb17`。这些是既有发行证据，不作为本轮v8.1.7测试通过证明。
- b9ea2dc并发修复另有 `C:/Users/子羽/.codex/artifacts/bps-concurrency-20261121/fix-validation/`，具体合同见新树 `docs/execution/BPS-CONCURRENCY-FIX-20261121.zh-CN.md`。

## 上游追踪与构建版本

CPA以普通目录进入单仓库，不是submodule。原始v8.0.63导入保留在 `be8fae57583f75237c5c90be1924eeab84de0f0f`；v8.0.80整合提交及其 `docs/UPSTREAMS.md` 附加了新来源，未改写旧导入含义。

构建显示的Version由 `deploy/package-release.py:92` 的 `build_commands` 将本站发行版本作为Docker build arg `VERSION` 注入；CPA Dockerfile第14、20行通过ldflags写`main.Version/main.Commit/main.BuildDate`，`components/cpa/cmd/server/main.go:57`再赋给buildinfo。默认本地buildinfo为dev/none/unknown。页面或二进制Version可能是本站发行号，不能把修改VERSION字符串当作上游源码升级。

## 必须保留的定制边界

- v8.0.80整合的BPS/Kilo executor、通道scope、continuation/no-replay、动态插件维护与调度、模型发现、观测链和配置持久化。
- b9ea2dc的凭证并发覆盖修复：刷新/Prepare/文件重载沿管理员设置，清空不被旧插件字段恢复，写盘失败不发布运行态；涉及管理字段、pluginhost/auth_provider、watcher、filestore、conductor lifecycle/selection、credential_concurrency、metadata_merge和auth types共10个组件文件。
- 现有生产接线合同 `deploy/production-wiring.contract.json` 需要随实际组件源码映射更新；不能直接用官方二进制替换本站内核。
- Manager、Worker产品输入不随内核升级自动升级上游；SIWC调查是独立结论，是否补UI由主协调席按用户请求边界裁定。

## 最小验证建议

先以v8.0.80与目标官方tag的真实差异确定受影响包及本站交集，再选择专项。重点是插件scheduler重配置/热更新/目录错误路由与Gemini后续parts排序，以及跨度中实际改变的认证/请求边界；不要只测v8.1.6→v8.1.7的三个提交后宣称覆盖整个8.0.80→8.1.7升级。

动态库支持需使用Linux CGO构建与隔离插件加载边界；CGO=0的可执行文件只证明编译，不能证明cpa-bps动态库可用。复用依赖未变的已有证据，统一最终build一次。上次全模块保留已有失败，若本轮遇到同项应核固定基线证据，不扩大成历史返工。

## 当前实施基线

P9 已核远端 `HEAD` 与 `main` 均为 `b9ea2dc4c6bb377d31143970a6df1084297e4b09`。本轮实施树为 `C:/Users/子羽/.codex/worktrees/cpa-core-v817-20261127/MX-CPAMP`，分支 `codex/cpa-core-v817-20261127`。

官方 v8.1.7 输入已固定为 `47f18e4d82bd72f7d2b5ecf5ede93e4837a4625e`；相对 v8.0.80 共 679 项变化，其中 63 项与本站定制相交。原有两个脏工作区保持原状，未作为本轮源码输入。

本轮实现、专项验证、独立审查和生产发布状态分别记录；当前生产版本未查询。

下一责任人：CPA 实施席完成整合与受影响验证；独立审查席核最终接缝与证据；P9 裁定本地候选交付。
