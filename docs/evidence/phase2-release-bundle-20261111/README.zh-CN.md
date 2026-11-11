# 二期镜像分发候选

日期：2026-11-11。本地源码基线 `0b890099af09d9e0819f07d97d3ccbb44618fc95`。三个产品目录未改动，复用[固定产物与业务证据](../bps-tool-diagnostic-20261111/FINAL.zh-CN.md)，没有重新构建、登录或模型请求。

## 归档

本地目录：`C:/Users/子羽/.codex/artifacts/mx-cpamp-release-phase2-20261111-candidate`。

- 文件：`mx-cpamp-images-linux-amd64.tar.gz`，227124363 字节。
- SHA256：`99362d58a8c182cf9266fc64632dbdbce67f20085246fad9743cee2c5652f744`。
- `docker image save` 导出三个固定 tag，由 Node gzip（level 6）流式写入。Docker exit0、gzip 完成、stderr 无输出；时间及退出状态见 [pack-meta.json](pack-meta.json)。
- 大归档留在本地 artifacts，不进入 Git；仓库保存 manifest 和小型验证回执。

## Linux 实际加载

使用缓存 `docker:29.7.2-dind`，镜像完整 ID 为 `sha256:3ef33f2e220b79ed3ef3b99d81746f06f306cd6340e2cb7331d17ae996e74cb6`。独立 daemon、network none、vfs、专用临时卷，没有挂载宿主 Docker socket。

实际执行 `docker image load --input /bundle/mx-cpamp-images-linux-amd64.tar.gz`，随后逐一 inspect 三个 tag，核对 linux/amd64、revision 及实际 ID；exit0，临时 daemon 和卷定向清理确认。见 [archive-linux-load.log](archive-linux-load.log)。

| 组件 | 经典 Linux 引擎加载后的实际 ID |
|---|---|
| CPA | `sha256:8a47d757034a09d391188113a5b72a745149f87da880a5313e2fb558b5d2512e` |
| Manager | `sha256:7c1e360f0ad697560eb1d4bcca60ee40782be6ff5d7b72dcc89a85ce225b3d8e` |
| Worker | `sha256:30689442b2f61b2c7ff27eae008291b014fe7161e842a60202a6574c912be4dd` |

[manifest](mx-cpamp-images.manifest.json) 为 schema 1，每组件只登记已验 Desktop 与经典 Linux 两种完整 ID。它们来自同一分发归档，不是两次产品构建。发布工具须同时核固定列表、平台和 revision，再用现场实际不可变 ID 启动；不接受任意 tag 同名镜像。

## 结论与责任

分发归档及 Linux 加载通过；这不是 OVH 加载、正式切换或完整业务验收。发布工具真实边界与余项仍由主会话收口，未推送、上传 Release 或部署。生产不存在本次新备份，不使用旧 rc1 备份替代。
