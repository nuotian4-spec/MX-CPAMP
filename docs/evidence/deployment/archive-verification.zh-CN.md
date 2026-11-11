# 镜像归档核证

日期：2026-11-10。执行与核证：P9。产品组件与原本地验收输入一致，仅增加分发标签，不重新构建。

## 输入与归档

源镜像及实际 revision：

| 镜像 | Docker Desktop 镜像 ID | revision |
|---|---|---|
| `mx-cpa:integration` | `sha256:58d0177203d8f4e4968cebc60c59bdb0e236404a05c27656a38351dc1022ab93` | `5c90a92eef27f450c7080bc8735c88d873fc8915` |
| `mx-cpamp:integration` | `sha256:85a8f76852c08de220ba132490cbdf234b87f1bdeba91a6e0d9bfa905769a25c` | `07572357832b69a30e3ee6c45f9f0cc2c3350e0c` |
| `mx-oauth-login:candidate` | `sha256:64168881ce990f58d24736d1f8488549457ed2c775a072c1d0572bd524b7926b` | `42b04ac71df347e9c22fa35bad484c437da52d4b` |

`docker image tag` 增加三个 `20261110-rc1` 标签，随后 `docker image save --output mx-cpamp-images-linux-amd64.tar mx-cpa:20261110-rc1 mx-cpamp:20261110-rc1 mx-oauth-login:20261110-rc1`，退出 0。使用 .NET GZipStream 压缩该 tar 流；最终文件 `mx-cpamp-images-linux-amd64.tar.gz`，226493978 字节。

归档 SHA256：`89952ca57460c55cea1aa04e69c510df9bb1c6a8395777ea33cf38bee6d0fc6f`。准备脚本据此验证下载内容，匹配后才加载镜像。配套 [manifest](mx-cpamp-images.manifest.json) 同时给出每项实际镜像 ID、revision 和平台。

## 独立 Linux Docker 验证

使用已缓存 `docker:29.7.2-dind`，独立 daemon、`--network none`、临时专用数据卷、vfs 存储驱动；归档目录只读挂载。实际运行 `docker image load --input /bundle/mx-cpamp-images-linux-amd64.tar.gz`，三份镜像加载成功，退出 0。

随后 `docker image inspect` 逐项核对 `Id`、`Os/Architecture`、`org.opencontainers.image.revision`，退出 0。原始脱敏输出见 [Linux 加载身份](archive-linux-load.log)。经典存储的 config ID 与 Docker Desktop containerd 的镜像 ID 表示不同；manifest 明确列出这两种实际身份，并同时要求平台与 revision 匹配。

测试 daemon 和专用数据卷已定向删除；相同名称容器和卷的最终查询均无输出。没有清理其他容器、卷或镜像。

## 分发状态

GitHub [候选版 mx-cpamp-20261110-rc1](https://github.com/nuotian4-spec/MX-CPAMP/releases/tag/mx-cpamp-20261110-rc1) 已公开发布，API 返回 `draft=false`、`prerelease=true`，远端 tag 绑定 `3baeb518da2dbbff14befe386701a9d2815c9a9c`。两项资产均 `state=uploaded`，GitHub 归档 digest 与本地上述 SHA256 相同。manifest 资产 digest 为 `sha256:a42af112370d3e5bd2f10448c63a58a171ad9715650c87349774b8b93169d5c7`，公开下载后再次匹配。

固定提交的三个交付脚本已从公开 raw URL 实际下载，逐文件与独立审查输入相同。准备命令见 [上线说明](../../deployment-release.zh-CN.md)。生产准备和切换仍待用户执行。

产品专项与三组件合成授权集成沿用 [本地候选记录](../../local-candidate.zh-CN.md)。本记录证明归档与加载身份，不代表生产服务或真实账号业务已验收。
