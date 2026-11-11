# 完整登录入口验收

2026-11-11。产品基线`b48ff4f4248e4ef939406a229553982cf730cd61`；工具源提交`830a94110f1a71a5341bcf0d30b9e69d80767f14`，主线整合`7fdd7a5`。没有产品源码或构建输入变化。

原专项窗口未启动和配置Worker，实际`password_login_configured=false`，符合两张2FA卡的依赖门控。完整工具同时启动固定CPA、Manager和Worker，保持门控，独审放行。三脚本整合后与源提交无差异，README仅复制命令路径调整；复用已通过断网预检和产品专项。

| 实际核验 | 结果 | 证据 |
|---|---|---|
| 新完整窗口启动 | exit0，三固定镜像及内部namespace通过 | `MANUAL-START.jsonl/.exit` |
| Manager能力及鉴权 | true；缺管理员key401 | 同上 |
| Worker健康与鉴权 | 正确key200，错误/缺失401，未知任务404 | 同上 |
| 两通道及活动批次 | 均空 | 同上 |
| 浏览器两处2FA输入 | 分别输入非凭据文字，均settable且值存在；随后清空 | `BROWSER-INPUT.ax.txt`、`BROWSER-CLEARED.ax.txt` |
| 四种登录入口 | 两处2FA、Codex OAuth、BPS OAuth均实际显示 | `BROWSER-INPUT.ax.txt` |
| 独立审查 | 放行；录入与真实登录分开判定 | `review/REVIEW.zh-CN.md` |

`BROWSER-INPUT.png`可见BPS文字录入及Codex OAuth；两张输入框共同录入以完整AX为证。未点击检查列表、开始批量登录或生成授权链接，没有提供账号、密码或TOTP，没有创建真实登录任务。`two_factor_input_verified=true`；`two_factor_live_verified=false`。

已结束的BPS专项窗口中，既有服务端只读巡检为HTTP200/allowed=true/keep，凭证总数和探测集合均1，无删启停或重登；见`LIVE-READONLY.json`。模型发现与发布沿用前窗口真实证据，本轮不重测。旧专项窗口按归属清理exit0、令牌状态未保留，见`MANUAL-WINDOW-CLEANUP.jsonl/.exit`。

新入口`http://127.0.0.1:11034/management.html#/oauth`，归属`mx-full-login-manual-19e8a490cf66`，截止`2026-11-11T10:13:43.149Z`（北京时间18:13:43）。状态与私有访问文件在`C:/Users/子羽/.codex/artifacts/mx-full-login-manual-19e8a490cf66/`，密钥不入仓。过期只停进程，完成后仍需工具stop清理。

无SQL/schema迁移、推送或OVH动作，`write_release_ready=false`。当前责任人：主会话归档交付并最终按归属清理；下一责任人：用户在新完整窗口继续已确认范围的操作。
