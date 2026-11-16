# 本地隔离预览交接

## 已启动范围

- 应用容器：`unified-update-preview-20261116`，实际运行本轮最终 CPA、Manager 及嵌入 web。
- 网络：`unified-update-preview-20261116`，`internal=true`，`10.253.16.0/24`；应用 IP `10.253.16.2`，没有默认路由。
- 网络内地址：Manager `http://unified-update-preview-20261116:18317`；CPA `http://unified-update-preview-20261116:8317`。
- 应用未挂载 Docker socket。仅挂载本轮产物、隔离预览目录和只读 deploy 脚本。
- CPA 使用 `--local-model`、关闭控制面板与面板自动下载、空账号目录；启动日志确认 0 auth files、0 provider keys。
- Manager 数据目录为全新隔离目录，只有本轮合成 SQLite 数据；不读取其他站点或生产数据库。

隔离文件目录：`C:/Users/子羽/.codex/tmp/unified-update-20261116/preview`。其中保存 `start-preview.sh`、`config.yaml`、`host-ready.json`、`host.log`、`cpa.log`、`manager.log` 以及独立 Manager 数据。

## 合成凭据

- Manager 管理入口：`synthetic-admin-only`。
- CPA 管理密钥：`synthetic-preview-only`。

这些字符串仅用于本地合成预览，不是实际账号、供应商 Token 或许可。

## 实际 HTTP 结果

| 检查 | 结果 | 工具回执 |
| --- | --- | --- |
| Manager `/` | 307，产品监听正常 | `a0efed` |
| CPA `/v8/management/plugins` | 200，插件列表为空 | `a0efed` |
| CPA `/v8/management/updates/core` | 200，真实 Go v8 → Unix socket → 产品宿主 HTTP | `a4a7cb` |

核心 discovery：`site_id=fixture`、`registered=true`、`official_version=v8.0.73`、`site_version=v1.2.3`、`releases=[v1.2.3]`、`blockers=[]`。固定发行来源与 Docker 运行边界来自 P08 合成夹具，不访问真实发行站点。预览阶段未点击或调用核心 install。

合成插件 ZIP：`C:/Users/子羽/.codex/tmp/unified-update-20261116/preview/fixtures/synthetic-sample-1.0.0.zip`，2775 字节。它是 GCC 编译的合成 C ABI 插件 `sample`，不含真实 BPS；A 仅生成该文件，未上传或安装。

## 已知限制与下一责任人

Docker Desktop 的 internal 网络没有兑现宿主 18345/18346 端口发布；这两个宿主入口不是可用浏览器地址。P9 负责在同一 internal 网络内执行真实浏览器验证并另存回执。A 的容器内 HTTP 结果不等于浏览器验收。

核心产品宿主使用 P08 的真实 Unix HTTP 与独立 worker 实现，但 systemd、Docker 站点维护与发行传输在此为合成边界，不代表现场安装或真实业务验收。

P9 完成后可停止本轮专用容器，保留日志和归档：

```powershell
& 'D:/Docker/resources/bin/docker.exe' stop unified-update-preview-20261116
```

下一责任人：P9（隔离真实浏览器验收和总控收口）。
