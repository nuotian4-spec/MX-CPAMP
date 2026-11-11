# BPS 官方回调 scope 兼容验证

- 基线：`6675be419e6de8806cf5e7f53d14885db4a612c1`
- 产品及测试提交：`ebca93d7c8381530a308ab19df72fef3fdc2ed2a`
- 隔离分支：`codex/bps-callback-scope`
- 产品改动：`handleBPSOAuthCallback` 查询参数白名单接受单值 `scope`。
- `scope` 只作为兼容参数接受，不参与身份、权限或令牌交换。重复参数、未知参数及既有 state/来源校验保持生效。
- 无数据库动作。本专项未执行真实登录、回调、刷新、模型调用、推送或部署。

## 验证输入

全部请求经现有公开 v8 handler：`StartOAuthV8` → `PostOAuthCallbackV8` → `GetAuthStatus`。仅令牌 HTTP transport 使用合成响应；凭证文件保存及运行时注册使用现有真实实现。

- 官方地址结构采用项目固定 `bps.RedirectURI`，会话 state 由本地 handler 生成。
- 合成授权码：`synthetic-code`。
- 合成单值 scope：`openid offline_access email profile organization.read`。
- 重复 scope：两个值 `openid`、`email`。
- 未知参数：`extra=unknown`。
- 合成令牌及用户：沿用既有 `bpsTokenResponse` 测试夹具，不包含真实凭证或完整用户回调。

## 命令与结果

运行环境：Linux 容器，Go `1.26.11`，断网 `--network none`，缓存构建镜像 `sha256:75195664cd12258c72160cf4121c931ae4176719d6545a9e5f35fabfe2abbc55`。

先只添加测试，产品仍为基线行为：

```powershell
docker run --rm --network none --mount type=bind,source=D:/MX-CPAMP-worktrees/bps-callback-scope/components/cpa,target=/app --entrypoint sh sha256:75195664cd12258c72160cf4121c931ae4176719d6545a9e5f35fabfe2abbc55 -c 'gofmt -w internal/api/handlers/management/auth_files_bps_test.go && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/api/handlers/management -run ''^TestBPSOAuthCallbackScope$'' -count=1 -v'
```

真实退出码 `1`，见 `red.log`、`red.exit`。单值 scope 返回 `400 invalid_callback_query`；未知参数仍被拒绝。

白名单增加 scope 后，测试 status 断言按现有 `adopted` 契约核验，最终命令：

```powershell
docker run --rm --network none --mount type=bind,source=D:/MX-CPAMP-worktrees/bps-callback-scope/components/cpa,target=/app --entrypoint sh sha256:75195664cd12258c72160cf4121c931ae4176719d6545a9e5f35fabfe2abbc55 -c 'gofmt -w internal/api/handlers/management/auth_files_bps_test.go && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/api/handlers/management -run ''^TestBPSOAuthCallback(Scope|ValidationAndCompletion)$'' -count=1 -v'
git diff --check
```

两条命令真实退出码均为 `0`。最终专项输出见 `green.log`、`green.exit`：

- 单值 scope 完成一次合成换码、一个凭证文件保存及一个运行时凭证注册，状态为 `ok/saved/adopted`。
- 重复 scope 返回 `400 conflicting_callback_parameters`，未启动换码或保存注册。
- 未知参数返回 `400 invalid_callback_query`，未启动换码或保存注册。
- 原有无 scope 正常回调及错误 state、重复 state、错误 path、未知参数用例通过。

`status-contract.log`、`status-contract.exit` 保留同一最小产品修复后、最终测试断言对齐前的真实执行结果：status 返回的是 `adopted`，不是 `registered`；该记录退出码为 `1`，不作为通过证据。

## 验收边界

本证据证明本地 handler 对官方单值 scope 的兼容及拒绝边界，不代表用户生产 BPS 登录成功。产品完整构建由主会话统一执行，避免重复构建。
