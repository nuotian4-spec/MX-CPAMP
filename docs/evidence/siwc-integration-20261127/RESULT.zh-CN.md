# CPAMP SIWC 本地接入记录

日期：2026-11-27（Asia/Shanghai）。结论：SIWC本地接入完成。后端、Web、新增专项、页面、最终产物接线和来源映射均获独立审查PASS，P9放行本地候选交付。

## 功能

现有 Codex 账号详情增加 SIWC 状态、绑定和重新绑定。用户打开授权链接并提交完整回调后，页面轮询同一授权会话；只有服务端确认成功且回读目标凭证后才显示完成。保留原账号身份、通道、代理及外层 OAuth 凭证。

Manager 增加 `/api/siwc` 五条专用接口，连接引用、凭证 id/index 和会话 state 贯穿授权流程。Home、运行时及非文件凭证保持只读。没有新增通用 v8 代理、独立 provider、解绑或启用开关。

回调接收不等于授权成功。取消未生效时回查同一 state；过期或 CPA 重启后释放失效会话。确定的输入拒绝允许修正，提交结果不明时只查状态；切换账号或连接后不采纳旧回执。

## 输入与证据

候选工作树：`C:/Users/子羽/.codex/worktrees/cpa-core-v817-20261127/MX-CPAMP`。

基础提交：`b9ea2dc4c6bb377d31143970a6df1084297e4b09`。CPA v8.1.7 组件树为 `8501bb9d2095cb9e7bc71b6bc008aa097b6670cc`，本轮不改变内核，复用既有升级验证。

本轮CPAMP组件树：`ba548720f8d46f16835b8d5bed5b2d99aeaf316a`；联合候选树：`a2e51ff45f1f04e3f706f28f474b2b752e0bc905`。Web的12文件增量在导入前后按字节核对，独立审查确认主候选与已验输入一致，见`web/WEB-IMPORT.json`。

| 边界 | 结果与依据 |
|---|---|
| Manager 真实路由到合成 CPA HTTP | 16 根、31 叶子通过，退出码0；见 [后端记录](backend/RESULT.zh-CN.md) |
| 最终 CPA 与 Manager 成品 | 禁网合成拒绝回调链路9项通过，退出码0；见`backend/real-chain-result.json` |
| Web API 与组件生命周期 | 有效 API 组6项、组件组13项、新增输入分类1项，共20项通过；按各有效组复用证据 |
| 实际账号详情页面 | 1280×900 / 390×844、明暗四图已检查；窄屏操作可纵向滚动触达，无可见水平溢出 |
| Web → 静态嵌入 → Manager | 唯一最终tsc、Vite、Linux Manager构建均退出0；见`web/build-final.log`和`web/manager-build-exit.txt` |
| 来源映射 | 5125项精确映射通过，新增接入仅影响20项CPAMP文件，CPA映射保持；见`web/WIRING-FINAL.json` |
| 独立审查 | 全部本轮边界PASS；见[审查记录](review/REVIEW.zh-CN.md) |

## 交付边界

专项使用合成账号和本地模拟CPA，未进行真实OpenAI SIWC授权、模型调用或生产部署。无产品数据库schema/迁移动作。代码为本地未提交候选，发布应绑定后续固定提交。

成品联合验收另外使用真实CPA与Manager二进制，范围为合成文件读取、生成链接、拒绝回调和失败终态，不覆盖真实授权成功及令牌交换。容器`network=none`，两个测试服务已停止、临时合成状态已清理；CPA启动时的公共版本刷新尝试受禁网限制，不构成外部请求成功证据。

页面截图使用真实AppRouter和AccountsPage接合成HTTP。背景的“CPA连接已变更”来自合成服务未覆盖既有`/api/bps/settings`的connection_ref，原有凭证列表校验按合同报错；该提示不是SIWC的连接失败，页面观察不替代生产连接验收。截图见`web/desktop-light.jpg`、`desktop-dark.jpg`、`mobile-light.jpg`和`mobile-dark.jpg`。

生成的内嵌HTML保留构建输出行尾，`git diff --check`会报告生成物空白；产品源码无对应空白问题。已验Web与内嵌HTML逐字节一致，保留该成品供本地接线验收。

使用方式见 [SIWC 账号绑定](../../siwc.zh-CN.md)。实施分工与当前责任见 [执行入口](../../execution/SIWC-INTEGRATION-20261127.zh-CN.md)。

当前态：本地候选完成，测试服务与临时预览已停止。下一责任人：P9在后续入仓/发布任务中绑定固定提交；真实授权与生产验收单独记录。
