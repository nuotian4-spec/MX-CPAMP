# BPS图片宿主与管理设置

日期：2026-11-11。基线：`3a2633a2c6246df9e3079d449aac8ee23e5f273d`。B线分支：`codex/bps-images-settings`。产品输入依次为配置契约`2837ae9`、图片宿主与CPA管理`e602335`、有效路径隔离`7ab7ff3`、Manager与页面`2a3e933`、附件回执契约`d733e68`；`993eaf8`增加多图上传失败专项。

## 配置与入口

协议和图片默认开启，默认`native`。四个管理字段为`enabled`、`images_enabled`、`image_transport`、`image_relay_base_url`；固定限制只读。CPA管理入口为`GET/PUT /v8/management/bps/settings`，沿用v8管理鉴权；Manager入口为`GET/PUT /api/bps/settings`，需要Manager管理员认证。

Manager与凭证并发页面共用实例引用服务。读取设置时颁发`connection_ref`；保存前在共享写入互斥内重核CPA地址、管理密钥和引用。连接切换后，旧页面在新目标上产生零次写入。浏览器只持有Manager管理员凭据，CPA管理密钥由服务端取用，响应按字段白名单输出。

保存先克隆配置、落盘，再同步发布独立运行快照；在途请求保留原快照。新增开关的显式`false`保留到YAML，重载后仍为关闭。管理页面位于系统页，显示关图拒绝范围、传输模式、HTTPS基址和固定限制。

v8配置示例：

```yaml
upstream:
  bps:
    protocol:
      enabled: true
      images-enabled: true
      image-transport: native
      image-relay-base-url: ""
      state-dir: /state/bps-state
```

`config.BPSStateDirectory(cfg)`统一执行空白裁剪、`~`展开、clean和绝对路径解析。缺省目录为有效`auth-dir`父目录下的`bps-state`，例如`/state/auths`对应`/state/bps-state`。默认与显式目录均拒绝进入凭证扫描树；若`auth-dir`自身名为`bps-state`导致默认同名，应显式配置另一个私有目录。工具绑定使用`bindings`，图片使用`image-relay`，两者独立。

## 图片处理

`helps.PrepareBPSImages(ctx,cfg,auth,raw,scope)`在原凭证准入后处理规范化Responses请求，返回供体wire、Bridge和释放函数。完整协议与全部内联图片在上传前校验。普通消息内联图片上传为BPS附件；上传成功严格读取供体`openai_file_id`，回执上限64KiB。附件上传沿用供体60秒时限和固定`Mozilla/5.0` User-Agent；更短父截止时间优先，时限覆盖HTTP请求与响应体读取，结束后释放子context。工具内联截图经校验后保留在工具结果中，继续由原生图片计划进入协议，保留调用关联、文字和顺序。

原生附件缓存只保留附件ID，沿用供体30分钟/512项限制；键作用域包括受信调用者/会话、CPA凭证引用、上游账号及令牌。空会话作用域禁用跨请求复用。同请求重复图片只上传一次。任意图片校验或上传失败时不产生模型wire，不自动重放、不换凭证；多图第二次上传失败也直接收口。

关闭图片时，当前消息、历史消息和工具结果中的内联图片、HTTPS图片、原生`file_id`均明确拒绝，原始历史保持不变。工具参数和纯文本中的图片字样不按图片输入处理。最终payload规则之后的只读开关验证由A线宿主调用负责。

`relay`必须有有效HTTPS origin，不接受凭据、路径、查询或片段。中转沿用供体私有磁盘存储及随机能力链接，仅提供`GET/HEAD /api/bps-images/:token`。没有公开上传、目录浏览或静态文件映射。开关变更不注销在途链接；GET返回图片，HEAD无正文，未知/过期能力为404。

固定限制：TTL30分钟；每图20MiB、64MP；每请求最多20张，已解码内联图片合计32MiB；每存储目录1GiB、512项。链接有效性与存储容量、过期和孤儿清理由供体核心负责。外部HTTPS图片和既有`file_id`沿供体交给上游，本站不下载其内容，因此字节及像素预检适用于内联图片。临时能力元数据只在进程内保存，重启后旧链接失效，遗留文件按供体策略清理。

## 验证

固定离线Go：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/gomodcache/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe`。环境：`GOTOOLCHAIN=local`、`GOMODCACHE=C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/gomodcache`、`GOPROXY=off`、`GOSUMDB=off`。

| 验证 | 命令 | 结果与证据 |
|---|---|---|
| 开关默认/显式false、独立克隆、HTTPS origin、默认/显式状态隔离 | CPA目录：`go test ./internal/config -run TestBPSProtocol -count=1` | exit0，`7ab7ff3`输入；[配置路径证据](config-path-tests.log) |
| 落盘与即时快照、并发读取、附件缓存/回执/截图/关图/多图/中转 | CPA目录：`go test ./internal/api/handlers/management ./internal/runtime/executor/helps -run 'TestBPSSettings\|TestBPSImage' -count=1` | exit0，`993eaf8`输入；[图片宿主证据](cpa-host-tests.log) |
| 附件60秒截止、较短父截止、供体User-Agent、响应头/响应体阶段父取消及同作用域释放 | CPA目录：`go test ./internal/runtime/executor/helps -run 'TestBPSImageUploadContextDeadline\|TestBPSImageUploadParentCancellation' -count=1 -timeout 20s` | exit0；本提交附件上下文实现与专项输入，见[限时上传证据](cpa-upload-context-tests.log) |
| 真实CPA路由鉴权与只读图片入口 | CPA目录：`go test ./internal/api -run TestBPSSettingsAndImageReadRoutes -count=1` | exit0；[路由证据](cpa-routes-tests.log) |
| Manager管理员门禁、字段白名单、旧连接引用零写入、HTTPS前置拒绝 | Manager目录：`go test ./internal/service/credentialconcurrency ./internal/http/controller/bpssettings ./internal/http/router -run TestBPSSettings -count=1` | exit0；[Manager证据](manager-tests.log) |
| 供体图片TTL、容量、读权限、尺寸及工具图片顺序 | A线：`go test ./internal/basispoints ./internal/basispoints/urlvalidator -count=1 -timeout 120s` | 复用A线同核心输入记录；图片项未报失败，整包exit1，见下方已知问题与[核心日志](../bps-protocol/core-tests.log) |
| 页面保存/HTTPS/切换作用域及API固定入口 | `BPSSettingsCard.test.tsx`、`bpsSettings.test.ts` | 已加入专项；本工作树无既有Node依赖，未执行。最终统一Manager构建含tsc与Vite，由主会话执行 |

路由证据与Manager证据绑定的产品输入在后续仅有测试或注释变化，适用证据复用。原图片宿主日志绑定`993eaf8`；附件上下文与User-Agent变化由新增限时专项覆盖，其余宿主输入未变。配置路径专项绑定`7ab7ff3`，该配置之后未变。本地测试全部使用临时目录与合成凭证，未真实登录或调用模型，未推送或部署。

## 已知问题与发布边界

A线核心整包存在三项基线失败：`TestCatalogContractLongHistoryMismatchFailsClosed`、`TestFunctionCodeBranchCatalogAndReplay`、`TestCodexThreadDelegationPreservesPairedTools`；A线用原始stream输入定向重跑仍失败，见[基线反例日志](../bps-protocol/core-baseline-three.log)。本记录不将核心整包列为通过，结论由主会话独审收口。

后续发布需新增CPA私有状态持久挂载，并使公开HTTPS中转路由落到持图CPA实例。原生模式不要求公开图片域名；只启用relay时才需要公开读取路由。此线未修改生产Caddy或部署配置。没有SQL/schema迁移；运行时写入为协议设置和临时图片，A线另有最小工具绑定记录。最终统一CPA/Manager构建、隔离健康与真实业务验收由主会话负责。
