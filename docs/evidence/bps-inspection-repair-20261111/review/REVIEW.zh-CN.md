# BPS 模型与只读巡检独立审查

基线：`5d431b9be0828f94576d1807e1cc87b822cc1350`。本记录仅由独立审查席维护。

结论：本地候选放行。需求与约定两轴代码审查、后端安全审查、CPA/Manager 最终构建及断网合成组合证据齐备，无剩余阻断项。主线核验头：`e6cc324e952a08d4fab8aa32c94f35c733fb08f2`。下一责任人：主会话归档最终回执与当前态；真实官方 access 与生产发布另按明确范围接续。

## 契约审查

2026-11-11，已核已确认方案 `docs/bps-inspection-repair-plan.zh-CN.md`、CPA 约定、Manager 贡献约定及 A 线 `docs/contracts/bps-access-v8.zh-CN.md` 首版。契约审查通过，无阻断项；产品实现与专项证据待稳定变更后核验。

- 精确引用当前 CPA 实例注册凭证，只允许 BPS；拒绝未知、重复和不匹配参数，不接受令牌、URL 或出口覆盖。
- 只请求固定官方 access GET，5 秒总预算服从更短父截止和取消，不跟随重定向。超时例外仅覆盖该管理探针。
- 管理定位失败返回对应 HTTP 错误；定位成功后的上游失败返回 HTTP 200，并携带 `error`、`upstream_status`，同时 `allowed/models=null`。有效 `allowed=false` 对应空模型数组。调用方须检查正文语义。
- 查询中引用、身份、令牌或出口快照变化时丢弃结果，不跨账号重试。返回白名单字段，不含上游正文、令牌或出口信息。
- 额度保持未知；权限观察、发布、注册和真实推理分别表达。BPS 不进入旧 Codex 删除、启停和自动恢复分支。
- 发现目录仅留页面内存；人工保存只修改现有全局模型配置。既有定时目标保持，无 schema 动作。

本席未执行测试、构建、真实请求或凭据读取。下一责任人：A/B 执行席提供稳定代码变更与同输入专项证据，独立审查席定向核验。

## A 线代码审查

已只读核 `bps-access-inspection` 工作树的管理路由、handler、请求/解析帮助文件、有限超时例外与公开 handler 专项源码。产品与专项提交：`8b4fe6e83637535b7ae4e54c3e42f4bdf602e5a0`；证据提交：`5e55bb7d21f4db3e797e94ef68eac5d5e454cb42`。工作区干净，A 线审查通过，无剩余阻断项。

- 原有管理鉴权包裹新增 v8 路由，拒绝错误引用及非 BPS 类型，不新增 v0 入口。
- 固定上游 GET 与 BPS headers，禁重定向、有限读取及五秒父取消预算均在专用探针内；没有调用登录、刷新、推理、保存或删除函数。
- 官方允许状态、受限/禁用模型过滤、结构失败与空目录的区别符合契约；输出不包含完整上游正文、凭据或出口信息。
- 注册引用、令牌、身份及有效出口在返回前复核。已核补入全局代理热重载反例：无独立代理凭证的有效全局出口改变后返回 `credential_changed`，权限和目录均为 null。

引用执行席同输入证据 `cpa/README.zh-CN.md` 和 `cpa/bps-access-go-test.log`：Go 1.26.11 缓存容器断网运行 `go test ./internal/api -run TestBPSAccess -count=1 -v`，五个顶层测试通过，退出 0；`git diff --check` 退出 0。专项穿过公开 v8 路由和管理鉴权，覆盖父取消、短截止、全局出口/身份漂移、失败 null 语义与零凭证写入。本席读取日志与代码，不重复运行专项。最终 `cmd/server` 编译纳入主会话统一构建。

主会话已整合为产品提交 `69fd9e6` 与证据提交 `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b`，并提供与源产品 `8b4fe6e` 目录等价比较退出 0。沿用上述专项证据；最终 CPA 镜像 revision 绑定主线 `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b`。

已读取主会话 `build/CPA-BUILD.exit=0` 与 `build/CPA-IMAGE.json`：镜像 `mx-cpa:bps-inspection-repair-20261111`，ID `sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc`，revision 与上述主线一致，Linux amd64，运行时代理环境数量 0。

## B 线代码审查

已核产品提交 `affae3c1ca5f8284ebd9a15f39546d0625a70645`、`c357ed531dae8819011304f5ca0b98c8e70b3e26` 及最终产品头 `875dd976c036583dd525b1a10fda07af7b9a43b1`。证据提交 `aee54af44d954df722f39673651f716c7d895f7b`，工作区干净。需求与约定两轴代码审查、后端安全审查通过，无剩余阻断项；完整构建由主会话统一执行。

- BPS 消费正文错误与权限状态，不把 HTTP 200 一律视为权限通过；完整目录不进入巡检历史或新增持久缓存。
- 默认目标仍为 Codex，旧目标配置保持。两端 BPS 结果采用 keep、未知额度、未知推理和禁止自动恢复；请求失败显式进入 BPS 权限未知状态，未知 provider 不进入默认 Codex 探针。
- 权限查询使用展示凭证时的连接引用。列表加载前后引用一致才携带有效引用，查询时不重新绑定新实例；旧引用被拒绝。缺少 BPS 管理能力时普通账号列表沿用原读取，未绑定 BPS 不允许查询。
- 模型 GET 读取全局配置 ETag；PUT 受既有管理鉴权、连接引用和写入锁保护，只写 `/config/oauth/providers/bps/models`，沿用 CPA 的条件更新与 YAML 字段保留实现。
- 目录获取及一键选用只改变页面状态和草稿，人工保存才发布全局目录。保存回读失败后草稿保留并锁定后续写入，局部刷新恢复可信版本；同一连接刷新保留选择，连接切换清除旧观察。
- 全局发布数、官方允许数与本站注册数分别展示，权限不匹配与未确认均不宣称可调用。额度与推理保持未知。
- 无 SQL/schema 迁移，不改变旧定时巡检目标，不触及域名、客户端密钥、Worker 或既有服务拓扑。

已关闭的审查意见对应有效出口漂移、未知状态与通道分发、展示引用绑定、保存回读失效锁写及旧 CPA 普通列表兼容；均附相关定向反例。AccountsPage 的业务变更收敛为一行绑定接入，未保留全页格式重排。

已读取 `manager-web/RECEIPT.zh-CN.md` 和 `manager-web/INTERFACES.zh-CN.md`：执行席记录 Go 五包定向检查退出 0（其中 cpa 包无匹配用例，已明确说明）、Web 六文件 53 例退出 0、最终兼容补丁相关三文件 8 例退出 0、类型和变更 lint 退出 0。53 例与后续 8 例有重叠，不累加为不同用例总数。最终补丁仅改变列表绑定 helper 与专项，Go、类型及其余 UI 验证按输入等价复用。本席不重复执行。

格式回执的实际集合已明确：AccountsPage 保留原排版且未纳入 Prettier 检查；其余新增/变更 Web 文件格式检查退出 0，不把排除项写成通过。

主会话已整合为 `472e438`、`a21eb25`、`de1c4356cd80363c6b28384fdf3e3a704380ec26`，证据映射为 `a47589d`，并提供源组件输入等价比较退出 0；Manager 最终镜像 revision 绑定 `de1c4356cd80363c6b28384fdf3e3a704380ec26`。

已读取 `build/MANAGER-BUILD.exit=0` 和 `build/MANAGER-IMAGE.json`：镜像 `mx-cpamp:bps-inspection-repair-20261111`，ID `sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429`，revision 与上述主线一致，Linux amd64，运行时代理环境数量 0。

## 断网组合入口审查

已只读核 `integration/run.mjs`、`check.mjs`：无有效 access/refresh 的合成 BPS 凭证、三容器共享断网 namespace、无端口发布、只读根与 tmpfs；checks_runner 仅以 Node 入口执行检查，不启动 Worker 或登录服务。真实 API 覆盖管理鉴权、实例引用、模型字段条件保存/清空和手动 BPS 巡检；定时入口关闭，不执行真实上游请求。finally 核本轮唯一归属后清理。

工具提交 `0f8c42800ccf5e788b8335c6d4070aad80a4e25f` 已按实际 BFF 契约断言旧配置版本为 409 `config_changed`，并使用固定摘要字段 `error`。执行席记录两文件 Node 语法检查及 diff-check 退出 0。

已读取 `integration/ATTEMPT-1.jsonl` 和 `.exit`：真实退出 1，断网与管理鉴权边界、access 空凭证语义、模型保存/注册/清空、其他 YAML 保留及旧版本拒绝均通过，失败阶段为 `inspection_config`；归属清理 `verified=true`，合成状态未保留。该轮保留为失败记录，不计为组合通过证据。

已核工具修订 `e6cc324e952a08d4fab8aa32c94f35c733fb08f2`：保留原 collector 配置，仅改本轮手动 BPS 巡检配置；诊断只输出 HTTP 状态与白名单错误类别，不补写首轮未记录的 HTTP 事实。CPA/Manager 产品和镜像输入未变化，复用最终构建。

最终 `integration/INTEGRATION.jsonl` 与 `.exit` 已独立读取，实际退出 0：

- CPA `54e47035…` / revision `06a2f9f…`，Manager `54648d3c…` / revision `de1c4356…` 与已审最终构建完全一致；checks_runner 使用固定镜像且未启动 OAuth 服务。
- 三容器根只读、tmpfs、无 Docker 日志、无发布端口、无外部网络，未启动 Worker。
- 管理未认证返回 401，错误实例引用被拒绝；无 token 合成凭证返回 `credential_incomplete`，额度未知，没有真实 access 请求。
- 读取与草稿不发布；人工保存后真实注册目录更新，清空后凭证保留，其他 YAML 保留，旧版本拒绝。
- 巡检配置 HTTP 200；BPS 集合 1、结果 keep、额度未知、零自动动作，既有 SQLite 记录往返通过，定时入口关闭。
- `complete.result=passed`，归属清理 `verified=true`，合成状态未保留。

本结论限于本地代码、构建和合成组合。未执行真实官方 access、登录、刷新、推理、线上巡检、push 或部署；不宣称生产成功、官方权限通过或真实推理成功。审查到此收口，不追加调查或重复检查。
