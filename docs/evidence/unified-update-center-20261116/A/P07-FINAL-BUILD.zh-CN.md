# 最终本地构建回执

## 构建输入

- 候选：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`。
- 基线 HEAD：`9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`。
- 实际输入：`P07-BUILD-INPUT.json`，4636 个 component 文件的 Git blob/mode 精确映射；不是以未变 HEAD 代替 dirty 输入。
- revision 标签：`9dce11c-dirty-unified-update`；版本标签：`local-unified-update`。
- 源码增量审查：`../S/FINAL-LOCAL-REVIEW.json`，PASS，映射缺漏/漂移均为 0；`deploy/production-wiring.contract.json` 已引用该实际回执。

## 产物

产物目录：`C:/Users/子羽/.codex/tmp/unified-update-20261116`。

| 产物 | 字节数 | 实际结果 | 回执 |
| --- | ---: | --- | --- |
| `web/index.html` | 6817480 | web 最终构建 exit 0 | `P07-WEB-BUILD.log`，工具 `431f6e` |
| `CLIProxyAPI` | 105353240 | Linux amd64、CGO=1，exit 0 | `P07-CPA-MANAGER-BUILD.log` 的 CPA 结果，工具 `6fd7c7` |
| `cpa-manager-plus` | 32239357 | Linux amd64、CGO=0，exit 0 | `P07-MANAGER-LOCAL-CACHE-BUILD.log`，工具 `a272b7` |

Manager 嵌入上述最终 web HTML 与图标。Manager 最终构建使用 Go 1.26.11、`-buildvcs=false`，保持 go.mod/go.sum 锁定版本；文件代理为 `file:///hostmod/cache/download`，模块解压目录为容器 `/tmp/gomod`，`GOSUMDB=off`，Docker 使用 `--pull never --network none`。缓存来自本机 `C:/Users/子羽/go/pkg/mod` 的只读挂载，没有外部依赖请求。

最终 Manager 构建标志：

```text
CGO_ENABLED=0 go build -buildvcs=false -ldflags '-X github.com/seakee/cpa-manager-plus/apps/manager-server/internal/buildinfo.Version=local-unified-update -X github.com/seakee/cpa-manager-plus/apps/manager-server/internal/buildinfo.SourceCommit=9dce11c-dirty-unified-update' -o /out/cpa-manager-plus ./cmd/cpa-manager-plus
```

## 验收边界

这是未提交的本地候选构建。没有 commit、push、发行发布或生产部署；不代表真实插件、许可、账号或模型业务验收。浏览器验收由 P9 单独记录。
