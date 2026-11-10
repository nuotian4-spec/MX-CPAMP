# 登录诊断独立审查与隔离构建

日期：2026-11-11。产品输入：`a5eaebbb777152ce71900af447289e61e8088316`，对应B线 `a6731e26d574aa9356e05b2f1cbb8b90c4d3783e`；共同基线 `49ab9afc75d0a3de11e0647f1b87659e27f03a49`。诊断改动16文件，未修改原认证请求顺序、身份校验、挑战处理或生产配置。

独立只读审查通过，无P1/P2。核实Worker runner、Tasks、Manager、Web的 `stage/reason` 白名单；真正命中MFA后才标TOTP；后续执行复位阶段；进入CPA保存时才标 `cpa_import`；取消和保存未知结果未改成成功。`git diff --check 49ab9af..a5eaebb` 退出0。复用同产品输入的 [诊断专项](diagnostics.zh-CN.md)，未重复运行全量测试。

进程被杀或崩溃前没有返回终态时仍可能没有具体阶段，按未知处理。当前审查不证明真实登录成功。

P9在主工作区使用固定Go1.26.10构建临时Linux Manager，`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`，执行：

```text
go build -ldflags "-X github.com/seakee/cpa-manager-plus/apps/manager-server/internal/buildinfo.Version=phase2-diagnostic -X github.com/seakee/cpa-manager-plus/apps/manager-server/internal/buildinfo.SourceCommit=a5eaebb" -o C:/Users/子羽/.codex/artifacts/mx-cpamp-phase2/diagnostic-a5eaebb/cpa-manager-plus ./cmd/cpa-manager-plus
```

退出0，产物24770628字节。该临时二进制使用现有嵌入页面，服务于API诊断复测；没有重新构建Web，也不作为最终页面发布产物。二进制内revision为短值 `a5eaebb`，源码完整提交如上，正式候选另绑定完整提交。

用户已确认旧测试账号密码或2FA发生变更，原材料不再使用。真实登录及刷新/模型调用等待更新材料；本地实现继续。无新增SQL迁移或生产写入。
