# BPS 账号用量页面验收工具独立审查

2026-11-11。结论：静态放行，由主会话执行一次断网 preflight，再启动全新空窗口交用户自主页面验收。

审查输入 `44297cad0670f061dd42182a57fb1541cc6c3fc0`，基线 `6d7cbc6dfcbc72ceb6ac057539c045d873873622`。变更仅新增 `docs/evidence/bps-quota-manual-20261111/` 下五个工具及说明文件；产品、旧完整登录工具及旧回执未改。只读核对代码、当前接口契约和证据，未运行测试、构建、容器、真实登录或官方请求。

## 固定输入与窗口边界

| 组件 | ref | 完整镜像 ID | revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-quota-identity-20261111` | `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b` | `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` |
| Manager | `mx-cpamp:bps-quota-repair-20261111` | `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd` | `d26cbf7595ecf41ba8cbfd90c956291ac351b471` |
| Worker | `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

三镜像与产品 `bps-quota-repair-20261111/FINAL.zh-CN.md` 一致。启动先核 ref、完整 ID、revision 及 linux/amd64，实际按 ID 运行。产品专项、最终构建与组合证据复用，不重新执行。

- 新随机容器前缀 `mx-bps-quota-manual-`，归属 label `mx.bps-quota-manual`，不复用旧容器、令牌或业务数据。
- 仅 Manager 的 18317 监听端口发布到宿主 `127.0.0.1` 随机端口。CPA 8317 与 Worker 18417 无宿主发布，Manager/Worker 共享 CPA 完整容器 ID 对应的 network namespace。
- 根文件系统只读、Docker 日志 `none`，业务状态和配置/密钥放 tmpfs。无业务卷、Docker socket；Worker 唯一 bind 为只读 `/checks` 工具目录。
- 四类随机密钥经标准输入写入容器 tmpfs；不写入 `state.json` 或终端。管理员访问文件位于本机私有 ACL 目录。核验只输出白名单状态、数量和布尔，不输出账号、令牌、窗口值或原始错误。
- 清理逐容器校验本窗口 owner label，再删除并查询该 label 残留；删除访问文件并标记 cleaned。TTL 默认 3600 秒，到期仅停止进程，说明明确要求显式 stop 收尾。
- preflight 的根 CPA 为 `network none`、无发布端口；伴随容器共享该 namespace，结束后按本窗口归属清理。

## Worker 与本地读取契约

完整 Worker 门控保留：Manager 能力为 true、缺管理员鉴权 401；Worker 正确密钥 health 200、错误/缺密钥 401、未知任务 404；Codex/BPS 最近批次和 active 批次在空窗口必须为空。Manager 与 Worker 使用同一随机共享密钥。

`worker.mjs`、`proxy-runner.mjs` 的原样复用见 `CHECKS.json` 字节等价记录。已读两者：空 EOF 经 Tasks 所用代理执行入口及原 `login.mjs`；执行进程在环境过滤后设置 7890 代理，要求空输入失败、进程退出及探针缓冲区清空，只保存 startup 布尔。未创建登录任务或提交账号材料。

新增 verify 与当前 controller → service → repository 契约吻合：

- `GET /v0/management/codex-inspection/runs?limit=20` 返回 `{items: runs}`；repository 用零长非 nil slice，空表返回 `[]`。
- `run.id` 为 `int64` JSON 数值；新临时数据库内的正整数 ID 满足 `Number.isSafeInteger`。详情返回 `RunDetail`，`results` 同样为空数组或结果数组。
- BPS 结果的 `errorDetail` 中 `usage_source`、`usage_upstream_status`、`usage_error`，以及 `quotaWindows` 与当前生产者字段一致；verify 只累计合格来源观察数量，不回显摘要正文。
- verify 读取本地凭证列表与至多 20 条已有服务端巡检详情。未调用生成 OAuth、提交登录、access、usage、刷新、推理或启动巡检入口。

语法与 diff-check 引用执行席 `CHECKS.json`：三脚本 node --check 均为 0、diff-check 为 0。独审未重复运行。

## 验收口径与下一责任人

未发现阻断本地空窗口准备的问题。上述结论是工具静态审查；实际 preflight、start 和真实账号页面验收仍待主会话接续。空凭证、能力 true、健康检查及合成组合通过均不代表真实 OAuth、官方用量或页面验收完成。

工具主动官方请求为 0；后台刷新没有硬禁，次数未监测。verify 的观察标记仅针对已保存的服务端巡检，浏览器本地巡检或主动刷新未入巡检记录时不会被计入，用户页面结果须单独确认。

本轮无产品改动、SQL/schema 迁移、生产数据写入、推送或 OVH 部署。下一责任人：主会话合入工具后执行一次断网 preflight，成功再 start 全新空面板；引导用户自主完成 BPS OAuth、一次用量刷新及一次服务端巡检，最后核验并按归属 stop 清理。

## 启动回执复核

整合提交 `03df0337ff5554e4eac92c0f87cee0adcd1d7df9` 的三脚本与已审 `44297cad0670f061dd42182a57fb1541cc6c3fc0` 无差异；README 已明确选择 BPS 的服务端巡检并保持自动巡检关闭。

只读核 `PREFLIGHT.jsonl` / `PREFLIGHT.exit` 与 `START.jsonl` / `START.exit`，未发额外请求：

- 断网 preflight exit 0，三镜像 ID/revision 与上表一致，`external_network=none`；空凭证/模型/批次/巡检均成立，Manager 与 Worker 完整能力和鉴权状态通过，空 EOF 探针各布尔符合预期；结束 `cleanup.verified=true`、`token_state_retained=false`。
- 正式 start exit 0，ready 时间字段和访问入口齐备：`http://127.0.0.1:3303/management.html#/oauth`；state 目录 `C:/Users/子羽/.codex/artifacts/mx-bps-quota-manual-ddbbbfeb3310`；到期 `2026-11-11T12:05:02.982Z`，即北京时间 20:05:02。
- 正式窗口 `credentials.total=0`、`credentials.bps=0`、本地巡检 0、批次为空、`worker_configured=true`，Manager capability 为 true；Worker 正确密钥 200、错误/缺密钥 401、未知任务 404。
- READY 的 `readonly=true`、`tmpfs_state=true`、`logs=false`、`cpa_published=false`；手工 OAuth 未启动，工具主动 access / usage / refresh / inference 均为 0。后台刷新仍未硬禁，不将这些计数字段扩写为所有后台流量已监测。

结论：启动门槛放行，真实页面显示、BPS OAuth、官方用量刷新与服务端巡检待用户完成。主会话回报访问文件和浏览器打开均为 queued，该回报不证明浏览器已实际显示。此次窗口尚无真实账号或官方用量观察，`official_usage_observed=false` 保持原值。

下一责任人：用户自主页面验收；主会话随后仅按已有授权读取本地 verify 回执、记录真实结果并 stop 清理。本席未读取访问文件或真实凭据，未追加测试、容器动作或官方请求。
