# P01 静态解析分项回执

基线：`9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`。候选根：`C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP`。

新增产品：`components/cpa/internal/pluginstore/import.go`、`import_test.go`；前端接口类型 `components/cpamp/apps/web/src/types/pluginImport.ts`。

已跑命令（工作目录为候选 `components/cpa`，使用缓存 Go 1.26.10，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`）：

```text
go test ./internal/pluginstore -run '^TestPrepareImportArchive' -count=1
ok  github.com/router-for-me/CLIProxyAPI/v8/internal/pluginstore  0.074s
TEST_EXIT=0
```

命令工具实际输出转录，未为补记录重复执行。输入为测试中的惰性 ELF 字节夹具和统一包目录形状；断言规范化结果仅含根目录库，库字节不变，脚本和面板未进入安装工件。没有执行或加载任何二进制。

本记录只证明该正向静态解析分项，不代表异常样本、HTTP 导入确认、跨进程互斥、异步真实注册、0.5.58 许可或整体 P01 已完成。下一任务：共享站点门禁与安装任务纵切。当前责任人：原 A；下一审查人：原 S。
