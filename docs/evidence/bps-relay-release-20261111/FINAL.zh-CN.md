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
- 当前服务器状态：`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2`，已complete；旧失败状态目录和bundle原位保留。

`production_release_complete=true`、`two_factor_acceptance=user_confirmation`。发布工具固定提交`602aee042d6ba161096c71c4a9e4cce57f996c87`，新TOOLS为`/opt/cpa-stack/mx-release-20261111-rc2-r2`。用户已完成r2 prepare，并在真实TTY输入一次DEPLOY完成apply，见[生产完整回执](../bps-relay-compose-compat-20261111/SERVER-COMPLETE.zh-CN.md)。切前漂移、候选身份/挂载、内部认证、Caddy重载生效及HTTPS路由检查均通过；不要重复prepare/apply。

## 数据与现场验收

无SQL/schema迁移、运行时DELETE或旧库回灌。本轮apply已停止Worker、Manager、CPA，创建私有停态备份，写协议/state配置、Compose和wrapper，并原inode写入Caddyfile、在原容器重载。原域名、Base URL、访问Key、既有数据和证书卷保持；新状态、绑定和备份原位保留。停态备份位置为`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2/backup`。

公网有效图片GET/HEAD及实际官方模型抓图属于新候选切换后的现场验收；离线HTTPS专项不冒称生产业务已通过。登录/模型在途unknown由维护确认明确提示，不当成零。旧`security_check`作为历史结果保留，不覆盖用户此次验收确认，也不承诺所有账号无人值守。

修复和发布材料已推送，固定提交两文件raw下载与已审Git blob逐字核对通过，生产已release_complete。当前责任人：用户回传最后的只读镜像身份与备份权限文本，并在原域名刷新面板；下一责任人：主会话核结果收口服务器发布。页面体验、公网有效图片取图和真实模型业务按实际回执分别记录；既有2FA用户验收继续复用。不改旧RC2资产、旧STATE或镜像归档。
