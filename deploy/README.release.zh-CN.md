# 固定版本发布

正式源码主线为 GitHub `main`。在 Actions 的“固定版本三镜像发布”中选择 `main`，填写版本（如 `v1.0.0`）及已进入 `main` 的完整 40 位小写提交 SHA。所选提交须包含本流水线、打包器、定向测试及三个发布脚本。

流水线使用标准 `ubuntu-24.04` runner，从指定提交重新构建 CPA、Manager-Web、Worker 三个 `linux/amd64` 镜像。CPA 保留 CGO、BPS 源码及版本编译参数；Manager-Web 沿用前端构建和三份静态资源嵌入链路；Worker 从锁文件安装依赖。不引用旧镜像，不使用 Actions 缓存或 artifact 存储，不配置服务器 SSH 或凭据。

## 版本与资产

- 仅手动触发；`main` push 不自动发布。既有版本标签、Release 和本地版本目录均不覆盖。
- 构建、镜像平台、revision 标签、归档配置、脚本复制全部完成后才创建版本标签。
- 先创建 Release 草稿，再上传全部六项资产；核对远端资产名称、数量、大小、上传状态与标签提交后才发布。
- 失败可留下版本标签或草稿，但不发布缺件版本。失败版本保留现场；下次使用新的版本号，不自动覆盖或删除。
- Release 固定地址按版本使用，不依赖 `latest`。预发布后缀会标为 prerelease。

六项资产为：

| 资产 | 用途 |
| --- | --- |
| `mx-cpamp-images-linux-amd64.tar.gz` | 三镜像 docker save 归档 |
| `mx-cpamp-images.manifest.json` | 现有 `phase2-release.py` 的 schema 1 消费格式 |
| `kilo-release.py` | 当前 Kilo 发布入口 |
| `kilo-release.sh` | Linux shell 入口 |
| `phase2-release.py` | 当前底层发布实现 |
| `release.json` | 版本、完整提交、候选镜像映射及其余五项资产的大小和 SHA-256 |

镜像名称分别为 `mx-cpa:VERSION`、`mx-cpamp:VERSION`、`mx-oauth-login:VERSION`。每个候选均记录完整源码 revision；`accepted_ids` 包含构建端 inspect ID 和归档配置 ID，兼容现有加载校验。发布包只复制列明的脚本，不携带 plan、账号、密钥或服务器配置。`release.json` 不是服务器计划文件，现有发布入口仍按其既有命令与审批边界执行。

## 本地定向验证

```sh
python3 -B deploy/test_package_release.py
```

定向测试使用合成镜像归档和模拟 Docker 命令，并调用真实 `phase2-release.py` 到 `docker load` 前边界。测试不启动产品、不加载镜像、不登录账号、不访问服务器。测试通过代表发布契约通过；真实三镜像构建和 GitHub Release 上传仍以一次成功的 Actions 执行及其资产为证。

服务器一键 update 属于后续交付，本流水线不部署、不执行数据库操作。
