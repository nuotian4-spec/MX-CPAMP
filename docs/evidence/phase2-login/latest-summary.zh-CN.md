# 批次终态摘要恢复

日期：2026-11-11。代码输入：`1542120c6c77da8b689c9495cb5f9631938cc47e`加本提交。

`GET /api/oauth-login/batches/latest?profile=codex|bps`返回当前CPA连接与指定profile最近的保留摘要或null，覆盖已完成、已取消与中断；终态仍按1小时清理，重启不恢复。连接绑定使用接收时的CPA URL和管理密钥，与当前解析的setup比较。active接口保持只返回活动批次。

页面挂载时恢复最近摘要，正在提交的新批次不被较晚返回的摘要覆盖。浏览器不保存batch_id或材料；查看旧结果不创建新授权任务。

受影响专项（固定工具链见concurrency-ui.zh-CN.md）：
- Manager目录：`go test ./internal/service/oauthlogin ./internal/http/controller/oauthlogin`，exit0，两包通过；新增终态恢复、profile/CPA隔离、保留期清理测试。
- Web目录：固定Node执行`../../node_modules/vitest/vitest.mjs run src/features/oauth/InternalLoginCards.test.tsx`，exit0，8项通过；新增完成/中断后卸载再挂载恢复，不创建、不取消任务。
- 原始stdout/stderr由本次命令直接Tee保存为`latest-manager-output.txt`、`latest-web-output.txt`。

最终Web/Manager镜像由整合主线统一构建，本修复不重复全量构建。无SQL/schema迁移，无真实账号操作。
