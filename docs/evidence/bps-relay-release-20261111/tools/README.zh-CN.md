# 双模式图片发布工具

2026-11-11。最终工具输入为 `0a98a2bc8f259c1c73bfcc3a850f2f0ef145492e`，生命周期与控制器输入为其前置提交 `5cc747e79a580e1a0b2b4748553427010cdfc53d`，基线 `bab1afb07e3cf25e78394b6cb959aaa5999b31bd`。最后增量仅对齐状态路径HOME展开，周期实际旧来源 `/root/bps-state` 的解析结果前后相同，原成功/失败证据复用。仅修改 `deploy/bps-final-release.py`，新增本目录控制器与证据；产品、三镜像及原Shell入口保持原输入。

## 发布行为

协议值按产品逐字段presence合并：根 `bps-protocol` 为兼容来源，`upstream.bps.protocol` 已存在的叶字段覆盖对应根字段，含false、空和null。canonical父节点非mapping拒绝；null叶按产品默认处理。候选仅规范化 `image-transport`、`image-relay-base-url`、`state-dir`，删除这三项旧重复叶，其他配置原样保留。默认native，已有有效native/relay保留，origin固定 `https://cpa.maixun.cyou`，state为 `/var/lib/cpa/bps-state`。固定origin及其单尾斜杠视作同origin；其他非空origin拒绝。

原有效状态来源继续在prepare和drift真实docker exec检查，存在或unknown均拒绝；状态路径按产品支持的 `~`、`~/`、`~\` 展开，HOME为已核的 `/root`、相对路径按实际进程cwd解析。宿主目标目录安装前再次检查不存在，随后创建0700目录。独立状态目录为 `.mx-upgrade-bps-relay-20261111`，不接续旧native prepared。

prepare原位读取Caddyfile、真实adapt与admin运行配置，要求二者一致、原inode及挂载可见字节一致。只接受唯一CPA HTTPS站点 `cpa.maixun.cyou`，其上游须为既有CPA地址；文本入口为该域名或带https/:443的单站点头，其他形态在维护前拒绝。新增下载handle仅匹配GET/HEAD与 `/api/bps-images/*`，目标 `cli-proxy-api:8317`。移除新增route后，完整适配配置须与原语义一致，包含原Manager、模型路由顺序、分组、TLS和端口；再由原Caddy镜像validate。prepare只暂存，取消不写live或reload。

apply继续一次DEPLOY、切换前漂移核验、三应用停态备份和候选启动。随后保持原Caddyfile inode写入，核容器看到新字节，在原容器reload，并核admin确实加载候选。阶段分别为 `publishing_caddy`、`reloading_caddy`、`caddy_runtime_health`、`authenticated_health`、`caddy_https_routes`，失败保留maintenance，不自动回灌或重建Caddy。

原备份已包含Caddyfile。新增私有 `RECOVERY-CADDY.txt` 说明磁盘/运行配置分层、原正文来源及受控原inode恢复；证书卷不覆盖。reload成功不代表外部TLS或有效图片取图成功。

## 真实验证

| 专项 | 输入与结果 | 证据 |
| --- | --- | --- |
| 配置纯契约10例、路径HOME/cwd四例、候选不改源、站点上游错向拒绝 | 最终工具0a98a2b，exit0 | [contract-checks.log](contract-checks.log) |
| prepare→PTY取消→apply，最终三镜像 | 生命周期输入5cc747e，exit0，清理verified | [run.log](run.log) |
| 实际Caddy validate/reload、原ID/inode/证书卷、挂载字节、admin配置 | 同上，通过 | [run.log](run.log) |
| 最终CPA实际设置回读、受信内部HTTPS下的模型鉴权与下载/保护路由 | 同上，通过 | [run.log](run.log) |
| 真实reload失败、盘候选/运行旧、原Caddy运行、原备份可读、maintenance与拒重入 | 预期原发布命令非零，控制器exit0，清理verified | [caddy-reload-failure.log](caddy-reload-failure.log) |
| 实际Linux loader12例及固定CPA五次启动/Settings GET200 | 复用B独立输入，state不在Settings DTO内 | [配置契约](../contracts/README.zh-CN.md) |
| `~`、`~/private-state`、`~\bps-state` 的实际Linux loader与预检对照 | B专项28275d1，三例exit0 | [实际路径loader](../contracts/path-loader.log)、[路径对照](../contracts/path.log) |

成功现场包含根/v8混合配置：canonical null覆盖根旧relay/state，显式false保留；最终Manager GET真实转发至固定CPA，核native、固定origin及两项开关。状态有效路径的实际loader证据采用B专项，不将Settings回读冒称state验证。

控制器仅替ROOT/PROJECT/STATE/BASELINE事实常量，调用交付原main与实际Docker/Compose。采用独立Linux命名卷、唯一项目、内部网络、合成密钥/数据库和缓存Caddy。runner断网，仅runner挂Docker socket，应用不挂。TLS使用本轮Caddy内部CA，Worker仅在fixture信任该CA，生产程序保留系统信任与主机名校验。

内部受信TLS探针实际穿过Caddy：`/v1/models` 正确凭据200、错误凭据401；未知图片token的GET/HEAD为404，HEAD无正文；POST、管理路径和 `/state` 为404。目录根或嵌套无效路径允许到原handler后404，不增加token正则或公开目录映射。本专项没有生成有效图片能力；B已用相同matcher取得[实际库+Caddy的正向图片证据](../contracts/https/README.zh-CN.md)，输入 `502afd3f98ee0da04182e65c37ce9e991956e808`，二者边界分别保留。

故障fixture在原drift已通过、输出stopping_apps后，仅将本轮staged Caddy候选改为合成非法正文，使交付原reload真实失败。核stage=reloading_caddy、原inode、磁盘新而admin仍旧、Caddy原ID运行、三个候选应用未自动恢复、备份中原Caddyfile可读、再次apply拒绝。该注入不替换发布函数、Docker或Caddy。

工作目录 `D:/MX-CPAMP-worktrees/bps-access-inspection`：

```powershell
& './docs/evidence/bps-relay-release-20261111/tools/run.ps1' -BundleRoot 'C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111'
& './docs/evidence/bps-relay-release-20261111/tools/run.ps1' -BundleRoot 'C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111' -CaddyReloadFailure
```

runner固定 `sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48`；三候选与归档manifest沿用已验最终包，工具逐项核身份并真实load。两条日志均有 `fixture_cleanup=verified` 和 `command_exit=0`。

## 复用与剩余边界

[input-equivalence.json](input-equivalence.json) 逐函数AST核验27个原函数保持一致。原完整120秒/无TTY/EOF、维护后备份失败保留、秘密及配置来源专项按[既有回执](../../bps-final-release-20261111/tools/README.zh-CN.md) 复用；旧矩阵各断言与原整轮exit1口径保留，不改称全量通过。产品输入未变，未重建镜像或重跑产品全量门禁。

本轮只核本机合成现场。生产原Caddyfile文本形态/admin配置尚须prepare原位匹配；公网证书链/真实外部HTTPS取图及正向图片业务仍未由本专项证明。Codex正向2FA由用户确认已通过，本轮不再重测。生产活动unknown保持原TTY停机提示，`write_release_ready=false`。没有真实登录、刷新、模型调用、生产请求、数据库迁移、push或部署。

下一责任人：C独审本工具与本轮证据；主会话整合B配置/图片证据及完整剩余业务边界。
