# Kilo公开错误发布接线增量审查

日期：2026-11-17。审查责任人：S。
结论：放行本轮固定版本打包所需的接线映射补证；不代表已发布、已部署或真实业务验收。

## 输入与映射

- 基线：`67445a35ba450468948447492b560c204afa7321`，与本轮实时核实的远端main一致。
- 产品输入：`../A/frozen-inputs.json`的18项；10个运行代码文件、8个测试文件，13修改、5新增。
- 旧合同与基线的4650项组件文件完全一致。候选映射4655项，相比基线仅这18项变化；其余4637项映射保持原样。
- 18项当前字节均匹配A冻结记录。精确组件映射使用独立临时Git索引从固定基线仅加入18项生成；未改当前索引、分支或产品文件。
- `production-wiring.contract.json`更新18项source_files、追加本轮lineage并绑定本收据；候选描述绑定准确冻结输入，不预造最终提交SHA。

## 继承证据边界

公开错误是本轮有意改变的接口呈现：code/message隐藏供应商信息并按现有内部事实分类，HTTP/SSE携带request_id。本轮不承诺公开响应与原版本完全相同。

未变化的接线包括路由注册及方法、客户端Key作用域、AuthManager接口、账号选择及执行、配置存储与普通启动装配、Manager/Worker、插件注册和BPS执行、工具续接策略、发布工作流和镜像构建文件。运行代码的变更发生在错误呈现/诊断边界；原始Error、cause、status、作用域、NoReplay及精确限制规则保留。通用handler采用Kilo错误/作用域判定，非Kilo保持原分支。

据此复用原五类生产接线记录（m10-scope、m10-joint、bps-native、bps-stream、bps-legacy）。`production-wiring.evidence.json`未写入，保留原输入身份、执行日期及工作区换行；Git差异为空。历史测试不改称本轮重跑。合同中的test_runs、coverage及既有限制保持原值。

变化影响由本轮A执行回执、S最终独立审查及V本地验收覆盖：真实HTTP/SSE、Alpha独立出口、WS升级前501、无账号/无出口/compact、非Kilo对照、内部语义及request_id关联。两平台编译复用已验输入。本轮未重复运行Go测试或构建。

## 本轮实际检查

- `validate_wiring_contract`：通过，历史五类记录与原要求完整匹配。
- `validate_source_mapping`：通过，4655项与候选精确Git树一致，映射差异恰为冻结18项。
- 冻结输入比对：18/18一致。
- 历史evidence无Git差异，原工作区换行保留；test_runs/coverage/limitations未变。

## 已知限制

原始TARGETED_RC=1、INCREMENTAL_RC=1保留；受影响分项已定向闭环，原基线10个custom-tool续接409缺口仍未修复。本地CGO_ENABLED=0双平台编译不等于生产Linux/cgo插件运行验收。第三方错误实际根因与真实账号/模型业务仍未验。

本轮无数据库动作、无服务启停、无部署、无真实账号或模型请求。下一责任人：A把本合同及本收据纳入精准白名单提交；P9核固定提交并通过既有Actions发布，流水线重新构建镜像并绑定正式提交，生产维护另行确认。
