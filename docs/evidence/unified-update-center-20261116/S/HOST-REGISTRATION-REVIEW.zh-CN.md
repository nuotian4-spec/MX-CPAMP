# 宿主中断修复与首次登记审查

日期：2026-11-16 CST。候选：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`；基线 `9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`。本轮精确输入位于 `S/host-registration-input/`，采集时间及文件列表见 INPUT.json。行号以该快照为准。

**结论：本轮已读代码范围通过；S4-01 停态修复成立，登记同路径问题 S5-01 已修。未发现需要继续返修的代码阻断。首次安装专项尚待 A 最终回执，真实 systemd/Compose、浏览器与现场验收分别保留，不作整体交付放行。**

## 1. S4-01 中断停态修复通过

`deploy/mx-update-host.py:236-259` 在同 operation owner 锁内检查维护标记；维护后调用真实 halt 函数，成功/失败分别持久化 halt_confirmed，失败不释放 owner。维护前中断不停止原服务。`:78-101` 按固定 project、working_dir 和四服务名字识别实际容器，先停止 Caddy 与三应用，再 inspect 验证均未 Running，身份不符不误停其它项目。`:261-269` daemon 对账复用此逻辑。

`deploy/mx-update-operation@.service:7-15` 为独立 root oneshot，固定脚本和站点，运行 ID 通过实例参数传入；ExecStopPost 调用 interrupted，KillMode=control-group，未依赖 Manager/CPA 进程。任务 ID 在宿主入口受格式与持久 owner 校验。已核 P05 的维护后停态、停止失败保 owner 两类断言；该证据中 Docker/systemd 是 mock，未称真实系统信号停服验收。

## 2. 登记权限、路径与 S5-01 通过

`deploy/mx-update-host-install.py:49-71,82-111` 要求 Linux root、显式 --confirm-maintenance、固定站点文件，并在原 site_lock 中验证 root/0600 站点权限与确认前运行基线；拒绝跨站覆盖登记。只安装固定 MATERIALS/UNITS，脚本0700、单元0644，状态与运行目录0700。CPA须root访问0600锁与socket，未授予 Docker socket。

`overlay:36-46` 固定环境与本站锁/状态/Unix目录挂载；`:143-147` 对重建 CPA 的真实挂载来源核验。`mx-update-host.service:8-16` 固定root脚本、注册文件、0700 RuntimeDirectory，独立任务与daemon均无公网监听。

S5-01 已修：公开 --site 输入可以直接使用 `/etc/mx-update/site.json`。`mx-update-host-install.py:125-130` 当前通过 `if registry != site_path` 保留该文件；仅其它来源执行重建链接。同路径不再删除配置并生成自引用。修复已读入精确快照，相关安装专项由 A 补证，不新增并行门禁。

## 3. 完整链、活动指针与恢复通过代码审查

`mx-update-host-install.py:64-77` 从三应用真实 Compose labels 获取相同完整链，校验原 base 前缀，将桥接层插在原 base 后、既有尾部覆盖层前，不丢原覆盖层。`:84-90` 备份站点、CPA配置、活动指针及完整原/新链。`:121-163` 停止原四服务，更新 site/base 与活动 pointer 的 compose_files，再按完整链同镜像重建三应用，恢复原 Caddy；最后核镜像、Compose链、Caddy身份、挂载与入口健康后才清 owner。

`:165-173` 维护失败停止登记四服务并保留 recovery owner；`:176-201` 恢复确认要求该备份/站点/owner 匹配，旧指针、原四服务镜像及完整链恢复且健康符合后记 failed/recovered，不冒称更新成功。未自动回灌数据库，恢复命令不代替人工定向恢复原材料。

## 4. 一次确认、固定目标与幂等沿用已核契约

网页确认只提交该 CPA 固定候选。宿主同候选返回既有 operation，不再次启动；维护确认比对本次固定发行和四容器 ID，之后不再要求交互 stdin。安装登记 CLI 的 --site 与 --confirm-maintenance 仅授权首次固定站点维护，锁内重核同一基线；`:72-73` 拒绝重复桥接材料，避免重复插链/重启，不把重复安装冒充成功无操作。core 与登记均在共享站点锁/持久 owner 下推进。

## 5. 执行证据与待验边界

实读 A/P02-PLUGIN-HTTP-TEST.log 全部通过；A/P04-V8-SYNTHETIC-ROUTER.log 三鉴权分支、门禁、真实 C ABI 合成库加载、同版写前拒绝、失败恢复与保留停用通过。P04 使用正式 v8 Router 和真实 pluginhost 加载合成库，不是安装器 stub。

实读 A/P05-HOST-UNITTEST.zh-CN.md 与 P05-HOST-TEST-OUTPUT.txt：29项 OK，host_unittest_exit=0。真实部分为文件锁、持久 JSON、工作线程、Unix HTTP；Docker/systemctl/curl/发行获取和 run_cycle 阶段回调为合成。S 未复跑上述专项。

仍需后续证据：A 的首次安装脚本专项最终回执；真实独立进程/systemd单位启动、中断与daemon重启接续；完整 Compose 部署链和挂载实际接通；浏览器选择CPA、一次确认与重连后任务追踪。生产站点与真实账号/许可/模型业务验收另列，不据本报告执行。

数据库动作：无 schema/SQL/migration，恢复不回灌数据库。持久化包含站点登记、systemd文件、宿主脚本、Compose覆盖层、active pointer、操作owner及配置/旧链备份。S只写证据与快照，无产品修改、联网、真实凭据读取或部署。当前执行责任人 A，阶段验收责任人 P9；S 后续只核新增差异。
