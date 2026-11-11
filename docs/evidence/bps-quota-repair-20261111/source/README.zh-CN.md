# BPS 额度来源受控诊断

基线：`c39582aa515f57c4a1297b589139a1bd07a95be4`；仅使用本轮现存临时 CPA/Worker，状态目录固定为 `C:/Users/子羽/.codex/artifacts/mx-full-login-manual-19e8a490cf66`，窗口于 `2026-11-11T10:13:43.149Z` 到期。

## 命令与预算

```powershell
node D:/MX-CPAMP-worktrees/bps-access-inspection/docs/evidence/bps-quota-repair-20261111/source/diagnose.mjs --prepare
```

`--prepare` 只检查状态归属、CPA/Worker 原命名空间、现成 Python/curl_cffi、脚本语法，以及现存凭证中唯一 `type=bps`、`profile=excel` 和固定 client_id；输出计数、字段类型，不访问官方。

独审放行后由主会话执行一次：

```powershell
node D:/MX-CPAMP-worktrees/bps-access-inspection/docs/evidence/bps-quota-repair-20261111/source/diagnose.mjs --run
```

`--run` 再做相同本地检查，使用该唯一 BPS bearer/account_id，按顺序最多两个 GET：

1. `https://bps.openai.com/basispoints/api/responses/access?include_models=true`
2. 首次 HTTP 200、allowed=true 且 model_catalog.models 为合法数组（允许为空）后，`https://chatgpt.com/backend-api/wham/usage`

每个 GET 超时20秒、零重试、不跟随重定向；任一步失败即停。进程最多60秒，临近到期少于60秒即停。没有登录、刷新、推理、模型请求、服务/配置/批次修改。

HTTP 从既有 Worker 进程命名空间发出，明确固定 `http://host.docker.internal:7890` 代理及 TLS 校验。BPS headers 复用 CPA `ApplyBPSHeaders`；Wham headers 复用 CPAMP `requestCodexUsageAt` 与其默认 User-Agent，不调用 CPA 的自动令牌替换或刷新逻辑。

## 凭证与输出

`docker exec` 读取 `/state/auths/*.json` 到捕获内存；文件名、token、email、account_id、JWT 与原始响应不回显、不落盘。凭证/正文可变缓冲在 finally 覆写，解析对象与请求头清空；Python 一次性进程随后退出。语言运行时产生的不可变字符串副本由进程退出释放，不宣称物理内存的确定擦除。

输出仅含固定白名单根键/额度路径的名称和类型、白名单数值/布尔值、HTTP、白名单 `error.code`。未知根键/额度子树键仅计数；日期字符串仅记类型，数字 reset 保留数值。原始错误 message、model id、身份信息与任意字符串值不输出；未知错误码统一为 `other`。纯本地反例命令：`python source/projection_test.py`，不读取凭证或访问网络。

## 事实来源

- BPS 扁平文件：`components/cpa/internal/auth/bps/storage.go` 的 `TokenStorage.SaveTokenToFile`。
- BPS 识别：`components/cpa/internal/api/handlers/management/auth_files_bps.go` 的 profile/client 校验及 `importedRecord`。
- BPS URL/headers：`components/cpa/internal/runtime/executor/helps/bps_access.go`、`bps_protocol.go:ApplyBPSHeaders`。
- Codex URL/headers：`components/cpamp/apps/manager-server/internal/service/codexinspection/service.go:requestCodexUsageAt`。
- Codex User-Agent：`components/cpamp/apps/manager-server/internal/model/codex_inspection.go` 默认配置。

## 本轮准备回执

`--prepare`：退出1，固定 CPA/Worker 归属、原 namespace、现成 Python/curl_cffi 与语法通过；实际 auth_file_count=1、codex=1、bps=0，在唯一 BPS 门槛停止。官方 access/wham 各0次，刷新/推理各0次。证据见同目录 `PREPARE.jsonl`、`PREPARE.exit`；本地 `node --check` 退出0。

真实请求尚未执行；后续需该固定窗口中唯一 BPS 凭证确实存在，再由主会话按独审结论推进。
