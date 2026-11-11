# 原 ImageRelay 与真实 HTTPS 图片契约

结论：离线专项通过。命令 `python docs/evidence/bps-relay-release-20261111/contracts/https/verify.py` 真实退出码 `0`，逐项输出见 `fixture.log`。本目录为前一份配置回执中“正向 HTTPS 待补证”的后续证据。

测试专用 Go 进程调用未改动的 `basispoints.NewImageRelay`、公共 `Rewrite(raw, scope)` 和实际 `ServeHTTP`。启动时在内存生成 2×3 PNG，Rewrite 产生随机能力 URL 与私有文件；没有 HTTP seed 入口、固定 token 或产品调试接口。产品源码与固定 CPA revision `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` 等价，等价检查见上级配置回执。

编译仅临时复制本目录 `main.go` 到 `components/cpa/cmd` 下唯一目录，以满足 Go internal 导入规则。使用缓存 Go 环境、`GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`，编译容器 `--network none`；没有产品二进制或镜像重建。

真实 Caddy 与 fixture 处于本轮新建的 Docker `--internal` 网络。`relay.example.com` 仅是该网络内 Caddy 的 DNS 别名；没有外部 DNS/域名连接或公开端口。Caddy 以 `tls internal` 签发本地证书；客户端明确加载本轮 CA，保持 `CERT_REQUIRED` 与 `check_hostname=True`，请求使用相同测试主机名。

## 实际通过

- TLS GET 返回 200，响应字节与内存生成的原 PNG 完全相同，Content-Type 为 image/png，保留 private/no-store 与 nosniff。
- 同一有效随机能力的 TLS HEAD 返回 200、空体且 Content-Length 等于 PNG 字节数。
- 未知单段能力返回 404。
- 路由保持最终 `method GET HEAD` 加 `path /api/bps-images/*`，不增加目录或 token 预校验。目录根及嵌套无效 token 转到原 ImageRelay 后返回 404，无目录/状态内容。
- POST、DELETE、OPTIONS 被 Caddy 兜底拒绝；`/state/private-state.txt`、管理路径和根目录不返回图片或私有状态。
- 服务端真实计数只有 GET_valid=1、HEAD_valid=1、GET_unknown=2、GET_directory=1；其他方法、状态目录、管理路径均未转发至 relay。
- fixture 进程收到 TERM 后退出 0；本轮容器、网络、临时 cmd 源、能力文件与 CA 均已清理，`components/cpa` 的 git diff/status 均为空。

## 证据边界

本专项证明原 ImageRelay 库与真实 Caddy TLS 路由的离线契约，不是最终生产二进制的公网抓图或官方模型业务验收。配置加载与五类固定 CPA Settings 回读没有重跑。

过期归属单项 `go test ./internal/basispoints -run '^TestIndependentImageRelayOwnersEnforceExpiry$' -count=1 -v` 在同一缓存 Linux 环境断网运行，exit 0，真实命令和输出见 `expiry.log`。源码与固定 `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` 等价；用例对已创建的实际图片 entry 设置过去的失效时间，核 GET/HEAD 返回 404、另一独立宿主的有效图片继续返回 200，并核过期存储释放。没有等待真实 30 分钟，也没有重复 HTTPS 或整包测试。
