# BPS 额度来源诊断独立审查

日期：2026-11-11。仅审受控来源诊断，不实施产品，不发真实请求。最终源提交 `9e3f9232628c60379ca42b99bd5fdc4644158608`，基于 `74541d6c5b357140c6e11a4cd0fd7ccc0c921e0a`；主线整合为 `8d2f814`，执行树干净。

结论：来源诊断入口放行。主会话可在用户已批准范围内直接执行一次 `--run`；该动作自身重核本地状态与唯一 BPS，无需重复 `--prepare`。本席未执行任一真实请求。

## 范围与数据处理

- 状态目录、随机归属与现有窗口固定；CPA/Worker 必须运行、归属及镜像与窗口记录一致、共享原 namespace，距到期超过 60 秒。只复用现有容器，不新建服务、修改配置或清理窗口。
- 按 BPS 扁平凭证的实际 `type=bps` 选择，要求唯一，另核 `profile=excel` 与固定 client_id。`access_token` 和 `account_id` 必须为非空字符串且无换行；不解析或输出 JWT，不以 Codex 凭证补位。文件名及凭证仅经捕获内存与 stdin，不进入 argv、环境或记录。
- `--prepare` 无官方请求。`--run` 仅用同一 BPS bearer/account_id 顺序发 access GET 一次，HTTP 200、allowed=true 且目录合法才发 Wham GET 一次，合法空目录可继续。各请求 20 秒超时，固定同一 7890 代理、TLS 校验开启，不跟随重定向、不重试；任一步错误退出。无登录、刷新、推理或其他模型请求。
- BPS 请求头逐项对照当前 CPA `ApplyBPSHeaders`；Wham 请求头及固定 User-Agent 对照 Manager `requestCodexUsageAt` 与默认配置。未借用 CPA 自动令牌替换或刷新逻辑。
- 输出仅固定 `ROOT_KEYS/AUTH_KEYS/QUOTA_SCHEMA` 的字段名、类型和批准的数值/布尔值；未知键只计数，日期字符串只记类型。模型 ID、身份信息、token、任意字符串值、原始错误 message 和正文均不输出、不落盘。错误码使用关闭的白名单，其他错误归稳定类别。
- 可变凭证/正文缓冲在 finally 覆写，解析容器、请求头、cookie 与响应内容清空，一次性进程退出。README 明确不承诺清除语言运行时全部不可变字符串副本；回执中的缓冲清理不外推为物理内存确定擦除。

## 已核证据

旧 `PREPARE.jsonl` 与 `PREPARE.exit=1` 如实保留：归属、namespace、依赖及语法通过，但当时凭证总数 `1`、Codex `1`、BPS `0`，在本地停止；access/Wham、刷新、推理均 `0`。此历史结果不替代用户随后导入 BPS 后 `--run` 的实际计数。

已只读最终投影实现及 `projection_test.py`：该测试通过 AST 仅加载真实白名单及投影函数，不加载凭证或网络主流程，覆盖未知根/嵌套身份/token 样键、模型 ID、秘密字符串及 ISO 日期不回显，保留合法数值零、布尔 false 与数值 reset。引用执行席实际回报的本地反例与 diff 检查退出 `0`，本席未重复执行。产品、容器与真实官方验证均未由本席运行。

## 来源结果的接入判断

1. Wham HTTP 200 且有可识别额度数值：作为独立“官方账号用量”观测接入，保留来源与时间；不直接称作 BPS 专属配额。access/模型发现维持现有只读权限契约，不附带每次用量查询。
2. Wham 401/403：权限与用量状态分别保留，BPS access 成功不被用量失败覆盖；额度未知，不自动刷新、重登或启停账号。
3. HTTP 200 但无已识别额度结构/数值：仍为未知，不补成 0 或 100，不套用另一份 Codex 凭证的数据。诊断 `passed` 或仅存在 `rate_limit={}` 不代表额度业务可用。

未来巡检可组合权限与独立用量结果，但仍保持 `keep` 及零自动动作；本轮仅放行来源诊断，不包含产品接入、发布或部署。

## 真实来源回执核证

已只读 `source/SOURCE-RUN.jsonl` 与 `SOURCE-RUN.exit=0`。实际运行重新确认原归属和 namespace、7890 代理，凭证总数 `1`、BPS `1`、Codex `0`，profile/client 匹配；随后 access HTTP `200`、allowed=true、合法模型数量 `2`。

同一 BPS bearer/account_id 的 Wham GET 返回 HTTP `200`，白名单观测到 `rate_limit.allowed=true`、`limit_reached=false`，主窗口 `used_percent=17`、`limit_window_seconds=2592000`、`reset_after_seconds=2586075`、`reset_at=1796977220`。secondary window、code-review 和 additional-rate-limits 为 null；credits 两布尔值为 false，balance 为 null，不据此推断余额、BPS 专属配额或与 Codex 共享额度桶。

完成回执为 passed，实际 access/Wham GET 各 `1`，刷新/推理均 `0`，可变敏感缓冲清理已报告。该证据证明本次 BPS 授权可读取带明确主窗口数值的官方账号用量；未保留原始正文或任意身份值。本席仅核脱敏记录，未再次请求官方。

当前责任人：主会话固定独立“官方账号用量”契约并安排最小产品接入。下一责任人：执行席按已核来源实现，独立审查核权限/用量状态分离及零自动动作。`official_source_verified=true`、`quota_product_ready=false`、`write_release_ready=false`。

## CPA 产品候选独立审查

结论：CPA 线放行。候选 `d563ef2862931d4ce836d4372140ee4e4f31ca19`，基线 `f4dd43e4c217ca6eb7762ca4d4f747665280657c`。只读核对真实 diff、实现、契约及 `cpa/RECEIPT.zh-CN.md`、`usage-go-test.log`、`usage-go-test.exit=0`；复用同一产品输入的专项，不重复执行测试或真实请求。

新增 `GET /v8/management/bps/credentials/usage` 独立于原 access，沿用管理鉴权并精确核对 BPS 引用。固定同一 BPS bearer/account_id、Wham 地址与已核 User-Agent，有效代理、父请求最多5秒、拒绝重定向；查询前后身份、token、注册版本与有效代理漂移时丢弃结果。仅投影 `source=chatgpt_account_usage`、白名单 plan 及主 rate_limit 的两窗口；合法0、reset-only 与未知分离，非200和非法结构归稳定错误，不回传原文、身份、模型、credits 或其他额度桶。未发现刷新、重登、推理或凭证/配置写路径，旧 access 的 `quota:null` 契约未改。

专项已覆盖管理鉴权和精确引用、数据/HTTP分类、取消预算、身份/代理漂移、无写入以及真实脱敏数值和合法0等反例，退出0可引用。无 schema、SQL 或数据库动作。本结论仅放行 CPA 候选；Manager/Web 展示、快照接入、组合构建和产品验收仍待对应证据，`quota_product_ready=false`、`write_release_ready=false`。

当前责任人：Manager/Web 执行席完成独立用量接入。下一责任人：独立审查席核权限/用量分离、快照失效及各展示入口，再由主会话统一集成验证。

## 最终镜像断网组合材料审查

工具提交 `92fa20c94b6ee23c9550abad5692fb3586440197` 仅新增 `integration/**`，产品父提交仍为 `d563ef2`。只读对比原已验证 harness：`run.mjs` 内容等价；`check.mjs` 新增真实 CPA usage、Manager BFF usage、鉴权/连接引用、BPS 快照写查与巡检独立来源断言。引用 `SYNTAX.txt` 的 Node 语法检查与 diff-check 退出0，本席未运行工具或容器。

工具只接受本地最终镜像及完整 ID/revision，核对后按 ID 启动；CPA `network none`，Manager/checks 共享其 namespace，无公开端口，配置与合成 SQLite/凭证均 tmpfs、根只读、日志关闭。合成凭证 access/refresh token 均空；usage 必须由真实公开 handler 返回 `credential_incomplete`、`source=chatgpt_account_usage`、plan/rate 为 null，故本组合不会把断网失败或合成值视为官方用量成功。快照合成数值17单独标明 synthetic，要求 provider=bps、`feature/bps_account_usage` 往返；原 access 的 quota:null、inference_verified=false、auto_recover_eligible=false 继续断言。

巡检检查 provider 往返、keep、零自动动作、未知用量及独立 usage_source/error，最后比较原凭证状态；finally 仅按本次随机 label 清理并核残留。材料可用于主会话唯一最终镜像组合；执行退出码、镜像身份和清理结果仍须实际回执，不以语法通过替代组合通过。无产品、数据库或生产写入。

## Manager / Web 产品候选独立审查

结论：Manager/Web 产品候选 `28258fd65ab0dff4a68dc470b8193e22ade6d076` 放行，专项回执提交 `859b17c`；基线 `f4dd43e4c217ca6eb7762ca4d4f747665280657c`。已只读稳定产品 diff、直接依赖、专项用例及已保存日志。主会话整合 `d26cbf7595ecf41ba8cbfd90c956291ac351b471` 后报告整棵 `components/cpamp` 与候选相同、比较退出0，可复用候选专项。本席未修改产品、重复专项或构建，未操作容器、读取真实凭据或请求官方。

`/api/bps/usage` 先管理员鉴权、再核连接引用，调用 CPA 独立 usage GET，结果校验 provider/source/引用/时间及错误结构并保留 no-store；连接前后发生实质变更时拒绝回执。BPS access 与 usage 分别记录，任一失败不会覆盖另一项的独立结论。主动查询、前后端巡检、列表/卡片/详情、监控和快照链均接入；显示“官方账号用量”、套餐、窗口及巡检观察时间，合法0与 reset-only 保留，失败为错误或未知。模型范围固定 `feature/bps_account_usage/complete=false`，不外推 BPS 专属、共桶或全模型额度；巡检维持 keep、isQuota=false、autoRecoverEligible=false 及零自动管理动作。旧 access 契约和其他 provider 的业务逻辑保持。

已沿完整链核身份：CPA 的查询快照保护由 A 线覆盖；BPS 主动 store 与页面快照缓存用独立身份键，包含文件、provider、index、账户身份和文件更新时间。巡检摘要保留原 provider/index/account_id/accountSnapshot，匹配当前身份、至少一项账户身份存在且观察晚于文件更新后才构造状态；空覆盖不回退旧 store。持久快照的 BPS 专用元组键用于 Write、Query 和巡检入库；Web 请求真实携带 account_id 与 accountSnapshot，查询后页面缓存继续按 BPS 身份键读写。其他 provider 继续使用原身份算法。已核前端同名同 index 换账户、旧 last_refresh 配新 modtime及缺记录身份拒绝；SQLite 用例实际写入17后，仅换 account_id 或仅换 accountSnapshot 均查空，原账户仍读到原窗口，缺身份拒绝建键。

证据复用：执行席回执报告 Go 6包 BPS 专项退出0、Web 9文件295例退出0；最终展示和快照身份补丁仅补受影响项。已核保存的 `manager-web/results-panel-vitest.log`（1文件11例）、`bps-snapshot-identity-go.log`（quotasnapshot 全包10.299秒）、`bps-snapshot-identity-vitest.log`（2文件11例）和格式日志；具体命令、输入等价范围、lint/diff退出码见 `manager-web/RECEIPT.zh-CN.md`。前段专项引用执行席真实命令回报，不冒称本席重跑或已持久化完整日志。最终 Manager 类型检查、构建与断网组合仍由主会话统一执行一次。

无 schema、migration 或手工线上 SQL；产品会沿已有 SQLite 快照表保存新 BPS 用量记录，不清理旧数据。此结论放行本地产品候选，未声明真实页面、真实账号调用或生产部署通过。`official_source_verified=true`、`product_review_passed=true`、`quota_product_ready=false`、`write_release_ready=false`；后二项待主会话最终镜像组合和对应回执。

最终组合工具追加提交 `75bd4134c94723f75858c98382021dbd040fa590` 已只读放行：快照写查移至同次巡检之后，精确核文件/index及固定合成 accountId，再使用巡检记录的完整身份元组；不从 sub 或显示标签补账户身份，scope 保持 `feature/bps_account_usage`。该追加不改变 run 边界或增加真实请求；本席未运行。

当前责任人：主会话完成唯一最终 Manager 构建及断网组合，记录镜像身份、真实退出码和归属清理。下一责任人：主会话根据组合结果准备交付或定向修复；未经实际生产回执不写部署完成。

## 最终镜像组合身份边界审查

结论：本次组合未通过，发布仍阻塞。已只读 `integration/RUN.jsonl`、`RUN.exit=1`；实际 CPA 镜像为 `sha256:7adb02d9f8674f808e87af95b4c9244a6e73511551a0e6982514a60cbd3b4327`、revision `d07b4b1ed4845be0b2474a50ec75d39c254b937e`，Manager 为 `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd`、revision `d26cbf7595ecf41ba8cbfd90c956291ac351b471`。鉴权、access、usage、模型及巡检阶段通过，随后在 `snapshot_exact_inspection_identity` 退出；没有快照写查通过回执。finally 清理 `verified=true`、`synthetic_state_retained=false`。此前静态放行和专项通过保持各自范围，均不等于该最终组合通过。

已沿真实代码边界核查：巡检 DTO 的 `AccountID` JSON 名是 `accountId`，工具断言与此一致；固定合成凭证确有 `metadata.account_id=synthetic-bps-account`。CPA `buildAuthFileEntryLocked` 当前输出 email/account/project，但未把 BPS 的 account_id 投影到凭证列表；仅 Codex 的 id_token claims 有独立账户投影。Manager 从 `/v0/management/auth-files` 原始列表经 `cpaauthfiles.FromMap`、`toAccount`、`resultFromAccount` 形成巡检结果，入口未提供账户 ID 时该链不会恢复真实 account_id。此为产品列表与消费端之间的身份边界缺口；工具不能用 sub、显示标签或省略 accountId 替代所要求的真实身份。

最小修复范围为 BPS 专属的凭证列表账户投影，并覆盖 v0/v8 共用公开 builder 及缺失字段反例；Manager 现有真实账户传递与完整快照键应继续保持。修复后由主会话按变更输入补受影响专项、对应镜像和断网组合；本席仅复核候选和回执，不自行重跑或发真实请求。`integration_passed=false`、`quota_product_ready=false`、`write_release_ready=false`。

补充已核专项原始证据提交 `e6b755c4f70e05aee32e727dc289cc0b35c01e55`：`manager-web/prior-tool-receipts.json` 保存原 call_id、完整 stdout 和 exit0；本席核到 Go 6包全部 ok、Web 9文件295例及受影响 ESLint 的原始工具回执。此为既有专项证据补齐，未重跑、未改变产品输入，也不覆盖上述实际组合失败。

当前责任人：主会话安排 CPA BPS 列表身份最小修复，执行席提交公开入口反例。下一责任人：独立审查席核新边界，主会话完成受影响镜像与组合回执。

## BPS 平铺列表身份修复审查

候选 `bb9686514378693042b89c100cafdbdd2cda3a7d` 定向放行。产品仅在 CPA 共用 `buildAuthFileEntryLocked` 对 BPS 增加 `metadata.account_id` 的非空字符串投影，v0/v8 均经过该 builder；不依赖 id_token，不从 sub、标签、邮箱补账户 ID。其他 provider、Manager/Web、用量请求及凭证存储逻辑未改。该投影补齐 `account_id → FromMap.AccountID → 巡检 accountId → auth_account_id_snapshot`，不放宽既有完整快照身份键。

已只读实际临时平铺 JSON 与公开 v8 凭证列表专项、v0/v8 共用 builder 的有效/空/缺失/坏类型/其他 provider 五场景，及 `cpa/identity-listing-go-test.log`、`.exit=0`。用例核真实 account_id/index/provider、无 token/refresh/sub 回显及零凭证存储写入。引用该最小专项，本席未重跑。组合工具仅增加五项布尔诊断，原 `accountId===synthetic-bps-account` 精确断言及快照写查保留；不记录身份原文、不添加上游请求。

该结论放行修复代码和专项，不覆盖已保留的旧组合失败。Manager/Web 产品输入未变可复用既有镜像及专项；主会话只重建受影响 CPA，再执行匹配镜像的断网组合，实际退出0和清理回执到齐后方能关闭组合阻塞。无 schema、SQL、迁移或生产动作，`integration_passed=false`、`write_release_ready=false` 继续保持至新回执。

当前责任人：主会话重建本修复 CPA 镜像并补最终组合。下一责任人：主会话核新回执，独立审查席仅按新差异或失败补核。

追加专项提交 `d9b742c1a637c75b4018aa6b8a57d005088d1adf` 已只读放行：产品与 `bb96865` 等价，公开列表用例改经实际 `registerManagementRoutes` 注册，同时请求 `/v8/management/credentials` 与 Manager 使用的 `/v0/management/auth-files`，读取无 id_token 的真实临时平铺 JSON，核同一 account_id/index/provider、无凭证泄露和零写入。`identity-public-go-test.log/.exit=0` 已核；该补证不要求重复产品构建。最终组合仍待主会话真实回执。

## 最终本地候选放行

结论：最终本地候选放行，`integration/RUN-FINAL.jsonl` 与 `.exit=0` 已只读核证。CPA 为 `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b`、revision `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c`；Manager 复用 `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd`、revision `d26cbf7595ecf41ba8cbfd90c956291ac351b471`。与 `build/CPA-FINAL-IMAGE.json`、`MANAGER-IMAGE.json` 身份一致；CPA 受影响构建及 Manager 一次构建均 exit0，后者包含 tsc/Vite/Go，运行环境代理变量均0项。Worker 输入未变的复用记录已核。

最终断网组合的鉴权、access、usage、模型、巡检阶段及五项快照身份布尔值全部通过；BPS 真实公开快照写查往返通过，scope 保持 `feature/bps_account_usage`。complete=passed，归属清理 `verified=true`、`synthetic_state_retained=false`。旧 `RUN.exit=1` 与失败日志继续保留为历史事实，新日志单独证明修复后的镜像组合。本席只读核证，没有重跑构建、容器、测试或官方请求。

本结论覆盖本地代码、专项、最终构建和合成断网组合。真实来源诊断的既有官方观测不外推为新产品在线业务验收；新产品真实页面、真实账户查询以及生产部署仍未验证。`local_candidate_ready=true`、`integration_passed=true`、`online_product_verified=false`、`production_verified=false`、`write_release_ready=false`。

当前责任人：主会话统一提交本轮证据和交付状态。下一责任人：主会话按已确认范围安排新产品在线验收或发布准备；本独立审查席结束 review 文件写入。
