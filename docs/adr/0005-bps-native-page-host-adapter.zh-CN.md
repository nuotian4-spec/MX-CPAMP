---
status: accepted
---

# 插件原生页的动作由管理面板承接，不执行插件页面脚本

cpa-bps 的原生 `index.html` 用 `<script src="app.js" defer>`、`<script src="auto.js" defer>` 实现邮箱密码自动授权：脚本把页面动作通过 `bps-management-*` 消息交给宿主，由宿主转发到 CPA 的管理接口。当前 CPAMP 以 `sandbox="allow-same-origin"`（无 `allow-scripts`）嵌入该页，脚本不执行，原生流程不会自跑。

我们选择由 Manager 自己承接：读取原生 DOM 的输入，用 TypeScript 调用插件已有的 `/cpa-bps/auto-login` 等接口，再把真实结果写回原生页面。

被否掉的替代方案是给 iframe 加 `allow-scripts` 并实现 `bps-management-hello/ready/request/response` 宿主协议，让插件脚本自己跑完。那是供应商自家面板的做法，行为最贴近原生，但代价是：同源 + 允许脚本意味着插件页可以脚本化面板自身的 window 与 DOM；同时现有手动 OAuth 绑定要整体替换成消息协议。既然业务验收看的是凭证结果，而不是插件 JS 是否执行，我们保留不执行页面脚本的边界，把管理密钥留在 Manager 内。

代价是页面文案与插件脚本不完全一致：插件 `auto.js` 在 `status==='ok'` 时即显示“CPA 已保存账号”，我们额外要求 `saved` 与 `adopted` 为真才显示成功。这是刻意收紧，避免“已保存但未注册”被当成成功。若供应商重做页面结构或更换控件 id，先退到手动 OAuth 入口，不按版本号写特例。
