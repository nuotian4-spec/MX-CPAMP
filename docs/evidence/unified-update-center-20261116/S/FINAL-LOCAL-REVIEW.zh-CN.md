# 统一更新中心最终本地代码审查

2026-11-16 CST。候选 `C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`，基线 `9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`，未提交工作树；构建标签 `9dce11c-dirty-unified-update`。

**结论：当前增量代码与源码映射审查 PASS，S2 至 S5 已发现问题均闭环，未发现新增代码阻断。此结论不是发布、浏览器、真实 systemd/Compose 或现场业务放行。**

## 输入与证据对应

- `host-registration-input/INPUT.json` 中5个文件与当前内容完全相同；`core-host-input/INPUT.json` 中其余12个管理路由、安装器、锁与页面请求接线文件也无新增差异。复用此前审查，不重开旧项。
- `A/P07-BUILD-INPUT.json` 列出的4636个 component 文件与当前Git工作树文件集一致，逐文件Git blob匹配，缺漏及漂移均为0；`deploy/production-wiring.contract.json` 的 source_files 与该映射完全相同。34个变更路径是本次候选，不将旧发行HEAD本身冒充全部候选内容。
- 新增行为在管理请求、显式安装和宿主任务入口启动；既有CPA常规初始化不要求启动更新服务。未登记时更新相关写入明确拒绝，不能把这一运行时前提误写为普通管理写操作完全无变化。`normal_startup_preserved=true` 仅表示常规启动路径保留，不表示现场启动实测完成。

## P06 首次登记专项

实读 `A/P06-INSTALL-UNITTEST.zh-CN.md`、`A/P06-INSTALL-TEST-OUTPUT.txt` 和 `deploy/test_mx_update_host_install.py`，18项通过、exit0。真实执行安装/恢复控制流程、临时系统目录、文件备份、权限/链接、同inode flock以及单位文件解析；command、Docker现场、halt和健康边界被替换，脚本底层外部命令被测试拒绝。S5同登记路径保留普通文件用例通过。该证据覆盖安装文件/控制契约，不是实装systemd或重建真实Compose项目。

## P08 跨语言、跨进程联动

实读 `A/P08-CORE-JOINT.log`、`components/cpa/internal/api/server_update_integration_test.go:249-355` 与 `deploy/testdata/update-host-fixture.py`：正式v8管理Router完成鉴权、固定预览和确认；真实Unix HTTP交给产品UpdateHost；launcher创建独立Python worker并运行产品 `UpdateHost.run`、`mx-update.run_cycle` 和实际 kilo `Release` 生命周期。

`server_update_integration_test.go:306-318` 验证同文件锁持有期间Go插件写门禁409、另一个Python进程调用手工updater的site_lock拒入；`:319-327` 在同一Go测试进程重建管理facade，以原operation ID恢复查询；`:329-352` 释放门闩后检查completed、备份文件、独立worker PID和active owner清除。夹具调用实际生命周期生成临时备份tar、plan、completed和pointer；发行下载、Docker/健康、现场快照和systemd启动器由离线合成边界提供。

这证明本地产品控制路径与进程/文件锁联动，不等于真实Manager/CPA操作系统进程重启，也未证明systemd发信号后的真实Compose停态。P05的29项继续覆盖合成Docker边界下的中断停态与负回执，本轮未重复运行。

## 构建与验收边界

读取时 `A/P07-WEB-BUILD.log` 的tsc/Vite构建完成；`P07-CPA-MANAGER-BUILD.log` 明确 cpa_final_build_exit=0，Manager部分仍显示GOPROXY=off下缺少modernc.org/sqlite、golang.org/x/sys缓存，尚无成功回执。该构建状态由A继续收口，不认定为产品逻辑缺陷或整套构建通过。

下一责任人P9/A：补Manager最终构建输入一致性，使用隔离真实CPA/Manager及已构建web完成浏览器确认/切换CPA/重连追踪验收。真实systemd单位启停/中断/重启、真实Compose首次登记与更新、供应商插件/许可/模型及现场业务仍未验，须按原边界单列。官方版本发现不代表本站已适配；本轮未升级上游内核。

数据库动作：无schema/SQL/migration、无自动DB回灌；持久化包括站点/单位/脚本登记、Compose覆盖层、操作owner、备份、活动pointer及插件配置/库。S仅写证据，不改产品或A/P9文件；未联网、未读取真实凭据、未复跑通过项、未提交推送部署。

机器回执：`FINAL-LOCAL-REVIEW.json`。A可按本回执填回增量审查字段，但必须保留本地未提交和未现场验证边界；后续源码变化须差异复核，不能沿用映射结论。

构建补充：已实读 `A/P07-MANAGER-LOCAL-CACHE-BUILD.log`，manager_local_cache_final_build_exit=0，P07 三产物构建已完成；后续 Manager 白名单/连接引用增量由单独冻结输入审查，不由本历史回执覆盖。
