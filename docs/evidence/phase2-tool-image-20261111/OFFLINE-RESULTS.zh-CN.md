# gpt-6-luna 工具往返离线回执

工具输入：前一独立提交 fe7eec17131d0e6cb29197295453c8ad7f0fae31 后的本目录适配；仅工具、fixture 和文档变化，产品和历史真实回执未修改。没有真实登录、模型请求、Docker、联网、构建、推送或部署。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| node docs/evidence/phase2-tool-image-20261111/verify-fixture.mjs | 0 | text_tool_roundtrip_image_budget_semantics_redaction=passed |
| node docs/evidence/phase2-tool-image-20261111/verify-diagnostics.mjs | 0 | public_failure_diagnostics_redaction=passed |
| node docs/evidence/phase2-tool-image-20261111/verify-harness.mjs | 0 | tool_image_harness_cleanup_revision_redaction=passed |
| 本目录全部 .mjs 逐个 node --check | 0 | all_mjs_syntax=passed |

通过边界：公开请求实际总数0/1/3/4、工具单项最多2；登录/refresh失败零请求；文本任何失败停止后续；工具首轮失败跳过结果但继续图片；完整历史及原call_id结果配对，不携previous_response_id，不执行任意命令；非工具200、非pong续轮200、非红图片200均失败；白名单脱敏；无凭证预检仅健康/鉴权GET；cleanup、revision及输入漂移检查。

下一责任人：主会话统一独审，绑定CPA/Manager/Worker最终镜像完整revision，再按用户预算真实执行。预留 REAL-RECEIPT.jsonl 只在真实执行时创建，当前不存在。没有SQL/schema或生产数据动作。

---

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
