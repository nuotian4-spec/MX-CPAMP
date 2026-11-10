# 登录阶段诊断专项

日期：2026-11-11。产品基线：`49ab9afc75d0a3de11e0647f1b87659e27f03a49`。新增终态 `stage/reason` 白名单，从真实授权执行位置传至 Worker、Manager 与页面；原认证请求顺序、参数、挑战处理、PKCE和身份校验不变。

| 命令（相对各组件目录） | 结果 |
|---|---|
| Node 22.23.4 `node --test components/oauth-login/test/profiles.test.mjs` | 退出0，18项通过 |
| Linux rc1镜像、`--network none --init`，只读挂载Worker源码，`node --test test/worker.test.mjs test/profiles.test.mjs` | 退出0，21项通过；进程组取消与结果清理通过 |
| Go 1.26.10 `go test ./internal/service/oauthlogin ./internal/http/controller/oauthlogin` | 退出0，两包通过 |
| Web `vitest run src/services/api/loginTasks.test.ts src/features/oauth/InternalLoginCards.test.tsx` | 退出0，8项通过 |

覆盖授权、密码、TOTP、工作区和兑换阶段失败、无MFA不标TOTP、MFA缺factor、邮箱验证码分支、后续失败不遗留上一阶段、观察器异常不改变授权、未知字段过滤与秘密哨兵。仅模拟上游与本地专项；没有真实登录、模型调用或生产操作。

本提交是诊断中间交付，最终页面构建与全线集成将在批次和并发管理接入后统一执行；不宣称完整二期已完成。数据库动作：无。
