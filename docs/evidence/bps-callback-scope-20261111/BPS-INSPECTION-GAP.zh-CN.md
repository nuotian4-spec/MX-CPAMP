# BPS 巡检支持缺口

日期：2026-11-11。用户在本轮手工OAuth窗口报告巡检集合为0。截图日志为`totalFiles=1`、`probeSetCount=0`、`sampledCount=0`、`targetTypes=[codex]`。该结果表示读取到一份文件，但没有匹配所选Codex类型；本轮唯一凭证属于BPS，OAuth保存注册已单独通过。

## 已核事实

独立只读核6个关键文件，未执行巡检、外部请求或读取凭据：

| 通道 | 面板目标 | 后端校验 | 专用探针 |
|---|---|---|---|
| Codex | 支持 | 支持 | 支持 |
| xAI | 支持 | 支持 | 支持 |
| BPS | 缺失 | 拒绝 | 缺失 |

- `components/cpamp/apps/web/src/features/monitoring/components/InspectionConfigFields.tsx:87`仅有codex、xai和两者组合。
- Web共享`codexInspectionSettings.ts`只规范化codex/xai。
- `components/cpamp/apps/manager-server/internal/model/codex_inspection.go:344`起的校验明确拒绝其他类型，规范化也只保留二者。
- `internal/service/codexinspection/service.go:545`起先获取全部文件，再按目标provider过滤，符合本轮日志的1→0。
- 同文件`:1744`起的探针分发仅有xAI专用分支，其余调用Codex `wham/usage`；Web本地巡检同样是xAI/默认Codex二选一。

因此调整现有选项不足以让BPS参与巡检。单加BPS选项会误走Codex用量接口；现状不等于BPS账号不可用或OAuth失效。

## 待实施边界

后续应成套接入BPS目标枚举、配置规范化/校验、Web与服务端筛选和独立探针、结果呈现。BPS探针需先核受支持的健康/身份检查与额度来源，区分身份有效、模型可用和额度状态；没有可靠额度证据时显示未知。是否发真实模型探测及其预算需明确，不能以本次手工OAuth预算继续请求。

当前只完成诊断与缺口记录，没有修改巡检产品代码、自动动作或定时配置；没有真实BPS巡检通过证据。主会话负责后续方案与实施边界，完整验收后统一发布；现有模型调用、登录和巡检证据分别记录。
