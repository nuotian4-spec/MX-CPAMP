# P06 首次接入离线专项

日期：2026-11-16

结论：18 项 unittest 全部通过，`host_install_unittest_exit=0`。真实工具回执 `953338`；完整输出见同目录 `P06-INSTALL-TEST-OUTPUT.txt`。P05 保持原回执，本轮未重跑。

## 执行命令

```powershell
$root='C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP'
$deploy="$root/deploy"
docker run --rm --pull never --network none --entrypoint python3 --mount "type=bind,source=$deploy,target=/deploy,readonly" mx-cpa-buildenv:bps-tools-images-20261111 -B /deploy/test_mx_update_host_install.py
```

原始回执末尾：

```text
Ran 18 tests in 0.563s

OK
host_install_unittest_exit=0
```

## 覆盖

| 契约 | 实测断言 |
| --- | --- |
| 显式确认 | 未提供维护确认时，临时站点和系统目录内容完全不变，所有系统边界均未调用 |
| 单站登记 | 已有另一站点登记直接拒绝，原登记内容和文件树保持不变 |
| 同路径登记 | `site_path == system_root/etc/mx-update/site.json` 时保留 0600 普通文件，未形成自指向符号链接 |
| 文件锁 | 接入前后锁文件 inode 和内容保持不变；真实手工锁阻止应用重建 |
| 完整重建 | 新桥接文件插入基础链与既有扩展链之间；一次重建完整 APPS，不单独重启 CPA；镜像不变，原 Caddy 容器 ID 与链保留 |
| 备份 | 配置、站点登记与原活动指针逐字节备份；已有系统脚本和单位文件保存后再替换 |
| 成功收口 | 注册入口指向当前站点；完成记录声明入口健康通过；任务成为 completed，清除 active 所有者 |
| 失败收口 | 停态后重建失败或入口健康失败触发 halt；halt 抛错仍持久化 recovery_required 和 active 所有者；没有完成记录 |
| 恢复确认 | 镜像、应用链、Caddy 链、活动指针任一不符均拒绝且不查入口；入口不健康保留所有者；原现场与入口通过后任务收口为 failed/recovered 并清除所有者 |
| 系统目录注入 | CLI 调用安装函数不传测试系统目录；CLI 拒绝 `--system-root` 参数 |
| 单位契约 | 实际单位文件包含固定 registry、worker ExecStopPost 中断回调、root、control-group；无 Manager 进程依赖；宿主运行目录持久保留 |

## 证据边界

- 实际执行安装与恢复函数、CLI 参数分支、临时文件创建及备份、符号链接、权限、文件 inode 和 Linux `site_lock`；实际解析仓库中的两个 systemd 单位文件。
- `system_root` 注入独立临时目录。候选 deploy 目录只读挂载，Docker 容器网络关闭，未挂载宿主 Docker socket。
- `command`、`inspect_runtime`、`docker_json`、`halt_registered_runtime` 使用合成边界；底层直接 subprocess 调用被测试拒绝。没有执行真实 Docker 站点、systemctl、curl 或数据库恢复。
- 此回执证明首次接入的控制流程与文件契约，不替代真实 systemd 运行、真实站点维护或生产验收。
- 本写域仅新增 `deploy/test_mx_update_host_install.py` 和本组 `P06-INSTALL-*` 证据；产品脚本、已有测试和前端保持原责任边界。

下一责任人：A，汇合首次接入与宿主专项证据，执行后续集中验证。
