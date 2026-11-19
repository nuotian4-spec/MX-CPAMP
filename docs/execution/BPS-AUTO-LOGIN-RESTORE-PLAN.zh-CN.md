# BPS 原生邮箱密码授权：接口核查与本地候选

## 结论先行

`cpa-bps 0.5.86` **自带**由插件自己支持的单账号提交与结果交接接口。先前记录的“现有材料未确认另有该接口”，依据只是包内说明与供应商公开页面，没有查看插件二进制与页面脚本；那是材料没覆盖，不是功能不存在。

本机实测依据（`D:\子羽\QQ\cpa-bps-unified-0.5.86-linux-amd64-binary.zip`）：

- `plugin/dist/cpa-bps.so` 内嵌原生页 HTML、`app.js`、`auto.js`，以及路由字符串 `/v0/management/cpa-bps/auto-login`。
- `app.js` 把页面动作映射成 `start` / `status` / `callback` / `auto` / `auto-status` 五类宿主请求；`auto.js` 实现邮箱密码队列：建会话、逐条提交、轮询、确认保存、停止。
- 这些脚本被 `<script src="auto.js" defer>`、`<script src="app.js" defer>` 引入，当前 Manager 的 `sandbox="allow-same-origin"`（无 `allow-scripts`）使它们不执行，因此原生流程不会自跑。

## 插件接口契约

| 步骤 | 调用 | 约定 |
| --- | --- | --- |
| 建本次会话 | `GET /cpa-bps-auth-url` | 返回 `{url, state}`；`state` 绑定本次，`url` 必须是 `auth.openai.com/api/accounts/authorize` 且 `redirect_uri` 为 BPS 回调 |
| 提交单账号 | `POST /cpa-bps/auto-login` `{state, account}` | `account` 即一行 `邮箱----密码----2FA密钥`；**只有 `status=pending` 算已受理**，其余 status 为明确未启动，`message` 可展示 |
| 查进度 | `GET /cpa-bps/auto-login?state=` | `pending` 继续，`ready`/`success` 表示登录完成，其他为终止；每笔 1500ms 轮询，插件侧另有 150 秒上限 |
| 确认保存 | `GET /get-auth-status?state=` | `status=ok` 且 `saved=true` 且 `adopted=true` 才代表本次授权成为可管理凭证 |
| 停止本批 | `POST /cpa-bps/auto-login` `{state, cancel:true}` | 只结束同一笔，不重发提交；服务端也执行自己的截止时间 |

配套 `bps-auto-login/` 服务（127.0.0.1:18788，Bearer 密钥）由插件侧调用，`GET /health` 返回 `{"status":"ready"}`，是不提交账号凭据即可判定就绪的检查。网页上传 ZIP 不会安装该服务；本次既未探测生产上的该服务，也不把它作为页面可用的前提。

## 本轮范围（已决定）

1. **由 Manager 承接，不执行插件页面脚本。** 保留 `sandbox="allow-same-origin"`，管理密钥不进 iframe。不采用 `allow-scripts` + `bps-management-*` 宿主协议：那会让同源插件页可脚本化面板自身 DOM，且需替换现有手动绑定。
2. **成功判据取三元组。** `status=ok`、`saved=true`、`adopted=true` 同时成立才写“授权成功，CPA 已保存并注册账号”。插件 `auto.js` 只看 `status==='ok'`，故页面措辞会与插件原生脚本不同；这是刻意收紧。
3. **保留完整队列语义。** 每行一个、最多 100、顺序执行、可停止；把上限砍到 1 行只限制输入框，不构成另一种授权方式。
4. **本轮只出本地候选，不部署。**

## 本地实现

- `services/api/bpsAutoLogin.ts`：`submit` / `status` / `cancel` 三个封装，校验 `state`、账号行长与状态码形态；密码只出现在提交体里。
- `features/plugins/bpsAutoLoginMenu.ts`：解析（与插件 `auto.js` 同规则：首末 `----` 分隔、密码非空、`^[A-Z2-7]{16,208}=*$` 的 2FA 密钥、邮箱去重、单行 ≤4096、单批 ≤100）、逐条提交、轮询、确认保存、停止。
- `features/plugins/bpsManualMenu.ts`：接入上述绑定，返回 `{autoReady, manualReady, disconnect}`；手动 OAuth 逻辑保持原样。
- `PluginResourcePage.tsx` 与四种语言文案：区分“自动授权已接通”与“本次不可用”。

安全面：提交后立即清空输入框；结果行只写邮箱；查询与取消只带 `state`；`pagehide` 与断开时清空输入并结束本批；`401/403` 中止整批并提示重新登录，`404/405` 明确提示核心未提供该接口并保留手动入口。

## 本地验证

| 项 | 结果 |
| --- | --- |
| 插件目录单测 | 76 项通过 |
| `tsc --noEmit` | 无输出 |
| eslint（改动文件） | 无输出 |
| `vite build` | 通过 |
| Chromium 联调（复用 `joint-tests/bpsNativeManual.browser.mjs`） | 4 个手动布局场景 + 5 个自动场景全部通过 |

自动场景实测输出：

| 场景 | 页面行文本 | 提交 | 查询 | 保存确认 |
| --- | --- | --- | --- | --- |
| 正常保存 | 授权成功，CPA 已保存并注册账号 | 1 | 2 | 1 |
| `adopted=false` | 结果待确认：CPA 尚未确认保存并注册 | 1 | 1 | 1 |
| 插件拒绝 | 账号或密码不正确 | 1 | 0 | 0 |
| 接口 404 | 核心插件未提供自动授权接口 | 1 | 0 | 0 |
| 用户停止 | 已停止；若已完成授权，请到凭证管理核对 | 1+取消 1 | — | 0 |

全量单测有 13 项失败，均在 accounts / config / kilo / update 页面，与被改文件无 import 关系；其中 11 项在干净基线 worktree 同样复现。`updatePages.integration.test.tsx` 整文件加载失败的原因是该文件自身 `vi.mock('@/features/demo/demoMode')` 未导出 `DEMO_API_BASE`（该文件与相关 demo 模块在本轮均未改动，且只存在于本分支），本轮不处理。

## 尚未成立的部分

- **生产未部署。** 生产仍是 `v2026.11.20-rc.1`，本轮改动只在本地分支。
- **真实授权未验证。** 没有账号持有人参与，没有完成过一笔真实 BPS 授权；`status=ok`、`saved=true`、`adopted=true` 只在模拟 CPA 响应下验证过，凭证列表出现 BPS 凭证这一条**未验证**。
- **未探测生产辅助服务。** `bps-auto-login` 容器是否就绪未知；若未就绪，真实提交会以插件返回的状态失败，页面按失败显示，不会误报成功。

## 验收口径（未变）

账号持有人从原生菜单完成一笔授权，页面显示成功，CPA 凭证列表出现对应的 BPS 凭证；三者同时满足 `status=ok`、`saved=true`、`adopted=true`。HTTP 200、模拟成功、表单可输入都不算真实授权成功。
