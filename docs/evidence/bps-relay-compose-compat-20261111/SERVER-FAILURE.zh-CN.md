# RC2 prepare 合并检查失败

2026-11-11。用户回传两脚本SHA检查均OK，随后为 `stage=prepare_bundle`、`stage=prepare_site`、`release_failed stage=prepare_site code=candidate_merge_changed`。没有apply、DEPLOY或维护开始回执。

入口在SSH交互Shell直接运行`set -eu`，prepare非零退出可导致登录Shell退出、SSH断开。后续入口将严格模式限制在`sudo bash`子进程，不影响父SSH Shell。

已核交付源码：失败位于候选Compose的完整JSON比较。用户随后回传只读诊断：唯一差异为`/services/cli-proxy-api/volumes/4/bind/create_host_path`，expected为布尔true、rendered缺省，`truncated=false`。状态仍为prepare_site，ready/maintenance/complete均false。该字段表示差异是本次严格比较失败原因，现有归一修复精确覆盖；false、null、其他bind选项及实质挂载字段继续严格比较。

源码表明此阶段只下载/核验/加载镜像并在本次私有STATE暂存候选配置；没有执行停服务、停态备份、live配置写入、创建live BPS状态目录或Caddy重载。用户现场最后容器是否仍运行由后续prepare即时核验，不用旧只读回执冒称本次实时检查。

保留失败目录`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111`和已下载bundle。修复工具采用独立`/opt/cpa-stack/.mx-upgrade-bps-relay-20261111-r2`，从旧目录只读复制bundle并经原manifest、归档摘要、镜像ID/revision核验，不编辑旧plan、ready或maintenance。

局部兼容工具已整合为`602aee042d6ba161096c71c4a9e4cce57f996c87`，真实Linux新STATE prepare、PTY CANCEL、apply周期及清理exit0，独审通过。现场诊断与既有empty_bind用例对应，可复用该专项，不重跑产品或完整生命周期。Compose2.33显式true的旧比较通过这一历史结果仍保留；本次现场原因来自用户真实字段回执。

已推送已审工具，固定完整提交的两文件raw下载逐字核验通过；[新版准备入口](PREPARE.zh-CN.md)已绑定新TOOLS和旧bundle。当前责任人：用户执行新prepare并回传结果；下一责任人：主会话核证后交TTY apply。既有产品、三镜像及已通过业务保持输入等价，不重建产品、不重复下载227MB归档，不复用旧plan/ready。
