# BPS 额度来源与接入边界

2026-11-11。当前代码基线`15c6d6208c7915b5376ebd0e78673bca4147809b`。用户反馈Codex巡检显示剩余额度，而BPS没有额度。

## 已核结论

当前BPS尚未接入额度查询，不属于单纯前端漏展示。原BPS巡检方案只接只读权限及模型目录；额度缺少可靠来源时保持未知。没有证据证明官方没有额度接口，也没有保留的原始官方响应证明有效额度已经返回后被丢弃。

| 链路 | 当前实现 |
|---|---|
| Codex | `codexinspection/service.go:38`查询`chatgpt.com/backend-api/wham/usage`，解析使用比例及重置窗口，1835行生成额度窗口 |
| BPS采集 | CPA `internal/runtime/executor/helps/bps_access.go:17`查询`responses/access?include_models=true`；115行解析器仅提取allowed和模型，Quota字段未赋值 |
| Manager契约 | `internal/service/cpa/bps_access.go:38`拒绝非null额度 |
| Web契约 | `src/services/api/bpsAccess.ts:13/37`固定quota:null并校验 |
| BPS巡检 | Manager `codexinspection/bps_probe.go:16`置UsedPercent=nil；Web `bpsInspectionProbe.ts:24`为空额度窗口 |
| 结果页 | `CodexInspectionResultsPanel.tsx:348`对BPS显示额度未知、推理未验证 |

## 供体核实

供体`D:/sub2api/wt-ranxi-v2180-20261030`，HEAD`d7b49212a7de7683e1f3fb7480d1959a2030da85`。只读实现确认：

- `backend/internal/service/openai_quota_service.go:27`已有Codex用量源，不证明适用于BPS。
- `openai_excel_bps_model_access.go:21/115`仅接BPS权限和模型目录。
- `openai_bps_gateway.go:218`读取单次input/output/cached token消耗，`openai_gateway_usage.go:197`计算本站费用；二者不是官方剩余额度。
- `openai_excel_bps_ratelimit.go:23`独立处理BPS429及Retry-After冷却，不写Codex额度。
- `channel_monitor_validate.go:80`不支持BPS额度巡检。因此未找到可直接迁移的BPS剩余额度实现。

用户尚未查看BPS/Excel官方额度页面。公开插件清单`https://bps.openai.com/basispoints/api/office/manifest.xml`本轮GET200，清单SourceLocation及其实际引用的当前主包均GET200；旧供体hash资产已404。当前主包：`https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/assets/x-square-Bj24DH9Y.js`。

当前官方主包处理`usage_limit_reached`、`usage_not_included`，从失败载荷提取`plan_type`、`resets_at`、`resets_in_seconds`，界面用`resets_at`显示重置日期。`View Usage`跳到`https://chatgpt.com/codex/settings/usage`；这个跳转不单独证明BPS与Codex共用额度，也不证明BPS OAuth凭证可查询Codex用量接口。本轮未定位到独立只读剩余额度接口，已核字段来自失败载荷，不是常态剩余量或总额度。

公开来源与精确字符offset记录在`C:/Users/子羽/.codex/artifacts/mx-bps-quota-source-20261111/public-source/FINDINGS.zh-CN.md`。公开资产核实与鉴权业务请求分开，没有为此使用cookie、读取凭据，或发起登录、access、刷新或推理。

## 接入要求

先确认官方只读额度来源、凭证适用范围及字段含义，包括剩余量/使用量/上限、单位、模型或账号作用域、窗口和重置时间。取得来源后同时贯通CPA解析、Manager/Web契约、双巡检入口、记录和账号卡；保持权限与额度分别表达。真实零值、未知、受限、失败及旧记录应分别验证。

只用单次消耗或429冷却推算百分比没有依据。仅接入新百分比组件也不能补齐当前缺失的采集链路。不会把Codex额度直接当作BPS额度。

本轮没有产品修改、SQL/schema迁移、测试/构建、推送或OVH动作。当前结论：实现缺口和供体边界已确认，官方剩余额度来源仍待证；不宣称已修好。下一实施入口：先受控验证BPS授权与官方用量源的适用关系及返回语义，再派采集、契约、巡检和展示整线接入；错误载荷的额度用尽及重置提示可单独建模，但不代替剩余百分比。当前责任人：主会话。
