# Compose 默认绑定选项兼容审查

2026-11-11。基线 `1f68c974643d5006a6d42289756226b99d508e9c`，工具 `85e4eff876f2a0cd0a40bcae55517d151346c883`，证据 `7ca6ca7f824e81ac0e38a9e2afe608a7d81448e9`。结论：默认绑定选项的局部兼容修法及新状态目录真实生命周期通过定向审查，无新增 P1/P2；OVH 现场 `candidate_merge_changed` 的具体字段差异仍待证，未将本项写成该事故根因已关闭。

本席只读核稳定 diff、完整比较函数、prepare 调用、真实 Compose 专项、生命周期控制器与日志、旧故障复用依据。未运行测试、构建、容器、网络命令、真实登录/模型请求或生产操作，仅写本审查，由主会话统一提交。

## 代码边界

本轮产品差异仅为独立 `STATE=.mx-upgrade-bps-relay-20261111-r2`、两个纯比较函数和 prepare 一处调用。

- 仅 `type=bind` 的卷把布尔 `create_host_path=true` 与省略该默认值归一；空 bind 与省略 bind 等价。false、null、其他 bind 选项及卷的 type/source/target/read_only 均继续严格参与比较。
- 深拷贝后归一，不修改候选 Compose、expected、实际 rendered 或 plan 保存的原始配置。原 target 排序、运行时挂载核验与切前严格漂移检查保持。
- 使用新 r2 状态目录，不删除或改写旧失败目录，不补造 ready/complete，不绕过维护标记。缓存归档可作为 `--bundle-dir` 只读来源；新目录复制后仍经过原 manifest、摘要、精确镜像身份和实际 load 验证，不凭旧文件名直接信任。
- 停三应用、备份、候选启动、原 Caddy inode 写入及 reload、认证、生成 Linux wrapper、失败停态与拒重入逻辑未变。没有 SQL/schema 迁移、运行时 DELETE 或产品镜像变化。

## 已核证据

`compose-compat-linux.log` 为真实 Linux Compose 2.33.0 配置输出。默认 bind 省略的输出与 expected 显式 true 在旧直接比较下拒绝，经新比较通过；false/source/target/read_only/propagation/selinux/type 七类实质漂移均拒绝，相同 false 保留，输入未修改。此专项不创建服务。

同一日志明确记录 `explicit_default_old_strict_comparison_passed=true`：原显式 true 输入在该环境的旧比较也通过。helper 随后将 override 中 bind 改为缺省，才产生旧拒绝反例。因此这份证据证明默认表示兼容契约，未复现原交付同输入在 OVH Compose 5.7.0 的具体失败；不得用它确定现场根因。宿主 Windows Compose 的路径/表示问题未扩为本轮目标，也未冒称目标 Linux 证据。

`run.log` 的唯一新状态目录周期为真实 Linux prepare → PTY CANCEL → apply，最终 `fixture_cleanup=verified`、`command_exit=0`。已核：

- prepare/取消不改 live 文件、容器或运行配置，无维护与 live state；随后一次 DEPLOY 成功完成新目录生命周期。
- 三最终镜像完整身份、三文件 Compose 链、实际挂载、Manager 设置回读和正负鉴权成立；新 state 权限700、停态归档600，生成的 Linux wrapper 实际执行。
- 原 Caddy ID/inode/证书卷保持，挂载可见新正文、admin 候选生效、受信内部 HTTPS 路由成立；complete 再次 apply 拒绝。
- 仅唯一隔离项目/内部网络及合成凭据，无官方或生产请求；清理确认。

`input-equivalence.json` 与稳定 diff 对应：42 个原函数 AST 一致，唯一改变的原函数为 prepare，两新增函数仅用于本轮比较。reload-failure 复用前轮实际故障日志，明确为复用；未重新执行故障周期，也未把旧证据写成本轮新跑。产品、归档、HTTPS 图片库、2FA 和其他输入未变，不重复全量门禁。

## 现场接续

当前只能确认旧失败位于 `prepare_site`，维护未开始；具体不等字段由主会话的只读现场诊断收口。现场诊断应仅输出固定类别/差异位置，不回显配置值或密钥。若差异超出已审默认 bind 范围，应先修匹配输入的反例，不让用户反复尝试相同 prepare。

用户终端入口应使用 `sudo bash <<'…'` 子 Shell，把 `set -e` 限在该子进程，避免在 SSH 登录 Shell 顶层设置失败退出。实际交付命令尚待主会话提供，本审查不把此要求写成已验入口。复用旧 bundle、新工具目录和新 STATE 的具体路径仍须与最终交付一致；旧状态及归档原位保留。

下一责任人：主会话先核现场字段差异，绑定最终代码/证据与独立用户入口，再交准备命令。生产切换仍由用户 TTY 一次 DEPLOY 确认；生产完成、备份及公网 HTTPS 取图只以真实现场回执收口。

## 只读字段诊断入口

已定向只读核 `diagnose-merge.py`、执行席 `test-diagnose.py` 与 `diagnose.log`，未运行。脚本固定导入原 rc2 工具，先设 `dont_write_bytecode`，导入名使原 `main` 不执行；只有旧状态为 `prepare_site`、无 ready/maintenance/complete 且候选 Compose 存在才复算。命令路径仅为候选 image inspect 和两次 Compose config，没有镜像加载、服务动作、state/配置写入。输出限定差异位置、presence/type 与 create_host_path 布尔值；私有键匿名，原值和异常原文不输出，最多30条并显示截断。执行席缓存 Linux 专项已核真实 config、哨兵脱敏、零文件写入和截断结果，exit0；镜像身份入口为合成替身，不冒称该专项执行了生产 inspect。

主会话固定用户入口为 `sudo python3 -B - <<'PY'`，中间逐字嵌入该脚本、末行 `PY`。父 SSH Shell 不设 `set -e`，没有下载或落盘；脚本异常只退出该 Python 子进程。此只读诊断入口通过代码审查，等待用户真实字段摘要后再判断现场失败原因，不先重试 prepare。
