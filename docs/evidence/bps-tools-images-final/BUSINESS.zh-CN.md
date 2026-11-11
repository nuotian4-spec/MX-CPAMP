# gpt-6-luna真实业务验收

日期：2026-11-11。产品及三镜像固定输入见[构建记录](BUILD.zh-CN.md)。执行使用用户已提供的最新专用材料，仅通过进程stdin传入；没有在宿主文件或环境变量保存密码、TOTP种子。

| 环节 | 实际结果 |
|---|---|
| 单行BPS批次 | succeeded，阶段cpa_import，保存与唯一注册通过 |
| 并发覆盖 | 设为3并保存 |
| 正常刷新 | 一次；同auth/profile/client/identity，access与refresh token轮换，单独并发保留 |
| gpt-6-luna文本 | 一次，HTTP200，completed，精确OK，文本门禁通过 |
| 工具首轮 | 一次，公开接口HTTP502；internal_server_error / server_error，类别unknown，没有取得合法ping调用 |
| 工具结果续轮 | 未发送；首轮未通过，不执行任何模型返回命令 |
| 原生图片输入 | 一次，HTTP200，completed，正确识别32×32测试图为红色 |
| 最终状态 | 模型请求共3次，工具往返未完成，整体exit1；stderr无输出 |
| 收尾 | auth_target_confirmed=true，admission_drained=true；cleanup=true，token_state_retained=false；输入及harness未漂移 |

[真实白名单输出](../phase2-tool-image-20261111/REAL-RECEIPT.jsonl)与[退出元数据](../phase2-tool-image-20261111/REAL-RECEIPT.meta.json)独立保存。实际请求符合已批准的最多四次预算；没有再次发起工具首轮或追加登录、刷新。

三个请求均使用隔离环境中唯一BPS凭证和 `gpt-6-luna`，没有跨账号或Codex回退。图片测试使用默认原生模式，不是HTTPS中转模式的真实验收。实际上游尝试次数未计量，保持unknown。

工具错误正文未保存，现有白名单仅保留公开502及稳定码，不能据此判断是上游HTTP拒绝、桥接解析或其他原因。进入离线定位，不重复真实请求，也不把两个成功项写成完整工具链通过。原 `gpt-5.6-sol` 的[HTTP403记录](../phase2-business-20261111/CORRECTED-RECEIPT.zh-CN.md)继续保留。

本轮没有生产连接、推送、部署或SQL/schema迁移。OVH现场出口、真实HTTPS中转、BPS手工OAuth及Codex正向2FA不在本次通过项内。

后续用户新增授权的[独立工具往返验收](../bps-tool-diagnostic-20261111/BUSINESS.zh-CN.md)已通过，首轮和续轮均HTTP200；该记录绑定更新后的诊断候选，不覆盖本轮失败或反推其原因。文本及原生图片没有重测。
