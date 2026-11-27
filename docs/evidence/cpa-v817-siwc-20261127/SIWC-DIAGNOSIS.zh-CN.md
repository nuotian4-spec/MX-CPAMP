# CPAMP 缺少 SIWC 入口的调查

日期：2026-11-27（Asia/Shanghai）。范围：当前本地候选源码，只读调查；未执行登录、账号请求、测试或产品修改。

源码根：C:/Users/子羽/.codex/worktrees/cpa-core-v817-20261127/MX-CPAMP。基线提交：b9ea2dc4c6bb377d31143970a6df1084297e4b09。调查链已在此基线定点核实；独审另核v8.1.7目标中的SIWC授权、绑定与摘要合同保持一致，平台续期差异见下文。下列源码位置均相对此根；本报告不据本地源码推定生产版本。

## 结论

CPAMP 尚未接入已有 CPA 的 SIWC 管理合同，缺口覆盖界面、管理桥接及账号摘要读取。当前源码没有 SIWC 开关隐藏入口的实现；单独升级 CPA 内核不会自动增加 CPAMP 的绑定入口。

SIWC 是 OpenAI **Sign in with ChatGPT**。CPA 将它作为现有 Codex OAuth 凭证的第二凭证分区，成功绑定后保存嵌套 `siwc` 与 `use_siwc=true`，provider 继续为 `codex`。它不是一个独立的新 provider，也不是直接从空白创建 SIWC 账号的接口。

## 根因链及源码证据

| 层 | 已核事实 | 源码位置 |
|---|---|---|
| CPA 授权能力 | 已有 SIWC URL 生成、token 交换与续期；资源为 `https://api.openai.com/v1`，scope 包含 direct token 使用授权 | `components/cpa/internal/auth/codex/siwc_auth.go:24`、`:70` |
| CPA 管理入口 | 已注册 `POST /v8/management/oauth/siwc/bind`、`GET /v8/management/oauth/siwc/status`，共用 v8 callback/status/session | `components/cpa/internal/api/server_management_v8.go:14`、`:99` |
| 绑定目标 | 开始绑定必须指定已有凭证 `name`；仅普通文件型 Codex OAuth，要求外层 access/refresh token；返回 `flow=siwc_bind`、300秒寿命 | `components/cpa/internal/api/handlers/management/oauth_siwc.go:54`、`:219`、`:306` |
| Web 登录入口 | 内置列表提供普通 `codex`；`oauthApi.startAuth` 调用 `/{provider}-auth-url`，不存在 SIWC bind/status API 或绑定交互 | `components/cpamp/apps/web/src/features/oauth/OAuthPage.tsx:140`；`components/cpamp/apps/web/src/services/api/oauth.ts:44` |
| Web 管理基址 | 基址固定追加 `/v0/management`，账号列表请求 `/auth-files` | `components/cpamp/apps/web/src/utils/constants.ts:17`；`utils/connection.ts:16`；`services/api/authFiles.ts:1204` |
| Manager 反向代理 | 通用管理转发只处理 `/v0/management/`；v8 按特定路径开放凭证模型、API-key 配置与更新入口，未注册 SIWC v8 路由 | `components/cpamp/apps/manager-server/internal/service/proxy/service.go:84`、`:182`、`:192`；`internal/http/router/router.go:149`、`:162`、`:166`、`:170` |
| Manager 登录服务 | OAuth action 白名单只有 `auth-url/callback/status/session`，不接受 `siwc/bind`；其他action还要求已有该服务登记的state，不能直接套用一个外部SIWC会话 | `components/cpamp/apps/manager-server/internal/service/oauthlogin/service.go:520`、`:531` |
| 凭证摘要 | `siwc_bind_supported`、`use_siwc`、`siwc_status`、`siwc_bound`、`siwc_expires_at` 仅 v8 Codex 条目输出；旧 v0 列表不给这些字段 | `components/cpa/internal/api/handlers/management/auth_files.go:902` |

只读搜索：`rg -n -i 'siwc|sign.in.with.chatgpt'` 在新基线定点扫描 Web OAuth功能/API/凭证类型以及Manager OAuth服务/代理源码，无匹配；CPA 对应源码与文档有明确匹配。由调用链可确定，缺入口来自产品未接线，不是已实现功能被设置隐藏。

## 已有合同与适用范围

CPA 的合同正文为 `components/cpa/docs/management-api-v8.md:195`。

1. 在已有普通 Codex 凭证上发起绑定；账号详情是自然入口。仅当明确收到 `siwc_bind_supported=true` 才提供按钮，字段缺失按未支持处理。
2. 打开返回授权 URL 后，完整回调形如 `http://127.0.0.1:1455/auth/callback?...`。远程部署采用复制地址栏完整 URL 并提交，CPA 管理接口不会在服务器新开 1455 监听。OVH Linux 不需要因此增加暴露端口。
3. 回调必须保留上游签发的 `client_id`；`dynamic_agent_client` 只是初始授权标识，不得代替签发值。v8 callback 接受完整 URL 或 state/code/client_id，明确错误回调可只有 error。
4. `callback` 接收成功只表示开始交换，仍须轮询 v8 `oauth/status`；只有 `ok` 才表示嵌套凭证落盘并发布到 auth manager。`wait` 不报绑定成功。
5. 取消、重复回调、目标凭证被替换/删除已有内核围栏。会话仅存 CPA 进程内、5分钟有效，重启会失效；多实例需要请求黏着到同一 CPA 实例。
6. 内核文档限定 standalone CPA + 本地文件型 Codex OAuth；Home、运行时/非文件来源不应展示可绑定操作。Linux的`siwc_power_other.go`直接返回ready，OVH/Linux不是本次入口缺失原因。v8.1.7新增`siwc_power_windows.go`，Windows续期经过`CheckWindowsRefreshReadiness`就绪检查，休眠/联网未就绪时可推迟；Darwin也有电源状态续期推迟。平台续期门控不是CPAMP隐藏绑定入口的逻辑。
7. 此版本没有单独的启用/解绑/独立创建 SIWC 管理操作，不虚构开关或解绑按钮。
8. 新基线的绑定在 token 交换前解析目标账号的 proxy_url 并创建专属客户端（oauth_siwc.go:284-293）；Manager 桥接应保留该账号绑定，不自行改成全局出口。

## 推荐的最小补齐方案

维持现有 Codex 账号和通道身份，在账号详情中新增“绑定 SIWC / 重新绑定 SIWC”对话框，展示本地 `off/valid/invalid`、到期时间与适用条件。授权入口仍归属于 Codex，不新增 provider 卡片或新的账户类别。

Manager 新增窄范围 SIWC 管理桥接，复用现有管理员认证、CPA connection_ref 与凭证身份解析：读取 v8 `/credentials` 的非秘密 SIWC 摘要，调用 v8 bind，登记 state 对应的连接和目标，再接回调、状态与取消。不要把通用 v8 全路径无条件开放，因为现有代理的凭证/配置变更围栏仍识别 v0 路径。

复用登录会话框架时必须显式区分 `siwc_bind` 和普通 Codex 登录。当前 `OAuth` 服务只允许四种 action，且 callback 要求 code 非空（`service.go:602`），需要对 SIWC 的授权拒绝回调、issued client_id、目标身份和5分钟期限做对应适配。完整回调内容不进入日志、持久化浏览器存储或错误诊断。连接切换、详情切账号后丢弃迟到回执，并对旧会话执行既定取消。

旧 CPA 返回 bind 404 时明确显示不支持；不自动退回普通 Codex 登录，避免把“绑定第二分区”改成“重新创建普通账号”。绑定完成后回读该凭证 v8 摘要，确认本地保存/启用状态；不自动发起模型请求来证明有效。

## 最小验收范围

- Manager：管理员认证、缺失/错误 connection_ref、不同目标凭证、v8摘要缺字段、旧内核404、首次绑定/重复409、取消与迟到结果、错误回调保留error、完整URL保留issued client_id。均使用本地合成服务，真实账号调用0。
- Web：eligible按钮/特殊来源只读、完整回调提交、wait不报成功、终态回读、连接/账号切换丢弃迟到回执、5分钟过期、旧内核不回退普通登录。仅新专项一次，失败只跑影响子集。
- 如采用现有 CPA 合同且内核绑定实现未变，复用其同版现有SIWC专项证据；不重复验证无关BPS/Kilo内核。最终Web静态更新后再构建内嵌Manager。

## 当前边界与下一责任人

本席只读调查并写本报告，没有实现 SIWC 界面或桥接，也没有登录及生产验证。当前已能确认“缺少接线”的源码原因；用户生产实际版本和登录可用性不据此宣称已确认。

下一责任人：P9 将此调查与 CPA v8.1.7 升级结果合并汇报；若本轮扩大为 SIWC 接入实施，由独立写席按上述窄范围方案执行，审查席只审查。
