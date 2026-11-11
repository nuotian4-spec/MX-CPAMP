# 工具定向验收离线结果

日期：2026-11-11。新工具提交为 `35b13aedc70a1496b2fce59bcf8877e0685d1b70`、`607b7fc3fa63cf367952b43bbf344d6e829fa9f6`、`08dfefad901fee8e0bf01b36a03fa723ea967da3`。最终测试输入 `ecd7406d5f1991acdf1ef7b01b7661b94a14ff9d`，其中新增诊断模块来自 `b8ad2cd7f05ec8be281b62cec051749268ad54ad` 的本地cherry-pick；两份诊断模块源码相同。

使用固定Node `C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`。工作目录 `D:/MX-CPAMP-worktrees/tool-retry-harness`。

| 命令 | 结果 | 证据 |
|---|---|---|
| `node docs/evidence/bps-tool-diagnostic-20261111/verify-fixture.mjs` | exit0，27场景 | [fixture.log](fixture.log) |
| `node docs/evidence/bps-tool-diagnostic-20261111/verify-diagnostics.mjs` | exit0，五类正常诊断及未知字段反例 | [diagnostics.log](diagnostics.log) |
| `node docs/evidence/bps-tool-diagnostic-20261111/verify-harness.mjs` | exit0，Docker/fetch替身 | [harness.log](harness.log) |

三项在固定测试输入上执行。首轮夹带custom_tool_call只提交一次请求；续轮夹带custom_tool_call不得通过。两层脱敏保留实际HTTP和带内语义状态，原始正文、身份、凭据哨兵均没有进入输出。

Docker替身验证逐镜像ID与revision门禁、不可变ID启动、容器实际Image、预检零业务、失败后清理与两个目录的运行输入漂移。测试未调用真实Docker、fetch网络、登录或模型请求。

初始工具 `35b13ae` 的十个mjs模块均经 `node --check`，exit0；后续变更的check、fixture和新增diagnostics测试已在最终专项实际导入执行。`git diff --check 3eec7a6..HEAD` 通过。没有新增依赖、产品修改、推送、生产操作或数据库动作；本分支的产品变动仅是A诊断提交的验证依赖。

真实CPA新镜像ID/revision及无凭证隔离预检由主会话在最终产品构建后绑定。当前没有新增真实业务回执，工具仍待用户新授权后运行。下一责任人：主会话整合与独审，冻结最终CPA镜像并交付具体实测范围。
