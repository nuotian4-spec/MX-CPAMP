# 固定版本发布

正式源码主线为 GitHub `main`。在 Actions 的“固定版本三镜像发布”中选择 `main`，填写版本（如 `v1.0.0`）及已进入 `main` 的完整 40 位小写提交 SHA。所选提交须包含本流水线、打包器、更新入口、定向测试和生产接线合同。

流水线使用标准 `ubuntu-24.04` runner，从指定提交重新构建 CPA、Manager-Web、Worker 三个 `linux/amd64` 镜像。CPA 保留 CGO、BPS 源码及版本编译参数；Manager-Web 沿用前端构建和三份静态资源嵌入链路；Worker 从锁文件安装依赖。不引用旧镜像，不使用 Actions 缓存或 artifact 存储，不配置服务器 SSH 或凭据。

## 版本与资产

- 仅手动触发；`main` push 不自动发布。既有版本标签、Release 和本地版本目录均不覆盖。
- 构建、镜像平台、revision 标签、归档配置、脚本复制全部完成后才创建版本标签。
- 先创建 Release 草稿，再上传全部十六项资产；核对远端资产名称、数量、大小、上传状态与标签提交后才发布。
- 失败可留下版本标签或草稿，但不发布缺件版本。失败版本保留现场；下次使用新的版本号，不自动覆盖或删除。
- Release 固定地址按版本使用，不依赖 `latest`。预发布后缀会标为 prerelease。

十六项资产为：

| 资产 | 用途 |
| --- | --- |
| `mx-cpamp-images-linux-amd64.tar.gz` | 三镜像 docker save 归档 |
| `mx-cpamp-images.manifest.json` | 现有 `phase2-release.py` 的 schema 1 消费格式 |
| `kilo-release.py` | 当前 Kilo 发布入口 |
| `kilo-release.sh` | Linux shell 入口 |
| `phase2-release.py` | 当前底层发布实现 |
| `mx-update.py`、`mx-update.sh` | 首次站点登记后的固定版本一条命令更新入口 |
| `mx-update.site.example.json`、`README.update.zh-CN.md` | 站点登记示例与日常操作说明 |
| `capability_probe.py`、`capability-matrix.json` | 策略观察与双层能力消费者 |
| `production-wiring.contract.json`、`production-wiring.evidence.json` | 审查后的源码映射及继承原运行证据 |
| `production-wiring.json` | 打包时从实际 checkout 绑定的候选 revision |
| `capability-image-evidence.json` | 新 CPA 镜像策略入口成功/失败分支的 CI 实测 |
| `release.json` | 版本、完整提交、候选镜像映射及其余十五项资产的大小和 SHA-256 |

镜像名称分别为 `mx-cpa:VERSION`、`mx-cpamp:VERSION`、`mx-oauth-login:VERSION`。每个候选均记录完整源码 revision；`accepted_ids` 包含构建端 inspect ID 和归档配置 ID，兼容现有加载校验。发布包只复制列明的脚本，不携带 plan、账号、密钥或服务器配置。`release.json` 不是服务器计划文件，现有发布入口仍按其既有命令与审批边界执行。

## 本地定向验证

```sh
python3 -B deploy/test_mx_update.py
python3 -B deploy/test_package_release.py PackageTests.test_complete_package_and_script_identity PackageTests.test_publish_only_after_all_remote_assets_match UpdatePackagingTests
```

定向测试使用合成镜像归档和模拟 Docker 命令，并调用真实 `phase2-release.py` 到 `docker load` 前边界。测试不启动产品、不加载镜像、不登录账号、不访问服务器。测试通过代表发布契约通过；真实三镜像构建和 GitHub Release 上传仍以一次成功的 Actions 执行及其资产为证。

打包器先核继承合同和相关源码 Git tree，缺增量审查或有未覆盖变更即停止。构建后以候选 CPA 镜像断网运行合成策略探测与坏配置退出，不启动生产服务、不挂载真实账号数据库。详细日常更新见同版 `README.update.zh-CN.md`。本流水线只发布资产，不部署、不执行数据库操作。
