# P05 宿主桥离线专项

日期：2026-11-16

结论：29 项 unittest 全部通过，`host_unittest_exit=0`。执行使用 Linux Python 3.11.2，真实工具回执 `fc7aba`；完整输出见同目录 `P05-HOST-TEST-OUTPUT.txt`。

## 执行命令

```powershell
$root='C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP'
$deploy="$root/deploy"
docker run --rm --pull never --network none --entrypoint python3 --mount "type=bind,source=$deploy,target=/deploy,readonly" mx-cpa-buildenv:bps-tools-images-20261111 -B /deploy/test_mx_update_host.py
```

原始回执末尾：

```text
Ran 29 tests in 0.215s

OK
host_unittest_exit=0
```

## 覆盖

| 契约 | 实测断言 |
| --- | --- |
| 同站共享锁 | 实际 `flock` 阻止手工锁期间的网页安装；网页持久所有者阻止实际 `mx-update.py main()`；第二候选返回原所有者 |
| 接受与幂等 | 同一候选只产生一个任务和一次启动；安装前及 worker 启动前的基线变化被拒绝 |
| 启动竞争 | 真实线程持有 worker 文件锁时，迟到的 systemctl 启动失败不覆盖运行任务；无 worker 的失败保留恢复所有者 |
| 固定维护确认 | `confirm_fixed` 分别拒绝四服务任一容器 ID 改变；固定发行材料变化被拒绝 |
| worker 结果 | 实际宿主完成判断绑定活动指针、plan 镜像和原 Caddy；错误运行镜像进入恢复状态 |
| 失败分界 | 维护前失败成为终态并释放所有者；维护后失败保留恢复状态、维护标记与所有者 |
| 中断与停态 | 终态不被 interrupted 覆写；维护前不发 stop；维护后按 Caddy、三应用顺序停止并 inspect 四服务；stop 抛错或仍 Running 均持久化 `halt_confirmed=false` 且保留所有者 |
| daemon 对账 | active、activating、deactivating 保留任务；失效 systemd worker 标记恢复且保留所有者 |
| 恢复确认 | 拒绝错误镜像、替换 Caddy、缺失完成记录；绑定新完成记录并通过入口健康后才能完成；恢复原镜像只收口为 failed，不冒充已更新 |
| Unix HTTP | 实际 Unix socket 与 HTTP handler 拒绝缺失或错误站点 ID；三个端点拒绝额外 URL/source/site 字段；精确候选请求与重复安装返回固定任务 |

## 证据边界

- 实际运行：宿主 `UpdateHost`、`inspect_runtime`、`halt_registered_runtime`、HTTP handler、临时站点文件、持久 JSON、Linux 文件锁、工作线程、Unix socket，以及手工 updater 的入口锁路径。
- 合成边界：Docker、systemctl、curl 与发行获取；不挂载 Docker socket，容器网络关闭，未访问真实站点、许可服务或外部网络。
- worker 下层 `run_cycle` 使用注入的合成阶段回调。该回调通过真实确认函数和进度回调，写入真实临时 plan、completed、活动指针，并更改合成 Docker 快照；本证据只证明宿主层契约，不替代 updater 全链、真实 systemd 单位启动、浏览器或生产验收。
- 本写域仅新增 `deploy/test_mx_update_host.py` 与本组 `P05-HOST-*` 证据；产品脚本和已有测试由原责任人维护。

下一责任人：A，汇合宿主专项与既有 updater 全链证据，并执行后续集中验证。
