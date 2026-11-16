# Manager 正式更新接缝交付

## 正式路径

网页更新请求经过当前 Manager 管理员鉴权，仅代理以下 CPA v8 更新路径与方法：

- 插件导入：POST `plugins/imports`，GET/DELETE `plugins/imports/:id`，POST `plugins/imports/:id/install`。
- 任务读取：GET `updates/operations/:id`，包含 `latest` 的 type 查询参数。
- 核心更新：GET `updates/core`，POST `updates/core/previews`，POST `updates/core/previews/:id/install`。

Manager 使用已保存 CPA 地址与管理密钥，保持请求路径、查询参数、原始 ZIP 字节、响应状态和正文。未开放整个 v8 管理面；配置、凭证、商店等无关 v8 路由继续隔离，既有 v0 声明不变。

## 连接绑定

复用现有 `X-CPA-Connection-Ref`、`CredentialConcurrencyService` 和代理事务 guard，不引入新的连接存储。API 实例第一次发起只读握手，并发首次请求共享同一 Promise；返回引用后，该实例的上传、候选生成、安装、取消和任务读取都固定携带该引用。

Manager 在转发前及响应完成时核对保存的 CPA 地址与密钥。同一 Manager 更换 CPA 连接后，旧引用返回 409，不向旧或新 CPA 重放操作。前端收到业务请求 409 后保留原引用，不自动重绑或重新安装。直接连接 CPA 时，握手不返回 Manager 引用，后续保留直接 CPA 的请求方式。

引用由 Manager 进程持有。Manager 真正重启后，旧页面引用失效；重新打开页面取得新引用，再读取持久化任务及原操作 ID。任务刷新按钮不会静默绑定另一 CPA。

## 精确输入与构建

- 基线 HEAD：`9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`；未提交本地候选。
- 最终输入：`P12-BUILD-INPUT.json`，4638 个 component 文件，较 P07 为 9 个相关增量路径。
- 仅重建 web 与 Manager；CPA 继续使用 P07 已验证产物。
- 最终 web：`C:/Users/子羽/.codex/tmp/unified-update-20261116/web-seam/index.html`。
- 最终 Manager：`C:/Users/子羽/.codex/tmp/unified-update-20261116/cpa-manager-plus-bound`，revision 标签 `9dce11c-dirty-unified-update-bound`，嵌入上述最终 web。
- 构建仍为本机锁定依赖、`--pull never --network none`，没有外部依赖请求。

## 专项回执

| 范围 | 结果 | 真实证据 |
| --- | --- | --- |
| Manager 新正式接缝及相邻原代理 | 5 个顶层测试 PASS，含 9 个转发向量、15 个拒入向量，以及并发引用/换靶后六类操作零转发 | `P12-MANAGER-CONNECTION-GREEN.log`，`423742`，exit 0 |
| 前端 API 与原页面接线 | API 9 + 页面 11，20/20 PASS；仅相关两 API 文件 ESLint、Prettier 与 TypeScript 通过 | `P12-WEB-CONNECTION-TEST-BUILD.log`，`0fbd05`，exit 0 |
| 最终 web 构建 | PASS | 同上，exit 0 |
| 最终 Manager 构建 | PASS | `P12-MANAGER-FINAL-BUILD.log`，`15c0e0`，exit 0 |
| 实际 Manager → CPA → Unix 宿主 discovery | latest 200、32 字符连接引用、核心 discovery 200，site=fixture | `P12-LIVE-MANAGER-CPA.log`，`d26d66`，exit 0 |
| 实际 CPA 直连握手 | 200，不返回 Manager 引用 | 同上 |

原 P01–P09 未重复运行。P11、P12 的定向红灯日志保留为接缝回归测试的检出证据；最终构建与输入以 P12 为准。

## 独立浏览器入口

- 容器：`unified-update-manager-browser-20261116`。
- 同一 internal 网络内地址：`http://unified-update-manager-browser-20261116:18317`，IP `10.253.16.3`。
- 初次启动：容器 PID 1，Docker 宿主 PID 4130；后续重启以实际 inspect 为准。
- 只读挂载最终产物；持久化合成数据：`C:/Users/子羽/.codex/tmp/unified-update-20261116/preview/manager-browser-data`。
- 原 CPA/Unix 宿主容器 `unified-update-preview-20261116` 保持运行，其任务、门闩和合成材料不变。
- 合成管理凭据保持 `synthetic-admin-only`，不是真实账号或供应商凭据。

S 已完成本次九文件增量与 4638 文件精确映射审查，结论 PASS；真实回执为 `docs/evidence/unified-update-center-20261116/S/MANAGER-CONNECTION-REVIEW.json`。`production-wiring.contract.json` 的 `incremental_review` 已按该回执回填。

下一责任人：P9 完成真实浏览器 ZIP、核心固定候选与独立 Manager 停启验收。容器内 HTTP 不替代浏览器验收；合成 Docker/发行边界不替代真实 systemd、站点维护或供应商业务验收。
