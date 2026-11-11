# OVH 完整只读回执

2026-11-11。来源为用户回传附件 `b8c7ca4c-3938-4640-b146-54c752a2da43/已粘贴的文本.txt`，执行入口是已审完整预检。提取[26条JSONL](READONLY-20261111.jsonl)，仅移除首行终端回显前缀及头尾非JSON回显。附件有 `read_only_facts_collected` 完成态，没有显式进程退出码，不补写服务器exit0。

[本地核证](CHECKS.json)退出0，独立只读核证结论一致：

- 四服务完整容器ID、镜像ref/ID/revision、各自Compose链、Source/Target/RW、网络及端口与旧18条基线一致。Manager healthy；其余not_configured细化了旧unknown，不表示健康故障。
- Compose 5.7.0在现场成功解析原双文件链，服务集匹配。原Caddy仍单文件链，没有服务切换。
- CPA为200/401，Manager预知不存在任务为404/401，Worker为200/401；三项内部正负认证探针均通过。
- 登录批次及并发接口均404，计数为null/unknown，不能推断无活动。单登录任务和Worker活动仍无只读枚举接口。
- Caddyfile的实际adapt摘要未截断：第0终止分支通用请求到Manager；第1终止分支的白名单含`/v1/*`及三个未知路径到CPA，同组保留静态兜底。父host、其他路径及静态正文未输出；这不是Caddy运行中admin配置快照。
- 未观察到显式relay路由。预检按根`bps-protocol`布局报告基址未配置；后续源码实核确认v8布局会逐字段覆盖根布局，因此有效基址仍需合并后确认，不能只凭此读数认定运行时无配置。脱敏树也不证明所有可能的HTTPS取图路径必然不可达。
- 宿主`cpa/bps-state`不存在；按根布局计算的默认容器路径为`/root/bps-state`，未映射。v8有效路径及旧状态存在性仍unknown。发布工具将按产品优先级核有效来源，继续保留遮蔽保护。

主会话另只读核对用户原DNS截图：最终记录为`cpa.maixun.cyou`和`cpamp.maixun.cyou`。经7890代理、默认TLS校验的无凭据GET分别得到CPA `/v1/models` HTTP401和Manager `/health` HTTP200，curl均exit0；未读取响应正文、发送Key或调用官方接口。这只证明两个现有HTTPS入口的该次可达与状态，不证明图片relay成功。

当前责任人：主会话归档回执并派发[relay与有效配置专项](../../../execution/bps-relay-release-20261111.zh-CN.md)；下一责任人：两执行席补原CPA站点的精确GET/HEAD下载路由、协议origin、有效配置合并及改变的发布边界，独审后再交生产材料。既有真实业务及未变门禁复用；不再重复整轮盘点。Codex正向2FA与生产实际HTTPS取图仍待对应验收。

`write_release_ready=false`。本次用户执行只读采集，代理只做本地核证与两次无凭据HTTPS GET，没有服务、配置、数据库或官方账号写入。
