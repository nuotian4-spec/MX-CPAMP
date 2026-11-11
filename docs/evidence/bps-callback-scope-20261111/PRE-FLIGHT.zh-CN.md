# scope候选组合预检

日期：2026-11-11。仅新增本轮[scope-preflight.mjs](scope-preflight.mjs)，原手工验收启动器及已审Manager候选预检保持原状。

| 组件 | 固定ref | 实际镜像ID | revision |
| --- | --- | --- | --- |
| CPA | `mx-cpa:bps-callback-scope-20261111` | `sha256:d96e26782b4b080a703ee9668bf1c653e18cf164035b2bd9e11bcac2d11023fd` | `346b9479e12bc45ccb0e72556acc4cad4e9a0a04` |
| Manager | `mx-cpamp:oauth-input-diagnostics-20261111` | `sha256:cdf235bee2e22f2c33af145b8fb4d27e93b4478162b8738dcadd3595f3b74c4a` | `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44` |

实际inspect核上述固定ID/revision及linux/amd64，不接受任意同名tag。

## 命令与结果

工作目录：`D:/MX-CPAMP-worktrees/manual-bps-oauth`。

```powershell
& 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe' --check docs/evidence/bps-callback-scope-20261111/scope-preflight.mjs
& 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe' docs/evidence/bps-callback-scope-20261111/scope-preflight.mjs
```

两条命令exit0。仅执行一次实际组合预检，[PRE-FLIGHT.jsonl](PRE-FLIGHT.jsonl)记录固定身份、health200、capabilities无密钥401/管理员鉴权200且`password_login_configured=false`、面板静态提示存在、磁盘和runtime认证列表均0，以及清理确认。

两容器根只读、数据/配置/密钥tmpfs、日志none；仅回环随机临时Manager入口，CPA业务端口未发布。没有旧账号、Worker、真实OAuth会话、回调兑换、主动刷新或模型请求。静态提示检查不等于渲染后的页面验收；后台令牌调度未硬性封锁，不把启动器自身0次当作完整出站观测。

finally仅核本轮唯一label并删除两个容器，`cleanup.verified=true`，令牌状态未保留。不建立用户授权窗口，不交临时端口，不操作OVH、push或部署。本脚本与证据没有产品diff或SQL/schema迁移。

下一责任人：主会话整合本轮固定组合预检，并将scope合成handler证据与真实OAuth待验分别记录。
