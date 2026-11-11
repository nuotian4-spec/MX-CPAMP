# 本机手工 BPS OAuth 结果

日期：2026-11-11。固定镜像与入口见 [README](README.zh-CN.md)。用户已授权一次手工 OAuth；本轮未完成保存注册，不作为业务通过。

## 现场证据

- 用户报告 Codex 2FA、BPS 2FA 均为 `batch_not_accepted`，BPS OAuth 为“回调结果未确认”。原截图包含敏感输入，不复制入仓，不复用其中材料。
- 本窗口只启动 CPA 与 Manager，未配置 Worker。认证空 JSON 调用 tasks 得 HTTP503，代码在 Worker 未配置时先拒绝，不访问登录上游。因此两处批次拒绝不是两次上游登录失败。
- 05:25:25 UTC，使用页面已有会话标识只做 GET：Manager 与 CPA 的状态均为 HTTP200/`wait`。没有重提回调、重兑授权码或创建新会话。见 [USER-STATUS.json](USER-STATUS.json)。不保存会话标识或完整回调。
- 健康、配置和原 CPA 管理代理均可达，只有状态码和布尔值入档，见 [READONLY-CONNECTION.jsonl](READONLY-CONNECTION.jsonl)。可达不证明本次换码成功。
- 05:26:10 UTC，最终核验保存文件、运行时条目和 BPS 注册数量均为 0，见 [USER-VERIFY.json](USER-VERIFY.json)。结果为 pending，不以脚本 exit0 代替 OAuth 成功；该结果不单独证明从未发起过换码。
- 05:26:11 UTC，按本轮唯一 label 删除两个容器及临时管理员访问文件，cleanup verified、token_state_retained=false。用户账号未导入生产，OVH未改动。后台刷新原调度未硬禁，本轮启动器没有主动刷新或模型请求，也没有完整出站观测。

## 已定位范围

产品前端没有检查 Worker 是否配置，导致手工 OAuth 专项环境仍显示可点击的两处 2FA 按钮。另在离线源码中发现 CPA 明确回调输入拒绝的400可被 Manager映成502，从而触发“结果未确认”提示；它是独立可复现缺陷，不等于已确认用户本次具体回调拒绝原因。

当前由执行席在隔离树补充认证可用性布尔契约和明确回调拒绝分类；不放宽校验、不透传回调正文、不改变未知提交锁。当前责任人：主会话整合修复与独审证据。下一责任人：主会话核本地候选；若再做真实授权，另行明确范围，不重用本次材料或会话。
