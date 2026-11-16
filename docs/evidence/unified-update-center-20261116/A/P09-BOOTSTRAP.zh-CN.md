# 独立 bootstrap 材料回执

## 实际材料

- `deploy/README.update-host.zh-CN.md`
- `deploy/package-update-host.py`
- `deploy/test_package_update_host.py`

UX1 单写交付，写域已冻结。既有 `package-release.py`、ASSETS 集合和发行 workflow 保持不变。

## 专项证据

UX1 实际命令：

```powershell
$deploy='C:/Users/子羽/.codex/worktrees/unified-update-20261116/MX-CPAMP/deploy'
docker run --rm --pull never --network none --entrypoint python3 --mount "type=bind,source=$deploy,target=/deploy,readonly" mx-cpa-buildenv:bps-tools-images-20261111 -B /deploy/test_package_update_host.py
```

UX1 工具回执 `d47077`：3/3，0.059 秒，`bootstrap_package_unittest_exit=0`。覆盖六个固定成员的原字节、普通文件/权限/root 所有者元数据、已有目标拒绝覆盖并保留原字节。P05/P06 未重复执行。

## 实际归档

- 绝对路径：`C:/Users/子羽/.codex/tmp/unified-update-20261116/preview/mx-update-bootstrap.tar.gz`。
- 字节数：17806。
- A 实际生成命令：`docker exec unified-update-preview-20261116 python3 -B /deploy/package-update-host.py --output /preview/mx-update-bootstrap.tar.gz`。
- 工具回执：`4d9df4`，`bootstrap_package_exit=0`；仅生成一次。
- 三脚本 `mx-update.py`、`mx-update-host.py`、`mx-update-host-install.py` 权限为 0700；两个 service 与 README 权限为 0644，归档所有者元数据 root:root。
- 无 `release.json`、镜像、站点配置、账号或数据库。

## 验收边界

本轮没有执行真实 Linux root 首次安装、真实 systemd 任务或生产站点维护。该归档是独立 bootstrap 材料，不是核心固定版本发行包。
