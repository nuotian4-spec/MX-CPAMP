# 登录入口完整运行环境与验收

日期：2026-11-11。产品基线`b48ff4f4248e4ef939406a229553982cf730cd61`。用户反馈Codex+BPS巡检已找到账号，但两个2FA表单不能输入，要求避免逐个入口返工。按pua-p9分工实施，本轮修正临时验收拓扑，不修改前端依赖门控。

## 已核事实

当前专项窗口只启动CPA与Manager，未配置Manager的`OAUTH_LOGIN_URL`和`OAUTH_LOGIN_SECRET_FILE`。实时能力接口返回`password_login_configured=false`；两处2FA均消费这一能力，因此输入、检查和提交同时禁用。手工OAuth不依赖该Worker配置。旧组件专项已经覆盖配置存在/缺失/未知/失败、切实例和批次互斥，相关产品输入未变，可复用。

09:02:36 UTC只读运行记录确认BPS真实巡检通过：目标Codex+BPS，读取1、集合1、BPS HTTP200/allowed=true/keep、额度未知、零删启停或重登。没有Codex凭证结果，不外推Codex探测成功。脱敏记录见[实时摘要](../evidence/login-entry-closure-20261111/LIVE-READONLY.json)。本轮主会话只读已有结果，没有补发官方请求。

本地保存/注册BPS1且active，profile/client、方法及令牌存在性通过；本窗口按约定未重复发布模型，模型0。旧模型发布/注册2的通过事实按前窗口证据复用，不把新工具pending写成整轮通过。原专项窗口已由已审stop按归属清理，exit0、token_state_retained=false，证据位于本轮目录。

## 一次覆盖的依赖与入口

| 入口 | 完整运行依赖 | 本轮核验 |
|---|---|---|
| Codex手工OAuth | CPA原管理授权路由 | 路由/页面入口和既有同输入证据，不创建授权会话 |
| BPS手工OAuth | Manager→CPA | 独立可用，不因2FA能力改变；既有真实保存及巡检证据复用 |
| Codex 2FA | Manager→Worker→CPA | 已配置能力true、内部Worker鉴权与健康、空批次时可输入；不提交登录材料 |
| BPS 2FA | Manager→Worker→CPA | 与Codex同一能力依赖、profile独立，批次互斥状态明确 |

能力true仅说明URL与密钥已配置，还须核Worker正确密钥health200、错误及缺密钥401、未知任务404。Worker以node用户运行，共享密钥须对其可读；代理子进程路径复用既有runner并穿过实际预检，不能仅给父进程环境变量后声称真实登录代理可用。

## 分工与完成标准

执行席`bps_access_manual`在原A树独立新分支`codex/full-login-window`实施，仅写`docs/evidence/full-login-manual-20261111/`，不含review。WHAT：完整三应用的start/status/verify/stop与断网preflight、固定镜像、实际鉴权、代理输入路径及清理回执。主会话P9负责执行包、整合、启动和交付；独审`callback_compatibility`只读入口，独占`docs/evidence/login-entry-closure-20261111/review/`。

HOW MUCH：复用已有三镜像和未变产品专项，不重建产品或增加依赖；默认本机临时窗口1小时并输出实际到期，仍须显式按归属清理。DONE：语法、diff和完整三应用断网预检exit0；实际Worker鉴权、Manager能力、空批次与四入口依赖证据齐备；独审放行后才启动完整空窗口。仅本机Manager回环端口可访问，CPA/Worker不发布，根只读、状态/密钥tmpfs、日志关闭。

DON'T：不移除disabled、不伪造capability、不触发真实OAuth、access、刷新、推理或2FA任务，不读旧凭据；不修改产品或OVH，不push/deploy。表单可输入、内部连通与真实2FA成功分开记录；Codex既有security_check仍保留原结论。本轮不要求用户重测已通过的模型发现或巡检。

无SQL/schema迁移。临时环境的空SQLite、配置和密钥只在本轮tmpfs；旧业务结果均已脱敏归档。

## 完整窗口回执

执行提交`830a94110f1a71a5341bcf0d30b9e69d80767f14`已独审放行并整合为`7fdd7a5`；仅README复制命令改为主仓路径，三脚本字节未变，复用一次断网预检。2026-11-11 09:13:47 UTC真实start退出0，三固定镜像身份、共享namespace和tmpfs边界通过。Manager能力true，Worker正确/错误/缺失密钥及未知任务200/401/401/404，两profile和活动批次均空。

入口为`http://127.0.0.1:11034/management.html#/oauth`，状态指针`C:/Users/子羽/.codex/artifacts/mx-full-login-manual-19e8a490cf66/state.json`，截止`2026-11-11T10:13:43.149Z`（北京时间18:13:43），默认3600秒。访问密钥只在该私有目录的本机访问文件中，不入仓。

主会话使用浏览器登录该本机窗口，记住凭证为关闭；四入口均实际显示，两处2FA账号列表分别输入非凭据中文标记，AX确认两者均可设置且值存在，随后清空并核对。未点击检查列表、开始批量登录或生成授权链接，没有提交账号或登录任务。浏览器证据见`BROWSER-INPUT.ax.txt`、`BROWSER-CLEARED.ax.txt`和`BROWSER-INPUT.png`；启动检查见`MANUAL-START.jsonl/.exit`，均在本轮证据目录。新面板页签已保留。

当前结论：完整运行拓扑与两处2FA实际输入通过，真实2FA授权、保存注册仍另验。已通过的模型发现和BPS巡检不重开，Codex历史security_check结论不变。当前责任人：主会话归档、交付与最终按归属清理；下一责任人：用户在新窗口按已确认范围操作。`two_factor_live_verified=false`、`write_release_ready=false`。
