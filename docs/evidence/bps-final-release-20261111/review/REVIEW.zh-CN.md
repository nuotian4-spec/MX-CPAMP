# BPS 最终候选发布材料审查

2026-11-11。事实基线 `c2adc4ad92a2bf564bf8dfcbaa40aee78d8c9ff3`。结论：新发布工具、固定归档及其最小真实Linux生命周期通过独立代码/安全审查，无本轮新增待处理P1/P2；完整业务及现场发布缺证保持，`write_release_ready=false`。本记录仅定向读取最新接续、旧发布独审/真实边界、最新用量候选与页面结果、部署必要函数及本轮稳定提交，不重做产品门禁。

## 已通过与可复用

| 项目 | 判断 | 直接证据及复用依据 |
|---|---|---|
| BPS 手工 OAuth、回调及唯一保存注册 | 已通过 | `bps-callback-scope-20261111/MANUAL-RESULT.zh-CN.md`；scope 最小兼容后的真实授权、保存注册及清理，不以 2FA 成功替代手工回调 |
| BPS 模型发现/人工发布、权限巡检 | 已通过 | 当前 `phase2-release-next` 与 `login-entry-closure` 的真实记录；模型发现与发布分离，巡检 keep，无删除、启停、重登 |
| BPS 官方账号用量全链路 | 已通过 | `bps-quota-repair-20261111/FINAL.zh-CN.md` 及 `bps-quota-manual-20261111/FINAL-RESULT.json`；最新候选固定，1份 BPS、1条服务端巡检、1条合格官方用量观察，账号卡与详情显示官方账号用量、剩余99%及重置/观察时间，窗口清理完成 |
| BPS 同 client 正常刷新、文本及默认原生图片 | 输入等价复用 | `bps-tools-images-final/BUSINESS.zh-CN.md`；刷新、gpt-6-luna 文本和原生图片有独立真实成功，后续用量及展示修复未改变对应 Worker/推理业务路径；不得写成 HTTPS relay 成功 |
| BPS Responses 普通工具往返 | 输入等价复用 | `bps-tool-diagnostic-20261111/BUSINESS.zh-CN.md`；一次登录、ping→pong 两轮均200、同凭证及清理通过；旧502仍保留，未证明其历史根因已修 |
| 批次、并发、协议形态及图片历史语义 | 复用对应离线证据 | `phase2-review.zh-CN.md`、`bps-tools-images-final/REVIEW.zh-CN.md`；包括默认20、累计30秒等待、每凭证限制、100条串行及敏感材料内存生命周期；Chat/SSE及截图/历史图片不扩写成新增真实实测 |
| 私有 BPS state 原卷跨重建与工具绑定 | 输入等价复用 | `bps-state-durability-20261111/README.zh-CN.md`；真实 Compose 原卷复用、空卷反例和合成续轮。发布须保留独立 state-dir、挂载及切后新增绑定，旧归档不含切后新增数据 |
| 旧发布流程核心 | 按函数输入等价复用 | `phase2-release-review-20261111/README.zh-CN.md`、`phase2-release-boundaries-20261111/README.zh-CN.md`；prepare不改live、确认/漂移/secret来源、停态备份、真实切换、正负鉴权、Caddy原完整ID、wrapper、complete拒重入及维护后失败停态均有真实Linux证据。新工具变更命名与候选后仍须补受影响生命周期，不重跑未变120秒等待 |

最新三镜像固定身份沿用 `bps-quota-repair-20261111/FINAL.zh-CN.md`：CPA `eeb5bf…` / revision `b9f98a54…`，Manager `db9f59…` / revision `d26cbf75…`，Worker `2bfdb4…` / revision `95dd9ca2…`。旧 phase2 归档及旧 accepted_ids 绑定早期候选，不能直接作为本轮新三镜像身份。

## 发布前具体缺口

| 项目 | 已核事实 | 收口所需证据或决定 |
|---|---|---|
| 最终镜像分发及独立新工具 | 本轮已补齐，见下文新分发和新工具定向审查 | A/B稳定提交、完整归档身份及最小真实Linux生命周期通过；不重建未变产品，不将此项放行扩写为下列业务或现场缺口已完成 |
| HTTPS 图片中转 | 用户明确选择双模式、默认native。产品有 GET/HEAD `/api/bps-images/:token`；现 `phase2-release.py:sources()` 仅接受native且拒已设relay origin，prepare/apply未配置或验证Caddy relay | 不能以默认native成功移出已选relay。需核既有Caddy实际配置/顺序，准备精确 GET/HEAD `/api/bps-images/*` → 持图 CPA 的受控路由和有效HTTPS origin，并取得真实HTTPS取图证据；若阶段延期或缩小本次发布范围须由用户明确决定 |
| Codex 正向 2FA | `real-2fa-20261111/RECEIPT.zh-CN.md` 为 `needs_user_action/web_login/security_check`；完整窗口只证明输入可用及Worker依赖，`two_factor_live_verified=false` | 受支持交互及可定位阶段证据仍待收口；不得用能力true、BPS成功、切换账号/出口假设代替Codex成功。需完成一次明确范围的正向验收，或由用户明确接受该待验边界；不擅自重试 |
| BPS 2FA 当前交互边界 | 早期单条、批次、工具验收均有BPS成功；最近用户反馈 `security_check/needs_user_action`，Worker未改 | 保留既有成功与此次官方交互终态两项事实，不推断密码、TOTP、封号或出口根因，不写成已修或所有账号可无人值守。若以本轮最新账号正向2FA为发布必验项，需明确范围的定向证据或用户接受边界 |
| 生产登录及模型在途 | `phase2-release-preflight/SERVER-BASELINE.zh-CN.md`、短盘点均为 `login=unknown/model_inflight=unknown`；新apply将停Worker/Manager/CPA，旧确认已提示可能中断 | unknown不是0。维护前用已审只读活动入口定向核批次/模型计数；单任务无只读枚举接口仍保留unknown。需明确排空/暂停输入的实际口径，或在一次DEPLOY中明确接受中断风险，不以单纯健康探针当活动排空 |
| Caddy 现场路由与外部入口 | 只核过Caddy原ID、单Compose链与挂载，未核实际adapt后的路由；旧health只测内部模型目录、Manager未知任务和Worker鉴权 | 保持Caddy ID不变不证明 `/api`、模型、回调或relay路由正确。定向核实际adapt中路由匹配/顺序及目标，现域名/Base URL/API Key须保持；relay必须落持图CPA，不能公开state、上传或目录 |
| OVH prepare/apply及恢复现场 | 既有18条基线足够复用；实际source/KV/data key/secret检查已在工具但仅合成通过，新停态备份尚未产生 | 新工具prepare及切前漂移按原基线核真实来源、即时鉴权和受保护输入。apply后核真实完整镜像ID、三应用完整Compose链、挂载、Caddy原ID及新备份回执；不能用本地合成或旧rc1备份冒充 |

没有发现需机械重做全量产品测试的理由。HTTPS relay与Codex正向2FA是已约定完整验收的具体剩余项；新发布材料准备完成不会自动使其通过。现有网络/存储来源早拒绝、保持原数据及失败停态规则继续保留。

## 数据、授权与下一责任人

无SQL/schema迁移或计划中的运行时DELETE。正式apply仍有停三应用、私有停态备份、新BPS状态目录/挂载、CPA配置、三文件wrapper及metadata/state写入；切换后新增绑定必须原位保留，不自动整库回灌。

用户原授权覆盖仓库交付、镜像分发和生产切换，本轮不为单纯push另设批准流程；最新接续仍要求完整验收后统一发布，未凭旧范围外推新功能通过。主会话回报真实远端为 main `cae2b599e864153e95c241b3070a7336dcddfad0`、工作分支 `66736ed731535f94dfce76a1c88a752a98bcc86a`，仅旧rc1 prerelease；本席未访问远端，待最终发布绑定直接核该白名单回执。

当前责任人：A完成独立新工具及最小Linux生命周期，B完成最新归档与经典Linux实载；下一责任人：本席定向安全审查稳定提交/回执，主会话收口剩余业务及现场证据后交具体可审发布材料。当前不发生产写入命令，不推送或部署。

## 新分发归档定向审查

B证据提交 `5c89657f1e2102ce31a38d8450a9fb0852060ca8`，基线同上，仅新增 `bundle/` 证据。只读核 `pack.mjs`、manifest、pack-meta、独立归档复核、两端镜像身份、Linux fixture/daemon、实际load及清理原文，未运行打包、容器、构建或网络命令。

结论：新固定分发归档通过，旧候选分发缺口在本地材料层关闭。归档 `mx-cpamp-images-linux-amd64.tar.gz` 为 227177346 字节，SHA256 `256ddca4ec162bc1249ccea95ca1e36ce536b4909fa1ac89b77af1b477910c27`；manifest、pack-meta、独立文件摘要复核一致，docker export exit0、gzip完成，旧归档原位保留。

| 组件 | Desktop accepted ID | 经典Linux实载 accepted ID |
|---|---|---|
| CPA | `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b` | `sha256:09d30cc6bc3808a07b06953faecb0bb49036a5240513132e6cb8b7c035b1b335` |
| Manager | `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd` | `sha256:a7b21c514227d3c48426b8cb1bb443cc8d5ca8000c626da190099ef922c5b247` |
| Worker | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `sha256:30689442b2f61b2c7ff27eae008291b014fe7161e842a60202a6574c912be4dd` |

三组件 ref、revision 与最新产品FINAL一致，均linux/amd64。经典Linux为独立 Docker 29.7.2/vfs daemon；fixture网络none、零发布端口、只读归档目录、专用数据卷，无宿主Docker socket，不启动三个产品容器。实际load exit0，inspect身份与manifest精确一致；fixture容器及专用卷删除exit0且查无残留。上述两组ID是同归档两种存储后端的实际结果，不是放宽任意同tag身份。

本席亦已直接核 `REMOTE-BASELINE.json`，其白名单内容与主会话远端回报一致，`remote_read_exit=0`，当前没有push或新Release。该远端快照只用于本轮发布绑定，不代替后续推送后的真实远端核验。

## 新发布工具及真实生命周期定向审查

工具提交 `3c9c63aaeb865c81631f37b6f3b373edaee694c5`，最终证据提交 `34bef9d4411c98667f4ea3a4beab0b9642e2df35`。只读核新旧交付diff、新sh入口、实际控制器与invoke、`tools/run.log`、`tools/equivalence.json` 及最终README，未重跑语法、产品测试、Linux控制器、容器或上游请求。

新入口 `deploy/bps-final-release.{sh,py}` 使用独立 `/opt/cpa-stack/.mx-upgrade-bps-final-20261111`，第三Compose为 `compose.bps-final.yaml`，原子临时配置/wrapper也使用独立名称。旧phase2工具、旧prepared及历史状态不修改。CANDIDATES的ref、完整revision和两个accepted_ids与B新manifest逐项一致；load仍核精确候选、平台及压缩归档摘要，按不可变ID启动。

新旧生产函数差异限于上述命名、恢复标题和候选常量；`equivalence.json`记录32函数AST归一等价及旧py/sh与已验 `39eb250ef8407213a4e7ff7dea444cdc007370f1` 一致。独审直接读diff确认来源、secret优先级、确认、漂移、停态备份、健康和失败保留逻辑未放宽。旧无TTY/EOF/120秒超时、漂移、secret/来源反例及维护后备份失败证据按未变函数复用，原历史整轮exit1继续保留，不重写为全通过。

新控制器只替换隔离ROOT/PROJECT/STATE/BASELINE事实，不替换交付函数、Docker或Compose。真实Linux runner断网，只有控制器挂宿主Docker socket；四个应用为唯一project、内部网络和合成数据，无宿主端口、真实账号、登录或模型调用。已读run.log终态 `fixture_cleanup=verified`、`command_exit=0`，覆盖：

- 实际sh shebang入口无参数拒绝，原prepare真实读取新归档、来源与密钥并生成候选；四服务ID、原配置/wrapper/secret字节保持，live BPS state仍不存在。
- 真实PTY取消停在维护前，无maintenance及服务变化；随后一次DEPLOY实际停止三应用、生成私有停态备份、安装候选并认证。
- 新CPA/Manager/Worker完整ID与最新Desktop候选一致，实际health核完整挂载与三文件链、正向200/404/200及无效密钥401；Caddy完整ID不变且运行。
- 新生成Linux wrapper实际执行config；备份tar中Manager目录、CPA配置及必要secret可读，Worker secret与停前原字节一致，tar0600，新state0700；complete再次apply拒绝。
- finally清理仅本轮唯一归属，容器/网络/卷无残留。

本轮本地发布工具与分发材料放行。主会话报告已整合至 `db97e37`；由主会话补完整整合提交、产品/工具输入一致性及diff-check即可，不重复本轮Linux生命周期或已通过业务请求。HTTPS relay、Codex正向2FA、BPS当前官方交互边界、生产活动unknown、实际Caddy路由及OVH现场回执仍按上表收口，不因为新工具名含final自动通过。

本席没有产品、测试、归档字节或生产修改，未推送/部署。下一责任人：主会话统一提交本审查与只读远端基线、更新当前态，并向用户清楚交付已通过本地材料及剩余验收/范围决定；完整发布前保持 `write_release_ready=false`。

## 只读预检的 Caddy 原序摘要审查

增量提交 `2351a7c9532253ab09cd139013e073a28ca57d2e`，基线 `db97e37f177a9223130d95035cda57ef831b112d`。变更仅 `deploy/phase2-readonly-preflight.sh` 的纯摘要函数、一处输出扩展及 `preflight/` 专项证据。只读核diff、完整函数、实际专项控制器、`fixture.log`、三条原序脱敏样例和主会话的新只读交付文档；未运行专项或访问生产。

结论：只读预检增量放行，无新增P1/P2。新骨架保留适配后服务器、route、handle及subroute的数组原序和各层index；匹配分支、GET/HEAD、精确relay/api/v1路径、CPA/Manager固定目标均为枚举。terminal保留布尔，同一原group映射同一数字，不输出原组名。域名、header/query、rewrite正文、任意upstream及未知处理器名均不回显；未知匹配器/选项、rewrite及异常结构明确unknown，未据摘要自动宣布路由正确。

骨架输出限制为服务器/每列表最多16项、路由与处理器合计128、嵌套深度12，超限置 `skeleton_truncated=true` 和unknown。父匹配仍包裹子路由，不能只从子节点的固定CPA目标推断其实际可达；`effective_order` 保留人工审查口径。

引用执行席同提交的专项exit0：缓存真实Caddy adapt覆盖relay在通用API前/后、嵌套、分组、终止和固定目标；合成秘密哨兵覆盖host/header/query/rewrite/任意upstream且不进入摘要；未知类型/路径/方法/处理器及大小/深度反例通过；纯函数运行期I/O、HTTP与写入审计禁用。交付本体及同delimiter外层here-doc分别经缓存Linux bash -n通过，未执行预检。

旧内嵌Python移除新纯函数与该处emit扩展后AST等价，diff亦确认原GET、内部IP范围、拒重定向、鉴权来源/正负探针、活动判定及零写入路径未改；复用原完整预检的合成现场证据。404、缺字段和非法计数保持unknown，单登录任务/Worker任务无只读枚举仍unknown，不转为0。脚本没有镜像load、服务切换、官方登录、刷新、推理或POST。

主会话 `docs/execution/bps-final-readonly-next-20261111.zh-CN.md` 的PowerShell入口读取当前本地已审脚本、归一LF，再用单引号delimiter拼接原文here-doc并 Set-Clipboard；没有把脚本文字当PowerShell执行，不依赖未push raw或编码脚本。用户后续终端粘贴的现场结果仍须实际回传，本次语法与脱敏专项不代替该结果。

下一责任人：主会话整合并交付只读入口；用户采集后主会话核Caddy原序树、当前鉴权与活动。HTTPS中转真实取图、Codex正向2FA及完整业务发布缺证保持，`write_release_ready=false`。
