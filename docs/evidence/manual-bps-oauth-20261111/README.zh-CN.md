# 手工 BPS OAuth 本机隔离入口

日期：2026-11-11。源码基线 `4fa30283eec56c4f62c365ac8a39a56d57add96d`。用户已确认一次手工 OAuth，仅验证官方授权、回调、兑换及唯一保存注册；不主动刷新，不调用模型，不操作 OVH。

## 已准备

- 固定 CPA/Manager 产物 ID 与 revision；没有重建产品，没有 Worker，没有导入旧凭据。
- 仅 Manager 的 18317 入口映射到宿主 `127.0.0.1` 随机端口。Manager 共享 CPA 网络命名空间，CPA 8317 没有宿主映射。
- 两容器根只读、日志驱动 none；数据、配置、密钥及账号位于容器 tmpfs。仅临时管理员访问材料写入本机 ACL 私有 artifacts 目录，不进入 Git、argv、环境变量或 stdout。
- Manager 配置关闭 quota cooldown、account actions 及自动禁用。CPA 使用内置模型表与现有本机 7890 代理；没有自动更新控制面板。
- 原产品有后台令牌调度，没有为本轮修改或硬性禁用。启动器不调用刷新/模型接口，`harness_initiated_*` 仅指启动器自身，不等于观察到全部上游请求为零。预算是本轮操作约定，不是出站流量防火墙。

无凭据实际启动 [START.jsonl](START.jsonl) exit0：health 200、面板 200、无管理员密钥 401、已鉴权但未知 OAuth state 404。没有创建 OAuth 会话。初次就绪断言的 [START-FIRST.jsonl](START-FIRST.jsonl) 保留非零结果与清理确认。

[VERIFY-EMPTY.jsonl](VERIFY-EMPTY.jsonl) exit0，空池保存数、注册数均为 0，结果为 pending；不把健康或命令 exit0 当成 OAuth 成功。

## 本机操作

运行时目录、实际 URL、访问文件和到期时间以本次 START.jsonl 为准；临时入口不是生产域名。URL 使用 `/management.html#/oauth`，不要访问裸 `/oauth`。

用户在本机打开访问文件，复制临时管理员密钥登录。不要填写 CPA 内部地址或 CPA 管理密钥，不勾选记住密钥。进入 BPS OAuth 卡片，生成并打开一次官方授权链接；官方登录完成后，把完整回调 URL 填回本机面板，提交一次，等待保存注册结果。密码、TOTP、完整回调与临时管理员密钥不贴到聊天。

有效窗口为 15 分钟。容器内 timeout 只会到期停止服务进程，不会删除容器或本机访问文件；结束后仍须运行 stop，确认本轮归属并清理。成功后先让页面收到结果，再由主会话 verify 并立即 stop。

## 启动、查询、核验、清理

固定 Node：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`。

```powershell
$node='C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe'
$run='D:/MX-CPAMP/docs/evidence/manual-bps-oauth-20261111/run.mjs'
& $node $run start
& $node $run status '本次回执中的绝对state_dir'
& $node $run verify '本次回执中的绝对state_dir'
& $node $run stop '本次回执中的绝对state_dir'
```

verify 从容器 tmpfs 读取管理员密钥，经真实 Manager 代理取得认证文件列表，独立核磁盘 BPS 文件数量和 runtime 注册条目数量，再比较唯一条目、profile/client、oauth 标记与 access/refresh 存在性。只输出计数、布尔值与 passed/pending，不输出邮箱、账号 ID、文件名或令牌。它不创建 OAuth 会话、不兑换回调、不刷新、不调用模型。

stop 只删除本轮唯一 label 对应的两个容器与访问文件，保留不含秘密的 state/verification 回执；不操作其他项目资源。

本轮无 SQL/schema 迁移、生产写入、push 或部署；运行时 Manager 临时库与 OAuth 凭据写入仅在隔离 tmpfs。[本次用户结果](USER-RESULT.zh-CN.md)为未完成：会话仍wait、保存/注册均0，随后归属清理确认。已结束的地址和访问文件不得当成当前入口。下一责任人：主会话收口诊断修复与独审，不重复本次授权。
