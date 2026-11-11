# BPS 官方回调兼容候选

日期：2026-11-11。本地修复已通过专项、独审、构建及无凭据预检；手工BPS OAuth的真实保存注册仍待验。生产未改动，未推送或重新分发归档。

## 已核问题与修复

用户确认填写的是官方登录后的BPS回调。只读现有官方页签得到[非秘密结构](../manual-bps-oauth-20261111/CALLBACK-SHAPE.json)：固定origin/path、code/scope/state参数名、无fragment，未读取参数值。旧CPA查询白名单漏了scope，导致该结构在换码前被拒绝为`invalid_callback_query`。原请求HTTP拒绝码未保留，现场结果与代码路径证据分别记录。

产品仅在白名单接受单值scope，不将其用于身份、权限或令牌交换；重复scope、未知参数及既有来源/state/client/iss校验仍生效。固定供体`0ef3ce8fe2db1193eeb4cd93f9db9301b4252982`的授权scope相同，回调只提取code/state，不拒绝scope。

产品及测试源提交`ebca93d7c8381530a308ab19df72fef3fdc2ed2a`，主线对应`6376dd7`；专项证据源提交`2ccf7ab0aa4c559a56bda56036f4a19cfa77a206`，主线`346b9479e12bc45ccb0e72556acc4cad4e9a0a04`。

## 验证与审查

- [handler专项](README.zh-CN.md)：断网Linux Go1.26.11，真实Start→Callback→Status，先测旧行为exit1，最小修复后exit0。单值scope合成换码/凭证文件/运行时注册各一次；重复及未知参数400且零换码；原ValidationAndCompletion通过。仅令牌HTTP使用合成响应，不代表真实OAuth成功。
- 独立只读审查核上述产品diff、红绿退出码和最终测试断言，无阻断；未重复运行专项或构建。
- [原Dockerfile构建](CPA-BUILD.log)真实[exit0](CPA-BUILD.exit)，包含CGO Linux `cmd/server`编译。构建提交为`346b9479e12bc45ccb0e72556acc4cad4e9a0a04`，仅重建CPA。
- [一次组合预检](PRE-FLIGHT.zh-CN.md)真实exit0：镜像身份、health200、capabilities未认证401/已认证200且false、静态页面提示、保存和运行时空池，以及归属清理全部通过。该项不是渲染页面或真实登录验收。

Manager自`7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44`、Worker自`95dd9ca2c16d5469c43ce7c910f38ff271af45e3`至本轮整合后的各自产品目录无diff，复用原镜像及适用证据。CPA与执行席最终专项输入逐目录无diff，不重复全包测试。

## 固定产物

| 组件 | 镜像 | 完整ID | revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-callback-scope-20261111` | `sha256:d96e26782b4b080a703ee9668bf1c653e18cf164035b2bd9e11bcac2d11023fd` | `346b9479e12bc45ccb0e72556acc4cad4e9a0a04` |
| Manager | `mx-cpamp:oauth-input-diagnostics-20261111` | `sha256:cdf235bee2e22f2c33af145b8fb4d27e93b4478162b8738dcadd3595f3b74c4a` | `7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44` |
| Worker（本轮未启动） | `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

CPA实际linux/amd64，运行镜像代理环境项0，见[身份记录](CPA-IMAGE.json)。构建代理只用于本机获取依赖。

实际构建命令（工作目录`D:/MX-CPAMP`，已执行，不需重复）：

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
docker build --progress plain --build-arg 'HTTP_PROXY=http://host.docker.internal:7890' --build-arg 'HTTPS_PROXY=http://host.docker.internal:7890' --build-arg 'NO_PROXY=localhost,127.0.0.1,::1' --build-arg VERSION=bps-callback-scope-20261111 --build-arg COMMIT=346b9479e12bc45ccb0e72556acc4cad4e9a0a04 --build-arg BUILD_DATE=2026-11-11T05:56:51Z --label org.opencontainers.image.revision=346b9479e12bc45ccb0e72556acc4cad4e9a0a04 -t mx-cpa:bps-callback-scope-20261111 -f components/cpa/Dockerfile components/cpa
```

## 接续

当前责任人：主会话，本地兼容修复完成。新版[手工验收入口](scope-manual-run.mjs)仅替换已审旧入口的双镜像固定身份与独占label，保留15分钟、tmpfs、私有访问材料、只读status/verify及归属stop；语法与差异核验通过，未执行start。

用户已明确确认手工BPS OAuth一轮：官方授权后提交一次回调，核唯一凭证文件、运行时注册、profile/client及令牌存在性后立即清理；不主动刷新、不调用模型、不启动Worker、不操作OVH。启动命令在CreateProcess前被工具自动审批拒绝，仅返回`blocked by policy`，脚本未执行；见[启动状态](MANUAL-START-STATUS.json)。只读核本轮label容器为0、START及exit回执均不存在，没有新授权窗口或账号导入。

下一责任人：用户在本机Windows PowerShell运行已审`scope-manual-run.mjs start`，仅回传非秘密ready JSON；主会话据真实入口继续核空池、交付手工操作、保存注册核验及归属清理。此前一次范围授权仍有效，无需重复确认。旧会话、地址、授权码及截图凭据不复用，当前没有已启动的入口。

此前两处2FA错误来自未配置Worker的手工专项窗口，诊断Manager已禁用相应入口；该修复不替代Codex正向2FA。Codex历史`security_check`、HTTPS中转及OVH现场仍待验。无SQL/schema迁移、数据清理或生产写入；旧分发包尚未重新绑定新CPA/Manager，`write_release_ready=false`。
