# 登录入口运行拓扑独立审查

日期：2026-11-11。基线：`b48ff4f4248e4ef939406a229553982cf730cd61`。仅只读源码、旧证据与脱敏现场记录；本席不操作现有窗口、真实登录或上游，不重跑产品测试，不改产品。

## 两处 2FA 输入门控

当前禁用来自临时验收窗口缺少 Worker 配置，符合现有产品契约，未见新回归证据。

- `InternalLoginCards.tsx:187` 仅在 `password_login_configured === true` 时采用 `configured`；false、未知、加载和查询错误分别保留禁用状态。Codex/BPS 两张批量卡共用该状态。
- `BatchPasswordLogin.tsx:45` 的 `canCreate` 同时控制 textarea、检查列表、开始登录及提交函数；活动批次、另一通道活动批次或 busy 也会禁用。修复运行拓扑后应自然开放，不解除门控。
- `oauthlogin/service.go:132` 仅判断 Worker URL 和 secret 是否存在。`config/config.go:160` 从 `OAUTH_LOGIN_URL` 和 `OAUTH_LOGIN_SECRET_FILE` 等读取；这不是 Worker 探活结果。
- 现 `bps-access-manual-20261111/access-manual-run.mjs` 只启动 CPA/Manager，不启动 Worker，也未给 Manager 注入上述两项；它用于手工 OAuth、模型发现和只读巡检，覆盖范围不足以验收两条 2FA 链路。

已读旧配置门控专项及 `InternalLoginCards.test.tsx:103-205`：false 禁用两张输入卡但手工 BPS 可用；未知/失败/加载禁用；配置确认后开放；旧实例迟到响应不解锁新实例；已有批次查询取消保留。旧证据 `components/cpamp/tests/evidence/oauth-input-diagnostics-20261111.md` 记录 Go 两包与 Web 三文件 30 项退出 `0`。定向比较 `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44` 至本基线的两张产品卡、相关测试及能力 Service/Handler 无 diff，复用该证据；未重跑。

## 四入口与内部依赖

| 入口 | 链路 | Worker 依赖 |
|---|---|---|
| Codex OAuth | 原页面管理代理 → CPA 原 OAuth 路由 | 无 |
| BPS OAuth | Manager `/api/oauth-login/oauth/*` → CPA v8 OAuth | 无 |
| Codex 2FA 批量 | Manager 批次/任务 → 内部 Worker → Manager 校验并导入 CPA | 有 |
| BPS 2FA 批量 | 同上，profile 为 bps | 有 |

完整窗口应一次带齐 CPA、Manager、真实 Worker；Manager 同时持有 CPA 内部管理连接和 Worker URL/共享 secret。Worker 监听内部 `18417`，`server.mjs` 对包括 `GET /health` 在内的所有路由先做 Bearer 鉴权；secret 至少 32 字符，Manager 与 Worker 使用同一内容。

Worker Dockerfile 为 `USER node`，有 Node、Python 与锁定依赖；需保留实际 UID 可读的 secret 和可写 `/tmp`，使用 `--init` 管理登录子进程。旧 Linux 实证明确 root:1000/0440 可读，root:root/0600 失败；既有临时工具采用 `/run/secrets` tmpfs uid/gid 1000。Worker 不挂载 CPA 凭证、Manager 数据或 Docker socket，不发布业务端口。

Worker 任务子进程过滤父级代理环境；CPA 的 `requests.proxy-url` 不控制该进程。旧本地 `proxy-runner.mjs` 在实际登录 runner 内设置既有 7890 代理后导入 `/app/src/login.mjs`，完整临时窗口需沿实际该路径核输入传递，不能只给 Worker 父进程设置代理便认定生效。本轮不以官方请求检查出口。

Worker 源目录从构建 revision `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` 至本基线无 diff；既有 `mx-oauth-login:phase2-local-20261111` 的 `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` 可复用，不需重建。最近源码变更提交 `a5eaebbb777152ce71900af447289e61e8088316` 在该构建 revision 之前，不据其日期推断镜像过旧。

## 已核真实巡检记录

`LIVE-READONLY.json`：查询时间 `2026-11-11T09:02:36.1146936Z`，能力为 false；巡检任务已完成，目标 `[codex,bps]`、凭证总数 `1`、集合数 `1`。唯一 BPS 结果为 `HTTP 200 / allowed=true / keep`，额度未知，自动恢复不适用，delete/disable/enable/reauth 均为 `0`，未执行自动动作。该记录可证明 BPS 只读权限巡检到达账号；其中没有 Codex 凭证结果，不证明 Codex 实际探测或任何 2FA 登录成功。

## 完整窗口验收建议

新入口稳定后只核受影响运行边界：三固定镜像与 namespace、secret 的 UID 可读性、真实 Worker 带 key 健康 `200`/无 key `401`、Manager 能力无 key `401`/有 key 严格 true、CPA 内部鉴权、空账号及空批次、四入口仍存在及两张 2FA 卡的输入条件。配置存在、真实内部健康、输入开放、账号登录成功分别记录；前面三项不代替最后一项。

既有门控专项输入未变，继续复用。新入口改为显式一小时窗口以容纳人工页面操作，不更改产品任务 TTL；当前活跃窗口保持原状。真实手工/2FA 操作继续由用户发起，验证工具不自动提交密码或 TOTP，不凭控件开放宣称业务完成。

## 完整临时入口定向核证

入口准备放行。执行提交 `830a94110f1a71a5341bcf0d30b9e69d80767f14`，分支 `codex/full-login-window`，执行树干净；仅新增 `docs/evidence/full-login-manual-20261111/` 十个文件。已只读三脚本、README、准备回执、输入等价记录与实际预检输出。

- 三组件绑定完整镜像 ID、revision 及 linux/amd64；CPA/Manager 固定前轮最终候选，Worker 输入等价沿用旧镜像。新随机归属与空 tmpfs，不复用旧凭据、挂载或窗口。Manager 与 Worker 共享 namespace，Worker 原 node 用户、`--init`、UID/GID1000 secret tmpfs、共享密钥及 Manager URL 均完整；仅新面板回环随机端口可发布。
- `preflight/PREFLIGHT.exit=0`：真实三服务运行于 `network none`。Manager 健康/页面 `200`，无 key `401`、未知 OAuth state `404`；能力有 key 严格 true、无 key `401`。Worker 独立使用正确/错误/缺失 key 及未知任务实测 `200/401/401/404`。经 Manager 的 CPA 内部连接成功读取空凭证列表；发布模型、Codex/BPS 最近批次及活动批次均空。
- Worker 正常启动先在与 Tasks 相同的过滤环境下执行同一 `proxy-runner.mjs`，runner 设置 7890 代理并实际导入原 `login.mjs`。空 EOF 在 JSON 解析处拒绝，未进入 `runLogin`；子进程退出、收集缓冲清理有布尔回执。随后真实 `createServer` 和 `Tasks` 使用同一 runner 接受内部健康请求，未创建登录任务或提供账号材料。该证据不证明真实材料路径、官方网络可达或生产出口等价。
- 实际回执记录默认 `ttl_seconds=3600`、对应到期时间、三组件运行及 `cleanup verified=true`、`token_state_retained=false`。到期只停进程，仍需显式 stop，文档与工具一致；旧窗口未清理或变更。
- 三脚本语法、输入等价与暂存 diff 检查均为 `0`。预检后脚本未再改，仅补文档和证据；不重复预检、构建或产品专项。原 Codex OAuth 页面还可引用 `manager-login/web-tests-final.log` 的五文件 67 项通过记录，相关页面最后变更早于本轮；本次门控专项三文件 30 项记录独立保留，不混称同一轮全量。

四入口结构及输入条件由当前源码、严格 true 的实际能力、空批次与既有同输入 React 证据共同支持；断网工具输出 `ui_evidence=existing_react_evidence_reused`，本身不代表实际浏览器录入、四入口真实授权或保存注册通过。

## 完整窗口启动与浏览器录入

主线整合 `7fdd7a5` 后，仅调整 README 本仓路径，脚本输入不变，复用已审预检。已只读 `MANUAL-START.jsonl` 与退出码 `0`：固定同三镜像，Manager 能力 true；Worker 正确/错误/缺失 key 及未知任务为 `200/401/401/404`，凭证/模型、两 profile 最近批次与活动批次均空；启动未建 OAuth 或提交登录。新归属 `19e8a490cf66`，仅回环面板 `127.0.0.1:11034`，`ttl_seconds=3600`，到期 `2026-11-11T10:13:43.149Z`。

实际浏览器输入已核：`BROWSER-INPUT.ax.txt` 同时列出 Codex/BPS 两张 2FA 卡及两处 settable 账号输入框，两者 Value 均为非凭据中文标记“本地输入检查（不提交）”；同份 AX 还包含 BPS 手工授权和 Codex OAuth 入口。`BROWSER-INPUT.png` 可见 BPS 输入框内的同一标记及下方 Codex OAuth 卡，与 AX 一致；图片本身不作为另一张 2FA 卡的可见证据。`BROWSER-CLEARED.ax.txt` 明确记录两输入框 Value 清空的 AX 差异。主会话操作回报未点击检查列表、开始登录或生成链接，未使用真实材料或提交任务。两处实际录入、四入口存在与输入清空已核，真实 2FA 登录、授权、保存注册仍待业务证据。

本席仅查看上述脱敏文本和截图，未操作浏览器、容器或上游。无数据库、推送或生产部署动作。当前责任人：主会话核证与交付；下一责任人：用户按明确范围操作，主会话依据业务回执验收并清理。`two_factor_input_verified=true`、`two_factor_live_verified=false`、`write_release_ready=false`。
