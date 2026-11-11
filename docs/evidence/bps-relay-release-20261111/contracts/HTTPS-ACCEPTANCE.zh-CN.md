# 正向 HTTPS 图片专项入口

当前结论：待补证。最终 CPA 二进制只注册匿名下载 GET/HEAD 路由，没有受支持的 seed 管理入口。图片能力由 `PrepareBPSImages` 内部的 `ImageRelay.Rewrite` 产生；本轮不触发官方请求、不新增产品调试接口。

现成可复用的真实实现及 helper：

- `components/cpa/internal/basispoints/image_relay_test.go`：`relayTestPNG` 在内存生成 PNG；`relayTestRequest` 构造图片请求；`ImageRelay.Rewrite` 创建随机能力与私有文件；`relayTestURL` 取实际返回的 URL。
- 同文件 `TestImageRelayRoundTripAndScope`：已有 ServeHTTP GET 字节、HEAD 空体及跨 scope 不复用能力的单进程测试。
- `components/cpa/internal/basispoints/image_relay_owner_expiry_test.go`：`TestIndependentImageRelayOwnersEnforceExpiry` 对实际存在的 entry 设置过期时间，再用 ServeHTTP 检查 GET/HEAD 404。

最小下一动作是给 `internal/basispoints` 加测试专用 Go overlay fixture，保留产品源码和最终二进制：

1. 在临时目录用原 `NewImageRelay`、内存 PNG 与 `Rewrite` 生成新能力，测试 HTTP 服务直接调用原 `ServeHTTP`。随机 URL 只写临时交换文件，不写死 token。
2. 启动缓存 Caddy，使用内部隔离网络和本地测试 CA，保留拟发布的 GET/HEAD relay → CPA 路由形状；不使用生产域名、生产证书或外部 DNS。
3. 客户端信任测试 CA 并校验测试主机名，通过真实 TLS 获取 URL：GET 200 且字节等于 PNG，HEAD 200、Content-Length 一致且空体。
4. 对同一已成功 URL 只在测试进程内把其真实 entry 设为过期；随后 GET/HEAD 都是 404。随机未知 token、目录根、状态目录/管理路径和非 GET/HEAD 方法均不返回图片或文件内容。
5. 删除临时容器、网络、交换文件和测试 CA，记录实际退出码。此证据只能证明原 ImageRelay 实现与真实 Caddy TLS 路由契约，不替代最终二进制的生产外部取图或官方模型业务验收。

本轮没有执行上述接线 fixture；已有未知 token 404、源码单进程测试及配置回读均不升级为正向 HTTPS 取图通过。
