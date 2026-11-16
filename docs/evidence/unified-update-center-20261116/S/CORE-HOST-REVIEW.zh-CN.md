# 核心更新宿主与同版覆盖增量审查

2026-11-16 CST，S 档。基线 `9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`；候选 `C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`。产品输入实存于本目录 `core-host-input/`，采集时间与来源见 `INPUT.json`。下列行号对应该快照。

**结论：S3-01 写前阻断代码通过；新增 S4-01 宿主中断停态缺陷待修。其余已读管理授权、固定目标、参数与持久所有者代码无新增阻断。安装登记/systemd 接线仍在实施，正式 Router/合成库日志未通过，不作整体放行。**

## 1. 管理授权与固定站点：已读代码通过

`components/cpa/internal/api/server_management_v8.go:17-20,111-113` 将 core 路由纳入现有管理鉴权；`handlers/management/handler.go:354-375` 在处理前验证管理 key 与远程管理配置。`components/cpamp/apps/web/src/services/api/pluginImports.ts:9-24,48-62` 向当前选定 CPA 的管理 API 发请求，`features/system/SystemPage.tsx:65-87,447-451` 连接变化重建请求/视图作用域。

`core_updates.go:18-47,63-78` 只从 CPA 部署环境取得绝对 Unix socket 与 site ID；网页输入仅 release_id、固定候选 ID 与本次确认，没有站点路径、socket、shell 或容器命令入口。管理密钥不转发宿主。`deploy/mx-update-host.py:327-362,384-392` 仅 Unix socket、0600、精确动作/字段与 site ID 校验，公开回执白名单 `:236-239` 不回传配置或认证材料。

## 2. 固定候选、重复请求与共享所有者：代码通过，部署接线待验

`mx-update-host.py:154-204` 预览存固定 release 与四服务基线，安装重核基线，同候选使用同 operation ID，重复安装返回既有操作。`:241-282` 独立 worker 再核基线，下载结果须等于确认 release，四服务 ID 与确认一致后复用既有 `run_cycle`；最终核活动指针、三服务镜像及原 Caddy 身份后才 completed。

`deploy/mx-update.py:95-114` 与 `components/cpa/internal/updatestate/lock_unix.go:10-11` 同用非阻塞 flock；Python 持久 active owner 阻止另起操作，仅同 owner 续接，Go `store.go:110-152,174-212` 发布终态后才释放或保留 recovery owner。core 队列先持久化再释放锁并启动 systemd，Manager 请求结束不拥有 worker 生命周期。Go 环境锁路径/状态目录必须由登记实际映射同一宿主文件；当前安装登记及 systemd 单元尚未落盘，A 已明确仍在写，不把上述代码逻辑当成真实跨进程/重启验收。

## 3. S4-01：中断回调未维持停态（P1，待修）

- 位置：`deploy/mx-update-host.py:210-224,226-234`；对照 `deploy/mx-update.py:299-315`。
- 可达条件：已进入维护，worker 在 starting/verifying 阶段被 SIGTERM/SIGKILL 或 systemd 超时终止，普通 Python 异常处理的 halt 不执行；已启动的 Docker 容器不会因 updater 进程被终止自动停止。
- 当前结果：interrupted 只写 recovery_required，未停止或核验四服务停态，因此现场可能保留部分运行的服务。持久 owner 阻止重复操作，但不等于失败停态保全。
- 最小修正：在同 owner 锁内，存在维护标记时依据登记 project/working_dir 与本轮固定身份核现场、停止本轮四服务并保存实际 halt 回执；停止失败仍保留 recovery owner。维护前中断不停止原服务，不自动恢复数据库。
- 已直接通知 A 并抄 P9。A 确认问题成立并在修；本报告快照尚未包含修复，不预先放行。

恢复现有部分 `mx-update-host.py:294-324` 重核旧指针/镜像/Compose 链，或本轮 completed 记录/commit/新镜像/原 Caddy，再核入口健康后收口，不以任意运行容器或仅版本号当恢复完成；未见自动数据库回灌。

## 4. S3-01：活动同版同路径覆盖写前阻断通过

`handlers/management/plugin_update_backup.go:45-48` 对原活动路径直接返回 ErrLoadedPluginLocked；ZIP `plugin_imports.go:366-369` 与商店 `plugin_store.go:277-281` 共用该 BeforeWrite。安装器 `internal/pluginstore/install.go:274-300` 相同字节提前 skip；不同字节先回调、被拒后尚未 writeFileAtomic。旧工件不改，不改版本/provider 绕缓存。该项为实际代码闭环，正式宿主断言仍以成功专项回执为准。原 S2 五项保持已收口，不重新审计。

## 5. 已有执行证据与下一可验面

本次未重跑测试。读取 `A/P04-V8-SYNTHETIC-ROUTER.log`：管理鉴权/共享门禁组因测试使用不存在的 PUT enabled 路由失败；真实合成库激活组失败；保留停用组 PASS；整体 FAIL。A 已确认修路由方法与 C ABI 响应夹具，当前日志不作通过证据。P9/UX 的前端既有专项保持原引用，本次不重复运行。

下一输入：S4-01 修复、安装登记/systemd 文件与现有最终专项日志。复核只接新增差异，区分函数测试、实际 Unix socket/共享 flock、真实 systemd/Compose 三层证据。官方发现版本仅展示，不等于本站已适配发行；代码安装目标仍取本站固定发行源，许可检查未隐式调用真实模型。

数据库动作：未见 schema/SQL/migration 或自动 DB 回灌。持久化包括预览、操作记录、active owner、备份、部署活动指针及配置/插件库。S 仅写本报告与输入快照，无产品写入、联网、真实凭据读取、提交或部署。当前修复责任人 A，下一差异复核原 S，阶段口径由 P9 收口。
