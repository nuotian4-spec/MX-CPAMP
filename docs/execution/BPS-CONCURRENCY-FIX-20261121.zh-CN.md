# BPS 凭证并发持久化修复

源码验收结论：本轮修复及专项验证通过，目标固定版本为 `v2026.11.21-rc.2`。发布结果以该版本的 GitHub Release 及 Actions 回执为准，生产验收按本文末尾步骤执行。

基线：`58cdb174d37ee2e80fd976a0e270de869ef2e1fa`，CPA v8.0.80。

## 问题与修复

生产回执确认目标凭证提交值为 30，运行时为 `null / 20 / default`，文件中的 `concurrency_limit` 缺失。合成凭证已复现：保存 30 成功后，插件刷新省略该字段会导致 CPA 将其从元数据及文件删除。现场缺少写入时序记录，因此该路径与现场吻合，但没有把生产上的每一次覆盖归因到已捕获的刷新调用。

- 并发覆盖值由 CPA 管理，刷新与请求准备沿用当前管理员设置，包含管理员刚刚清空的状态。
- 插件文件初次加载和 watcher 重载以原始文件中的字段为准，清除插件返回的旧运行时属性。
- 管理接口保存并发值时同时清除旧属性；提交 `null` 后，旧插件存储 JSON 中的覆盖值不会重新出现。
- 管理员修改遇到存储错误时返回错误，不发布修改后的运行时状态，也不执行持久化成功回调。普通运行时更新原有容错语义保持。
- 插件存储随凭证快照复制，避免失败写入前的元数据注入改变已发布的存储对象。

Manager 前端严格回读保持；没有更改默认值 20、并发排队算法、账号模型、插件二进制、商业许可或数据库。

## 本地验证

工具链为缓存中的 Go 1.26.10，`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`GOMODCACHE=C:/Users/子羽/go/pkg/mod`。工作目录为本工作树的 `components/cpa`。

新增专项测试覆盖真实管理 PATCH、Manager 持久化与合并、插件存储、文件加载和 watcher 合成路径，账号与插件返回值均为合成数据。

| 验收边界 | 结果 |
| --- | --- |
| 设置 30 后插件刷新省略字段 | 文件为 30，回读为 `30 / 30 / override` |
| watcher 重载和独立 Manager 重新加载 | 保持 `30 / 30 / override` |
| 主动清空，随后旧刷新结果返回 | 字段缺失，回读为 `null / 20 / default` |
| 插件返回旧 metadata / attribute | 以文件或本次管理修改为准 |
| 注入存储写入失败 | HTTP 500，成功回调未调用，磁盘和运行时保持原值 |
| 未修改的另一张凭证 | 保持继承默认 20 |

专项命令退出码 0，五个包全部通过：

```text
go test -mod=readonly ./internal/api/handlers/management ./sdk/cliproxy/auth ./sdk/auth ./internal/watcher/synthesizer ./internal/pluginhost -run 'Test(CredentialConcurrency|PatchAuthFileFields|PatchAuthFileStatus|MergeRefreshed|MergePrepared|MergeExistingAuthMetadata|FileTokenStore|SynthesizeAuthFile|PluginTokenStorage|RefreshAuth|ManagerCanceled|Manager.*Modif)' -count=1 -timeout 120s
```

Linux/amd64 离线编译退出码 0：`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -o C:/Users/子羽/.codex/artifacts/bps-concurrency-20261121/fix-validation/cli-proxy-api-linux-amd64 ./cmd/server`。这是编译检查；正式发布沿用 Actions 的 CGO 构建。

## 范围审查

本轮增量复核由当前执行会话完成，非独立审查。修改限于并发字段管理、插件存储快照和管理写入错误传播。`Modify` 的管理写入启用持久化错误传播，刷新、模型发现和普通 `Update` 的错误处理保持原语义；已有 `WithSkipPersist` 调用仍保留其语义。普通启动入口、通道策略、Manager、Worker 和部署脚本未变。发布合同只更新本轮组件文件映射及本记录指针，既有运行证据保留。

候选组件树：`be4b0d17f27234ceb55e6932dc47f23438909b5a`。十个组件路径的源码映射与发布合同匹配，`validate_wiring_contract` 和 `validate_source_mapping` 均通过。其余三条路径为本记录、并发合同说明和发布接线合同。

## 生产验收

代码更新后，将目标凭证重新保存为 30 一次。确认文件为 30，面板回读为 `30 / 30 / override`，后续正常凭证刷新及重载后仍保持。若主动清空，应回到 `null / 20 / default`，其余凭证保持原值。

已丢失的历史并发值没有可靠来源，修复不猜测恢复。未调用真实模型或真实供应商账号，未以本地测试代替生产验收。
