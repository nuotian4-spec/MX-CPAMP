# 工具基础离线回执

输入基线：`ee3279150b7e40b92d40537584d6bd16ceaf9353`；工作树 `D:/MX-CPAMP-worktrees/phase2-feature-probe`；仅新增本目录。产品、镜像和历史回执未修改，无 SQL/schema/数据动作，无构建、联网、登录、真实模型调用、推送或部署。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `node docs/evidence/phase2-tool-image-20261111/verify-fixture.mjs` | 0 | `tool_image_fixture_budget_shortcircuit_redaction=passed` |
| `node docs/evidence/phase2-tool-image-20261111/verify-diagnostics.mjs` | 0 | `public_failure_diagnostics_redaction=passed` |
| `node docs/evidence/phase2-tool-image-20261111/verify-harness.mjs` | 0 | `tool_image_harness_cleanup_revision_redaction=passed` |
| 本目录全部 `.mjs` 逐个 `node --check` | 0 | `all_mjs_syntax=passed` |

离线覆盖：登录及 refresh 失败零业务请求；业务路径工具、图片各一次；任一项 HTTP、传输或终态失败仍继续另一项；无第三个文本请求及工具结果轮次；PNG 尺寸和纯红像素；敏感哨兵不输出；创建回执失败和业务 exit 1 后 finally 清理；镜像 revision 不符及非法输入停止。

当前模型与协议适配暂停：用户最新指定 `gpt-6-luna` 并要求参考 D:/sub2api Excel/BPS 图片协议。上述结果只绑定当前旧模型 payload 工具基础，不证明新模型/协议就绪，也不证明真实 BPS 工具或图片能力可用。下一责任人为主会话，先确定产品移植范围，再适配 payload/fixture、独审和真实两次执行。
