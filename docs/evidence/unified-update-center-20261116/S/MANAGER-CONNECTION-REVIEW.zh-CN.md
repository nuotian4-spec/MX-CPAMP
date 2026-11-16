# Manager 更新代理与 CPA 连接引用增量审查

## 结论

PASS。P12 九文件增量已完成源码审查与专项证据核验，未发现待整改项。普通启动入口、v0 代理和直接 CPA 模式保持。此结论是本地候选代码审查，不是生产验收。

## 冻结输入

- HEAD：`9dce11c613c5bc2c45d922cd2c021a7d5bba7a25`。
- 构建标识：`9dce11c-dirty-unified-update-bound`。
- 输入：`A/P12-BUILD-INPUT.json`。
- 当前 `components/cpa`、`components/cpamp` 共 4638 个文件；路径集合及逐文件 Git blob 与 P12 一致，缺失和不匹配均为 0。
- `deploy/production-wiring.contract.json` 的 source_files 与 P12 完全一致。
- P07→P12 恰为声明的九文件增量；CPA 源码无变化，原 CPA 构建继续引用 P07。

## 审查范围

- 精确路径与方法白名单，拒绝额外段、点段及编码路径；没有 v8 通配代理。
- Manager 管理员鉴权先于代理，保存的 CPA 管理密钥覆盖上游授权头。
- 首次只读 GET 握手，同客户端并发共享连接引用；上传、预览、安装、取消、查询沿用同一引用。
- 保存的目标或密钥漂移时旧引用返回 409，专项验证原目标和新目标均无新增转发；前端不自动重绑、不重放业务请求。
- 保留 ZIP 原始请求体、查询参数、响应状态与正文；允许并暴露连接引用 CORS 头；直接 CPA 无引用模式保持。
- 普通启动入口和既有 v0 流程保持。

## 证据

- `A/P12-CONNECTION-REF-RED.log`：连接绑定专项先行失败证据。
- `A/P12-MANAGER-CONNECTION-GREEN.log`：5 项顶层测试通过，exit 0，含旧代理兼容、九种更新方法、鉴权与路径边界、并发引用与目标漂移零转发。
- `A/P12-WEB-CONNECTION-TEST-BUILD.log`：2 文件 20 项测试通过，eslint / tsc / web 构建完成，exit 0。
- `A/P12-MANAGER-FINAL-BUILD.log`：最终 Manager 构建 exit 0。
- 本轮 S 复用以上同输入证据，不重复执行已通过测试；独立完成当前路径及 Git blob 适用性核验。
- 原 S2–S5 继续引用 `S/FINAL-LOCAL-REVIEW.json` 及其关联回执，本回执补足 P12 增量。

## 已知边界

Manager 专项使用真实 Router 与合成 HTTP 上游；前端使用适配器测试，均不等于实际浏览器验收。真实 systemd / Compose、供应商许可与模型、生产现场未验。候选仍为本地未提交工作树，未推送、未部署。

## 下一责任人

A 按 `S/MANAGER-CONNECTION-REVIEW.json` 回填 contract 的 incremental_review；P9 完成实际浏览器验收。
