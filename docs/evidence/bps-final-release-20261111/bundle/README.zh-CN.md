# BPS 最新三镜像分发包

2026-11-11。源码基线 `c2adc4ad92a2bf564bf8dfcbaa40aee78d8c9ff3`，分支 `codex/bps-final-bundle-20261111`。复用[最终候选固定产物](../../bps-quota-repair-20261111/FINAL.zh-CN.md)，本轮只导出与验证分发归档，产品目录未改动，没有重建镜像、真实登录或模型调用。

## 归档

本地目录：`C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111`。

- 文件：`mx-cpamp-images-linux-amd64.tar.gz`。
- 大小：227177346 字节。
- SHA256：`256ddca4ec162bc1249ccea95ca1e36ce536b4909fa1ac89b77af1b477910c27`。
- 相邻文件：`mx-cpamp-images.manifest.json`、`pack-meta.json`、`archive-linux-load.log`。
- 打包：`docker image save` 三个固定 tag，由 [pack.mjs](pack.mjs) 以 gzip level 6 流式写入，Docker exit0、gzip 完成、stderr 无输出；见 [pack-meta.json](pack-meta.json) 和 [pack.exit](pack.exit)。
- SHA 对整个压缩归档计算，与现有发布工具 `digest()` 消费的字节一致；独立 `Get-FileHash -Algorithm SHA256` 比对通过，见 [archive-verify.json](archive-verify.json)。
- 旧 `mx-cpamp-release-phase2-20261111-candidate` 归档保留原路径、原大小 227124363 字节，未覆盖。大归档仅存 artifacts，不进入 Git。

## 固定身份

[schema 1 manifest](mx-cpamp-images.manifest.json) 只登记本轮真实核过的 Desktop 与经典 Linux 完整 ID。三个组件均为 `linux/amd64`；同一归档在两种镜像存储后端的实际 ID 分别记录，不代表两次产品构建。

| 组件 | tag / revision | Desktop 实际 ID | 经典 Linux 实载 ID |
| --- | --- | --- | --- |
| CPA | `mx-cpa:bps-quota-identity-20261111` / `b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c` | `sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b` | `sha256:09d30cc6bc3808a07b06953faecb0bb49036a5240513132e6cb8b7c035b1b335` |
| Manager | `mx-cpamp:bps-quota-repair-20261111` / `d26cbf7595ecf41ba8cbfd90c956291ac351b471` | `sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd` | `sha256:a7b21c514227d3c48426b8cb1bb443cc8d5ca8000c626da190099ef922c5b247` |
| Worker | `mx-oauth-login:phase2-local-20261111` / `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `sha256:30689442b2f61b2c7ff27eae008291b014fe7161e842a60202a6574c912be4dd` |

原始身份摘录：[desktop-images.json](desktop-images.json)、[linux-images.json](linux-images.json)。

## 经典 Linux 实际载入

复用缓存 `docker:29.7.2-dind`，完整 ID `sha256:3ef33f2e220b79ed3ef3b99d81746f06f306cd6340e2cb7331d17ae996e74cb6`；独立 daemon 的 `Driver=vfs`、版本 29.7.2，见 [linux-daemon.json](linux-daemon.json)。

本轮容器 `mx-bps-final-archive-load-20261111` 使用 `network=none`、零发布端口、只读归档目录和专用数据卷 `mx-bps-final-archive-load-20261111-data`，无宿主 Docker socket 挂载。见 [linux-fixture.json](linux-fixture.json)。未启动三个产品容器。

真实命令顺序：

```powershell
docker volume create mx-bps-final-archive-load-20261111-data
docker run -d --pull never --name mx-bps-final-archive-load-20261111 --privileged --network none --mount type=volume,src=mx-bps-final-archive-load-20261111-data,dst=/var/lib/docker --mount "type=bind,src=C:/Users/子羽/.codex/artifacts/mx-cpamp-release-bps-final-20261111,dst=/bundle,readonly" --entrypoint dockerd docker:29.7.2-dind --storage-driver=vfs --iptables=false --ip6tables=false --bridge=none --host=unix:///var/run/docker.sock --tls=false
docker exec mx-bps-final-archive-load-20261111 docker image load --input /bundle/mx-cpamp-images-linux-amd64.tar.gz
docker exec mx-bps-final-archive-load-20261111 docker image inspect mx-cpa:bps-quota-identity-20261111 mx-cpamp:bps-quota-repair-20261111 mx-oauth-login:phase2-local-20261111
docker rm -f mx-bps-final-archive-load-20261111
docker volume rm mx-bps-final-archive-load-20261111-data
```

载入 [archive-linux-load.log](archive-linux-load.log)、[exit0](archive-linux-load.exit)；身份核验 [exit0](archive-verify.exit)；定向清理 [cleanup.json](cleanup.json)、[exit0](cleanup.exit)。清理后容器和卷均不存在，新归档保留。

## 交付边界

分发归档与经典 Linux 实载通过。下一责任人：A 绑定新发布入口并执行隔离发布工具真实边界验证，独立审查席只读核身份闭环，主会话统一收口。本轮无推送、上传 Release、服务器部署或数据库动作。
