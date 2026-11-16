# ZIP 静态规范化首块审查

日期：2026-11-16 CST。审查档位：S。结论：当前静态解析增量未发现需返修的代码问题；仅此分项可继续接入，不构成 ZIP 安装或统一更新交付通过。

## 输入与范围

- 候选：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`；HEAD 基线 `9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`。
- 实际审查：`components/cpa/internal/pluginstore/import.go:1-228`、`import_test.go:1-65`；前端契约 `components/cpamp/apps/web/src/types/pluginImport.ts:1-31`。
- 只读直接依赖：`pluginstore/install.go` 的路径/普通文件/库扩展名 helper，`registry.go` 的 ID/version 校验，`version.go` 的版本规范化。
- 对照已保存原包 `D:/MX-CPAMP/docs/evidence/bps-plugin-update-0.5.58-20261116/P9/BINARY.json`：版本 0.5.58、linux/amd64、许可要求和 17 项字符串摘要清单，库路径 `plugin/dist/cpa-bps.so`。
- 本次读到的三份增量原文保存在同目录 `zip-static-input/`，标识此次审查输入，不覆盖后续修改。

## 结论

1. 路径与归档边界：规范化后的重复项、越界/绝对路径、反斜线、冒号及非常规文件被拒绝；限制压缩输入 64 MiB、最多 256 项、累计声明展开量 128 MiB，实际读取另有大小上限及 ZIP 读取错误处理。读取逻辑不向归档中的路径写文件。
2. 身份与平台：唯一 manifest/候选库、清单摘要绑定、ID/version 文件名匹配、运行平台与 ELF 机器号相符；本次 BPS 缺省 ID 按指定路径补齐。原包清单与目录结构受该路径支持。这里只确认静态声明和字节匹配，不确认发行者身份、实际宿主注册或 ABI 可用。
3. 输出保真：只将原库字节规范化为根目录 .so；不执行附带脚本/程序，不输出整包面板，也不改现有产品文件。现有正向夹具核库字节完全相同且输出只有一个 .so。
4. 验证范围：A 报告命令 `go test ./internal/pluginstore -run '^TestPrepareImportArchive' -count=1`，Go 1.26.10 缓存、GOPROXY=off、退出码 0；S 未重复执行。当前测试文件仅一个正向惰性 ELF 样本，因此不将其计为损坏包、错平台、重复/歧义库及大小边界的执行证据，更不计为实际加载证据。已向 A 索取原命令日志与输入回执位置，后续由该证据引用补齐。

## 余下接口与权限结论

本块没有管理 Router、上传暂存/取消、确认安装、保留停用、后端跨进程互斥或核心宿主任务实现；这些不因静态函数通过而提前放行。A 正在实施的块不纳入本次代码结论。

数据库/持久化动作：本块无数据库/schema/migration 或产品持久化写入；只返回内存中的规范化归档。S 本轮仅写此证据与输入快照，无产品修改、测试执行、真实账号、模型或服务器动作。

当前责任人：原 S 完成本块限定审查。下一责任人：原 A 提交管理 API/安装终态/互斥块及同输入专项回执；P9 仅更新静态分项，不更新整条导入为已完成。