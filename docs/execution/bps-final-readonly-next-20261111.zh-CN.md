# BPS 发布前现场只读接续

当前本地包与审查见[最终候选发布材料](../evidence/bps-final-release-20261111/FINAL.zh-CN.md)。本轮先补Caddy路由和活动摘要；不加载镜像、不切服务、不创建服务器脚本或报告文件，不发官方登录、刷新或推理请求。

## 执行入口

在这台Windows电脑的PowerShell执行，命令会把完整脚本执行块复制到剪贴板：

```powershell
$cpaPreflight = [IO.File]::ReadAllText('D:\MX-CPAMP\deploy\phase2-readonly-preflight.sh').Replace("`r`n", "`n")
Set-Clipboard -Value ("sudo bash <<'CPA_PREFLIGHT_READONLY_20261111'`n" + $cpaPreflight.TrimEnd() + "`nCPA_PREFLIGHT_READONLY_20261111`n")
```

切到OVH VPS现有SSH终端，粘贴并回车。将输出的JSON行原样回传；无需粘贴账号、密码、TOTP、完整回调或API Key。这个入口使用当前已审本地文件，不依赖尚未推送的GitHub下载地址。

## 结果口径

- 旧18条容器/挂载/空间基线按未变事实复用，本次只用新输出补路由与活动证据。
- Caddy输出有界、脱敏、按原JSON顺序的路由骨架。路径、方法、目标、嵌套与终止/分组只输出白名单分类；未知条件如实保留，不把摘要当作路由已正确。
- 内部GET只核现有认证与活动端点。404、缺字段或非法计数均记录unknown；单登录任务和Worker活动仍无只读枚举接口，不写成零。
- `write_release_ready=false`继续保留。主会话据此确定HTTPS图片中转精确路由和后续验收，不将健康GET当成真实图片或模型业务通过。

完整验收后再统一推送、发布资产及生产切换。未来apply涉及停止三应用、新停态备份和配置/状态写入；本次只读采集不执行apply。
