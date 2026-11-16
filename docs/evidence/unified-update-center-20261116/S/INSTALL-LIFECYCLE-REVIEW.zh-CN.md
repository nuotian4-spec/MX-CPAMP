# 第二块：安装任务、持久所有者与确认契约审查

日期：2026-11-16 CST。S 档。基线 HEAD：`9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`。候选根：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`。

结论：**关键安装/锁路径尚未通过。** 下列是本轮读取的实际增量问题与一个未完成合同；原 A 已接收前两项并开始修正，本文不将修复意向计为闭环。行号对应本轮首读版本，后续按具体修复差异复核。

## 需修正的实际问题

### S2-01：激活失败恢复整份旧配置会抹掉其它已保存设置（P1）

- 位置：`components/cpa/internal/api/handlers/management/update_operations.go:177-188`；同文件 `:44-54`；`config_basic.go:223,375-382`；`handler.go:492-521`。
- 可达条件：ZIP/商店安装捕获 previous 后，另一正常管理请求修改 debug、proxy-url 或 api-keys；这些路由不在当前门禁中，仍会保存并发起全配置 reload。候选插件激活失败后，代码用 previous 整体替换 h.cfg 并保存，覆盖已经成功保存的其他设置。
- 最小修正：恢复仅限本操作的插件配置字段，并让实际共享配置/reload 写路径参加同一后端门禁；不靠旧全局快照恢复。恢复后读取真实活动身份，回执不沿用恢复前观察值。
- 状态：A 已接收并确认按插件字段恢复，待复核代码和针对性回执。

### S2-02：合法插件 ID 子串可绕过门禁（P1）

- 位置：`update_operations.go:49-53`；既有路由 `components/cpa/internal/api/server_management.go:38-42`。
- 可达条件：合法 ID `imports-tools` 的 `/plugins/imports-tools/config` 被 imports 前缀排除；合法 ID `quota-tools` 的 `/plugins/quota-tools/enabled` 被 Contains("/quota") 排除。它们是既有公开插件管理路径，能够在另一安装/reload 持锁时修改配置。
- 最小修正：按完整路由或明确路径段匹配真正的 imports 与 quota 子资源；不得用插件 ID 内子串作为绕过依据。
- 状态：A 已接收并确认改精确路径段，待复核。

### S2-03：商店安装将许可缺证写成无需许可（P2）

- 位置：`plugin_store.go:356` 调用 bindPluginUpdate(..., false, ...)；`update_operations.go:146,170-174`。
- 可达条件：已有商店直接更新需许可的 cpa-bps，活动注册成功后 因 false 分支直接得到 `completed` 和 `license_status=not_required`。静态 ZIP 预览具备许可声明，但旧商店入口不能据此永久视为免许可。
- 最小修正：传递已有许可事实；未知时保留 unverified，不以缺少字段作“无需许可”证据。不补真实许可网络或模型请求作为本地测试前提。
- 状态：已告 A，待修复/说明。

### S2-04：成功删除插件被套用安装判据记为失败（P2）

- 位置：`plugins.go:428-444`；`update_operations.go:157-169`。
- 可达条件：插件全局启用时正常 DELETE 删除该插件的配置并 reload。新终态检查把配置不存在当作 enabled=true；插件实际已卸载 registered=false，又触发“候选未成为活动注册插件”，导致删除返回成功而持久任务显示 failed。
- 最小修正：按操作类型核实删除应不再注册、安装应匹配目标身份；保留真实状态，不把成功删除视为失败安装。
- 状态：已告 A，待修复。

## S2-05：明确不兼容结果的来源判定尚未完成

`plugin_imports.go:140-158` 当前只把精确配对 result=registered 标为 verified，其余统一 unverified；函数没有 incompatible 返回路径。`:246-252` 虽硬拒 incompatible，却只能消费到未实现的负判定。对有明确负配对结论的输入，需要按已定事实来源识别并硬拒，不能借 confirm_unverified 覆盖。若负结果合同尚未定义，应保留为未完成项，非新增逐包白名单或资格框架。已向 A 同步；尚无负配对执行证据。

## 已确认成立的当前实现

- `updatestate/store.go` 使用登记文件锁和持久 active.json：非终态所有者阻止下一写者；Detach 保留 recovery_required，Finish 先发布终态再移除 active 并解锁。Unix 路径使用与既有 updater 同类 flock，不是进程内 mutex。
- `plugin_imports.go` 的上传只静态解析/私有暂存，UUID 标识固定候选；安装要求 confirm，未知兼容和许可联网另设逐次确认，且确认不改 compatibility。确认后持锁复核 baseline，安装任务脱离 HTTP request cancellation；不直接执行包脚本。
- 原插件启停值在 `enablePluginConfigLocked` 中保留；`handler.go` 把操作上下文带到异步 reload，HTTP 返回不立即释放已排定任务。
- `PluginZipImportModal.tsx` 以 connectionScope 作为会话 key，失效旧请求/清勾选；确认绑定当前 preview.id；安装开始后关闭不发取消，查询错误只重查同任务。installed 接受结果不直接视为成功，需查询终态；许可异常和保持停用分别展示。

## 证据和未验边界

复用静态包首审，不重读未变化 import.go，不重跑 A 正向命令或前端 20 项。已读 `A/P01-PARSER-RECEIPT.zh-CN.md` 的实际输出转录：正向静态专项退出码 0；范围与旧 S 结论一致。前端 20/20 Docker 结果本轮由 P9 提供，未把该结果扩为真实 Router/宿主验证。A 正在处理 ojson 序列化与 management 编译；本文未将已知处理中编译问题另立阻断。

整体 Router 接入、所有同文件写的后端门禁、跨宿主 updater 的持久所有者一致性、必要 CPA 重启接续、恢复入口、核心 Unix socket/systemd 固定目标执行仍在实施或缺实际链路证据。Go 两个 Store 实例单测不等同已验证真实 Go/Python 跨进程、重启接续或生产权限。

数据库动作：无 schema/SQL/migration。新增持久化动作有私有 imports 归档/预览/配置备份、operations 记录、active.json；实际安装会写版本化插件库及产品 config.yaml。失败恢复会改产品配置，因此 S2-01 必须修正后再放行此路径。

当前责任人：原 A 修正上述问题并交付同输入专项证据。下一责任人：原 S 只复核修复差异，P9 保持安装链/统一互斥未验证状态。S 仅写本证据，无产品修改、网络许可、模型调用或服务器动作。