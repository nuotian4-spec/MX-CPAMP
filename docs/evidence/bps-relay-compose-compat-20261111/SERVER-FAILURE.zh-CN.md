# RC2 prepare 合并检查失败

2026-11-11。用户回传两脚本SHA检查均OK，随后为 `stage=prepare_bundle`、`stage=prepare_site`、`release_failed stage=prepare_site code=candidate_merge_changed`。没有apply、DEPLOY或维护开始回执。

入口在SSH交互Shell直接运行`set -eu`，prepare非零退出可导致登录Shell退出、SSH断开。后续入口将严格模式限制在`sudo bash`子进程，不影响父SSH Shell。

已核交付源码：失败位于候选Compose的完整JSON比较。此前真实Linux日志 `phase2-release-boundaries-20261111/matrix-compose-bind.log` 有精确反例 `/services/cli-proxy-api/volumes/4/bind:missing`，显示Compose省略默认bind块会击中该比较。当前OVH回执没有字段差异，不能把旧反例当成本次5.7.0的精确差异日志；修复须在保留其余严格比较的同时，仅归一该已复现默认字段表示。

源码表明此阶段只下载/核验/加载镜像并在本次私有STATE暂存候选配置；没有执行停服务、停态备份、live配置写入、创建live BPS状态目录或Caddy重载。用户现场最后容器是否仍运行由后续prepare即时核验，不用旧只读回执冒称本次实时检查。

保留失败目录`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111`和已下载bundle。修复工具采用独立`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2`，从旧目录只读复制bundle并经原manifest、归档摘要、镜像ID/revision核验，不编辑旧plan、ready或maintenance。

局部兼容工具已整合为`602aee042d6ba161096c71c4a9e4cce57f996c87`，真实Linux新STATE prepare、PTY CANCEL、apply周期及清理exit0，独审通过。Compose2.33对原显式true输入的旧比较也通过，故这些证据不证明OVH5.7具体根因已关闭。

当前责任人：主会话交一次只读字段诊断；下一责任人：用户回传脱敏结构摘要，主会话按实际差异收口后再交新准备入口。既有产品、三镜像及已通过业务保持输入等价，不重建产品、不重复下载227MB归档。当前不重跑prepare或apply。
