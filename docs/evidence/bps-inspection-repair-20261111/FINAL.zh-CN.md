# BPS 模型配置与只读巡检本地验收

日期：2026-11-11。分支：`codex/openai-bps-login`。实施基线：`5d431b9be0828f94576d1807e1cc87b822cc1350`。本地候选完成，专项、独立代码/安全审查、最终镜像构建及断网组合验收通过。

## 交付行为

- BPS设置支持选择凭证、查询官方允许模型、填入草稿、人工保存或清空全局发布清单；读取和一键选用不发布。保存只修改`oauth.providers.bps.models`，保留其他YAML字段及逐凭证禁用规则。
- 账号页区分全局发布数、官方允许数和本站注册数；未查询显示权限未确认，已发布但未获允许显示权限不匹配。缺少BPS接口的旧CPA仍可读取普通账号列表。
- 浏览器及服务端巡检支持BPS单选/混选，已有目标配置保持。BPS只查询权限并记录，保持`keep`、未知额度及禁止自动恢复，不进入Codex删除、启停、补登录分支。
- CPA使用精确BPS凭证和有效出口，固定官方access GET、5秒管理探测预算，服从父取消；不刷新、推理或保存凭证。查询期间身份、令牌或出口变化时丢弃结果。

## 产品与证据输入

| 内容 | 源提交 | 主线整合提交 |
|---|---|---|
| CPA access接口、解析与公开handler专项 | `8b4fe6e83637535b7ae4e54c3e42f4bdf602e5a0` | `69fd9e60c31310111c32d4556b1b92f9d6e6f1f5` |
| CPA专项证据 | `5e55bb7d21f4db3e797e94ef68eac5d5e454cb42` | `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b` |
| Manager/Web巡检 | `affae3c1ca5f8284ebd9a15f39546d0625a70645` | `472e43884db8669936eecf6f3d05763229c1081a` |
| 模型发现发布与权限展示 | `c357ed531dae8819011304f5ca0b98c8e70b3e26` | `a21eb2512d1d71c145b337ffe7be027f9cd8435d` |
| 普通账号列表兼容 | `875dd976c036583dd525b1a10fda07af7b9a43b1` | `de1c4356cd80363c6b28384fdf3e3a704380ec26` |
| Manager/Web专项证据及检查范围 | `aee54af44d954df722f39673651f716c7d895f7b`、`91885ad176f0d9addd243fde31c9b4668fddbd43` | `a47589d5d659c87e91c7bae286883dae3eb976c3`、`3f2964f9b3efd1b032a0aba40e53f28ff61cb5d5` |
| 最终组合工具 | `0f8c42800ccf5e788b8335c6d4070aad80a4e25f`、`18c8edca53bfa979dbcf86172123097d438ff746` | `4969db246c28925785d0e1903fc5b239ddfc6efd`、`e6cc324e952a08d4fab8aa32c94f35c733fb08f2` |

主线CPA组件与CPA构建revision比较退出0；主线Manager组件与源最终产品`875dd976`比较退出0。后续仅证据、组合工具和文档变更，复用对应产物与未受影响专项。

## 验证结果

| 验证 | 结果与证据 |
|---|---|
| CPA公开管理handler | 断网Go专项五个顶层组通过，exit0；包含鉴权、固定GET/headers、取消/短截止、响应过滤、失败语义、身份及全局有效出口漂移、零刷新/推理/保存，见[cpa回执](cpa/README.zh-CN.md) |
| Manager/Web专项 | Web6文件53例及最终受影响3文件8例均exit0，后者与前者有重叠、不累加；Go五包定向exit0，其中cpa包无匹配用例，客户端由BFF及巡检用例覆盖；类型、受影响lint/format、gofmt及diff检查通过，见[专项回执](manager-web/RECEIPT.zh-CN.md) |
| 最终构建 | CPA与Manager各一次成功，exit0；Manager包含`tsc && vite build`及Go编译；Worker输入不变复用，见[镜像回执](build/README.zh-CN.md) |
| 最终断网组合 | `INTEGRATION.exit=0`，access、models、inspection及cleanup全部通过，见[原始结果](integration/INTEGRATION.jsonl) |
| 独立审查 | 代码与后端安全审查通过，核源/主线等价、专项复用、镜像和组合结果，见[独审记录](review/REVIEW.zh-CN.md) |

归档收口核验：代码及可编辑文档的diff-check退出0，本次暂存内容未命中非空敏感字段或密钥格式，组件输入等价比较退出0，无schema/migration/SQL路径变化。Manager原始Docker构建日志含11处输出空格行，原样保留。

组合使用最终完整镜像ID：

- CPA：`sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc`；revision `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b`。
- Manager：`sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429`；revision `de1c4356cd80363c6b28384fdf3e3a704380ec26`。
- 检查程序镜像：`sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758`，仅执行Node检查，不启动Worker/OAuth服务。

最终执行命令：

```powershell
& 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe' docs/evidence/bps-inspection-repair-20261111/integration/run.mjs --cpa-image mx-cpa:bps-inspection-repair-20261111 --cpa-id sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc --cpa-revision 06a2f9f121b06e8609c5f9d148213dc6ceeaea6b --manager-image mx-cpamp:bps-inspection-repair-20261111 --manager-id sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429 --manager-revision de1c4356cd80363c6b28384fdf3e3a704380ec26
```

组合实际核验：管理未鉴权401、错实例引用拒绝、缺token返回`credential_incomplete`且权限/模型/额度保持null；读取不发布、人工保存后注册、旧revision拒绝、清空保留凭证和无关YAML；BPS集合1、`keep`、未知额度、零自动动作、SQLite记录读回及凭证启停不变。巡检scheduler关闭；collector沿用原环境托管配置。全程network none、只读根、tmpfs状态、无日志持久化、无发布端口；最终归属清理确认，`synthetic_state_retained=false`。

首轮组合`ATTEMPT-1.exit=1`及原始回执保留：access/models通过，在工具写入巡检配置阶段失败；工具额外修改collector的环境托管字段，源码对应保护拒绝路径，旧回执未记录HTTP。工具移除此无关修改后，以相同产品镜像完成上述exit0；未把首轮改写成通过，未重建产品。

## 数据、范围与下一步

无新增SQL/schema/table迁移。巡检复用既有SQLite运行记录；管理员人工保存模型写现有CPA配置。此次组合的记录、配置和合成凭证全部在tmpfs并已清理，未访问生产数据。

`local_candidate_complete=true`；`upstream_access_verified=false`；`production_deployed=false`；`write_release_ready=false`。

官方access的当前在线响应和真实页面体验仍待定向验收。下一责任人：主会话准备绑定本次最终镜像的一次新手工BPS OAuth、一次只读access查询及人工模型保存/展示验收材料，再明确该次真实范围；不使用已清理的旧授权窗口或凭据。真实刷新/推理、Codex正向2FA与HTTPS中转各自保留原验收边界。本轮未推送、发布分发包或部署OVH；旧分发归档尚未重新绑定新镜像。
