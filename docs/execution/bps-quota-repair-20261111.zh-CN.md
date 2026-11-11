# BPS 额度接入执行包

2026-11-11。基线`c39582aa515f57c4a1297b589139a1bd07a95be4`。用户已批准继续确认额度来源，并在来源成立后整线接入。

## 目标与执行顺序

先验证当前本机隔离窗口中BPS凭证能否读到官方账户用量；取到可靠字段后再固定契约，同时接通CPA采集、Manager/Web契约、双巡检、记录和账号卡。权限成功、用量查询成功和BPS专属额度是三个不同结论；官方用量页跳转不能单独证明额度共用。

来源诊断绑定`mx-full-login-manual-19e8a490cf66`三容器、固定镜像和`/state/auths`。只选择该窗口内唯一BPS凭证，不使用聊天旧密码/TOTP。prepare只核本地条件及唯一BPS计数；run独审后最多两次顺序GET：现有BPS access成功且结构有效，再向`https://chatgpt.com/backend-api/wham/usage`使用同BPS bearer和账户头查询一次。无重试/重定向；任何一步失败即停。结果只保留HTTP、稳定错误类别、字段类型及白名单额度数值，不输出或持久化凭证、账号标识、原始正文。

来源工具源提交`74541d6c5b357140c6e11a4cd0fd7ccc0c921e0a`已整合为`533da6a`。旧prepare退出1保留：只有Codex1、BPS0，官方请求0。用户随后明确回复已导入1份BPS、继续验证；独审后由run重新核实际计数，不重复旧prepare或任何真实查询。

没有登录、令牌刷新或模型调用；不重建、停止或修改活跃窗口。不因额度失败判定BPS凭证失效，不追加OAuth scope或换凭证测试。窗口到期后不重用消失的tmpfs状态。

## 分工与范围

- A席：隔离`codex/bps-quota-source`分支，仅`docs/evidence/bps-quota-repair-20261111/source/**`，准备可审的诊断及说明；未经独审不发官方请求。
- 独审：仅`docs/evidence/bps-quota-repair-20261111/review/**`，核真实请求预算、凭证映射、代理、输出投影与失败边界。
- 主会话P9：核授权、整合、执行已审诊断，依真实结果确定产品契约和实施分工，保持现有功能及有效证据。

## 来源结果与接入决策

| 实际结果 | 决策 |
|---|---|
| 用量200且结构/数值有效 | 保留来源、查询时间及实际窗口；仅展示观测到的官方账户用量，不冒称已证明BPS独立余额或共用桶 |
| 用量401/403或请求失败 | BPS权限与额度状态分离，额度保持未知，不刷新/登录/重试 |
| 用量200但无有效额度结构 | 记录无可用额度观察，保持未知，不合成0%或100% |

可靠来源确认后，采用独立用量观测接口，保持access接口与模型发现契约。实施分两条独立线：CPA采集/管理契约；Manager/Web巡检及账号展示。共享契约先固定、共享文件单写，独审贯穿。验证覆盖真实零值、未知、权限成功但用量失败、重置时间及旧记录；保持BPS只检测记录、不删启停或自动恢复。只补受影响专项，统一最终构建与隔离组合，不重复未变登录/工具/图片测试。

Manager/Web需一并处理的现存过滤：AccountsPage的display/observe/refresh；accountCredentialEvidence的窗口与时间边界；accountQuotaSummary的BPS override；accountQuotaDisplayWindows的source与映射；accountQuotaSnapshots的query及snapshotDefinition；quotasnapshot/evidence.go的provider和inventory scope；quotasnapshot/service.go的provider白名单。现有CodexInspectionResult已含窗口、库存观察与provider，可沿用JSON及现有表，不需新增schema。账号主动刷新和监控概览的provider路由也属于本轮额度展示链，不能只扩大白名单却落入失败默认分支。保留真实BPS身份，不写成Codex快照；Codex专属Header/重置推断保留其原provider条件。

## 真实来源结果与当前态

用户导入后，已审`--run`退出0：auth1/BPS1/Codex0；access200/allowed=true/models2，wham200，主窗口使用17%、周期2592000秒、reset_after2586075、reset_at1796977220，其他窗口null。实际GET各1，刷新/推理0，输出仅白名单。主线回执`f4dd43e4c217ca6eb7762ca4d4f747665280657c`与独审均已归档。`official_source_verified=true`，仅证明该授权可读官方账号用量。

A树新分支`codex/bps-account-usage-cpa`负责CPA独立usage接口、冻结契约、handler/解析专项和断网组合工具；B树`D:/MX-CPAMP-worktrees/bps-models-inspection`新分支`codex/bps-account-usage-manager`负责Manager/Web完整链路及专项。两线从`f4dd43e4`开始，来源验证后不再发官方请求。独审`callback_compatibility`只读跟进，主会话统一最终两镜像构建和一次组合，Worker输入不变复用。

无新增SQL/schema迁移，沿用既有巡检JSON与quota snapshot表；额度观察写入属于运行时记录，不改变凭证或调度动作。无推送或OVH部署。当前责任人：两线执行席；下一责任人：独审与主会话整合构建。`quota_product_ready=false`、`write_release_ready=false`。

## CPA 候选与窗口清理

A席产品提交 `d563ef2862931d4ce836d4372140ee4e4f31ca19` 经独审放行，主线整合 `d07b4b1ed4845be0b2474a50ec75d39c254b937e`；专项退出0，独立 usage 保持原 access 契约。CPA 最终镜像已构建退出0，身份见[构建回执](../evidence/bps-quota-repair-20261111/build/README.zh-CN.md)。组合材料 `92fa20c94b6ee23c9550abad5692fb3586440197` 经独审后整合 `d803a02`，尚未执行最终组合。

旧完整窗口到期后已按随机归属执行 stop，退出0、`verified=true`、`token_state_retained=false`，见 `source/WINDOW-CLEANUP.jsonl`。不重启该窗口或沿用旧凭证状态。用户反馈的 BPS 2FA `security_check / needs_user_action` 仅表示本次流程需要官方交互，不据此推断密码、TOTP、封号或出口原因；Worker 产品未改，本轮未追加登录。

当前责任人：Manager/Web 执行席完成全展示链路及专项；下一责任人：独审、主会话整合及一次最终构建/断网组合。额度来源已验，完整产品仍待验，`quota_product_ready=false`、`write_release_ready=false`。

## 本地候选收口

两线已整合并独审放行，产品输入 `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c`。[最终回执](../evidence/bps-quota-repair-20261111/FINAL.zh-CN.md)绑定完整镜像、专项及组合结果。Manager一次最终构建含tsc/Vite/Go全部通过；首轮组合定位CPA列表缺BPS平铺account_id，最小修复及真实v0/v8公开入口专项通过后，仅重建CPA。Manager/Worker产品输入等价复用。新组合exit0，精确身份、BPS快照写查及归属清理通过，旧组合exit1不改写。

没有SQL/schema迁移或依赖变更，新增观察沿用既有SQLite表；隔离测试写合成记录并清理，生产未改。当前责任人：主会话本地收口；下一责任人：主会话准备固定产物的页面验收和统一发布材料。`local_candidate_ready=true`、`integration_passed=true`、`quota_product_live_verified=false`、`write_release_ready=false`。不追加官方请求，不推送或部署；旧窗口及旧分发包不复用。
