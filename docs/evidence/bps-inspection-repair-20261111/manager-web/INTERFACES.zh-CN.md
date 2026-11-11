# Manager / Web 接口与隔离验收输入

产品输入：`875dd976c036583dd525b1a10fda07af7b9a43b1`。本文件为合成隔离环境的接口材料，命令未在真实账号执行。

## 管理员BFF

均使用Manager管理员Bearer鉴权和`Cache-Control: no-store`。

| 路由 | 输入/输出 |
|---|---|
| `GET /api/bps/settings` | 原有BPS设置，含当前`connection_ref` |
| `GET /api/bps/access?auth_index=...&connection_ref=...` | `connection_ref`必须来自展示该凭证时绑定的列表；原样消费CPA白名单归一字段，不返回token |
| `GET /api/bps/models` | `{models:string[],connection_ref:string,revision:string}`，revision为CPA全局配置ETag |
| `PUT /api/bps/models` | 完整JSON三字段`{models,connection_ref,revision}`；成功200 `{status:"saved"}` |

模型保存只转发`PUT /v8/management/config/oauth/providers/bps/models`，body为模型ID数组，`If-Match`为读取时的revision。CPA 412在Manager映射为409 `code=config_changed`；实例引用失配为409 `code=cpa_target_changed`。保存回执或后续读取未确认时前端禁止再写，局部刷新取得可信版本后恢复。

网页收集BPS凭证时在读取列表前后核同一个实例引用；之后查询使用展示时引用，不重新取得新引用绑定旧auth_index。CPA缺少BPS管理路由时普通列表保持可读，BPS没有有效引用则禁止权限请求。

```sh
# 仅在隔离组合环境设置占位值；ADMIN_TOKEN使用合成管理员凭据。
MANAGER_BASE='http://127.0.0.1:ISOLATED_PORT'
ADMIN_TOKEN='SYNTHETIC_ADMIN_TOKEN'
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" "$MANAGER_BASE/api/bps/models"
# 用上一步真实revision/connection_ref替换占位值；保存和清空都只改模型字段。
curl -fsS -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  --data '{"models":["gpt-synthetic"],"connection_ref":"REFERENCE_FROM_GET","revision":"ETAG_FROM_GET"}' \
  "$MANAGER_BASE/api/bps/models"
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$MANAGER_BASE/api/bps/access?auth_index=SYNTHETIC_INDEX&connection_ref=REFERENCE_FROM_DISPLAYED_LIST"
```

专用access探测成功定位后，CPA HTTP200正文中的`error`和`allowed`才决定观察状态。无token合成凭证应为`error=credential_incomplete`、`allowed=null`、`models=null`、`quota=null`、`inference_verified=false`、`auto_recover_eligible=false`；HTTP200本身不代表权限通过。

## 服务端手动巡检

先读取`GET /usage-service/config`，保留其余配置，设置`codexInspection.targetTypes=["bps"]`、`targetType="bps"`、`autoActionMode="none"`、`autoRecoverEnabled=false`。组合验收可设置`enabled=false`关闭定时入口；手动启动沿现有流程。

```sh
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$MANAGER_BASE/v0/management/codex-inspection/run"
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$MANAGER_BASE/v0/management/codex-inspection/runs/RUN_ID_FROM_START"
```

启动无body，返回`{run,results,logs}`；按返回的run.id读取直至终态。无token合成BPS凭证应集合1，结果`provider="bps"`、`action="keep"`、`usedPercent=null`、`autoRecoverEligible=false`，人工禁用保持。`errorDetail`为JSON摘要，仅含`allowed`、`queried_at`、`upstream_status`和`error`；不含models目录。`error`摘要字段名固定为`error`。

浏览器本地巡检同样使用独立BPS分支，设置及历史仍沿用现有本地存储。默认Codex选择不自动扩大。完整目录仅在设置页或账号页当前内存，不新增持久缓存或后台轮询。
