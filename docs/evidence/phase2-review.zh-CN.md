# 二期独立审查

日期：2026-11-11。当前整合产品提交：`4367adaf687ce336e81122fcdb530c0e66e6eb33`。范围：BPS授权交互、内存批次、凭证并发及其管理界面。独立只读审查与修复后隔离集成均通过；真实业务和生产状态另按对应回执判断。

| 范围 | 原提交 | 主线对应提交 | 结论 |
|---|---|---|---|
| 登录诊断 | `a6731e26d574aa9356e05b2f1cbb8b90c4d3783e` | `a5eaebbb777152ce71900af447289e61e8088316` | 阶段、原因与秘密白名单通过 |
| CPA并发核心 | `ee9a2665d296029874996fff0489eaa3a81c7fbf` | `caa493b` | 准入、等待、全部执行生命周期、配置映射通过 |
| 批次及OAuth交互 | `220a1d569f5b9eca060f71f9f29aeeb8f82873ef` | `fcd6eba` | 摘要恢复问题由后续修复关闭 |
| 并发BFF与页面 | `1542120c6c77da8b689c9495cb5f9631938cc47e` | `e876dc1` | 实例绑定问题由后续修复关闭 |
| 批次摘要恢复 | `bfc414ba160727cbc4f31cf06c73cd6d3b2c5ced` | `9b9c324` | 当前连接及profile的保留摘要、终态后返回页面通过 |
| 并发连接引用 | `8da013158ede9837ccf05ebaa3003845d327749b` | `95dd9ca` | 旧快照切实例后零写、正常引用稳定、秘密隔离通过 |
| 批次成功引用 | `dac8e78daee09eb549245f2884516e90da126f2f` | `bb14b3a` | BatchTask白名单、既有auth_index唯一映射、已保存待核提示通过 |
| BPS能力错误 | `2ed27c7702e4a58cffc28e8a668273b1267ebffc` | `4bc4207` | 仅能力过滤空池400；空候选及混合通道原语义保持，通过 |

CPA核查覆盖HTTP、Count、管理验证、credits fallback、SSE真实源退出，以及WS首turn移交、后续create异步准入、steering与自动successor、required-input、终态及断线收尾。默认20、累计30秒等待、身份替换隔离旧lease、刷新和补登录保留设置、Home权威、8类配置凭证均已审查。

Manager核查覆盖串行及Worker清理确认、取消与导入竞态、30分钟未启动材料清理、未知结果停止、当前实例绑定；配置Key由唯一auth_index映射并携带ETag/If-Match，浏览器不接收原始配置Key。页面恢复摘要不创建新授权；回调ACK不显示为保存成功。

复用固定产品输入的[CPA专项](phase2-cpa/README.md)、[B线批次](phase2-login/batches.zh-CN.md)、[并发管理](phase2-login/concurrency-ui.zh-CN.md)、[摘要恢复](phase2-login/latest-summary.zh-CN.md)及[连接引用](phase2-login/connection-binding.zh-CN.md)。已记录的聚合失败与定向修复分开保留，没有机械重复全量测试。各独审范围diff-check通过；整合后按组件比较与已审输入一致，整体diff-check通过。

隔离集成首次发现CPA实际AuthID为含邮箱的文件名，与批次不返回原邮箱的契约冲突，保留[首次回执](phase2-integration/final-candidate-20261111.zh-CN.md)。修复限定为批次使用既有auth_index返回成功引用，内部继续用真实ID操作CPA；既有单任务和并发BFF边界保持原样。定向独审确认所有批次入口均经白名单DTO，真实CPA查询接口支持精确映射，索引不明时停止并提示已保存，不重放。修复后[三组件集成](phase2-integration/final-candidate-r2-20261111.zh-CN.md)退出0，全部批次与并发断言通过，清理已确认。

验收工具 `02a3f24`、`c918565`、`efe4d49` 已独立审查。启动前登记待清理容器，命令回执丢失反例通过；逐组件revision严格核验；按auth_index及provider唯一定位内部真实ID。BPS真实工具预算仍为单批次、单目标正常刷新、一次公开请求；默认预检不读取材料，失败立即短路，未将fixture冒充业务成功。

真实BPS批次及正常刷新通过，首次模型请求HTTP500，材料及容器已清理。随后准确请求的离线诊断发现验收所带max_output_tokens属于既有BPS不支持字段；产品修复仅明确该请求能力错误，保留限制、混合调度与准入生命周期。定向独审复用6项公开处理器、12项候选边界和相关BPS专项；新CPA构建及无凭证预检通过。工具 `cc00385` 与 `4db8214` 的错误白名单及无max请求体也已独审、离线fixture通过。原真实错误类别仍未知；新的真实请求等待额外一次客户端预算。

没有SQL/schema迁移。后续运行时写入限于授权凭证、并发metadata及CPA配置。审查不替代最终镜像、隔离集成、真实刷新、模型、手工OAuth或生产验收；本轮未推送或部署。
