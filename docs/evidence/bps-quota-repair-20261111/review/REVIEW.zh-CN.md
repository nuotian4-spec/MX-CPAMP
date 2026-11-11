# BPS 额度来源诊断独立审查

日期：2026-11-11。仅审受控来源诊断，不实施产品，不发真实请求。最终源提交 `9e3f9232628c60379ca42b99bd5fdc4644158608`，基于 `74541d6c5b357140c6e11a4cd0fd7ccc0c921e0a`；主线整合为 `8d2f814`，执行树干净。

结论：来源诊断入口放行。主会话可在用户已批准范围内直接执行一次 `--run`；该动作自身重核本地状态与唯一 BPS，无需重复 `--prepare`。本席未执行任一真实请求。

## 范围与数据处理

- 状态目录、随机归属与现有窗口固定；CPA/Worker 必须运行、归属及镜像与窗口记录一致、共享原 namespace，距到期超过 60 秒。只复用现有容器，不新建服务、修改配置或清理窗口。
- 按 BPS 扁平凭证的实际 `type=bps` 选择，要求唯一，另核 `profile=excel` 与固定 client_id。`access_token` 和 `account_id` 必须为非空字符串且无换行；不解析或输出 JWT，不以 Codex 凭证补位。文件名及凭证仅经捕获内存与 stdin，不进入 argv、环境或记录。
- `--prepare` 无官方请求。`--run` 仅用同一 BPS bearer/account_id 顺序发 access GET 一次，HTTP 200、allowed=true 且目录合法才发 Wham GET 一次，合法空目录可继续。各请求 20 秒超时，固定同一 7890 代理、TLS 校验开启，不跟随重定向、不重试；任一步错误退出。无登录、刷新、推理或其他模型请求。
- BPS 请求头逐项对照当前 CPA `ApplyBPSHeaders`；Wham 请求头及固定 User-Agent 对照 Manager `requestCodexUsageAt` 与默认配置。未借用 CPA 自动令牌替换或刷新逻辑。
- 输出仅固定 `ROOT_KEYS/AUTH_KEYS/QUOTA_SCHEMA` 的字段名、类型和批准的数值/布尔值；未知键只计数，日期字符串只记类型。模型 ID、身份信息、token、任意字符串值、原始错误 message 和正文均不输出、不落盘。错误码使用关闭的白名单，其他错误归稳定类别。
- 可变凭证/正文缓冲在 finally 覆写，解析容器、请求头、cookie 与响应内容清空，一次性进程退出。README 明确不承诺清除语言运行时全部不可变字符串副本；回执中的缓冲清理不外推为物理内存确定擦除。

## 已核证据

旧 `PREPARE.jsonl` 与 `PREPARE.exit=1` 如实保留：归属、namespace、依赖及语法通过，但当时凭证总数 `1`、Codex `1`、BPS `0`，在本地停止；access/Wham、刷新、推理均 `0`。此历史结果不替代用户随后导入 BPS 后 `--run` 的实际计数。

已只读最终投影实现及 `projection_test.py`：该测试通过 AST 仅加载真实白名单及投影函数，不加载凭证或网络主流程，覆盖未知根/嵌套身份/token 样键、模型 ID、秘密字符串及 ISO 日期不回显，保留合法数值零、布尔 false 与数值 reset。引用执行席实际回报的本地反例与 diff 检查退出 `0`，本席未重复执行。产品、容器与真实官方验证均未由本席运行。

## 来源结果的接入判断

1. Wham HTTP 200 且有可识别额度数值：作为独立“官方账号用量”观测接入，保留来源与时间；不直接称作 BPS 专属配额。access/模型发现维持现有只读权限契约，不附带每次用量查询。
2. Wham 401/403：权限与用量状态分别保留，BPS access 成功不被用量失败覆盖；额度未知，不自动刷新、重登或启停账号。
3. HTTP 200 但无已识别额度结构/数值：仍为未知，不补成 0 或 100，不套用另一份 Codex 凭证的数据。诊断 `passed` 或仅存在 `rate_limit={}` 不代表额度业务可用。

未来巡检可组合权限与独立用量结果，但仍保持 `keep` 及零自动动作；本轮仅放行来源诊断，不包含产品接入、发布或部署。

## 真实来源回执核证

已只读 `source/SOURCE-RUN.jsonl` 与 `SOURCE-RUN.exit=0`。实际运行重新确认原归属和 namespace、7890 代理，凭证总数 `1`、BPS `1`、Codex `0`，profile/client 匹配；随后 access HTTP `200`、allowed=true、合法模型数量 `2`。

同一 BPS bearer/account_id 的 Wham GET 返回 HTTP `200`，白名单观测到 `rate_limit.allowed=true`、`limit_reached=false`，主窗口 `used_percent=17`、`limit_window_seconds=2592000`、`reset_after_seconds=2586075`、`reset_at=1796977220`。secondary window、code-review 和 additional-rate-limits 为 null；credits 两布尔值为 false，balance 为 null，不据此推断余额、BPS 专属配额或与 Codex 共享额度桶。

完成回执为 passed，实际 access/Wham GET 各 `1`，刷新/推理均 `0`，可变敏感缓冲清理已报告。该证据证明本次 BPS 授权可读取带明确主窗口数值的官方账号用量；未保留原始正文或任意身份值。本席仅核脱敏记录，未再次请求官方。

当前责任人：主会话固定独立“官方账号用量”契约并安排最小产品接入。下一责任人：执行席按已核来源实现，独立审查核权限/用量状态分离及零自动动作。`official_source_verified=true`、`quota_product_ready=false`、`write_release_ready=false`。
