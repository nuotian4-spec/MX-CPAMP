# 完整登录窗口准备回执

基线 `b48ff4f4248e4ef939406a229553982cf730cd61`，分支 `codex/full-login-window`，执行目录 `D:/MX-CPAMP-worktrees/bps-access-inspection`。仅新增本目录，独审另归主会话。

| 实际检查 | 退出码 | 证据 |
|---|---|---|
| 固定Node对 `run.mjs`、`worker.mjs`、`proxy-runner.mjs` 分别 `--check` | 0 | `preflight/NODE-CHECK.exit` |
| 固定Node执行 `run.mjs preflight` | 0 | `preflight/PREFLIGHT.jsonl`、`preflight/PREFLIGHT.exit` |
| `git diff --exit-code 95dd9ca2c16d5469c43ce7c910f38ff271af45e3 HEAD -- components/oauth-login` | 0 | `preflight/INPUT-EQUIVALENCE.json` |
| 登录页源码、测试与Web依赖相对 `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44` 比较 | 0 | 同上；具体范围见README |
| `git diff --cached --check` | 0 | `preflight/DIFF-CHECK.exit` |

固定Node：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`。实际复制命令见README。

一次network-none预检用最终CPA/Manager和已有Worker完整镜像。三应用真实运行，Worker原node用户成功读取UID1000 tmpfs密钥；Manager能力true，独立Worker正确/错误/无密钥及未知任务分别200/401/401/404。空凭证/模型与Codex/BPS/活动批次确认。Worker启动直接执行实际proxy-runner与原login模块，过滤环境后代理配置成立，空EOF被拒绝，子进程退出与缓冲清理确认；没有任务POST或任何账号材料。默认3600秒真实到期输出，finally清理 `verified=true`。

Worker源码等价，复用95dd9ca revision现有镜像；登录页源码、测试、依赖等价，复用既有React定向专项，未新增产品构建或重复产品专项。四入口结构和能力门控有源码及组件证据，实际浏览器与真实授权/保存注册仍另验。

本轮未真实启动联网窗口、登录、OAuth、access、刷新或推理，未读旧凭据；无数据库动作、push/deploy/OVH操作。后台刷新未硬禁、次数未监测。

当前责任人：主会话独审整合；下一责任人：主会话启动空窗口，用户按明确范围执行真实操作。未把输入可编辑、能力true或health200写成登录成功。
