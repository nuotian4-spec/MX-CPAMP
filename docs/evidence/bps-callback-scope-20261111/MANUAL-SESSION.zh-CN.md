# 修复版手工 BPS OAuth 验收窗口

日期：2026-11-11。用户已确认本轮一次手工BPS OAuth：官方授权、提交新回调和唯一保存注册，不主动刷新、不调用模型、不启动Worker、不操作OVH。

## 启动与空池

主会话的启动命令在创建进程前被工具拒绝，见[历史启动状态](MANUAL-START-STATUS.json)。随后用户在本机Windows PowerShell运行已审`scope-manual-run.mjs start`，交回[真实终端回执](MANUAL-USER-START.jsonl)；用户命令退出码未单独采集，不补写。

本轮非秘密状态指针：`C:/Users/子羽/.codex/artifacts/mx-bps-scope-manual-41169cbfa2fb/state.json`。CPA/Manager固定ID和revision与已审候选一致，实际状态为ready；06:11:08.755 UTC创建，06:26:08.760 UTC到期，即北京时间14:26:08。独审仅核状态允许字段，归属、身份、预算一致，未读取访问文件或凭据。

主会话06:12:06 UTC执行已审verify，真实exit0；[空池回执](MANUAL-VERIFY-EMPTY.jsonl)为pending，保存总数/运行时总数/BPS保存及注册均为0。此时没有开始OAuth，不将pending或exit0写成登录成功。

```powershell
& 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe' 'D:/MX-CPAMP/docs/evidence/bps-callback-scope-20261111/scope-manual-run.mjs' verify 'C:/Users/子羽/.codex/artifacts/mx-bps-scope-manual-41169cbfa2fb'
```

## 人工操作与接续

本机入口为`http://127.0.0.1:2702/management.html#/oauth`；临时管理员密钥由用户在本机私有`本机访问.txt`内读取，不进入仓库或聊天。打开面板与文件的Codex工具返回queued，不作为页面已显示证据。

用户登录本机面板，仅操作BPS OAuth：生成一次新授权链接、在官方页面完成授权，把本次官方回调完整地址填回本机面板并提交一次。两处2FA不在本轮范围，无Worker时应明确禁用。

本轮已完成：用户报告登录成功，06:18:54 UTC最终verify确认唯一保存注册、profile/client、OAuth方法、令牌存在性及runtime active全部通过，exit0；实际配置模型清单为空的只读诊断已完成，随后stop归属清理通过、临时令牌状态不保留。见[最终结果](MANUAL-RESULT.zh-CN.md)。本轮入口已结束，不重交旧回调或复用访问文件。当前责任人：主会话归档结果并核对新增巡检反馈。

没有产品改动、SQL/schema迁移、推送或生产操作。原生图片/工具、Codex正向2FA及HTTPS中转的既有验收边界不变。
