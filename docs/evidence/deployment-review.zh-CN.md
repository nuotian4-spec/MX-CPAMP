# 部署材料独立审查

日期：2026-11-10。执行：P8 Manager/Login；独立只读审查：review_login_architecture；整合核证：P9。

结论：指定部署范围内无剩余 P1/P2 阻断。产品组件与已有构建输入保持不变，未复跑产品门禁。

| 范围 | 最终结论 |
|---|---|
| prepare 重试 | STATE 暂存并完成真实 Compose 合并核验后，先安装入口再硬链发布；只续接本轮同 inode 材料，未知 override 拒绝 |
| Manager 持久路径 | 核实际选中 JSON 及其摘要；支持纯 ENV 默认分支，未知容器内配置在维护前拒绝 |
| 维护前鉴权 | 原 CPA API Key 和管理员密钥必须有效；管理员伪 key 必须 401；过期 bootstrap key 在停服前拒绝 |
| 切换后检查 | 使用真实任务查询接口，已认证的不存在任务返回 404；未认证 401 |
| 备份与失败 | 显式确认、漂移复核、停 CPA/Manager 并核停止态，再备份；覆盖 SQLite/WAL/data.key/auth/bbolt 与 custom KV；Caddy 保持原容器运行，失败无自动数据回灌 |

审查核对的 [Linux 专项记录](deployment/release-boundaries.log) 退出 0，测试资源清理已验证。覆盖真实 Compose、PTY 确认/取消/EOF/无 TTY、Worker 单文件 secret 实际读取、自定义 KV 归档、gzip/load 及实际组件鉴权。现场 inspect 与默认配置存在性由合成 fixture 提供，此项不等于生产环境准备或切换已通过。

三镜像大归档采用 [独立 Linux 加载证据](deployment/archive-verification.zh-CN.md)，不重复加载验证。

审阅输入 SHA256，供整合后逐文件比对：

```text
cpa-stack-release.sh 3a343da9df48b9c30e1888a711b5b4512c6fb8bf7c109811421d334f0ec6b231
cpa-stack-release.py 1090e3e4fed2b33ef8fe4369f2335a479b714cf1e1d797d965b9819383d68e56
cpa-stack-compose.sh 724a85482334b01e1ea56ec1cd4c2f795f33c4cda341d5eee5b75f648eb10de5
```

无新增 SQL/schema 迁移。生产 prepare/apply、真实 OAuth/2FA、令牌刷新及模型请求尚未执行。下一责任人：P9 核整合输入、公开候选资产并交付固定提交的准备命令；用户回传后继续受控切换。
