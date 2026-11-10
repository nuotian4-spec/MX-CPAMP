# CPA 登录 v8 管理契约

状态：本地候选实现；真实 OAuth 与收费模型调用未执行。

## 鉴权与秘密

管理请求沿用 CPA 管理鉴权。CPAMP 只放行下列精确路径与方法，不代理整个 v8 子树。密码/TOTP 永远不提交 CPA；token 仅在管理后端内存请求中传输，不进入浏览器响应。

## 受控内存导入

`POST /v8/management/oauth/import?provider=codex|bps`

请求 JSON：

```json
{"profile":"excel","client_id":"app_fnr0pYvVwwFDocDumLG3H2Bp","access_token":"TOKEN","refresh_token":"TOKEN","id_token":"OPTIONAL_ID_TOKEN","account_id":"ACCOUNT_ID","email":"EMAIL","expired":"2026-11-11T00:00:00Z","login_method":"2fa"}
```

- 单 profile 映射：`codex → codex → app_EMoamEEZ73f0CkXaXp7hrann`；`excel → bps → app_fnr0pYvVwwFDocDumLG3H2Bp`。
- `expired` 为 RFC3339 UTC；字段与凭据声明须一致。client 标签只是协议校验，不能证明任意 token 的真实授权来源；授权交换与可用性须真实回执。
- `login_method` 为 `oauth` 或 `2fa`，持久保留以供账号列表显示。BPS 无需持久保存 ID token；Codex 按既有格式处理。
- 同 provider、同官方账号 identity 的补登保留账号管理 metadata；不同通道独立文件，禁止按邮箱把 Excel 覆盖 Codex。
- 导入与刷新复用现有凭据 mutation/epoch 协调；补登后旧刷新结果不得覆盖新授权。
- 仅原子保存和热注册均完成后返回 200：`{"status":"ok","provider":"bps","auth_id":"bps-...json","saved":true,"registered":true}`。响应不含 token。
- 400/409/502/503 只给稳定脱敏错误码；失败不宣称已保存或已注册。

## OAuth

- `POST /v8/management/oauth/auth-url`，JSON `{"provider":"bps","is_webui":true}`；成功 `{"status":"ok","url":"OFFICIAL_URL","state":"STATE"}`。Codex 保留现有入口。
- `POST /v8/management/oauth/callback`，JSON `{"provider":"bps","state":"STATE","redirect_url":"OFFICIAL_CALLBACK_URL"}`。返回 `{"status":"ok"}` 仅代表接收回调，非导入成功。
- `GET /v8/management/oauth/status?state=STATE`：沿用 `status=wait|ok|error`；终态必须对应真实保存和注册。错误只给脱敏 `error`。
- `DELETE /v8/management/oauth/session?state=STATE`：沿用取消契约；已进入保存提交的任务以真实结果为准。

BPS 使用官方 Excel client、PKCE、官方回调及 `/oauth/token?unified=true`；浏览器到达官方 callback 后 code 是否已先被消费仍待真实授权证据。本轮不执行真实登录，不把假上游验证写成四入口全部可用。

## BPS catalog 与能力

v8 YAML 明确配置 `oauth.providers.bps.models: [MODEL_ID]`；没有默认模型清单。相同 public ID 通过现有 registry 去重并进入 mixed 账号槽位轮询，不新增调度器。

首期 BPS 接 `/v1/responses` 与 `/v1/chat/completions` 文本 HTTP/SSE。WS、previous_response_id、工具/opaque/compaction/媒体历史在选账号前排除 BPS。已有 Codex 请求保留原路径；混合候选中包含 BPS 且账号绑定历史缺少原账号 pin 时，直接返回 400，要求以完整文本开始新会话，避免换通道或账号回放。已有 pin 继续受原账号约束。

BPS 返回的 response ID 均标为 `resp_bps_*`；以该 ID 发起 `previous_response_id` 始终返回 400，不能被转交 Codex。工具调用 ID 不改写；BPS opaque reasoning 不对外发布。用户 payload 规则是最后语义屏障，之后仅序列化、传输和只读校验。

本次不新增 SQL 表或 migration；未来真实导入会新增或更新 CPA 凭据文件，属于运行时写入。
