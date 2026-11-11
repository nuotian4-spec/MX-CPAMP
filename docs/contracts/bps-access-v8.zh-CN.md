# BPS access v8 管理契约

冻结日期：2026-11-11。输入基线：`5d431b9be0828f94576d1807e1cc87b822cc1350`。

## 请求

`GET /v8/management/bps/credentials/access?auth_index=<index>`，使用既有 v8 管理鉴权。`auth_index` 必填、只允许一个，精确匹配当前注册凭证；可选 `auth_id` 必须与该凭证 ID 一致。拒绝未知或重复参数，不接受 token、URL 或出口覆盖。Manager 的实例选择沿用其既有实例绑定，本接口只解析当前 CPA 实例的凭证。

仅允许 provider=`bps`；不按文件名、邮箱、其他账号或 provider 回退。使用注册表快照中的 access token、account_id 和现有凭证/全局出口，复用 BPS headers 后将 Accept 设为 `application/json`。固定请求为 `GET https://bps.openai.com/basispoints/api/responses/access?include_models=true`，无请求体；5 秒总预算，保留更短父截止并响应取消，拒绝跟随重定向。

## 响应

已定位正确凭证后的查询结果统一 HTTP 200，`Cache-Control: no-store`：

```json
{
  "provider": "bps",
  "auth_index": "index",
  "auth_id": "credential-id",
  "queried_at": "2026-11-11T12:00:00Z",
  "allowed": true,
  "models": ["gpt-6-luna"],
  "upstream_status": 200,
  "error": null,
  "quota": null,
  "inference_verified": false,
  "auto_recover_eligible": false
}
```

`queried_at` 为本次查询完成的 UTC RFC3339 时间。结构有效时 `allowed` 为布尔，`models` 为去重后的允许模型 ID 数组；`allowed=false` 时为空数组。仅接收合法 `gpt-*` ID，过滤 restricted_models、available/enabled/model_picker_enabled=false、policy.state=disabled/unconfigured。空目录是有效结果。

失败时 `allowed/models` 均为 null，不伪造成空目录；`upstream_status` 为实际收到的上游 HTTP 状态，未收到响应为 null；`error` 为稳定字符串类别：`credential_incomplete`、`unauthorized`（401）、`forbidden`（403）、`rate_limited`（429）、`upstream_error`（其余非200）、`redirect_rejected`（3xx）、`invalid_response`、`timeout`、`canceled`、`transport_error`、`credential_changed`。成功时 error=null。`quota` 恒为 null；access 不证明推理成功，不更新推理成功记录，不参与自动恢复。

管理鉴权失败沿用既有 401/403；管理不可用沿用既有404。参数错误返回400 `{"error":"invalid_reference"}`；未找到返回404 `{"error":"credential_not_found"}`；非BPS返回400 `{"error":"provider_mismatch"}`；auth_id不匹配返回409 `{"error":"credential_changed"}`；注册表不可用返回503 `{"error":"auth_manager_unavailable"}`。这些请求拒绝不会发上游请求。

## 只读边界

不刷新、登录、推理、修改凭证/启停/模型清单，不读取旧凭据文件，不持久化目录缓存；不返回原始上游正文、令牌、出口地址或账户字段。查询进行中若注册引用或令牌/身份/出口快照已变化，丢弃本次权限结果并返回 `credential_changed`，不跨账号重试。上游权限接口的当前真实可用性留给后续明确授权的真实验收。
