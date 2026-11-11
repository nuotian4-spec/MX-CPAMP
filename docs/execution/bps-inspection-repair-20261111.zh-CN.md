# BPS 模型与巡检修复执行包

日期：2026-11-11（北京时间）。用户在完成Q1—Q5及整体方案交付后要求“下一步”，按已确认方案开展本地实施。固定基线`5d431b9be0828f94576d1807e1cc87b822cc1350`，方案见[模型与巡检修复](../bps-inspection-repair-plan.zh-CN.md)。

## 目标与范围

WHY：BPS OAuth真实唯一保存注册已通过，但模型清单缺配置入口、空提示不准确；当前巡检只支持Codex/xAI。WHAT：接通BPS只读权限/模型目录查询、人工确认全局发布清单、BPS巡检与结果展示。

已确认边界：BPS巡检只检测记录，不自动删、启用、禁用或恢复；权限查询不发推理、不强制刷新，不自动写模型配置或调度规则。完整发现目录只保留页面内存，巡检历史保存状态摘要，额度缺来源显示未知。

## 文件责任与交付

| 负责人 | 工作树 / 分支 | 文件域 | 完成标准 |
|---|---|---|---|
| A线 `bps_inspection_sources` | `D:/MX-CPAMP-worktrees/bps-access-inspection` / `codex/bps-access-inspection` | `components/cpa/**`、`docs/contracts/bps-access-v8.zh-CN.md`、独占cpa证据 | 先冻结v8契约，固定GET/精确凭证/出口与headers、5秒管理例外、归一解析及handler专项通过 |
| B线 `bps_repair_design` | `D:/MX-CPAMP-worktrees/bps-models-inspection` / `codex/bps-models-inspection` | `components/cpamp/**`、独占manager-web证据 | BFF、双巡检入口、目标选择、模型草稿/保存、权限差异与空提示专项通过 |
| 独审 `callback_compatibility` | 只读A/B与主工作树 | 仅独占review证据目录可写 | 核契约、只读边界、BPS不触发旧Codex自动动作、配置保留和证据一致性 |
| 主会话P9 | `D:/MX-CPAMP` / `codex/openai-bps-login` | 执行包、方案/当前态、整合与最终构建回执 | 接口协调、整合已审候选、一次最终CPA/Manager构建和隔离组合验收 |

HOW MUCH：两条产品实现线并行，共享文件单写；目标约30分钟形成本地经专项验证候选，若出现具体阻塞及时报告，不以时限代替验证。最终产品构建统一执行，Worker输入不变复用原产物。各线不得自行推送、部署或重复真实验收。

DONE：精确提交、diff范围、命令/真实退出码及输入均可核；只读GET、响应状态与模型过滤、BPS单选混选、零Codex误调用、零BPS自动动作、模型草稿与发布分离、原YAML和逐凭证规则保留、两端结果存储语义、未知额度均有专项证据。适用证据可复用，输入变化仅补受影响项。最终构建满足CPA编译及Web类型/产物验证，不机械重复全仓检查。

DON'T：不新增SQL/schema或新目录缓存，不改变OAuth/刷新、推理、工具、图片、调度、既有目标默认值、域名或客户端密钥；不读取旧凭据、截图或已清理窗口材料，不发真实登录/access/刷新/模型请求，不push/deploy。真实access验收需在本地候选完整、可审后另绑定明确的一次范围。

## 数据与当前态

无新增SQL/schema迁移。未来人工保存模型会写CPA现有配置；巡检结果沿用既有SQLite记录，这是运行时记录写入，不是修改凭证或发布模型。

当前结论：本地候选完成。A/B产品及专项已整合，独立代码/安全审查通过；CPA与Manager最终构建exit0，断网最终镜像组合exit0，BPS集合1、只记录/未知额度、模型保存注册与清空、SQLite往返及归属清理通过。产品、证据与镜像身份见[最终回执](../evidence/bps-inspection-repair-20261111/FINAL.zh-CN.md)。

当前责任人：主会话完成本地收口。下一责任人：主会话准备本次候选的定向真实access及页面验收材料，绑定新授权范围；官方access在线实测、生产验收仍待完成。无新真实登录/access/刷新/推理，无推送或部署，`write_release_ready=false`。
