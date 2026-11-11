# OAuth 输入诊断候选

日期：2026-11-11。本次用户报告三处登录失败，真实窗口结果见[用户回执](../manual-bps-oauth-20261111/USER-RESULT.zh-CN.md)。本候选为本地修复，没有推送或部署，也没有追加真实登录、回调、刷新或模型请求。

## 修复范围

- 认证配置查询仅返回 `password_login_configured`，不探活、不触发登录。Worker未配置、状态未知、查询失败或切换实例时，两处2FA表单禁用并显示原因；已有批次查询/取消与手工BPS独立保留。
- CPA明确回调输入400按既有白名单保留为400及稳定诊断码，不再统一映成502。未知400、5xx、网络异常及坏响应仍为结果未知，提交锁保持，防止自动重复兑换。
- 错误体只有在4096字节以内、完整单个JSON且到EOF时才可采用白名单。部分解码、尾随对象或超长体不误判明确拒绝。页面说明官方回调地址和授权链接的区别。

产品源提交 `aff9fe08b94158521e936ebf707bd57b517258b7`，主线对应 `9914927a0d30809da850e157e8162d2b29493fc2`。之后仅修测试 `act` 返回类型，源提交 `1fc9d776021025ed50ea544a41e3791d52926e55`、主线 `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44`，产品行为不变。

## 专项与独审

[专项记录](../../../components/cpamp/tests/evidence/oauth-input-diagnostics-20261111.md)及[工具输出摘录](../../../components/cpamp/tests/evidence/oauth-input-diagnostics-20261111-output.txt)：真实Service/Handler本机假CPA红测命中旧502与能力404；修复后Go两包、Web三文件30项、相关格式/lint均exit0。真实Worker集成按原开关跳过，不冒称全仓测试。独审核源码、鉴权、白名单、未知锁、实例切换及测试断言通过，无待处理P1/P2，未机械重复执行专项。

首Manager构建因新增测试的React `act` 回调返回 `Promise<VitestUtils>` 出现TS2769，exit1；原日志保留为 [首构建](mx-oauth-input-diagnostics-manager-build-20261111.log)。最小测试类型修复后，单条专项1通过20跳过、格式exit0，独审通过；未重跑Go及全部Web测试。

## 最终产物

原Dockerfile最终构建包含 `tsc && vite build` 与Go编译，exit0，见[最终构建](mx-oauth-input-diagnostics-manager-build-final-20261111.log)。运行镜像没有代理环境项，构建代理只用于本机依赖获取。

| 项目 | 值 |
|---|---|
| 镜像 | `mx-cpamp:oauth-input-diagnostics-20261111` |
| 实际ID | `sha256:cdf235bee2e22f2c33af145b8fb4d27e93b4478162b8738dcadd3595f3b74c4a` |
| revision | `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44` |
| 平台 | linux/amd64 |

CPA与Worker产品目录未变，复用各自原产物和适用证据。仅构建Manager；没有修改原分发包、旧manifest或生产发布状态。

[候选无凭据预检](../manual-bps-oauth-20261111/CANDIDATE-PREFLIGHT.jsonl)exit0：实际镜像身份一致，capabilities无key401/有key200且false，嵌入页面两项文案存在，空池保存和注册均0，临时容器归属清理确认。该项不是浏览器渲染验收，不创建OAuth会话、不登录或调用模型。

## 接续

用户本次回调输入没有采集，当前具体拒绝原因仍待证；离线400缺陷不反推为本次现场根因。旧手工窗口与访问密钥已清理，没有待用户继续提交的有效会话。

当前责任人：主会话，已完成本地修复、专项、独审、产物和无凭据预检。下一步仅确认用户当时填入的是官方登录后的回调地址还是授权链接，不收集完整地址。若再安排真实授权，另行确定一次范围，不复用旧会话或截图凭据。

无SQL/schema迁移、数据清理或生产操作。原二期分发包和发布工具仍绑定旧Manager，不能用于发布此新候选；完整业务余项及发布重新绑定仍待完成，`write_release_ready=false`。
