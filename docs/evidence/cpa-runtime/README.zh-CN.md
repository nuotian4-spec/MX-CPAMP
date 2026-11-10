# CPA 独立 BPS 通道候选回执

- 日期：2026-11-10。
- 工作树：`D:/MX-CPAMP-worktrees/runtime`；分支：`codex/bps-runtime`。
- 输入基线：`22d0f16cfcd741a95dd34810aca61ea688390f10`。
- 产品提交：`85de32f9d327d968cce7ea9178d6f557a850a93e`。
- 供体：`D:/sub2api/wt-account-egress-ui-20261030`，HEAD `c46cd611d2391f68d51a6bc08c34ab1f8b16ceaf`；仅提取已核协议，未改供体。

## 结果

独立 `bps` provider 使用 Excel OAuth client、固定 BPS endpoint、独立凭据文件、原子保存与刷新；复用 CPA registry、账号槽位调度与凭据 mutation/epoch。v8 支持 BPS OAuth、Codex/BPS 内存导入及保存后的热注册；账号列表显示登录方式和 profile，v0 响应形状保持原样。

BPS 首期支持文本 Responses/Chat Completions 的 HTTP 与 SSE。显式模型目录位于 `oauth.providers.bps.models`，空配置不发布默认模型。同名 ID 归并至现有模型注册表，两个 Codex 账号与一个 BPS 账号的稳定轮询仍为 2:1。

用户 payload 规则为最后语义屏障。WS、工具、媒体、opaque/compaction、续聊 ID 等请求在选账号前排除 BPS；混合候选的账号绑定历史缺少原账号 pin 时返回 400。BPS response ID 标记为 `resp_bps_*`，以该 ID 续聊直接返回 400，避免转交其他通道或账号。上游缺真实终态、返回 failed/incomplete 或工具输出时返回错误；已接受请求的中断不会被重放成成功。

## 验证

工具链：Go 1.26.10 Windows amd64，`GOTOOLCHAIN=local`；联网依赖使用本机 7890 代理。最终验证输入为上述产品提交，之后仅新增本目录证据。

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| BPS runtime、协议、catalog、原子 reload/save、账号槽位、历史边界与旧刷新结果屏障；8 个受影响包 | 退出码 0 | `runtime-tests.log` |
| Codex payload 屏障、executor 绑定、WS 当前上游/上次账号约束与既有混合权重定向回归 | 退出码 0 | `runtime-tests.log` |
| v8 登录方式/profile 摘要、token 不外泄及 v0 形状 | 退出码 0 | `runtime-tests.log` |
| OAuth/import/session/callback 管理专项与既有路径，32 个顶层测试 | 退出码 0；复用代码等价证据 | `auth-tests.log` |
| 全部 BPS client/storage 专项，含 invalid_client 分类 | 退出码 0 | `auth-terminal-tests.log` |
| `go build -o C:/Users/子羽/.codex/artifacts/cpa-runtime-20261110/server-85de32f.exe ./cmd/server` | 退出码 0 | `build.log` |
| 基线至产品提交 `git diff --check` | 退出码 0 | `final-checks.log` |
| SQL/schema/migration 路径命中 | 空；无数据库迁移 | `final-checks.log` |

`auth-tests.log` 的管理源码与产品提交相同，仅经过 gofmt；其 client 后续仅补充 invalid_client 分类，全部 client/storage 测试已在 `auth-terminal-tests.log` 重新通过。未更改其余已通过验证输入。

Windows 构建产物为 `C:/Users/子羽/.codex/artifacts/cpa-runtime-20261110/server-85de32f.exe`，103462912 字节。本线未构建 CPA Linux 镜像；B 线负责按固定产品提交构建一次，并完成真实 CPA、Manager、Worker 的假登录上游集成。

## 已知边界

- 未执行真实 OAuth、真实账号导入/补登、真实模型请求或生产部署；本回执证明本地合成输入下的实现与回归。
- BPS 官方 callback 的 code 是否已被官方页面消费，仍需真实授权回执证明；四入口的真实业务闭环尚待该证据。
- 首期 BPS 工具、媒体、opaque/compaction、background、原生 WS、previous_response_id 和 token counting 未开放；已有 Codex 能力由原路径承接。
- 未新增 SQL 表或 schema 迁移。未来真实导入会新增或更新 CPA 凭据文件；取消在保存提交阶段返回 409，以真实保存/注册结果收口。
- 未 push；下一责任人为 P9 独立审查与 B 线跨组件集成。
