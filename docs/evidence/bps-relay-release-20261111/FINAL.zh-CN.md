# BPS 双图片模式发布准备

2026-11-11。最终代码输入 `c0e2188141057531e71df0777241ef8ce0df566a`。产品与[固定三镜像](../bps-quota-repair-20261111/FINAL.zh-CN.md)输入等价，归档沿用227177346字节的已验版本，不重建或重新打包产品。

## 已完成

| 项目 | 结论 | 证据 |
|---|---|---|
| OVH现场 | 四服务未漂移，三项内部鉴权通过 | [26条回执](../bps-final-release-20261111/server/README.zh-CN.md)；活动接口404仍unknown，根布局读数不当成有效v8结论 |
| 配置有效值 | 根/v8逐字段优先、null/false/空值和HOME路径与产品一致 | [配置回执](contracts/README.zh-CN.md)，12例实际Linux loader、12例预检、固定CPA五种配置Settings GET200、三种HOME路径专项均通过 |
| 发布工具 | 保留默认native，预置原CPA域名的relay origin；原Caddy原inode写入和原容器reload | [工具回执](tools/README.zh-CN.md)，真实prepare/取消/apply、设置回读、路由/鉴权/原ID与证书卷、备份、重载失败停态及清理通过 |
| HTTPS有效图片 | 原ImageRelay与真实Caddy的受信CA/主机名契约通过 | [HTTPS回执](contracts/https/README.zh-CN.md)，GET原PNG、HEAD空体、未知/目录/方法/state/管理拒绝与清理exit0 |
| 图片过期 | 独立命名用例通过 | [expiry.log](contracts/https/expiry.log)，真实能力失效后GET/HEAD404、另一宿主仍有效及资源释放；未重跑全包 |
| 2FA | 沿用用户已完成的真实验收 | 用户本轮确认“2fa前面我已经测试过了，没问题”；`provenance=user_confirmation`，没有新增登录或补造机器回执 |
| 独立审查 | 无剩余新增P1/P2 | [REVIEW](review/REVIEW.zh-CN.md)，配置、状态遮蔽保护、Caddy与故障边界、图片及命名过期证据已核 |

源码来源：A `5cc747e79a580e1a0b2b4748553427010cdfc53d`、`0a98a2bc8f259c1c73bfcc3a850f2f0ef145492e`，确认文案 `015534cc9f1e9f45997a6cda0be4833395cb351d`；B预检 `b854d63aaf0cebe0aec51aaa68052ce916cfe6d8`。测试/文档追加及旧未变门禁复用关系见各专项，不累加交叠测试数量。

## 发布绑定

- 仓库：`nuotian4-spec/MX-CPAMP`，工作分支 `codex/openai-bps-login`，保留草稿PR1。
- 已发布标识：`mx-cpamp-20261111-rc2`，目标提交 `4fb5da07d5968ebcc8c52b5c5232506fb981e060`；[GitHub prerelease](https://github.com/nuotian4-spec/MX-CPAMP/releases/tag/mx-cpamp-20261111-rc2) 的脚本、manifest和227177346字节镜像归档已上传，资产摘要与本地文件一致。
- 资产目录：`C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111`。包括新 `bps-final-release.{sh,py}`、manifest及原已验三镜像压缩包。
- 新服务器状态：`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111`；不接续旧native或rc1 prepared。

`local_release_materials_ready=true`、`prepare_command_ready=true`、`two_factor_acceptance=user_confirmation`。生产旧prepare停在`prepare_site / candidate_merge_changed`，用户只读回执确认唯一差异为默认`create_host_path=true`被Compose省略，且ready/maintenance/complete均false。已审工具`602aee042d6ba161096c71c4a9e4cce57f996c87`精确覆盖该表示差异；[本次失败与接续](../bps-relay-compose-compat-20261111/SERVER-FAILURE.zh-CN.md)绑定新TOOLS、新r2状态与旧bundle。后续仅在新prepare成功后，由用户在真实TTY输入一次DEPLOY，工具再即时核漂移。

## 数据与现场验收

无SQL/schema迁移、运行时DELETE或旧库回灌。正式apply会停止Worker、Manager、CPA，创建私有停态备份，写协议/state配置、Compose和wrapper，并原inode写入Caddyfile、在原容器重载。原域名、Base URL、访问Key、既有数据和证书卷保持；切后新绑定须原位保留。失败保留maintenance、阶段、候选和备份，不自动重入。

公网有效图片GET/HEAD及实际官方模型抓图属于新候选切换后的现场验收；离线HTTPS专项不冒称生产业务已通过。登录/模型在途unknown由维护确认明确提示，不当成零。旧`security_check`作为历史结果保留，不覆盖用户此次验收确认，也不承诺所有账号无人值守。

当前责任人：主会话推送已审工具与证据，核固定完整提交的下载文件并交新版prepare入口。下一责任人：用户回传新prepare结果，主会话核证后交TTY apply；不改旧RC2资产、旧STATE或镜像归档。授权沿用既有仓库交付、镜像分发与生产切换范围，不另造重复批准。
