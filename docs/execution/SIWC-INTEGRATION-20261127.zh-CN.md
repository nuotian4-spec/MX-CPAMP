# CPAMP SIWC 接入

日期：2026-11-27（Asia/Shanghai）。用户已授权实施。当前状态：SIWC本地接入完成，独立审查PASS，P9已放行本地候选。交付证据见[本地接入记录](../evidence/siwc-integration-20261127/RESULT.zh-CN.md)。

## 目标

在现有普通Codex账号详情增加SIWC状态与绑定/重新绑定操作，完成生成授权链接、提交完整回调、轮询结果、取消与完成后回读。复用CPA v8.1.7已有合同，保留账号provider、代理和外层凭证；不增加独立provider或解绑/启用等内核未提供操作。

界面沿用现有账号详情、公共按钮/输入框/Drawer或Modal，只增加局部详情内容。参考既定界面规范D:/MX-CPAMP/docs/design/UI-DESIGN-SYSTEM.zh-CN.md；不改变主导航、全局主题或凭证原有信息块。

## 分工与输入

| 责任人 | 工作树/范围 |
|---|---|
| siwc_investigation | 主候选cpa-core-v817-20261127，Manager model/service/controller、必要app/router接线与专项 |
| cpa_upgrade_baseline | 独立siwc-web-20261127，Web API/组件/详情slot/必要i18n与专项；冻结后按before字节核对精确合入主候选 |
| cpa_v817_review | 只读接口合同、增量和真实回执；审查不实施、不重跑同版验证 |
| P9 | 协调接口、风险边界与最终当前态 |

主候选：C:/Users/子羽/.codex/worktrees/cpa-core-v817-20261127/MX-CPAMP。HEAD为b9ea2dc4c6bb377d31143970a6df1084297e4b09，叠加已验CPA v8.1.7候选；CPA tree为8501bb9d2095cb9e7bc71b6bc008aa097b6670cc。本轮不改内核，其验证证据复用。

## 接口与状态约束

后端先固定窄范围SIWC桥接合同，前端按同一合同接线；不开放通用v8代理。读取v8非秘密摘要，缺字段按不支持。操作仅管理员可用，并验证connection_ref、真实凭证身份与资格；开始后state关联原连接和目标，旧请求不转发到新连接。

Manager同源根为`/api/siwc`，不经默认`/v0/management`前缀。统一身份字段为`connection_ref/auth_id/auth_index`。

| 接口 | 输入与结果 |
|---|---|
| GET `/credential` | query传`auth_id/auth_index`；返回connection_ref、同一身份、provider=codex、siwc_bind_supported、use_siwc、siwc_status(off/valid/invalid)、siwc_bound及可选siwc_expires_at |
| POST `/bind` | JSON传统一身份；返回wait、flow=siwc_bind、url、state、expires_in=300及统一身份 |
| POST `/callback` | JSON传统一身份、state、完整redirect_url；返回wait与submitted=true |
| GET `/status` | query传统一身份和state；返回wait/ok/error，ok附服务端回读的credential摘要，错误附code |
| DELETE `/session` | query传统一身份和state；保留cancelled布尔值，状态需依据同state的实际授权结果 |

所有接口要求管理员认证并返回`Cache-Control: no-store`；连接改变返回409，旧内核不支持返回501 `siwc_unsupported`。`cancelled=false`只代表此次未取消；只有同state授权状态确为ok，才允许回读并确认本次完成，已有摘要不证明本次绑定成功。

正常回调保留上游签发client_id和完整URL，拒绝回调允许只有error。回调接收不是成功，wait期间保持待完成；ok后回读目标摘要才确认绑定。取消、重复409、五分钟过期、CPA重启会话丢失与旧内核404分别呈现，404不自动退回普通Codex登录。账号/连接切换与卸载不采纳迟到结果，旧会话清理由其原归属处理。

Home、runtime、非文件来源保持只读，以内核siwc_bind_supported为最终资格依据。完整URL、code、state及令牌不进入持久化浏览器存储或诊断日志。回读只是本地状态，不发真实推理证明有效。

回调的确定输入拒绝允许修正后重新提交；接收结果不明时保留会话并查询状态。已建会话因CPA重启失效时显示会话失效，区别于旧内核缺少接口。过期后释放界面中的旧会话，允许生成新链接；等待或取消未确认期间不误报本次成功。

## 验收与交付

- 后端：真实Manager路由+合成CPA HTTP，覆盖管理员/连接/目标归属、摘要、正常与error回调、client_id完整传递、wait/ok与readback、重复/取消/过期、404兼容；一组只运行一次。
- Web：API合同、实际详情入口与组件生命周期、特殊来源、长值/错误/等待/取消、迟到回执；新增专项一次，失败只重跑影响子集。
- 最小联合：使用合成服务覆盖真实Manager→CPA绑定合同及Web API/页面消费，明确各模拟边界；不另重复已验内核套件。
- 最终统一构建：Web产物先生成，再嵌入Manager并构建，更新受影响源码映射。CPA未改不重建。1280/390明暗主题下核真实详情页面新增操作及状态。
- 执行与独立审查证据齐备后P9收口。同版通过测试不重复，源输入变更才触发相关验证。

本轮不做真实OAuth、模型/额度请求，不修改生产配置、数据库，不提交/推送/部署。会话沿用进程内短期管理，不新增数据库表或迁移。

证据目录：docs/evidence/siwc-integration-20261127/{backend,web,review}。后端16根/31叶子、Web20项、真实成品禁网链路9项通过；四种实际详情页面观察和唯一Web→embed→Manager构建通过。5125项来源映射保持CPA输入不变，仅更新20项CPAMP文件。测试与预览服务已停止。

当前责任人：P9已收口本地交付。下一责任人：P9承接后续固定提交与发布入口；本轮无提交、推送、生产部署或真实授权验收。
