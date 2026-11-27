# SIWC Web 与最终联合构建

SIWC 已接入真实 Codex 账号详情的“配置”区域。入口保留绑定/重新绑定、授权链接、完整回调、状态查询与取消；Home、运行时及非文件来源只读。服务端明确过期可原位重建；明确回调输入拒绝可纠正，响应不明只查状态。

## 验证

- 有效专项 20 项：`final-unit-results.json` 仅复用 API 的 6 个 passed（该次组件 mock 装载失败，文件整体不为 PASS）；`final-component-results.json` 组件 13 个 passed；`state-mismatch-results.json` 新增错 state 单项 passed。最后单项的 13 个筛选 skip 复用此前同输入证据。原始日志均保留。
- `WEB-IMPORT.json`：12 个 Web 文件已核原始字节，精确导入联合树。
- `typecheck-final.log`、`build-final.log`、`manager-build-final.log`、`manager-build-exit.txt`：唯一最终 tsc → Vite → embed → Linux Manager 构建全部 exit 0。使用现有 Node 22.23.4、Go 1.26.10，离线模块缓存；未重建 CPA。
- `BUILD-RESULT.json`：构建顺序、组件树、Linux 成品与嵌入页面字节相等记录。
- `VISUAL-RESULT.json` 及四张 JPEG：真实 AppRouter / AccountsPage 详情，1280×900 与 390×844 明暗主题；授权、回调与操作区无横向溢出，手机可滚动触达。临时预览已停止，浏览器和视口已收回。
- `WIRING-DELTA.json`：5125 项最终来源映射，仅 20 项 CPAMP 增量；CPA 所有映射保持原值。独立审查通过后已写入最终合同，合同和来源映射校验通过（`WIRING-FINAL.json`）。

## 已知边界

视觉使用合成账号与 HTTP 回执，未访问真实授权页或调用模型。背景列表连接红条源于合成后端未覆盖 `/api/bps/settings` 的 connection_ref，触发既有 `bpsCredentialSnapshot.ts:17` 检查；截图保留此提示。真实 CPA→Manager 成品接线证据由后端席单独提供。

本地候选已构建，未提交、推送或部署。下一责任人：P9 汇总裁定。
