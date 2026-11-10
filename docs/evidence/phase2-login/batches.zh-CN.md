# 批次与入口专项

代码输入：基于 `a6731e26d574aa9356e05b2f1cbb8b90c4d3783e` 的本提交产品改动。工作树 `D:/MX-CPAMP-worktrees/phase2-login`。

- Manager目录，Go1.26.10，`GOTOOLCHAIN=local`，`go test ./internal/service/oauthlogin ./internal/http/controller/oauthlogin`：退出0，两包通过。2026-11-12工具回执显示服务包0.312s、控制器包0.110s。
- Web目录，Node22.23.4执行 `../../node_modules/vitest/vitest.mjs run src/services/api/loginTasks.test.ts src/services/api/loginBatches.test.ts src/features/oauth/InternalLoginCards.test.tsx`：退出0，3文件12项通过（01:17:10，1.07s）。Vite既有native-loader提示及路径提示未影响专项退出状态。

Manager覆盖串行/清理确认、普通失败继续、坏行和重复、摘要秘密哨兵、编号去重、单任务不得插队、后台TTL、取消、未知导入/清理失败中断、目标连接变更；页面覆盖解析边界、材料清空、额外交互诊断、离页不取消/重新查询、丢回执不重交、404终止、取消与OAuth链接/复制/ACK不冒充成功。

本项为假上游及页面专项，不等于真实批量登录或生产验收。最终Web/Manager构建将在并发管理页面接入后统一进行。无SQL/schema迁移。
