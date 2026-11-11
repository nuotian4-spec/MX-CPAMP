# 二期发布前收口

日期：2026-11-11。现场盘点来源基线 `a4eaecfe48efaa198d4c15d9165e61ac5da77443`。沿用完整验收后统一发布的决定；本地发布工具、分发归档与真实发布边界已完成，生产保持原状。本轮没有真实登录、刷新、模型请求、推送或部署。

## 已完成范围

BPS 2FA、保存注册、同client正常刷新、gpt-6-luna文本、默认原生图片及Responses普通工具往返分别已有真实回执。批次、并发、Chat/Responses普通与SSE、关图、工具截图及历史图片语义已有对应离线证据；不逐协议重复真实调用，不将离线结果写成全形态真实成功。

最新本地候选固定为[scope兼容回执](../evidence/bps-callback-scope-20261111/FINAL.zh-CN.md)中的CPA与Manager；Worker继续复用原产物。此前发布工具与分发归档的固定身份属于旧候选，未重新绑定，`write_release_ready=false`。

## 接续顺序

### 一、完成发布材料与本地持久化边界

当前责任人：主会话。OVH四服务、镜像、两条实际Compose链、挂载、空间和旧发布元信息已归档，不重复盘点。有效配置/state/KV与Manager密钥来源、权限和即时认证核验已接入新工具的prepare及维护前漂移检查；目前通过的是本地合成现场，OVH尚未执行。登录及模型在途仍为unknown，不当成零；HTTPS中转路由仍待验。

旧rc1发布工具固定官方三服务、单Compose基线和旧目标，现生产已有Worker及持久override；不能原样重跑。新材料使用独立release状态，保留旧完成回执及备份，绑定真实现场和固定候选镜像，不把本地tracking当成真实远端。

CPA新增位于凭据扫描树之外的私有bps-state持久挂载与显式state-dir，已有或未知状态早拒绝，不以空目录遮蔽。原卷跨容器重建和绑定复用已有真实Compose合成证据。新停态备份覆盖Manager数据、认证目录、KV及原配置/必要密钥；新bps-state在备份后建立，切换后新增绑定须原位保留，不在切前归档内。临时图片及重启后的旧中转链接不承诺持久可用。

没有SQL/schema迁移；有新增状态挂载、配置及运行时文件写入。保留域名、Base URL、API Key、既有数据和服务拓扑。准备/应用、漂移核验、失败停止和恢复仍分阶段，不交缺证的生产写入命令。

### 二、补齐剩余真实验收

| 项目 | 下一动作 | 边界 |
|---|---|---|
| 手工BPS OAuth | 生成可复制链接、实际官方授权、回调、兑换、唯一保存注册 | 原始问题的关键纵切；2FA成功不替代。新增真实登录前单独说明范围并取得确认 |
| Codex 2FA | 保留web_login/security_check事实，先核受支持交互及可定位证据，再决定一次定向验证 | 不盲重试，不把更换账号或出口当成既定修复；刷新和模型正向结果仍待证 |
| HTTPS中转 | 准备GET/HEAD精确Caddy路由至持图CPA，核真实HTTPS可达及取图，再按明确预算验证 | 用户已选双模式，不静默延期；native默认不需要改Caddy，中转不公开state或上传入口 |
| 工具截图/历史图片 | 复用离线关联与顺序证据；若要宣称组合真实可用，再安排最小组合验收 | 不穷举协议和图片格式，不重复普通附件成功项 |

手工OAuth所需人工交互与额外真实模型请求不包含在已结束的两轮工具预算内。新测试范围确认前，不使用旧预算继续登录或请求。

### 三、统一发布与现场确认

适用验收及新发布材料通过后，再推送已审提交、准备发布资产，交付锁定完整提交的可复制命令。用户确认DEPLOY后只切授权服务；上线核实际镜像、完整Compose、挂载、内部鉴权及管理页面。OVH回执与本地隔离证据分别归档，不以本地出口成功替代现场。

## 当前责任

下一责任人：主会话先准备最小生产只读盘点入口与新版发布执行包，协调手工BPS OAuth的受控验收；独审席只核新增挂载、路由、恢复及发布边界。现有产品和业务通过项不重开，旧403/502及供体三项基线失败继续保留。

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

下一责任人：用户在本机Windows PowerShell运行已审入口的start并回传非秘密ready JSON，主会话继续核验和最终归属清理；无需重复确认同一范围，旧会话、地址及访问文件不复用。Codex正向2FA与HTTPS中转仍待验，旧security_check不盲重试。旧固定分发包不适用于新CPA/Manager，完整验收后再统一重新绑定、推送和发布，不交生产写入命令。
