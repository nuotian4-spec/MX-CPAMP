# 有效配置与固定候选回读契约

预检基线为 `bab1afb07e3cf25e78394b6cb959aaa5999b31bd`。本次只修改既有只读预检的 BPS 字段解析和本目录证据；没有产品代码、镜像重建、官方模型调用、生产请求、推送或部署。

## 生效规则

`bps-protocol` 是兼容布局，`upstream.bps.protocol` 对每个存在的字段覆盖兼容值。缺字段继续继承；显式 `false`、`null`、空字符串均不是缺字段。`upstream`、`bps`、`protocol` 的 canonical 父节点须为映射，父节点 `null` 会被产品拒绝；根 `bps-protocol: null` 使用默认。

- `image-transport` 空字符串或 `null` 生效为 `native`，原生默认保持。
- `image-relay-base-url` 空字符串或 `null` 覆盖根值为空。HTTPS origin 允许单尾斜杠，Settings 按配置原字符串回读；任意路径、query、fragment、credentials 仍受原产品校验。
- `enabled`、`images-enabled` 的 `null` 生效为默认 `true`，显式 `false` 保留。
- `state-dir` 空、`null` 或纯空白时，使用有效 auth 目录的同级 `bps-state`；显式路径按容器 HOME/cwd 归一。
- `oauth.auth-dir` 按存在性覆盖根 `auth-dir`，空/`null` 使用 `~/.cli-proxy-api`。最终固定镜像实际 HOME 为 `/root`、cwd 为 `/CLIProxyAPI`。

预检用同一个有效合并结果读取 transport、origin 和 state；父结构无效时输出 `unknown`/`null`，不再把只读根值的 `relay_origin_configured=false` 当成 canonical 生效结论。输出仍只有既有路径、固定 transport 分类和 origin 是否配置的摘要，不输出 origin 本身或凭据。

## 已核证据

| 项目 | 实际输入与命令 | 结果 |
|---|---|---|
| 预检 12 例 | `python docs/evidence/bps-relay-release-20261111/contracts/verify.py --preflight-only`；根/v8/mixed/null/空值/尾斜杠/相对路径/HOME/auth 优先/根 null/父 null | exit 0，`preflight.log` 与 `preflight.example.jsonl` |
| Linux 实际配置加载器与状态路径 | 缓存构建环境，源码只读挂载 `/app`，Go overlay 加入本目录 `config_contract_test.go`；`GOPROXY=off GOTOOLCHAIN=local GOSUMDB=off go test -overlay /checks/overlay.json ./internal/config -run '^TestBPSRelayEffectiveContract$' -count=1 -v` | 子命令 exit 0，12 例通过，`loader.log`；测试穿过 `ParseConfigBytes` 与 `BPSStateDirectory` |
| 最终 CPA 实际启动及回读 | `python docs/evidence/bps-relay-release-20261111/contracts/verify.py --candidate-only`；五次启动最终固定镜像，分别为 root/v8/mixed/null leaves/trailing slash | exit 0，五次 `GET /v8/management/bps/settings` 均 200，白名单字段逐项相符，`candidate-settings.log` |
| 其他只读逻辑复用 | 移除新增纯函数并把 BPS 字段片段替回基线后，完整 Python AST 相同 | `unchanged_other_preflight_auth_io_activity_ast=passed`；原 Caddy、认证来源、固定 GET、活动统计与旧只读证据复用 |
| 固定镜像源码等价 | `git diff --quiet b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c bab1afb07e3cf25e78394b6cb959aaa5999b31bd -- components/cpa` | exit 0，loader 对应最终镜像产品源码 |
| 最终 diff | `git diff --check` | exit 0 |

最终 CPA 镜像为 `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b`，标签 revision 为 `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c`。Settings DTO 不返回 `state-dir`；路径结论来自实际 loader 专项，独立于 Settings 回读。

运行候选时所有容器使用 `--network none`；缓存 Python 客户端通过 `--network container:<fixture>` 共享该网络命名空间访问 localhost，合成管理 key 从 stdin 传入。没有配置模型凭据或发模型请求。CPA 内建版本刷新曾尝试联网，被 none 网络阻断；外连成功为零。所有 fixture CPA/客户端容器和临时目录已清理。

## 未完成边界

正向 HTTPS 图片 GET 字节、有效链接 HEAD 空体和同一真实链接过期后 404 尚未在本轮跑通。最终二进制没有受支持的管理 seed 入口，现有 ImageRelay 测试 helper 只连接 `httptest.NewRecorder`；无效 token 的 404 只证明负向路由。最小补证入口及验收口径见 `HTTPS-ACCEPTANCE.zh-CN.md`。
