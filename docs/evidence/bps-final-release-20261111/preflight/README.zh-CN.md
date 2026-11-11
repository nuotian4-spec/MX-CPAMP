# Caddy 原序脱敏摘要专项

输入基线：`db97e37f177a9223130d95035cda57ef831b112d`。本次只扩展既有 `deploy/phase2-readonly-preflight.sh` 的 Caddy JSON 摘要，不涉及产品构建、发布写入或生产请求。

`caddy_routes` 保留 `parse`、`relay_routes`、`generic_api_routes`、`effective_order`。新增字段：

- `ordered_servers`：按适配后 JSON 的对象/数组顺序列出服务器编号、路由、处理器及 `subroute.routes`，每层 `index` 均从零开始。
- `match`：保留匹配分支及列表顺序。`paths` 只输出 `relay`（精确 `/api/bps-images/*`）、`api`（精确 `/api/*`）、`v1`（精确 `/v1/*`）、`any`（未限定路径）、`unknown`。方法只输出 `GET`、`HEAD`、`any`、`unknown`。
- `targets`：只输出 `cpa`（精确 `cli-proxy-api:8317`）、`manager`（精确 `cpa-manager-plus:18317`）、`unknown`。
- `terminal` 保留布尔值；组名按首次出现映射为数字，同编号表示同一原始组名。组名本身不输出。
- `other_matchers`、`other_options`、`details`、`skeleton_status` 表明未知条件；`skeleton_truncated` 表明达到界限。每列表最多 16 项、服务器最多 16 个、路由/处理器合计最多 128 个、嵌套深度最多 12。

域名、header/query 值、重写文本、任意上游、任意处理器名及组名均不输出。树中的父匹配条件仍约束子路由。`observed` 仅表示已覆盖的结构被观察到；`effective_order` 继续保留人工复核口径，不据树自动判定路由正确。

## 已跑

命令：`python docs/evidence/bps-final-release-20261111/preflight/verify.py`，退出码 `0`。真实输出见 `fixture.log`，脱敏样例见 `caddy-skeleton.example.jsonl`。

- 使用缓存 `caddy:2.10.2-alpine`、`--pull never --network none` 执行真实 `caddy adapt`；分别验证 relay 在通用 API 之前/之后、GET/HEAD、CPA/Manager、嵌套及分组。
- 真实 Caddy 配置含合成秘密哨兵，覆盖 host、header、query、rewrite、任意 upstream；摘要无哨兵，未知匹配器、重写、目标及终止结构均保留。
- 覆盖未知路径、方法、处理器、结构及大小/深度界限。摘要函数执行期禁止文件、网络、子进程和写入审计事件。
- 对交付脚本本体与 `sudo bash <<'CPA_PREFLIGHT_READONLY_20261111'` 外层原文分别运行缓存 Linux `bash -n`；标准错误为空、退出码均为 `0`。以 UTF-8 字节传入且保留 LF，未执行脚本。
- `git diff --check` 退出码 `0`。

## 复用

专项把本轮纯函数与一处 `emit` 扩展移除后，对比基线内嵌 Python 的完整 AST，结果相同。原 GET、认证来源、活动状态、文件读取、Docker/Compose 调用均未变，因此复用 `docs/evidence/phase2-release-preflight/fixture.log` 的真实 Linux 四服务、文件字节/容器身份不变、认证状态、未知不记零等已通过证据；本轮不重复旧全矩阵。

## 仍待现场确认

未知匹配器（含脱敏 host 条件）、重写、非固定路径/方法/目标、未覆盖的处理器/选项、错误路由及被截断内容仍需人工复核。专项没有再次运行完整预检；现场路由、鉴权、活动与维护窗口资格以用户后续完整只读采集为准。
