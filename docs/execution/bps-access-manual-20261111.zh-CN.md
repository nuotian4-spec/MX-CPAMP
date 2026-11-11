# BPS 官方权限与模型页面验收入口包

日期：2026-11-11。本地候选基线：`2f339f8c6f71fba5161198898d27f5bfaabfe775`。前置结果见[本地验收](../evidence/bps-inspection-repair-20261111/FINAL.zh-CN.md)。用户要求“下一步”，本轮先准备一次新的手工验收入口，不复用已清理的旧窗口或令牌。

## 目标与范围

WHY：产品专项、独立审查、最终镜像与断网组合已通过，官方access当前响应和真实页面操作仍待验。WHAT：固定最终镜像的一次新手工BPS OAuth、系统设置中一次官方允许模型查询、人工保存全局模型清单及账号发布/注册展示核验。

操作顺序：登录临时本地面板，完成一次BPS手工OAuth；进入系统BPS模型设置，选择新凭证，点击一次“获取官方允许模型”；观察返回状态与目录，成功时填入草稿并人工保存；账号模型页仅查看发布/注册，不再点击权限查询。若查询失败，保留稳定错误和状态，不重试、不把未知结果保存成空清单。

本轮不启动浏览器或服务端巡检，不主动刷新、不调用模型，不启动Worker或操作OVH。完整模型目录只在页面内存；回执记录允许状态、目录数量、稳定错误、保存/注册数量和布尔，不记录邮箱、令牌、完整回调或目录正文。后台自动刷新未被硬禁时如实标注，不能把未监测写成零请求。

## 责任与文件域

| 负责人 | 文件域 | 交付 |
|---|---|---|
| 执行席 `bps_access_manual` | 隔离树`D:/MX-CPAMP-worktrees/bps-access-inspection`的新分支`codex/bps-access-manual`，仅新`docs/evidence/bps-access-manual-20261111/`，不含review目录 | 最小复用旧手工工具，固定新镜像的start/status/verify/stop、一次无账号断网预检和复制步骤 |
| 独审 `callback_compatibility` | 只读产品和新工具，仅主线新manual/review目录可写 | 核手工请求触发、镜像身份、localhost/tmpfs/清理边界、预检证据及准确操作范围 |
| 主会话P9 | 本入口包、整合与当前态 | 核证、整合并交付可复制用户步骤 |

HOW MUCH：复用已审手工入口和已存在最终镜像，约10分钟准备；只补新工具语法、相关断网启动边界及独审，不重建产品或重跑产品专项。DONE：完整提交、实际命令与退出码、固定镜像身份、无账号预检和清理回执齐备；真实授权由用户在官方页面完成，密码/TOTP与完整回调不进入聊天或仓库。

DON'T：不改产品、旧证据或生产配置；不读取旧凭据，不联网安装、不自动发OAuth/access/刷新/推理、不push/deploy。UI单次操作预算是本轮步骤约束，工具不冒称能硬性统计或拦截所有页面请求。

## 固定产物与运行边界

- CPA：`sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc`，revision `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b`。
- Manager：`sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429`，revision `de1c4356cd80363c6b28384fdf3e3a704380ec26`。

入口只发布本机127.0.0.1的Manager随机端口，CPA内部端口不发布；空tmpfs、根只读、Docker日志关闭、无Worker和旧挂载。900秒到期停进程，仍须stop按本轮归属清理容器与临时访问文件。7890代理仅用于本机，不写入服务器。

无SQL/schema迁移。未来人工保存清单仅写本轮隔离CPA配置，巡检历史仍未启动；不改变生产数据。

入口实现与证据源提交`6532e0152f47e1d98acd37092db04fcd6b4d15d5`已整合主线`2112c94`。最终语法、无账号断网预检和diff检查exit0，预检清理确认；主线脚本与源提交输入等价，复用该专项。操作见[手工入口](../evidence/bps-access-manual-20261111/README.zh-CN.md)，审查见[独审记录](../evidence/bps-access-manual-20261111/review/REVIEW.zh-CN.md)。

独审放行后主会话已启动新的本机空窗口，真实`MANUAL-START.exit=0`；启动时凭证0、发布模型0，未创建OAuth会话或查询官方access。地址`http://127.0.0.1:6722/management.html#/oauth`，有效期至`2026-11-11T08:50:46.268Z`（北京时间16:50:46）。非秘密现场指针：`C:/Users/子羽/.codex/artifacts/mx-bps-access-manual-b32b2eaf5650/state.json`；临时访问文件不入仓。

当前责任人：用户在新窗口完成一次官方手工授权、系统设置一次发现与模型保存展示；下一责任人：主会话根据用户回报填脱敏观察、执行本地verify并按归属stop清理。到期停进程不等于清理完成；如用户未完成，也须保留真实结果并清理，旧窗口不复用。真实官方权限结果待证，`write_release_ready=false`。

用户最新回报与只读核验：模型发现成功；本地保存/注册BPS为1，发布/注册模型为2，配置一致。用户巡检截图仍为`targetTypes=[codex]`，已读1但集合0，需在巡检配置独立选择BPS。下一步骤为用户保存BPS目标后手工一次只读巡检，主会话等待真实结果；完整页面观察尚未归档，当前verify为pending。详见[当前结果](../evidence/bps-access-manual-20261111/MANUAL-PROGRESS.zh-CN.md)。

最新接续：旧窗口到期后，按用户“重新启动”要求清理旧归属并创建新空窗口，两步exit0，见[重启回执](../evidence/bps-access-manual-20261111/MANUAL-RESTART-1.zh-CN.md)。新入口`127.0.0.1:13958`，截止`2026-11-11T09:08:56.055Z`；新state为`C:/Users/子羽/.codex/artifacts/mx-bps-access-manual-6afd1f5dfa6d/state.json`。用户重新授权后仅补一次BPS只读巡检，前次模型发现/保存证据复用；新窗口不重复该项。主会话待真实回报，随后按新归属清理。
