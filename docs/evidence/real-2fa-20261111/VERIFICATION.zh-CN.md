# 工具验证回执

日期：2026-11-11。只验证新工具及候选启动，没有提交任何登录任务、使用旧秘密、连接生产、模型调用或令牌刷新。

- `node --check`：run、check、worker、proxy-runner、diagnostics五脚本均退出0。
- 无网络脱敏契约断言：允许的Manager/Worker诊断枚举保留；未知stage/reason置null；password/html标记及未定义的http_status被移除，退出0。
- 叠加候选 `run.mjs --preflight`：退出0。完整参数见README；Worker源码与Manager Linux binary绑定产品源revision `a5eaebbb777152ce71900af447289e61e8088316`，Manager临时ldflags SourceCommit为短值 `a5eaebb`。
- 三组件健康、管理鉴权、readonly/lognone/tmpfs/无端口边界、固定本地代理TCP端口可达均通过；临时容器清理核实通过，候选输入前后摘要未变。完整脱敏输出见 `preflight-receipt.jsonl`。

此回执未证明真实终态stage/reason已正确穿透；无凭证preflight不产生该终态。下一责任人为P9，在诊断独审通过并收到新材料后选择单通道或both各一次复测。正式镜像、完整revision构建、真实CPA保存注册与业务验收保持待验。
