# Compose 合并表示兼容

工具输入 `85e4eff876f2a0cd0a40bcae55517d151346c883`，基线 `1f68c974643d5006a6d42289756226b99d508e9c`。改动只包含 `deploy/bps-final-release.py` 的候选 Compose 比较和独立状态目录 `.mx-upgrade-bps-relay-20261111-r2`。三产品镜像、归档、Shell入口、预检和 Caddy 拓扑保持原输入。

OVH 现场回执为 `prepare_site / candidate_merge_changed`，维护尚未开始。此次修法覆盖已知的等价 bind 默认字段表示边界；Compose 5.7.0 的具体现场差异仍待只读字段诊断。旧失败目录及其已验归档保留，新的 prepare 从新目录建立 plan 和 ready。

## 比较边界

只在 `type=bind` 的卷中归一 `create_host_path=true` 和空/缺省 bind；false、null、其他 bind 选项及卷其他字段继续参与严格比较。深拷贝后比较，不修改交付 Compose、实际渲染结果或输入对象。prepare 原有卷 target 排序保持不变，运行时挂载漂移核验保持不变。

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| 真实 Linux Compose 2.33 缺省 bind 输出 | 旧比较拒绝，新比较通过；显式 true 的实际输出仍含 true，单列保留 | [compose-compat-linux.log](compose-compat-linux.log) |
| false、source、target、read_only、propagation、selinux、type 漂移 | 7 类均拒绝；相同 false 保留可比，输入无修改 | 同上 |
| prepare → 真实 PTY CANCEL → apply | exit0，四真实容器，合成凭据，无真实模型请求 | [run.log](run.log) |
| 新目录 compose 链及恢复材料 | 新 STATE 完整周期通过，生成 Linux wrapper，停态备份600，运行目录700 | 同上 |
| Caddy 原ID、inode、证书卷及实际 reload | 通过，挂载新正文/admin运行配置、内部受信HTTPS和设置回读均通过 | 同上 |
| fixture 清理 | `fixture_cleanup=verified`，`command_exit=0` | 同上 |
| reload 失败阶段/maintenance/拒重入 | 复用前轮真实故障；`apply`、`publish_caddy`、`confirm_deploy`、运行时检查等原函数AST一致，只有prepare合并比较改变 | [input-equivalence.json](input-equivalence.json)、[前轮故障](../bps-relay-release-20261111/tools/caddy-reload-failure.log) |

执行目录 `D:/MX-CPAMP-worktrees/bps-relay-compose-compat`：

```powershell
docker run --rm --network none --mount "type=bind,source=$PWD,target=/repo,readonly" --entrypoint python3 sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48 /repo/docs/evidence/bps-relay-compose-compat-20261111/compose-compat.py /repo/deploy/bps-final-release.py
& './docs/evidence/bps-relay-compose-compat-20261111/run.ps1' -BundleRoot 'C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111'
```

两项命令均 exit0。Compose 兼容命令不启动服务；周期控制器仅替 ROOT/PROJECT/STATE/BASELINE 事实常量，执行原交付 main。runner 使用缓存镜像并断网，应用位于独立内部网络，真实 Docker socket 只挂 runner。宿主 Windows Compose 5.3 不冒充目标 Linux 表示证据，未扩大验证到网络下载或生产。

## 剩余边界

现场具体字段差异尚待核证；本地周期不证明生产上线、外部TLS或有效图片公网取图。产品输入未变，沿用既有产品专项和归档；未进行真实登录、刷新、模型调用、数据库迁移或生产写入。下一责任人：独立审查核本次差异和证据，主会话交付现场字段诊断及后续准备命令。
