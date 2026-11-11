# 二期发布前收口

日期：2026-11-11。现场盘点来源基线 `a4eaecfe48efaa198d4c15d9165e61ac5da77443`。沿用完整验收后统一发布的决定；本地发布工具、旧候选分发归档与真实发布边界已完成，生产保持原状。修复版手工BPS OAuth在本机隔离环境完成一次真实保存注册验收及清理，未追加主动刷新或模型请求，未推送或部署。

## 已完成范围

BPS手工OAuth、BPS 2FA、保存注册、同client正常刷新、gpt-6-luna文本、默认原生图片及Responses普通工具往返分别已有真实回执。批次、并发、Chat/Responses普通与SSE、关图、工具截图及历史图片语义已有对应离线证据；不逐协议重复真实调用，不将离线结果写成全形态真实成功。

最新本地候选固定为[BPS模型与巡检回执](../evidence/bps-inspection-repair-20261111/FINAL.zh-CN.md)中的CPA与Manager，包含已通过的scope兼容修复；Worker继续复用原产物。此前发布工具与分发归档的固定身份属于旧候选，未重新绑定，`write_release_ready=false`。

## 接续顺序

### 一、完成发布材料与本地持久化边界

当前责任人：主会话。OVH四服务、镜像、两条实际Compose链、挂载、空间和旧发布元信息已归档，不重复盘点。有效配置/state/KV与Manager密钥来源、权限和即时认证核验已接入新工具的prepare及维护前漂移检查；目前通过的是本地合成现场，OVH尚未执行。登录及模型在途仍为unknown，不当成零；HTTPS中转路由仍待验。

旧rc1发布工具固定官方三服务、单Compose基线和旧目标，现生产已有Worker及持久override；不能原样重跑。新材料使用独立release状态，保留旧完成回执及备份，绑定真实现场和固定候选镜像，不把本地tracking当成真实远端。

CPA新增位于凭据扫描树之外的私有bps-state持久挂载与显式state-dir，已有或未知状态早拒绝，不以空目录遮蔽。原卷跨容器重建和绑定复用已有真实Compose合成证据。新停态备份覆盖Manager数据、认证目录、KV及原配置/必要密钥；新bps-state在备份后建立，切换后新增绑定须原位保留，不在切前归档内。临时图片及重启后的旧中转链接不承诺持久可用。

没有SQL/schema迁移；有新增状态挂载、配置及运行时文件写入。保留域名、Base URL、API Key、既有数据和服务拓扑。准备/应用、漂移核验、失败停止和恢复仍分阶段，不交缺证的生产写入命令。

### 二、补齐剩余真实验收

| 项目 | 下一动作 | 边界 |
|---|---|---|
| 手工BPS OAuth | 本机修复版一次真实授权、回调及唯一保存注册已通过，归属清理确认 | [最终回执](../evidence/bps-callback-scope-20261111/MANUAL-RESULT.zh-CN.md)；生产尚未切换 |
| Codex 2FA | 保留web_login/security_check事实，先核受支持交互及可定位证据，再决定一次定向验证 | 不盲重试，不把更换账号或出口当成既定修复；刷新和模型正向结果仍待证 |
| HTTPS中转 | 准备GET/HEAD精确Caddy路由至持图CPA，核真实HTTPS可达及取图，再按明确预算验证 | 用户已选双模式，不静默延期；native默认不需要改Caddy，中转不公开state或上传入口 |
| 工具截图/历史图片 | 复用离线关联与顺序证据；若要宣称组合真实可用，再安排最小组合验收 | 不穷举协议和图片格式，不重复普通附件成功项 |

手工OAuth所需人工交互与额外真实模型请求不包含在已结束的两轮工具预算内。新测试范围确认前，不使用旧预算继续登录或请求。

### 三、统一发布与现场确认

适用验收及新发布材料通过后，再推送已审提交、准备发布资产，交付锁定完整提交的可复制命令。用户确认DEPLOY后只切授权服务；上线核实际镜像、完整Compose、挂载、内部鉴权及管理页面。OVH回执与本地隔离证据分别归档，不以本地出口成功替代现场。

## 当前责任

下一责任人：主会话准备最新候选的一次定向真实BPS access与模型页面验收材料，再绑定明确范围。既有生产只读盘点、发布工具及挂载/恢复证据按输入复用，分发资产待最终候选验收后重新绑定。现有业务通过项不重开，旧403/502及供体三项基线失败继续保留。

## 当前接续回执

本地材料已完成并整合：

- [状态持久化专项](../evidence/bps-state-durability-20261111/README.zh-CN.md)，源提交 `ad35bc8`，主线 `7a9001a`：固定CPA镜像、真实Compose原卷跨容器重建、最小绑定及合成工具续轮通过；错挂空卷被本轮验收guard阻断，清理确认，exit0，独审通过。该guard尚未接入生产发布工具；错误state-dir明确拒绝但CLI退出0的边界继续保留。
- [服务器短盘点入口](../evidence/phase2-release-preflight/short-preflight.sh)，源提交 `3794ab3`，主线 `443988c`：43行未编码here-doc，实际只执行Docker元信息和文件元信息读取。真实Linux/双Compose/Caddy旧链反例、秘密哨兵与前后状态一致测试exit0，短入口独审通过。完整脚本的认证与路由能力另有边界，不归入短入口结论。

当前责任人：主会话。用户已回传[OVH现场基线](../evidence/phase2-release-preflight/SERVER-BASELINE.zh-CN.md)，18条JSON完整、独立核证通过：四服务running，旧rc1镜像及revision匹配；三应用双Compose链、Caddy原单链，原挂载与旧备份元信息齐备，可用约91.72GiB，指定bps-state目录不存在。无需重复这次盘点。

新版发布工具代码输入 `39eb250ef8407213a4e7ff7dea444cdc007370f1` 已本地整合；[独立审查](../evidence/phase2-release-review-20261111/README.zh-CN.md)及[真实边界验收](../evidence/phase2-release-boundaries-20261111/README.zh-CN.md)完成：

- prepare不改运行服务/配置/wrapper，真实TTY取消、EOF、非TTY和120.58秒超时在维护前退出；配置/Compose漂移、secret权限及实际Env/Args来源反例早拒绝。
- 真实停止三应用后新备份、候选切换、三文件wrapper、正负认证探针、Caddy原完整ID不变、完成态拒重入通过。维护后备份失败保留stage、停态和maintenance标记，拒绝重复apply，不自动回灌。
- 最终入口/切换末段exit0；旧完整矩阵exit1及其中已通过项按两行shebang输入等价复用，维护失败专项exit0。原失败不改写为整轮成功，具体输入与清理回执见证据入口。
- [镜像分发归档](../evidence/phase2-release-bundle-20261111/README.zh-CN.md)为227124363字节，三组件经典Linux实际load/inspect及定向清理exit0；固定两种已验ID、平台及revision，无新产品构建。

没有SQL/schema迁移；未来apply会停止三应用、新建私有停态备份并写配置/metadata/state文件，Caddy容器不重建，原域名和访问Key不变。OVH尚无本次新备份、加载或切换回执；不把本地验收当成生产成功，也不把旧rc1归档当新停态备份。

用户已确认一次隔离的手工BPS OAuth官方授权、回调、兑换及唯一保存注册验收，不主动刷新或发模型请求。2026-11-11 05:12 UTC，本机窗口已就绪并独审通过，浏览器实际显示Manager登录页；尚未创建OAuth会话或导入账号。临时入口仅监听127.0.0.1:1770，固定CPA/Manager镜像、空tmpfs、无Worker、无旧数据。原后台刷新调度未硬禁；900秒到期仅停进程，仍须主会话执行归属清理。窗口截止05:26:52 UTC，非秘密现场指针为 `C:/Users/子羽/.codex/artifacts/mx-manual-bps-oauth-5256c87f4636/state.json`，临时管理员密钥不进入仓库或聊天。

本窗口已结束：[用户结果](../evidence/manual-bps-oauth-20261111/USER-RESULT.zh-CN.md)为未完成。既有Manager/CPA会话只读查询均wait，最终保存/注册为0；两处2FA误点由未配置Worker的本地503拒绝，不作为上游登录失败。05:26:11 UTC按归属清理两个容器与临时管理员访问文件，未重提回调或创建新授权。原截图/密码/TOTP/完整回调不入仓。

当前责任人：主会话。上述Manager回调400分类与无Worker前端门控已修复、独审通过；[本地诊断候选](../evidence/oauth-input-diagnostics-20261111/FINAL.zh-CN.md)完成Go两包、Web30项、最终Manager构建及无凭据隔离预检，均有对应exit0；首构建TS2769的exit1保留，最小测试类型修复后补受影响项。CPA/Worker未重建。没有追加真实OAuth、刷新或模型请求。

用户已确认填写官方登录后的BPS回调。只读现有官方页签核到code、scope、state三个参数名，未读取参数值；旧CPA白名单漏scope，实际结构命中明确拒绝路径。已最小兼容单值scope，重复及未知参数仍拒绝；真实历史请求的HTTP拒绝码未保留，不补写现场日志。

最新本地修复已完成合成handler红绿回归、独审、一次CPA最终构建及与诊断Manager的无凭据预检，见[固定候选与证据](../evidence/bps-callback-scope-20261111/FINAL.zh-CN.md)。Manager和Worker输入未变，分别复用既有构建；预检临时资源清理确认，没有追加真实OAuth、刷新或模型请求。

当前责任人：主会话。新的15分钟手工OAuth入口材料已准备、固定上述候选身份，用户已明确确认该次验收；验收只含一次官方授权、提交回调及唯一保存注册，不主动刷新、不调用模型，不启动Worker或操作OVH。启动命令在CreateProcess前被自动审批拒绝，工具仅返回`blocked by policy`；只读核本轮容器为0、启动回执不存在，见[启动状态](../evidence/bps-callback-scope-20261111/MANUAL-START-STATUS.json)。没有新授权窗口或账号导入，不通过其他通道绕过拦截。

用户随后运行已审start并完成该次官方授权；06:18:54 UTC最终verify确认唯一保存注册、profile/client、OAuth方法、令牌存在性及runtime active通过，exit0。实际配置模型清单未设，解释本轮模型0；随后stop归属清理通过、令牌状态不保留，独立核验完成，见[真实结果](../evidence/bps-callback-scope-20261111/MANUAL-RESULT.zh-CN.md)。用户新增巡检反馈已定位：读取到1份凭证，但targetTypes仅codex，且当前界面/后端/探针只支持Codex/xAI，BPS尚未接入，见[缺口记录](../evidence/bps-callback-scope-20261111/BPS-INSPECTION-GAP.zh-CN.md)。当前责任人：主会话，本轮登录验收已完成，BPS巡检待成套方案与实施边界。旧窗口、地址及访问文件不复用，不追加真实巡检或模型请求。Codex正向2FA与HTTPS中转仍待验，旧security_check不盲重试。旧固定分发包不适用于新CPA/Manager，完整验收后再统一重新绑定、推送和发布，不交生产写入命令。

最新实施接续：[BPS模型与巡检本地候选](../evidence/bps-inspection-repair-20261111/FINAL.zh-CN.md)已完成两线实施、专项与独立代码/安全审查；CPA/Manager各一次最终构建exit0，最终镜像断网组合exit0并确认归属清理。模型发现与人工发布分离、各凭证权限展示、BPS只检测记录及未知额度均已接入；无新增SQL/schema，沿用既有SQLite记录和CPA配置。当前责任人：主会话本地收口；下一责任人：主会话准备新候选定向真实access及页面验收材料。官方接口在线结果待证，旧窗口与令牌不复用；无新真实请求、推送或部署，`write_release_ready=false`。

最新真实验收入口：[BPS官方权限与模型页面](bps-access-manual-20261111.zh-CN.md)已完成脚本、无账号断网预检、独审及主线整合。用户要求“下一步”后，主会话仅启动新的空本机面板，`MANUAL-START.exit=0`；固定最终双镜像、凭证0/模型0、鉴权及页面检查通过，无新OAuth/access。入口`127.0.0.1:6722`，截止`2026-11-11T08:50:46.268Z`；非秘密state指针`C:/Users/子羽/.codex/artifacts/mx-bps-access-manual-b32b2eaf5650/state.json`。当前责任人：用户一次新授权→系统设置一次发现→全部选入草稿人工保存→账号仅看发布/注册；下一责任人：主会话核用户脱敏回报、只读verify及按归属stop。若受限/错误/空目录不重试、不保存未知结果。官方真实结果待证，OVH未动，`write_release_ready=false`。

最新窗口接续：旧窗口模型发现已有用户成功回报，本地保存/注册BPS1、发布/注册模型2；巡检截图目标仍Codex，集合0，不属于BPS探针失败。用户要求重启后，主会话清理已到期旧归属并创建新空窗口，两步exit0，见[重启回执](../evidence/bps-access-manual-20261111/MANUAL-RESTART-1.zh-CN.md)。新入口`127.0.0.1:13958`，截止`2026-11-11T09:08:56.055Z`，新state指针`C:/Users/子羽/.codex/artifacts/mx-bps-access-manual-6afd1f5dfa6d/state.json`。当前责任人：用户重新手工授权后，只补服务端目标BPS的一次只读巡检；前次模型证据复用。下一责任人：主会话核真实结果并stop新归属；尚无BPS真实巡检通过结论，OVH未动。
