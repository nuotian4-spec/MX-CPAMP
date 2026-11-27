# SIWC 接入独立审查

日期：2026-11-27。审查席：cpa_v817_review。结论：PASS，本地SIWC接入候选放行。独审只写本报告，产品/测试修改为0，测试/构建重跑为0。

## 范围与依据

唯一联合候选：`C:/Users/子羽/.codex/worktrees/cpa-core-v817-20261127/MX-CPAMP`。CPA v8.1.7 已通过独立升级审查，本轮复用其同版 SIWC 合同与证据。

基线HEAD：`b9ea2dc4c6bb377d31143970a6df1084297e4b09`。最终组件tree：`a2e51ff45f1f04e3f706f28f474b2b752e0bc905`；CPAMP tree：`ba548720f8d46f16835b8d5bed5b2d99aeaf316a`；CPA tree仍为`8501bb9d2095cb9e7bc71b6bc008aa097b6670cc`。

CPA 的 SIWC 是已有普通文件型 Codex OAuth 凭证的第二分区。`components/cpa/docs/management-api-v8.md:233` 定义 bind、完整回调、状态与取消；`oauth_siwc.go` 要求外层 Codex token、固定目标与 issued client_id。旧 v0 凭证列表缺少 SIWC 摘要，因此 Manager 需要专用窄桥接，Web 在凭证详情承载绑定/重新绑定。

## 接口预审

拟定 `/api/siwc/{credential,bind,callback,status,session}` 路径与精确方法可复用既有管理员认证、`no-store`、连接引用与服务端状态，不开放通用 v8 代理。

- `credential` 从 v8 摘要精确解析 `auth_id + auth_index`，输出窄字段并建立与实际 CPA setup 绑定的 `connection_ref`。
- 后续请求要求连接引用、目标身份；会话请求再要求 state。state 在 Manager 进程内保存原 setup 与目标，期限300秒。
- callback 保留完整 URL 的 issued client_id，允许仅 error 的授权拒绝；浏览器不选择上游 URL、PKCE、设备或 token 参数。
- callback 接收仅返回 wait；OAuth status 为 ok 后回读目标摘要才显示本次绑定完成。旧内核404明确报不支持，不回退普通登录。

### 已核对的边界

DELETE 的 `cancelled:false` 不等于绑定成功。CPA `oauth_sessions.go:480` 对不存在、过期、错误、完成或提交中状态均可返回 false，`auth_files_provider_oauth.go:1320` 仍返回 `status=ok`。Manager 已先读取同 state 的 OAuth 终态，只有其 ok 才回读凭证并报告本次完成；error/缺失不被原有 SIWC 摘要覆盖。

后端新服务按目标会话串行化回调、状态和取消；每次上游请求前后核对原连接引用；只输出声明的摘要字段。Home模式从既有`credentials/concurrency`的mode窄投影明确判只读，bind不发上游POST。会话操作404按会话失效处理，能力入口404仍明确不支持；固定loopback与重复参数在本地校验，明确400输入拒绝释放submitted，响应中断保留submitted只允许查状态。

### 前端生命周期

- 五分钟到期后DELETE遇到服务端410，hook清除active及session，允许原位生成新链接。
- 回调发送前设置submitted；传输失败或接收结果未知时禁重复提交并保留查询。明确400 `siwc_callback_invalid`或`siwc_callback_state_mismatch`允许纠正输入。
- 请求捕获当次AbortController；取消替换controller后丢弃旧轮询回执，账号、连接切换或卸载后按原scope取消，owner/sequence隔离迟到结果。

回读继续校验id/index/ref，回调没有持久存储路径；表单提交与切换目标清除输入。页面placeholder与专项回调使用实际`127.0.0.1:1455`合同。入口接入真实AccountsPage的凭证详情配置区，沿用公共Button/Input及既有样式变量。

## 已核专项证据

- 后端：`backend/command.txt`、`exit-code.txt`、`siwc-manager-tests.jsonl`，统一一次运行，16根/31叶子通过，exit0、失败事件0；覆盖管理员认证、窄摘要、连接/目标、Home只读、状态/取消、重启过期、并发提交与明确拒绝纠正。
- Web：`web/final-unit-results.json`中API组6项通过；该整份结果success=false，其组件加载失败部分不作为通过证据。组件采用`web/final-component-results.json`的13项通过，再引用`web/state-mismatch-results.json`新增单项通过，其余13项为筛选跳过。最终有效Web专项20项，不重复累计或重跑。
- 最小跨产物：`backend/real-chain-result.json`与原始命令/exit，真实已验CPA成品和最终Manager在`network=none`容器内运行，9项通过、exit0。合成文件经精确摘要读取、bind wait、完整error-only回调到同state错误终态；原凭证字节不变，无token交换或模型请求，两个服务及临时状态已清理。
- 最终产物：`web/BUILD-RESULT.json`及对应原始日志，唯一最终tsc/Vite/embed/Linux Manager构建均exit0。12个Web文件精确导入后逐字节复核一致；实际dist与Manager内嵌页面均为6,863,077字节、内容相等。CPA未重建。
- 实际UI：`web/VISUAL-RESULT.json`与4张JPEG，实际AppRouter→AccountsPage凭证详情，1280×900、390×844明暗主题均目视核对。SIWC沿用配置区，窄屏可滚动触达完整回调及三项操作，未见横向溢出或布局返修项。
- 来源映射：`web/WIRING-DELTA.json`与已落盘`deploy/production-wiring.contract.json`，5125项mode/blob与最终组件tree精确一致；增量仅20项CPAMP文件，CPA映射及历史lineage保持。`web/WIRING-FINAL.json`记录合同及来源映射校验均通过；实际合同的独审状态为PASS，回执指向本报告。源码diff未命中数据库schema、SQL或迁移。

## 验收边界

生成`management.html`含49处CRLF，其中1处为`<div id="root"></div>`后的CRCRLF，位于脚本和样式之外；基线HEAD页面为纯LF。这是本次产物行尾格式变化，默认`diff --check`仍报行尾，整份检查不记为通过。内嵌页面与已构建dist逐字节相等，现有真实页面四视口及跨产物证据覆盖该成品，未发现由此造成的运行或布局问题；保留已验字节，构建与专项无需重跑。

真实UI使用合成HTTP回执。截图背景列表的连接红条来自合成服务未覆盖`/api/bps/settings`，返回空对象触发既有`bpsCredentialSnapshot.ts:17`检查；已核fixture源码和请求日志，并原样保留提示。它不构成本次SIWC连接失败证据。

本轮证明本地产品接入与离线合同、产物链通过。成功保存分支复用同版CPA专项；真实OpenAI SIWC授权、账号登录及生产部署未验收。未打开真实授权页、未提交token交换、未调用模型或额度接口。无产品数据库动作，测试仅使用内存SQLite和已清理的临时状态。

候选未提交、推送或部署；发布绑定后续固定提交。来源映射已落盘并完成精确校验，本地候选审查收口。

界面依据：`D:/MX-CPAMP/docs/design/UI-DESIGN-SYSTEM.zh-CN.md`；普通凭证详情增量沿用既有组件和语义，不新增主导航或独立账号类型。

下一责任人：P9收口本地CPA升级和SIWC接入结果，裁定后续入仓与生产发布入口。
