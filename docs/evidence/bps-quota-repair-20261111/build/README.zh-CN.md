# BPS 官方账号用量镜像构建

2026-11-11，构建目录 `D:/MX-CPAMP`。构建日志、实际退出码及镜像检查结果均在本目录。

| 组件 | 镜像 | OCI revision | 完整镜像ID | 结果 |
|---|---|---|---|---|
| CPA 首候选 | `mx-cpa:bps-quota-repair-20261111` | `d07b4b1ed4845be0b2474a50ec75d39c254b937e` | `sha256:7adb02d9f8674f808e87af95b4c9244a6e73511551a0e6982514a60cbd3b4327` | 构建退出0，组合身份断言失败，保留为历史产物 |
| CPA 最终 | `mx-cpa:bps-quota-identity-20261111` | `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` | `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b` | 构建及最终组合退出0 |
| Manager | `mx-cpamp:bps-quota-repair-20261111` | `d26cbf7595ecf41ba8cbfd90c956291ac351b471` | `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd` | 构建退出0 |

CPA 使用 `components/cpa/Dockerfile`，构建包含 CGO 的 `cmd/server` 编译；版本参数为 `bps-quota-repair-20261111`，COMMIT 与 OCI revision 均绑定上述完整提交。构建代理仅使用本机现有 `host.docker.internal:7890`，未写入运行环境。实际检查为 Linux amd64、运行时代理变量0项，见 `CPA-IMAGE.json`。

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
docker build --progress plain --platform linux/amd64 --build-arg 'HTTP_PROXY=http://host.docker.internal:7890' --build-arg 'HTTPS_PROXY=http://host.docker.internal:7890' --build-arg 'NO_PROXY=localhost,127.0.0.1,::1' --build-arg VERSION=bps-quota-repair-20261111 --build-arg COMMIT=d07b4b1ed4845be0b2474a50ec75d39c254b937e --build-arg BUILD_DATE=2026-11-11T10:17:20Z --label org.opencontainers.image.revision=d07b4b1ed4845be0b2474a50ec75d39c254b937e -t mx-cpa:bps-quota-repair-20261111 -f components/cpa/Dockerfile components/cpa
```

Manager 产品源提交 `28258fd65ab0dff4a68dc470b8193e22ade6d076` 与整合提交 `d26cbf7595ecf41ba8cbfd90c956291ac351b471` 的 `components/cpamp` 比较退出0。一次构建包含 `tsc && vite build` 及 Manager Go 编译，均通过，见 `MANAGER-BUILD.log`、`MANAGER-BUILD.exit` 和 `MANAGER-IMAGE.json`。构建命令如下：

```powershell
docker build --progress plain --platform linux/amd64 --build-arg 'HTTP_PROXY=http://host.docker.internal:7890' --build-arg 'HTTPS_PROXY=http://host.docker.internal:7890' --build-arg 'NO_PROXY=localhost,127.0.0.1,::1' --build-arg VERSION=bps-quota-repair-20261111 --build-arg SOURCE_COMMIT=d26cbf7595ecf41ba8cbfd90c956291ac351b471 -t mx-cpamp:bps-quota-repair-20261111 -f components/cpamp/Dockerfile.manager-server components/cpamp
```

首次组合 `integration/RUN.exit=1`，在快照精确身份断言停止，归属清理通过。产品根因在 CPA BPS 列表遗漏平铺 `account_id`；最小修复和双公开入口专项已整合至 `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c`，仅重建受影响 CPA。Manager 与 Worker 产品输入不变，复用已有产物；Worker 实际身份及源码路径比较见 `WORKER-REUSE.json`。

最终 CPA 构建参数为 `COMMIT=b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c`、`BUILD_DATE=2026-11-11T10:46:31Z`，标签为 `mx-cpa:bps-quota-identity-20261111`，其余命令与首次 CPA 构建相同。实际输出见 `CPA-IDENTITY-BUILD.log`、`CPA-IDENTITY-BUILD.exit=0` 及 `CPA-FINAL-IMAGE.json`。两最终镜像运行时代理变量均0项。

最终组合 `integration/RUN-FINAL.exit=0`，镜像身份、精确账号身份、BPS 快照写查及归属清理全部通过。该结论为断网合成产品验收；官方来源成功另见 source 回执，新产品真实账号页面及生产验收尚未执行。
