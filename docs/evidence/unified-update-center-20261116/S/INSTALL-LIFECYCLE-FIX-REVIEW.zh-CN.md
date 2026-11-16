# 安装生命周期五项修复复核

日期：2026-11-16 CST。S 档。结论：**原 S2-01 至 S2-05 五项代码修复成立；原问题代码审查收口。整体安装链尚未放行，同版覆盖新增发现 S3-01 待修。**

候选：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`。实核 HEAD：`9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`。本次工作树精确输入保存在 `S/lifecycle-fix-input/`；INPUT.json 列来源与快照。下列行号以本轮读取版本为准。未审 core 正在实施部分。

## 五项修复结论

| 原项 | 代码结论 | 实际修复位置与剩余证据范围 |
| --- | --- | --- |
| S2-01 整份旧配置回写 | 通过 | `management/update_operations.go:211-242,253-264` 仅恢复 `Plugins.Configs[id]`，恢复后重新读活动版本、路径和注册状态，不匹配则保留 recovery_required；`plugin_imports.go:381-385` 与 `plugin_store.go:349-357` 保存失败也只恢复插件字段。不存在原来的全局 h.cfg=previous 回写。失败恢复补跑 PASS 由 P9报告，当前 A 落盘日志仍为首次失败版本，待补原输出位置，不复跑。 |
| S2-02 插件 ID 子串绕锁 | 通过 | `update_operations.go:51-65` 按路径段识别 v8 imports 与精确 quota 子资源；`imports-tools`、`quota-tools` 仍进入门禁。已有 `TestUpdateGuardUsesExactPluginPathSegments` 日志 PASS。此项不等同所有管理写入口/跨宿主锁全链已验。 |
| S2-03 商店许可误报 | 通过 | `plugin_store.go:362` 对 cpa-bps 传必需许可；`update_operations.go:143-144,168-170,202-207` 其余缺证保留 unknown，活动后 license_unverified，不再把 false 直接写成 not_required。未调用真实许可接口，不据此证明许可有效。 |
| S2-04 删除成功误记失败 | 通过 | `update_operations.go:97-100` 由 DELETE 的真实路由标记 remove；`:184-190` 以不再注册作为删除成功条件。已有 `TestUpdateDeleteDoesNotRequireRemovedPluginRegistration` 日志 PASS，但该测试直调完成函数，不扩称完整删除 Router/宿主链通过。 |
| S2-05 不兼容负判定 | 通过 | `plugin_imports.go:170-179` 对同插件/版本/平台/库摘要/host commit 的 incompatible 结论产生硬拒状态；`:135-138,275-277` 不可安装，confirm_unverified 不覆盖该判定。未知仍走本次显式确认，不变成已验证。已有负配对专项日志 PASS。 |

## 备份兼容检查

ZIP `plugin_imports.go:343,366-369` 和商店 `plugin_store.go:270-280` 已共用 `preparePluginBackup` 与安装器 `BeforeWrite(targetPath)`。`plugin_update_backup.go:12-50` 在实际站点状态目录按操作 ID 保留配置、活动库与将被覆盖的旧库，记录原路径/版本及 replaced-files；不是测试 stub 或另一套插件安装器。`:73-99` 按记录恢复旧库字节；最终恢复检查读取宿主真实活动身份。

这些代码证明备份与恢复接线存在，不证明所有同版覆盖状态已经正确。当前发现：

### S3-01：活动同路径、同版本但不同字节会误报候选已运行（P1）

- 位置：`components/cpa/internal/pluginstore/install.go:274-300,576-578`；`components/cpa/internal/pluginhost/host.go:287-305`；`management/update_operations.go:176-207`。
- 可达条件：Linux 上已加载插件，再通过支持的 ZIP 或商店入口导入同 ID/同版本、不同字节的包。安装器只对 Windows 已加载覆盖拒绝；Linux 覆盖同路径后，宿主只有路径变化才替换 loadedPlugin，相同路径继续使用旧实例。终态只比版本/路径/registered，因此旧实例也满足“本次候选活动”的判据；声明无许可时可显示 completed，需许可时也会误称新版已活动。
- 旧字节备份保全了恢复材料，但没有改变运行实例，也不能证明候选字节已经执行。该路径属于现有安装接口支持的同版覆盖，不依赖符号链接或毫秒竞态。
- 最小修正：在写入前拒绝活动同路径不同字节，明确需要受控维护/重启；同字节 skip 可保留。若本批衔接宿主重启，则按已确认范围保留原任务所有者直到新运行实例验证，不新增加载框架。
- 状态：P9 已裁定本批在活动覆盖前明确拒绝同路径不同字节，原工件不动，不改版本/provider 绕缓存；宿主维护接线尚未实现前保留限制。待 A 提交该修复输入。原五项不因此重新打开；该新增点独立跟踪。

## 执行证据口径

已读 `A/P02-PLUGIN-HTTP-TEST.log`：首次 HTTP 7 项中 5 PASS，取消样本和失败恢复样本 FAIL，HTTP_EXIT=1；不将其写成全过。P9通知 management 编译 exit0、updatestate 两项 PASS、失败恢复补跑 PASS，取消夹具已修待最终日志。未为补证据复跑该组；已向 A 请求已有补跑输出位置。未变化静态解析与前端专项继续引用，不重复运行。

数据库动作：无 schema/SQL/migration。持久化涉及操作记录、导入暂存、配置/旧库备份、版本化库和 config.yaml；恢复不回灌数据库。S 仅写报告与精确输入快照，未写产品、未读取真实凭据、未联网许可、未调用模型或部署。

下一可验面：A 处理 S3-01 并补已有最终日志后，核真实 Router → 共用安装器 → 宿主注册/失败恢复 → 操作终态，以及同一路径实际旧库备份；随后再接 core 的宿主任务/跨进程所有者验证。组件与辅助函数通过不代替完整安装链通过。当前责任人原 A，下一复核人原 S，P9更新原五项代码收口及剩余边界。
