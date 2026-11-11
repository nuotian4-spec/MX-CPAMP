# BPS 官方账号用量镜像构建

2026-11-11，构建目录 `D:/MX-CPAMP`。构建日志、实际退出码及镜像检查结果均在本目录。

| 组件 | 镜像 | OCI revision | 完整镜像ID | 结果 |
|---|---|---|---|---|
| CPA | `mx-cpa:bps-quota-repair-20261111` | `d07b4b1ed4845be0b2474a50ec75d39c254b937e` | `sha256:7adb02d9f8674f808e87af95b4c9244a6e73511551a0e6982514a60cbd3b4327` | 构建退出0 |

CPA 使用 `components/cpa/Dockerfile`，构建包含 CGO 的 `cmd/server` 编译；版本参数为 `bps-quota-repair-20261111`，COMMIT 与 OCI revision 均绑定上述完整提交。构建代理仅使用本机现有 `host.docker.internal:7890`，未写入运行环境。实际检查为 Linux amd64、运行时代理变量0项，见 `CPA-IMAGE.json`。

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
docker build --progress plain --platform linux/amd64 --build-arg 'HTTP_PROXY=http://host.docker.internal:7890' --build-arg 'HTTPS_PROXY=http://host.docker.internal:7890' --build-arg 'NO_PROXY=localhost,127.0.0.1,::1' --build-arg VERSION=bps-quota-repair-20261111 --build-arg COMMIT=d07b4b1ed4845be0b2474a50ec75d39c254b937e --build-arg BUILD_DATE=2026-11-11T10:17:20Z --label org.opencontainers.image.revision=d07b4b1ed4845be0b2474a50ec75d39c254b937e -t mx-cpa:bps-quota-repair-20261111 -f components/cpa/Dockerfile components/cpa
```

Manager/Web 最终构建及两镜像断网组合待对应候选完成；CPA 构建通过不替代组合、页面或生产业务验收。Worker 输入未变，保留既有产物。
