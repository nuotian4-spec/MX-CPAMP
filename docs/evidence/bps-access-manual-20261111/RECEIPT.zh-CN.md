# 本地入口准备回执

日期：2026-11-11。基线 `2f339f8c6f71fba5161198898d27f5bfaabfe775`，分支 `codex/bps-access-manual`，隔离树 `D:/MX-CPAMP-worktrees/bps-access-inspection`。仅新 manual 证据目录，不含独审的 review 目录。

新增 `access-manual-run.mjs` 与可复制的 PowerShell 步骤，复用既有手工工具。固定最终 CPA/Manager 镜像及 revision，提供 `start/status/verify/stop`；附 `preflight` 执行空凭证断网预检。未改产品、旧证据、主线或生产配置。

| 实际命令 | 退出码 | 证据 |
|---|---|---|
| `node --check docs/evidence/bps-access-manual-20261111/access-manual-run.mjs` | `0` | `preflight/NODE-CHECK.exit` |
| `node docs/evidence/bps-access-manual-20261111/access-manual-run.mjs preflight` | `0` | `preflight/PREFLIGHT.jsonl`、`preflight/PREFLIGHT.exit` |
| 最终 `node --check` | `0` | `preflight/FINAL-NODE-CHECK.exit` |
| `git diff --cached --check` | `0` | `preflight/DIFF-CHECK.exit` |

Node 实际路径：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`。各命令在上述隔离树执行。

预检以最终现有镜像执行，无新构建或联网安装。CPA `network none`、共享 namespace、根只读、空 tmpfs、日志 none、无端口/旧挂载/Worker；ready 和管理鉴权正确，空凭证与空模型确认，BPS 接口存在且缺引用本地拒绝。`verify` 空状态 `pending`，清理 `verified=true`、`token_state_retained=false`。最终仅收窄回执目录字段，预检涉及的启动、请求、镜像及清理输入等价，复用该次结果。

真实 `start`、OAuth、官方 access、刷新、推理和页面保存均未执行。未来一次 UI 操作预算依靠明确步骤，未硬性拦截；后台自动刷新未硬禁、次数未监测，不据此宣称运行期全部请求为零。

无数据库动作。人工保存仅影响本轮 tmpfs 配置。未 push、部署或操作 OVH。

当前责任人：主会话核证及整合；下一责任人：用户完成一次新 OAuth 与页面操作，主会话填脱敏观察、核验和清理。官方 access 在线结果待证。
