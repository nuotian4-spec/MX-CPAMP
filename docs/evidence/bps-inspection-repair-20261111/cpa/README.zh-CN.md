# CPA BPS access 本地候选证据

日期：2026-11-11。产品/专项提交：`8b4fe6e83637535b7ae4e54c3e42f4bdf602e5a0`；基线：`5d431b9be0828f94576d1807e1cc87b822cc1350`。

候选新增受既有管理鉴权保护的 `GET /v8/management/bps/credentials/access`。精确引用当前 BPS 凭证，沿用现有 headers 与出口，只请求固定官方 access GET；不刷新、登录、推理或保存凭据，不改模型池。契约见[冻结契约](../../../contracts/bps-access-v8.zh-CN.md)。

## 专项

实际命令（工作树根目录执行）：

```powershell
docker run --rm --network none --entrypoint sh -v D:/MX-CPAMP-worktrees/bps-access-inspection:/work -v C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/gomodcache:/go/pkg/mod -w /work/components/cpa mx-cpa-buildenv:bps-tools-images-20261111 -c 'gofmt -w internal/runtime/executor/helps/bps_access.go internal/api/handlers/management/bps_access.go internal/api/server_bps_access_test.go internal/api/server_management_v8.go && GOPROXY=off GOTOOLCHAIN=local go test ./internal/api -run TestBPSAccess -count=1 -v'
git diff --check
```

两个命令实际退出码均为0；gofmt退出0。构建环境镜像完整 ID 为 `sha256:75195664cd12258c72160cf4121c931ae4176719d6545a9e5f35fabfe2abbc55`，工具链为预置 Go 1.26.11，网络模式为 none。日志：[bps-access-go-test.log](bps-access-go-test.log)。

公开 v8 handler 专项包含5个顶层测试：允许/拒绝/空目录及restricted/disabled过滤，401/403/429/5xx/3xx、坏结构与过大正文，管理未认证、错误provider/未知引用/重复参数/非法引用/ID漂移，缺token、传输失败、凭据身份与全局有效出口漂移，header与body阶段真实父取消，以及更短父截止。合成 transport 断言每次仅一条固定 access GET、所选凭证 headers、无刷新/推理/保存/删除。失败 allowed/models 保持 null；有效 allowed=false 保持空目录；额度恒为null，推理与自动恢复标志恒为false。

独立只读审查席已放行最终产品 diff；全局有效出口热重载反例已包含在最终exit0输入。未扩大通用 executor URL 能力，v0无新增接口。最终cmd/server编译由主会话统一最终Docker构建核验，不在本线重复构建。

## 边界

没有真实上游请求、真实凭据读取、SQL/schema/数据库动作、push、部署或定时调度动作。本地合成专项不替代官方 access 的定向真实验收，也不代表推理成功或额度已知。下一责任人：主会话整合与最终隔离组合验收。
