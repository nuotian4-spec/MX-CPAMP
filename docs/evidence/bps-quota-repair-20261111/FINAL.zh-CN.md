# BPS 官方账号用量本地候选

2026-11-11。本地候选通过专项、独立审查、最终镜像构建和断网组合。产品整合输入 `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c`，分支 `codex/openai-bps-login`。

## 产品行为

CPA 独立查询官方账号用量，保留原 access / 模型发现契约。Manager/Web 已接通账号列表、卡片、详情、主动刷新、监控概览、浏览器及服务端巡检、快照写查与恢复；显示“官方账号用量”、套餐、实际窗口、观察及重置时间。合法0与未知区分，reset-only保留未知百分比。用量失败不覆盖权限结论，BPS 巡检始终 keep，不自动删除、启停或重登。

身份隔离覆盖 CPA 平铺 BPS `account_id` 投影、Manager 记录、主动缓存、巡检归并、持久快照和页面缓存。同名文件、同索引换账号或只更换 accountSnapshot 均不复用旧额度。模型作用域为 `feature / bps_account_usage / complete=false`，不推断 BPS 独占或共享额度桶。

## 证据

| 项目 | 结果 | 依据 |
|---|---|---|
| 官方来源 | 已核 | [SOURCE-RUN](source/SOURCE-RUN.jsonl)，exit0；用户导入的唯一BPS，access/wham各GET1次，均200，来源核证时主窗口已用17%；登录/刷新/推理0 |
| CPA usage | 通过 | [CPA专项](cpa/RECEIPT.zh-CN.md)，公开管理接口、白名单、精确引用、代理/身份漂移、超时/取消、合法0/未知等，exit0 |
| CPA 列表身份 | 通过 | [身份回执](cpa/IDENTITY-PRODUCER.zh-CN.md)，实际平铺文件经v0/v8公开列表，精确身份、无令牌回显及零存储写入，exit0 |
| Manager/Web | 通过并复用未变项 | [专项回执](manager-web/RECEIPT.zh-CN.md)，Go6包、Web9文件295项及受影响追加专项；SQLite同名换身份反例、标签与观察时间、lint均通过，不重复累计交叠用例 |
| 最终构建 | 通过 | [构建回执](build/README.zh-CN.md)，CPA CGO编译、Manager的tsc/Vite/Go构建均exit0；Manager/Worker按输入等价复用 |
| 最终组合 | 通过 | [RUN-FINAL](integration/RUN-FINAL.jsonl)，exit0；固定镜像、鉴权401、实例409、缺token未知用量、模型人工保存、BPS巡检与精确身份快照写查、清理全部通过 |
| 独立审查 | 放行本地候选 | [REVIEW](review/REVIEW.zh-CN.md)，包括最终修复、公开入口和真实组合回执；未重复测试或真实请求 |
| 收口 | 通过 | [FINAL-CHECKS](FINAL-CHECKS.json)，diff检查、Manager/Worker输入等价、无schema/迁移/部署及依赖清单改动 |

首轮组合 `RUN.exit=1` 在快照身份断言停止，旧记录和清理回执保留；CPA 列表补齐BPS平铺身份后，新组合完整通过。首轮结果不改写为成功。

## 固定产物

| 组件 | 镜像 | 完整ID | revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-quota-identity-20261111` | `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b` | `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` |
| Manager | `mx-cpamp:bps-quota-repair-20261111` | `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd` | `d26cbf7595ecf41ba8cbfd90c956291ac351b471` |
| Worker，复用 | `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

无SQL/schema迁移。新增BPS观察沿用现有SQLite快照表，组合测试在临时tmpfs内写入合成记录并清理；生产数据库、配置及容器未改。旧完整登录窗口也已按归属清理，`source/WINDOW-CLEANUP.exit=0`，不保留令牌状态。

## 当前边界与下一责任人

`official_source_verified=true`、`local_candidate_ready=true`、`integration_passed=true`、`quota_product_live_verified=true`、`write_release_ready=false`。新版本机页面验收已完成：1份BPS、1条服务端巡检和1条合格官方用量观察，账号卡与详情显示“官方账号用量”、剩余99%、重置时间及巡检观察时间；窗口已清理。该结果仅覆盖本次账号页面与服务端巡检，不代表全部BPS账号或模型具备相同权益。

BPS 2FA的 `security_check / needs_user_action` 保持官方交互终态，不推断密码、TOTP、封号或出口原因；本轮没有新增登录或修改Worker。当前责任人：主会话完成本地收口；下一责任人：主会话准备固定新产物的页面验收与统一发布材料，剩余真实2FA/HTTPS中转按已有边界单独接续。旧分发包不适用于本候选，本轮无推送或OVH部署。
