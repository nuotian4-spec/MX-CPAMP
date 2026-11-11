# BPS 官方账号用量只读契约

`GET /v8/management/bps/credentials/usage?auth_index=...&auth_id=...` 使用现有管理鉴权；`auth_index` 必填，`auth_id` 可选且精确匹配。重复、空值、未知查询参数拒绝；引用错误沿用 access 的 HTTP 400/404/409/503。响应 `Cache-Control: no-store`。

合法引用的查询返回 HTTP 200，固定结构如下，字段均显式保留 null：

```json
{"provider":"bps","auth_index":"reference","auth_id":"reference","source":"chatgpt_account_usage","queried_at":"RFC3339","upstream_status":200,"error":null,"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":17,"limit_window_seconds":2592000,"reset_after_seconds":null,"reset_at":1796977220},"secondary_window":null}}
```

- `provider` 固定 `bps`，`source` 固定 `chatgpt_account_usage`；只表示该 BPS 凭证可读取官方账号用量，不定义 BPS 独占额度或共享额度桶。
- `upstream_status` 为整数或 null；`queried_at` 为查询完成时 UTC RFC3339。
- `error` 为 null 或稳定类别：`credential_incomplete / unauthorized / forbidden / rate_limited / redirect_rejected / upstream_error / invalid_response / timeout / canceled / transport_error / credential_changed / quota_unavailable`。失败时 `plan_type`、`rate_limit` 均为 null，来源和引用保留。
- `plan_type` 为 `free / go / plus / prolite / pro / team / business / self_serve_business / enterprise / edu / education` 之一或 null；未知字符串投影为 null，非字符串非 null 拒绝。
- `rate_limit` 为对象或 null，仅包含 `allowed: bool|null`、`limit_reached: bool|null`、`primary_window: object|null`、`secondary_window: object|null`。
- 每个窗口仅含 `used_percent / limit_window_seconds / reset_after_seconds / reset_at`，均为 number 或 null。`used_percent` 范围为 0–100；`limit_window_seconds` 大于0；两个 reset 字段非负；所有数字必须有限，拒绝字符串、布尔及越界值。合法0原样保留。
- 窗口至少含合法 `used_percent` 或一个 reset 字段才可展示；只有 reset 的窗口保留 `used_percent:null`。空窗口、null、缺失或仅周期字段的窗口视为无可展示窗口。两个窗口均无可展示信息时为 `quota_unavailable`；坏类型或非法数值为 `invalid_response`。
- 仅投影主 `rate_limit`，不返回原始正文、账号、邮箱、模型、credits、code-review 或额外额度桶。

上游固定 GET `https://chatgpt.com/backend-api/wham/usage`，使用当前精确 BPS bearer 与 `Chatgpt-Account-Id`，User-Agent 固定为来源核实同款 `codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal`。父请求内最多5秒、无重定向、无重试、保留有效代理。查询期间身份、token、注册版本或有效出口变化则丢弃结果并返回 `credential_changed`。

该接口不登录、不刷新、不推理、不写凭证或配置；原 access 响应及其中 `quota:null` 保持原契约。无数据库动作。
