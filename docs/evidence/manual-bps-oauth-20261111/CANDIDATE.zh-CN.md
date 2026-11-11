# 登录输入诊断候选预检

日期：2026-11-11。仅新增[候选预检脚本](candidate-preflight.mjs)，旧手工验收 `run.mjs` 及其固定镜像输入保持不变。

| 组件 | 实际镜像 ID | revision |
| --- | --- | --- |
| Manager | `sha256:cdf235bee2e22f2c33af145b8fb4d27e93b4478162b8738dcadd3595f3b74c4a` | `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44` |
| CPA | `sha256:a4428de0eeda96d76cfa5733eed190ea8b45cd49083bf6b881f4bc9dfc1f5933` | `ef4d287ea3298a7680f8fc21e82db77417bbe634` |

两镜像实际 inspect 均匹配固定 ref、ID、linux/amd64 和 revision；不接受任意同名 tag。

## 实际结果

[CANDIDATE-PREFLIGHT.jsonl](CANDIDATE-PREFLIGHT.jsonl)：exit0，归属清理确认。

- 实际启动 Manager 与 CPA：仅回环随机临时入口，CPA端口不公开；根只读、日志none、临时配置/密钥/数据tmpfs，无旧账号或Worker。
- `GET /api/oauth-login/capabilities` 无密钥401、临时管理员鉴权200，`password_login_configured=false`。
- 实际嵌入面板HTTP200，包含“当前服务未配置2FA登录器”和回调校验提示；这是静态产物检查，不是渲染后的浏览器验收。
- 真实Manager代理认证列表与磁盘文件均为0。没有创建OAuth会话、重提回调、真实登录、主动刷新或模型请求；没有给用户新测试地址。
- finally核唯一label并删除两个容器，无本轮资源残留，临时令牌状态不存在。后台刷新未硬性封锁，不把启动器自身0次当成完整上游流量观测。

固定Node执行：

```powershell
& 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe' 'D:/MX-CPAMP/docs/evidence/manual-bps-oauth-20261111/candidate-preflight.mjs'
```

`node --check` 与本轮 `git diff --check` 均exit0。无产品diff、SQL/schema迁移、生产写入、push或部署。原手工OAuth失败不由这次无凭据预检改写为成功；下一责任人：主会话整合并独审本轮候选证据。
