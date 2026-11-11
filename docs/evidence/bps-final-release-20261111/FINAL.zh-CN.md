# BPS 最终候选发布材料

2026-11-11。产品基线 `c2adc4ad92a2bf564bf8dfcbaa40aee78d8c9ff3`；native材料整合输入 `db97e37f177a9223130d95035cda57ef831b112d`，其通过记录保留。最新OVH完整回执已核，现补根/v8有效配置兼容及HTTPS relay发布边界；沿用完整验收后统一发布。当前`local_release_materials_ready=false`、`quota_product_live_verified=true`、`write_release_ready=false`，待新工具及专项整合后再更新。

## 已核交付

| 项目 | 状态 | 证据 |
|---|---|---|
| BPS 官方账号用量页面 | 通过 | [页面验收](../bps-quota-manual-20261111/FINAL-RESULT.json)：1份BPS、1条服务端巡检、1条合格官方用量观察，账号卡与详情显示剩余99%及重置/观察时间；窗口已清理 |
| 最新三镜像分发包 | 通过 | [归档回执](bundle/README.zh-CN.md)：227177346字节；独立断网经典Linux实际load、平台/revision/双存储后端ID及定向清理均exit0 |
| 新发布工具 | 通过 | [工具回执](tools/README.zh-CN.md)：实际Linux入口、prepare、PTY取消、apply、正负鉴权、三文件链、原Caddy完整ID、停态备份与state权限、完成态拒重入及归属清理exit0 |
| 未变发布边界 | 复用 | 新旧32函数按文件名/标题归一等价；旧EOF/超时/漂移/secret来源与维护后失败证据继续适用，原失败记录保留 |
| 独立代码与安全审查 | 放行本地材料 | [审查记录](review/REVIEW.zh-CN.md)，无本轮新增待处理P1/P2 |
| 主线整合 | 通过 | [整合检查](INTEGRATION-CHECKS.json)：候选常量与manifest精确一致、artifact manifest字节一致、产品和旧工具输入未变、diff-check exit0，无新增schema/SQL/迁移 |
| Caddy只读原序摘要 | 专项通过 | [预检回执](preflight/README.zh-CN.md)：真实缓存Caddy adapt、顺序/嵌套/未知反例、秘密哨兵和LF原文here-doc语法exit0；既有认证/活动主逻辑AST等价复用 |

工具源提交 `3c9c63aaeb865c81631f37b6f3b373edaee694c5`，工具证据 `34bef9d4411c98667f4ea3a4beab0b9642e2df35`，归档证据 `5c89657f1e2102ce31a38d8450a9fb0852060ca8`。未重复产品构建、已通过的Linux生命周期或官方业务请求。

## 本地交付目录

`C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111`。

- `mx-cpamp-images-linux-amd64.tar.gz`：固定最新CPA、Manager与复用Worker；SHA256为 `256ddca4ec162bc1249ccea95ca1e36ce536b4909fa1ac89b77af1b477910c27`。
- `mx-cpamp-images.manifest.json`：精确ref、revision、linux/amd64及两种已实测完整ID。
- `bps-final-release.sh`、`bps-final-release.py`：独立状态目录 `/opt/cpa-stack/.mx-upgrade-bps-final-20261111`，第三Compose为 `compose.bps-final.yaml`。
- `README.zh-CN.md`：本地材料、剩余验收和现场接续口径。
- `phase2-readonly-preflight.sh`：有界原序Caddy摘要及既有内部鉴权/活动只读入口，单任务未知仍保留。

旧归档、旧prepared、完成回执及备份保留。本地交付目录不等于已上传Release或已部署OVH。

## 剩余验收与下一动作

1. **HTTPS图片中转**：用户已选双模式、默认原生。目前新发布工具只接受native，relay需要精确GET/HEAD路由至持图CPA、有效HTTPS origin及真实取图证据。先核实际Caddy路由骨架、匹配顺序与目标；不将原生图片成功写成relay成功。
2. **Codex正向2FA**：当前真实回执为 `security_check / needs_user_action / web_login`。按官方支持的交互路径定位和验收，不盲重试旧账号。BPS早期成功与最近需要人工交互的事实分别保留。
3. **生产在途与现场**：原18条OVH基线复用；新增采集仅补Caddy和活动摘要。单登录任务/Worker活动仍可能unknown。正式prepare、切前漂移、新停态备份及切后真实身份/鉴权由OVH现场回执确认。

当前责任人：主会话已核[26条OVH完整只读回执](server/README.zh-CN.md)，四服务未漂移、三鉴权通过，活动仍unknown。有效协议字段另有根/v8优先级兼容缺口，按[relay与发布配置执行包](../../execution/bps-relay-release-20261111.zh-CN.md)两线修复并补改变的Caddy/配置边界；旧native工具的通过记录保留，不扩大成双模式发布已完成。下一责任人：执行席、独审与主会话整合，用户暂无需重跑整轮盘点。完整验收通过后统一推送、分发与交付锁定完整提交的生产命令。

无SQL/schema迁移。未来apply会停止Worker、Manager、CPA，备份Manager数据、auth、KV、配置及必要secret，并写入新配置、wrapper及私有BPS状态目录；Caddy保持原容器。切换后新绑定原位保留，备份不含切后新增数据。本轮没有推送、Release上传或生产写入。
