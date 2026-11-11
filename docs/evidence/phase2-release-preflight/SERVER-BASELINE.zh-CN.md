# OVH二期发布基线回执

日期：2026-11-11。核证源码基线 `592ea1cd8a0d73ff22c913646c6cc7520d7b2e25`。来源为用户在OVH终端执行已审短盘点入口后回传的附件 `efd80026-53ff-4680-865e-bb50faa03e53/已粘贴的文本.txt`，没有远程代执行。

[现场JSONL](server-baseline-20261111.jsonl)保留18条完整结果，仅从首条服务行移除终端回显的 `PREFLIGHT` 前缀，其余JSON原文保留。命令回显中的杂字不作为执行指令或失败证据；四服务至只读完成终态齐备，无需重复本次盘点。没有显式进程退出码，不补写exit0。

## 已核事实

四服务均为running。CPA、Manager、Worker的完整镜像ID及revision匹配旧[分发清单](../deployment/mx-cpamp-images.manifest.json)，Caddy镜像ID匹配旧[生产回执](../deployment/server-release.zh-CN.md)。Manager为healthy，其余health记录unknown；不将unknown归为健康故障或业务健康通过。

| 服务 | 容器ID | Compose文件链 |
|---|---|---|
| cli-proxy-api | `0cf979adbe916fba8892309800f7940a28bb6038f17428e67b8f2c4de1120a53` | compose.yaml → compose.mx-cpamp.yaml |
| cpa-manager-plus | `ea0af0d5c8acff58abd9e96adb48687d24a4d03e381c2a123b1c4952931531eb` | compose.yaml → compose.mx-cpamp.yaml |
| oauth-login | `880b9f091993a61152a732568447ba60026eb09c1bad6fdfb746c27ffbf81cce` | compose.yaml → compose.mx-cpamp.yaml |
| caddy | `65e16a7220570b0847458b94eba1e779cff90a7cb50095e1dd00261fc41854ab` | compose.yaml |

工作目录统一 `/opt/cpa-stack`，文件链中两文件均为该目录下绝对路径，权限600、root:root，大小1725与1267字节。Caddy保留原单文件链是当前实际拓扑，不要求它与三个应用的链完全相同。

- 四服务均连接 `cpa-stack_default`；三个应用没有宿主发布端口，Caddy仅发布IPv4/IPv6 TCP 80、443。
- CPA保留 `/opt/cpa-stack/cpa/{config.yaml,logs,plugins,auth}` 原bind，目标分别为 `/CLIProxyAPI/{config.yaml,logs,plugins}` 与 `/root/.cli-proxy-api`，均RW。
- Manager保留 `/opt/cpa-stack/manager-data` → `/data` RW；管理员key与登录secret只读。Worker只挂同一只读登录secret，没有CPA数据卷。
- Caddy保留两命名卷及只读 `/opt/cpa-stack/Caddyfile`。挂载Source/Target/RW完整值见JSONL，后续比较只规范化顺序，不丢实质字段。
- 可用空间98,478,813,184字节，约91.72GiB；本回执没有计算新发布归档及新备份的最终空间需求。
- 旧 `.mx-upgrade-20261110-rc1/complete` 存在；旧状态及备份目录700，root:root。`data.tar.gz`、`environment-sources.tar.gz` 均600，分别20826与69字节，与历史回执一致。只核元信息，不据此宣称归档内容或恢复验证通过；旧备份不作为本次新停态备份。
- 管理入口 `/opt/cpa-stack/cpa-compose` 存在，700、root:root、268字节，本次没有执行或读取其正文。
- `/opt/cpa-stack/cpa/bps-state` 不存在，当前CPA没有独立BPS状态挂载。这不证明其他位置不存在绑定数据；准备新增目录前仍须核有效配置与旧默认状态来源，避免空挂载遮蔽。

## 证据边界与下一动作

独立只读核证通过，未再次运行Docker测试、访问生产或读取秘密。现场回执明确 `login=unknown`、`model_inflight=unknown`、`write_release_ready=false`。

尚未观察即时内部认证、Caddy实际路由、有效state-dir/KV路径、既有状态内容范围及备份恢复可用性。元信息完整不代表可停机切换，亦不替代手工BPS OAuth、Codex正向2FA或HTTPS中转验收。

当前责任人：主会话已归档现场基线。下一责任人：主会话以该四服务及各自Compose链为输入适配新版prepare/apply材料；有效配置/状态来源及路由缺口由匹配已审只读探针定向补证，不要求重复已收齐的镜像和挂载盘点。没有适用门槛证据前不交生产写入命令，不覆盖旧rc1状态与备份，不推送或部署。
