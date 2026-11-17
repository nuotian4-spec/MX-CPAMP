# Custom tool 续接原提交对照

基线：`67445a35ba450468948447492b560c204afa7321`。执行者：S。结论：指定10个custom_tool_call子项在原提交同样失败，属于本地基线已有功能缺口，不是本次公开错误呈现新增回归；成功断言保留。

## 输入与命令

- 使用已有 Go 1.26.10 windows/amd64，`GOPROXY=off`、`GOSUMDB=off`、`GOTOOLCHAIN=local`；仅本地合成fixture。
- 用 `go test -overlay` 将当时全部13个已修改CPA文件映射到 `git show 67445a35:路径` 原始字节，5个新增CPA文件映射空路径删除。产品文件未写入。
- 仅执行 `^TestKiloM7B(ToolImageContinuations|LongCallID)$/^custom_tool_call$/^(history=(true|false)|chat-history|chat-tool-only)$`，包 `./sdk/cliproxy`，`-count=1 -v -timeout=90s`。
- 执行后直接字节核对全部overlay来源为原提交；目标测试与基线一致；未覆盖修改/新增路径均为空。

## 结果

- Go test退出码1，包耗时6.512秒；10个指定叶子全部失败。
- ToolImageContinuations：history true/false × array/image-json-string/ordinary-json-string/ordinary-text，共8项；续接均409，公开原文为 `request_scoped: Kilo continuation no longer matches its original account or session`。见 `baseline-overlay/output.log:7,13,19,25,31,37,43,49`；失败汇总51-58。
- LongCallID：chat-history/chat-tool-only共2项；原提交同样先返回409，随后在期待第二条result的assertReleased处失败，见同日志64-74。日志中Active为0、只记录第一条成功result；这不等同于资源泄漏证据。
- 对照不证明第三方现场原因，不修改或降低原成功断言，也不把已知功能缺口算作公开呈现修复完成内容。

## 可追溯材料

同目录 `baseline-overlay/command.json` 保存完整argv、cwd与离线环境；`overlay.json` 保存映射；`binding.json` 保存原提交blob及字节数；`output.log` 为原始输出；`exit-code.txt` 保存1；`postcheck.json` 保存字节一致、零未覆盖路径及10叶子清单。

下一责任人：A独立记录409内部cause与既有缺口；P9决定后续功能修复排期。S继续本轮公开错误增量审查，不扩展修改续接实现。
