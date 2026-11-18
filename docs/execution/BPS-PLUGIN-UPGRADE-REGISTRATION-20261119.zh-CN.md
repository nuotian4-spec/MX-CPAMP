# BPS 插件升级：注册失败、恢复与报价

2026-11-19｜根因调查与修复方案。源码基线：[MX-CPAMP v2026.11.17-rc.3，`07dc177`](CPA-ATTACHMENT-OBSERVATION-RECEIPT-20261117.zh-CN.md#L97)。实施范围及实测结果见[本地验收回执](BPS-PLUGIN-UPGRADE-WORK-20261119.zh-CN.md)。术语沿用[项目词汇表](../../GLOSSARY.md)。

**隔离复现的根因：CPA 把自己的顶层安装元数据 `store` 一并传给插件，真实 0.5.85 拒绝该配置，首次注册报 `invalid_plugin_config`。** 仅移除传入配置中的 `store` 即可注册；许可接口随之从 404 变为 200，无需先激活。推荐在注册和重新配置共用的入口分离宿主字段，保留磁盘版本记录；再修复失败回退与页面状态。预算初估 **3–5 人日**。未读取生产日志，不能把截图当成同一首错的证明。

## 现场证据

| 来源 | 能确认什么 | 不能据此确认什么 |
| --- | --- | --- |
| 0.5.85 截图 | 卡片展示带版本的库路径，同时显示未注册、无菜单、许可未核实、激活框禁用。 | 该路径不是活动实例证明；截图没有该次宿主选库、打开库、注册或回退的首个错误。 |
| 0.5.62 旧现场附件 `e1c249…` | 任务落盘原因为 `Candidate activation failed; previous runtime identity was not restored`；查询却显示 `Operation interrupted…`。后续冷安装后插件已注册。 | 证明那次候选未按预期成为活动实例，恢复也未达到原身份；不证明初次失败由许可导致，也不证明 0.5.85 走了同一分支。冷安装成功不等于在线升级成功。 |
| 精确 RC3 镜像、真实 ZIP，冷启动对照 | 新版活动元数据报告 `0.5.85-dev`；许可 GET 返回 `200`、待激活。包版 `0.5.85` 与插件自报版本分别记录。 | 已注册不等于已激活；没有用真实授权码验证激活、OAuth 或推理。 |
| 同镜像，已注册 0.5.62 经网页后端 ZIP 接口升级至 0.5.85 | ZIP 预览 `201`、安装受理 `202`，随后首错 `plugin.register failed: invalid_plugin_config`；之后出现 `plugin_stopping`、任务 `recovery_required`，活动列表未注册，许可 GET 返回 `404`。 | `201/202` 是受理结果；`plugin_stopping` 与 `404` 均晚于首错。 |

生产侧只需补同一次操作的**脱敏**任务原始原因、所选库与原活动身份、首条宿主加载或注册错误，以判断是否与隔离实验一致。保留顺序和版本即可，无需授权码、许可文件正文、重装或线上激活。

### 同一配置只改一项

固定 CPA 镜像 `sha256:fc29a7faca9c4f662e0c026eba20f7a3ea4dc70d2b57345ddbafa0a24b56ab9b`、同一 0.5.85 库和文件路径。每组从新进程启动，排除进程内热替换或旧实例残留；其余插件配置不变。

| 注册配置的差异 | 插件注册 | 许可状态 GET |
| --- | --- | --- |
| 保留真实 ZIP 安装写入的完整顶层 `store` | `invalid_plugin_config`，未注册 | `404` |
| 只删除顶层 `store` | 已注册，元数据 `0.5.85-dev` | `200`，`license_activation_required` |
| 在无 `store` 的对照上仅加 `store: {}` | `invalid_plugin_config`，未注册 | `404` |

空映射已经能复现，无需归因于某个子键；本次真实安装记录没有 `release-tag`。实验回执：`$env:TEMP/bps85-rc3-evidence-52af0824aa/config-delta-receipt.json`。实验中的删除只用于鉴别原因，不是建议管理员删除生产配置。

## RC3 已确认的缺陷

| 位置 | 条件与后果 |
| --- | --- |
| [宿主首次注册](../../components/cpa/internal/pluginhost/host.go#L1356)、[保留加载实例](../../components/cpa/internal/pluginhost/host.go#L355) | 首次注册 RPC 返回错误时，回调记录错误后返回空插件；`safePluginCall` 正常返回时仍给出 `ok=true`。`lp.registered=true` 又早于元数据和能力校验。没有旧实例可替换时，已打开但未初始化的客户端可能留在 `h.loaded`，未进入活动快照；同路径下次走 `reconfigure` 而非首次 `register`。不等于所有注册失败都走此路径。 |
| [安装前备份触发条件](../../components/cpa/internal/pluginstore/install.go#L289)、[恢复覆盖文件](../../components/cpa/internal/api/handlers/management/plugin_update_backup.go#L77)、[选库优先级](../../components/cpa/internal/pluginhost/platform.go#L192) | 原活动库有独立备份，但 `BeforeWrite` 只在覆盖既有目标时执行；本次新增的版本库不在 `replacedFiles` 内。恢复若只还原覆盖文件和旧配置，且旧配置没有固定版本，新库仍在扫描目录时，下一次冷启动会优先选较高版本的库。内存中的旧实例恢复和冷启动仍能选回旧库，是两项不同的验收。 |
| [恢复旧活动身份](../../components/cpa/internal/api/handlers/management/update_operations.go#L211)、[按 ID 查询](../../components/cpa/internal/api/handlers/management/update_operations.go#L277)、[查最近任务](../../components/cpa/internal/api/handlers/management/update_operations.go#L305) | 恢复后若注册状态、活动版本或路径与升级前不一致，任务记录具体原因并解除进程内归属。`recovery_required` [不属于终态](../../components/cpa/internal/updatestate/store.go#L34)，两个 GET 遇到无归属任务便用通用“中断”替换**响应**里的原始原因，磁盘记录本身未被 GET 改写。 |

## 卡片与许可

- **路径不是活动身份。** [CPA 插件列表](../../components/cpa/internal/api/handlers/management/plugins.go#L102)以磁盘发现填 `path`，以配置填 `configured/enabled`，以活动快照填 `registered`、元数据和菜单。[卡片路径列](../../components/cpamp/apps/web/src/features/plugins/PluginsPage.tsx#L1113)直接显示 `path`。即使有旧实例在运行，卡片也可能把候选路径和旧实例的元数据排在一起。菜单只来自[成功登记且有菜单名的资源](../../components/cpa/internal/pluginhost/snapshot.go#L132)；已注册也可以不提供菜单。
- **灰框不代表授权码无效。** [许可组件](../../components/cpamp/apps/web/src/features/plugins/BpsLicensePanel.tsx#L65)只在管理连接有效且已注册时读取状态；无有效响应或读写进行中，输入禁用。`required=false` 时隐藏输入。供应商若明确返回 `required=true, active=false, code=license_activation_required`，[当前归一化](../../components/cpamp/apps/web/src/services/api/bpsLicense.ts#L36)会丢掉该已知含义，显示“状态代码未识别”。这只影响成功读取后的文案，不是未注册时灰框的原因。
- **未观察不应显示为否。** [列表解析](../../components/cpamp/apps/web/src/services/api/plugins.ts#L114)把缺失的注册字段转成 `false`、缺失菜单转为空数组；[列表刷新](../../components/cpamp/apps/web/src/features/plugins/PluginsPage.tsx#L349)遇到网络错误可保留旧卡片，[侧栏](../../components/cpamp/apps/web/src/components/layout/MainLayout.tsx#L474)读取失败却清空菜单。维护任务反映当时的观察，不代表当前实例。许可状态归供应商插件管理；既有包说明所述[挂载目录中的许可文件](BPS-PLUGIN-UPDATE-20261116.zh-CN.md#L20)不是本站应复制的第二份状态。

## 通用修复方案

1. **在公共运行配置副本上只排除顶层 `store`。** [ZIP 导入](../../components/cpa/internal/api/handlers/management/plugin_imports.go#L385)会向原实例配置[写入宿主安装元数据](../../components/cpa/internal/api/handlers/management/plugin_store.go#L551)。[获取期望版本](../../components/cpa/internal/pluginhost/config.go#L109)仍需从磁盘配置读取 `store`；现有 [runtimeConfigYAML](../../components/cpa/internal/pluginhost/config.go#L81) 却会将[完整深拷贝](../../components/cpa/internal/pluginhost/config.go#L157)编码后送入 [RPC](../../components/cpa/internal/pluginhost/rpc_client.go#L128)。在该副本编码前移除宿主保留的顶层 `store`，让 `register` 和 `reconfigure` 共用同一结果。不改磁盘配置、选库逻辑、启停意图、优先级或插件自有字段，不递归删除同名键，也不为某个版本单写补丁。
2. **明确本次操作的目标和恢复点。** 扩展现有任务备份，记录原配置、原活动版本与路径、候选目标原先是否存在。升级验证候选身份；失败则恢复原文件和配置，将本次新增的失败候选移出扫描范围，再核对活动身份和下一次冷启动将选择的文件。原先停用的保持停用，不动其他插件、账号或许可数据。
3. **分开处理热替换和冷启动。** 只有注册返回成功且元数据、能力有效，实例才标记为已注册。首次失败时释放未初始化客户端，下一次从首次注册开始。热替换受阻或同路径不能替换时，明确进入“待确认重启”；通过已有宿主桥接能力完成受控重启并复核，不自动改用冷安装。证明旧实例恢复后才解除恢复状态；未恢复时保留首错和恢复失败原因。
4. **复用当前管理页。** 卡片分别标明候选路径、活动实例和观察时间；列表读取失败标注“上次观测”，菜单能力未知与明确无菜单分开。任务详情优先显示持久化原因，缺原因才显示通用中断。只有许可 GET 成功后才按供应商合同展示状态；识别 `license_activation_required` 为待激活，保留未知代码的保守文案。切换活动实例后重新读取；授权码只走原有激活接口。本方案不新建服务、不按插件版本号分支、不绕过商业许可。

## 验收与预算

| 场景 | 放行条件 |
| --- | --- |
| 注册配置与连续升级 | 以精确 RC3 为复现基线，在修复候选上用真实供应商 ZIP，从已注册的 A 经网页升级至 B，再升至 C，不靠卸载重装。确认两种 RPC 均不带宿主顶层 `store`，磁盘记录与插件字段保留；每次核对宿主所选文件、活动版本、注册快照、声明的菜单与只读许可。 |
| 升级失败与恢复 | 同时覆盖首次注册失败后同路径再注册，以及“原版本未固定、新候选文件已落盘”的失败。候选未冒充成功；旧实例恢复，同挂载冷启动仍选择旧库；原为停用的仍停用。无法恢复时，两种任务查询都保留首错及恢复原因。 |
| 许可与管理页面 | 有有效许可的既有实例升级后保留许可挂载、可读状态仍为有效，不要求重新输入授权码；待激活的实例显示待激活。断联、缺字段、无菜单、404、未知代码各自展示，未注册或缺有效响应时不允许提交。真实有效许可的保留另由获授权环境验收，隔离实验不提交真实授权码，不以 OAuth 或推理代验。 |

**规划估算：3–5 人日。** 按更新与回退 1.5–2.5 人日、状态与界面 0.5–1 人日、真实 ZIP 隔离验收 1–1.5 人日计算。按假设日单价 **¥1,000**，示意预算 **¥3,000–5,000**，不是供应商许可费或交付承诺。估算以既有更新机制和宿主桥可复用为前提；若隔离实验确认还要新增宿主重启编排，先列影响和增量工时，再决定是否实施。无数据库动作；本方案不涉及生产变更。
