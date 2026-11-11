# BPS 官方账号用量最终镜像断网组合

工具复用 `bps-inspection-repair-20261111/integration` 的 run/check 结构，只接受本地已有最终 CPA/Manager 镜像及完整 ID、OCI revision；启动先核身份，再使用镜像 ID。工具不构建、不拉镜像。由主会话在唯一最终构建后填入本轮完整身份执行一次：

```powershell
node run.mjs --cpa-image CPA_FINAL_IMAGE --cpa-id CPA_FINAL_SHA256_ID --cpa-revision CPA_FINAL_REVISION --manager-image MANAGER_FINAL_IMAGE --manager-id MANAGER_FINAL_SHA256_ID --manager-revision MANAGER_FINAL_REVISION
```

checks runner 默认使用已缓存 `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758`，只执行 Node 检查程序；不启动 OAuth、登录或 Worker。CPA `network none`，Manager/checks 共享 CPA 网络命名空间，仅 localhost 通信。全部根只读、日志 none、无发布端口，临时配置、合成凭证、SQLite 和密钥位于 tmpfs；finally 只清理本次随机 label 归属容器并确认无残留。

合成 BPS 凭证无 access/refresh token，新增检查：

- CPA usage 与 Manager usage 管理鉴权401。
- CPA 精确 auth_index/auth_id，缺 token 返回 credential_incomplete，source=chatgpt_account_usage，plan/rate 为 null。
- Manager `/api/bps/usage?auth_index&connection_ref` 缺 token归一；坏 connection_ref 返回409。
- BPS 权限 probe 和未知 quota，巡检保持 keep、不自动恢复/删除/禁用/启用/重登；provider 记录往返和凭证状态保留。
- 快照写查使用同次巡检结果的 fileName/authIndex/accountId/accountSnapshot 完整身份元组；accountId 核对合成凭证固定值，accountSnapshot 空则省略，不从 sub 或显示标签推导。

旧模型读/保存/清空及 revision 保护、巡检配置边界沿用已验证 harness；collector 保持环境托管原输入，仅 scheduler 关闭、手动执行一次巡检。公开 handler 的实际数值、headers、取消、身份/出口漂移与错误分类复用 CPA/Manager 本轮专项，不在此重复请求官方。

本线仅 `node --check run.mjs`、`node --check check.mjs` 和 `git diff --check`，均退出0，详 `SYNTAX.txt`。未执行组合、构建、真实 access/usage、登录、刷新或推理。最终组合回执由主会话记录命令、两镜像身份、退出码与清理结果。

## 主会话最终执行

首轮 `RUN.jsonl / RUN.exit=1` 保留：旧 CPA 在精确身份断言停止，清理通过。CPA 身份投影修复和实际 v0/v8 列表专项通过后，仅重建 CPA，Manager 输入等价复用。最终 `RUN-FINAL.jsonl / RUN-FINAL.exit=0`：五项身份布尔全部 true，BPS 快照公开写查成功，所有前段断言及 cleanup 均通过。没有真实官方请求。

```powershell
& 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe' 'D:/MX-CPAMP/docs/evidence/bps-quota-repair-20261111/integration/run.mjs' --cpa-image 'mx-cpa:bps-quota-identity-20261111' --cpa-id 'sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b' --cpa-revision 'b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c' --manager-image 'mx-cpamp:bps-quota-repair-20261111' --manager-id 'sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd' --manager-revision 'd26cbf7595ecf41ba8cbfd90c956291ac351b471'
```
