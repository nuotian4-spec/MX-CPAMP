# BPS 图片中转发布增量审查

2026-11-11。结论：本轮发布工具、有效配置预检及离线 HTTPS 图片契约通过独立定向审查，无剩余新增 P1/P2；主会话绑定最终整合提交后可交付准备与切换材料。审查范围为有效配置解析、既有状态保护、精确下载路由、原 Caddy 文件与容器重载、候选配置回读及 HTTPS 契约。仅只读代码和脱敏证据；未运行测试、构建、容器、官方请求、网络命令或生产操作。本记录由主会话统一提交。

## 有效配置与只读预检

B 稳定提交 `b854d63aaf0cebe0aec51aaa68052ce916cfe6d8`，基线 `bab1afb07e3cf25e78394b6cb959aaa5999b31bd`。已读预检 diff、`contracts/verify.py`、实际 Go overlay 专项、各项日志及产品配置映射和状态路径实现。结论：本增量通过定向审查，无新增待处理 P1/P2。

- 根 `bps-protocol` 为兼容布局；`upstream.bps.protocol` 按字段存在性覆盖，缺字段继承，显式 false/null 保留其产品语义。canonical 父节点非映射时预检输出 unknown/null，对应真实 loader 拒绝，不用空字典吞掉。
- transport 空/null 使用 native；origin 空/null 覆盖根值。state 空/null/纯空白从有效 auth 同级派生；路径按实际容器 HOME、cwd、trim 与产品支持的波浪号形式归一。`oauth.auth-dir` 按存在性优先于根值，空/null 使用产品默认。
- 预检不回显 origin 或凭据，仅输出原有路径摘要、固定 transport 分类与 origin 是否配置。旧现场根读数 `relay_origin_configured=false` 和默认 state 路径不能外推为真实有效配置；需以本次合并规则重新解释或采集。

引用同提交证据，不重跑：12 例预检、12 例真实 Linux `ParseConfigBytes`/`BPSStateDirectory` 专项通过；固定最终 CPA 镜像分别以 root/v8/mixed/null leaves/trailing slash 启动，五次 `GET /v8/management/bps/settings` 均 200，白名单字段相符。实际 HOME `/root`、cwd `/CLIProxyAPI`，镜像完整 ID 与 revision 对应最终固定候选；产品源码相对该 revision 等价。Settings DTO 不返回 state-dir，路径结论只来自真实 loader 专项。

`preflight.log`、`candidate-settings.log`、`syntax.log` 为 exit0；`loader.log` 包含 12 例 PASS 与包级成功。预检移除新增纯函数并还原 BPS 取值片段后，完整其他 Python AST 与已审基线一致，原认证来源、固定 GET、活动 unknown 判定、Caddy 原序摘要和零写入路径复用。候选 fixture 为 network none，无模型或生产请求，临时容器清理确认；交付本体和用户外层 here-doc 的 Linux bash-n 均通过。

路径追加证据 `28275d11fd6e91fb3d7ddfad922f8dcf74006d26` 仅修改专项输入和记录，未改预检函数。`--path-only` 对 `~`、`~/`、`~\` 三种形式穿过实际 Linux loader、`BPSStateDirectory` 与预检，`path.log`/`path-loader.log` 均通过且清理确认；未重跑 Settings 或 HTTPS。

## 发布工具及下载路由

A 源码为 `5cc747e79a580e1a0b2b4748553427010cdfc53d`，状态路径增量 `0a98a2bc8f259c1c73bfcc3a850f2f0ef145492e`，最终证据 `7146032e667db702823835d81d5c39b2aaf72474`，文档收口 `aa44f7b1bbd5e0883d874591078a674e23214d08`。已读完整受影响函数、diff、实际控制器与 invoke、成功/重载失败日志、纯契约和输入等价记录，结论通过。

`effective_protocol` 与产品及 B 专项一致。候选仅规范化 transport、固定 HTTPS origin 与独立 state-dir，清除这三项 legacy 重复叶，其他配置保留；默认 native，已有有效传输模式保留。原状态目录在 prepare/drift 中真实检查，存在或 unknown 均早拒绝；新目录安装前再次确认不存在，防止新空挂载遮蔽旧状态。`resolve_state` 支持产品明示的三种 HOME 形式，相对路径按实际 cwd 解析，HOME 仍须为已核 `/root`。

Caddy 源检查同时核宿主与容器 bind 正文、原 inode、真实 adapt 及 admin 运行配置。候选只接受唯一既有 CPA HTTPS 站点；原站点反向代理目标必须属于既有 CPA 地址。去掉新增 route 后，完整适配语义与旧配置一致，包括其他站点、模型路由顺序、分组、TLS 和端口；原容器 validate 后只暂存，prepare/取消不写 live 或 reload。新状态目录 `.mx-upgrade-bps-relay-20261111` 与旧 prepared 分开。

新增路由契约为 GET/HEAD `/api/bps-images/*` 转发至持图 CPA。根路径及嵌套无效 token 可以由 CPA 返回 404；POST 等方法不进入新增下载 handler；state 和管理路径保持原站点兜底。此范围不要求重复实现 handler 内的 token/目录校验。

apply 保留一次 DEPLOY、原受保护来源与密钥检查、即时漂移、三应用停态备份及失败停态。发布 Caddy 正文使用原文件 `r+b` 写入、truncate、flush/fsync，保持 bind inode；随后核容器新字节、原容器 reload 和 admin 候选生效。原 ID 与证书卷保持，恢复文档区分磁盘与运行配置，原正文来自本次私有备份，不自动回灌业务或重建 Caddy。

引用 `tools/run.log`：真实 Linux prepare→PTY 取消→apply，三最终镜像完整身份、三文件 Compose 链、挂载、Manager 转发固定 CPA 的 settings、正负鉴权、原 Caddy ID/inode/证书卷、原 bind 新字节、运行候选、受信内部 HTTPS 以及 Linux wrapper 均成立；complete 再入拒绝，exit0/清理 verified。引用 `caddy-reload-failure.log`：仅控制器在 drift 通过后注入本轮 staged 非法正文，交付原 reload 真实失败，stage=`reloading_caddy`，磁盘候选而运行仍旧、原 Caddy 运行、原备份正文可读、maintenance 保留且再入拒绝；预期产品命令非零，专项控制器 exit0/清理 verified。

最终工具纯配置 10 例、路径 4 例和错上游拒绝 exit0。两轮生命周期输入为 5cc747；0a98 只增加状态路径展开，现场实际旧路径 `/root/bps-state` 的输出及后续命令参数前后相同，复用两轮证据，无需重跑。旧超时/无 TTY/EOF、secret/来源及维护后备份失败按未变函数复用；产品、三镜像和原 shell 入口未变，不追加产品全量门禁。

## 原图片宿主与受信 HTTPS

B 独立证据提交 `502afd3f98ee0da04182e65c37ce9e991956e808`。已读 `contracts/https` 的 Go fixture、客户端、控制器与 `fixture.log`。实际未改 `NewImageRelay`/公共 `Rewrite` 创建随机能力和私有 PNG，再经原 `ServeHTTP`、真实缓存 Caddy 与受信 CA/hostname TLS 下载；没有产品 seed 接口或固定 token。

有效 GET200 字节等于原 PNG，HEAD200 空体且 Content-Length 相等；private/no-store、nosniff 保留。未知单段与目录/嵌套无效 token 返回404；真实 relay 请求计数对应两次有效请求、两次未知及一次目录请求，POST/DELETE/OPTIONS、state、管理路径未转发。fixture 进程退出0，唯一容器、内部网络、临时源、能力文件和 CA 清理确认。编译断网、运行仅内部 DNS alias，无外部域名连接、真实账号、官方或生产请求，产品源码与固定 CPA 输入等价。

本项关闭本轮离线正向 HTTPS 契约。过期追加证据 `e8fea8592495ffe240f9e4298a88fe53cd0e0bf3` 的 `expiry.log` 为缓存 Linux、断网条件下独立命名用例 exit0，源码与最终固定 CPA 等价。已读该用例：先创建真实图片能力，再将该 entry 设为过去的失效时间，GET/HEAD 返回404、另一独立宿主仍200，过期存储释放；不是用未知 token 冒充过期。没有等待真实30分钟，不扩写为整包测试或再次 HTTPS 验收。

## 剩余验收边界

正向有效图片的隔离 HTTPS 契约已通过。原 Caddy TLS、实际 ImageRelay 库和配置回读与最终生产二进制公网抓图分层记录；公网证书链、外部有效链接 GET/HEAD 及官方图片业务属于新候选切换后的现场验收，不要求旧 rc1 提前证明尚未部署的能力。

主会话转达用户本轮明确确认“2fa前面我已经测试过了，没问题”，按既有真实验收确认收口，`provenance=user_confirmation`；相关 Worker/登录产品输入未变，不补造新登录日志或要求重复窗口。既有官方交互 `security_check` 记录作为历史事实保留，不再外推为所有当前登录均失败。生产登录/模型活动 unknown 仍不写成0；原 DEPLOY 明示停三应用可能中断在途请求，现场排空或接受该提示由实际切换回执记录。

没有 SQL/schema 迁移或本轮计划中的运行时 DELETE；正式 apply 的停服务、备份、配置/state 挂载与 Caddy 原 inode 写入仍属发布动作。生产原配置/admin 形态、漂移和真实来源由 prepare/drift 原位核验，OVH 是否完成只以真实 prepare/apply 和切后验收回执为准。

当前责任人：主会话整合 A/B 和本审查、固定完整提交并交付可执行材料；下一责任人：用户运行现场 prepare，主会话核实际回执后沿一次 DEPLOY 切换与切后验收。部署未执行，不能写成生产完成；本地材料的准备交付不再由切后验收项反向阻塞。
