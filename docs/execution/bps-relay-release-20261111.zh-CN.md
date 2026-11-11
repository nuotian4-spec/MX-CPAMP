# BPS HTTPS 中转与发布配置接续

2026-11-11。基线 `bab1afb07e3cf25e78394b6cb959aaa5999b31bd`。用户已选双图片模式、默认原生及完整验收后统一发布。最新[OVH只读回执](../evidence/bps-final-release-20261111/server/README.zh-CN.md)已核四服务未漂移、三项认证符合预期；登录/模型活动仍unknown。不重盘点或重复已通过业务。

## 目标与分工

| 责任人 | 允许范围 | 完成依据 |
|---|---|---|
| A：bps_access_manual | `deploy/bps-final-release.{py,sh}`、`docs/evidence/bps-relay-release-20261111/tools/**` | 产品有效配置优先级；候选触及字段规范写入；原CPA站点下载路由；prepare/取消零live变更；实际Caddy validate/reload、原ID/inode/卷、备份及失败阶段 |
| B：bps_quota_surfaces | `deploy/phase2-readonly-preflight.sh`、`docs/evidence/bps-relay-release-20261111/contracts/**` | 根/v8/混合布局有效字段与固定CPA Settings回读；预检反例；能完成的真实HTTPS合成取图及明确缺证 |
| C：callback_compatibility | 独立只读审查 | 定向配置、state来源、Caddy及改变的维护边界；不重跑已有门禁 |
| 主会话 | 执行包、现场回执、整合与当前态 | 精确输入/产物/命令/退出码、审查结论与实际边界一致 |

两执行席从同一基线在各自既有隔离worktree新建分支，文件域隔离。10分钟给实质检查点；20分钟遇具体阻塞停止扩展，交明确下一动作。缓存镜像与工具优先，合成测试断网，不构建产品。

## 具体契约

产品同时支持兼容根`bps-protocol`和规范v8 `upstream.bps.protocol`；后者按存在字段优先。现预检和发布工具只看根布局，须补有效来源与写入的一致性。候选只规范化本轮触及的`image-transport`、`image-relay-base-url`、`state-dir`，不扩为整配置迁移；空模式仍按产品默认native。实际Settings回读须与候选一致。

已有域名由用户原DNS截图和无凭据HTTPS只读访问确认：CPA为`https://cpa.maixun.cyou`，Manager为`https://cpamp.maixun.cyou`。prepare仍原位核Caddyfile、adapt及运行配置，失配在维护前停止。将GET/HEAD `/api/bps-images/*`下载路由加在原CPA站点，沿产品handler验证token；保留原URI、模型白名单、Manager路由、TLS和证书卷。不上新域名、端口或服务，不公开上传、目录或state。

原Caddy是单文件bind。停态备份后保持原inode写入Caddyfile，在原容器内受控reload；不得以rename换宿主文件后误以为容器读到了新内容。准备及取消均不改运行配置或live文件。切换失败保留stage、原备份、候选及实际停态，不自动整库回灌。

## 验证与边界

只补实际改变的配置、Caddy及维护边界。根/v8/冲突等受支持输入核真实固定CPA加载与Settings GET；Caddy核实际Linux adapt/validate/reload、路由顺序、原完整ID/inode/证书卷、原模型和Manager路径、下载GET/HEAD及拒绝上传/state。图片若无受支持的合成seed入口，保留具体缺证，不增加产品调试接口。

未变产品镜像、文本/工具/原生图片、120秒确认、secret来源和其他旧专项按输入等价复用。旧失败不改写为成功。Codex正向2FA及生产外部HTTPS取图结果分别待验，不自动使用旧凭据或新增官方请求。

没有SQL/schema迁移或运行时DELETE。未来正式apply包含停三应用、新停态备份、协议与state配置写入、Caddyfile写入及原容器reload；最终一次维护确认应明确这些动作。生产基线及域名/Base URL/API Key保持，切后新绑定原位保留。当前`write_release_ready=false`，本执行包不交生产写入命令。
