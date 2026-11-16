# 插件原包与本站更新功能映射

日期：2026-11-16（CST）。原包：`D:/子羽/QQ/cpa-bps-unified-0.5.58-linux-amd64-binary.zip`，6,715,972 字节。静态清单沿用已完成的原包调查，本轮未重复解压、重算全部清单或执行包内代码。

本映射覆盖外层 `BINARY.json` 及其声明的 17 个文件。它说明内容用途和本站承接方式。实现状态、证据和下一责任人统一维护在候选 `docs/execution/UNIFIED-UPDATE-IMPLEMENTATION-20261116.zh-CN.md` 的 P01–V02 表中；本地交付验收见 `docs/evidence/unified-update-center-20261116/P9/FINAL-ACCEPTANCE.zh-CN.md`，供应商许可及生产状态仍单列。

## 原包逐项映射

| 原包路径 | 原有用途 | 本站承接方式 | 关联功能 |
| --- | --- | --- | --- |
| `BINARY.json` | 包版本、平台、许可声明与文件清单 | 静态预览与完整性/身份校验输入；不据此宣称发行者身份或运行兼容已验证 | P01、P02 |
| `README.md` | 使用、平台及许可说明 | 作为包说明保留，关键平台/许可影响进入预览；不是执行指令 | P02、X03 |
| `TOOL-CHOICE.md` | 工具选择语义说明 | 保留为插件业务参考，更新流程不改工具协议；变更能力由实际二进制及对应证据确认 | X02、V02 |
| `WEB-SEARCH.md` | 搜索能力说明 | 保留为插件业务参考，安装不自动发起搜索或模型调用 | X02、V02 |
| `install.sh` | 整包安装入口 | 不执行；本站通过网页确认后调用既有安装服务与维护链 | P03、C03 |
| `panel/Dockerfile.bps-panel` | 包内专用面板镜像构建 | 不构建、不用来替换本站 Manager；沿用本站发行构建 | C03、X02 |
| `panel/LICENSE` | 包内面板许可说明 | 保留来源及许可材料，不作为面板替换授权 | X03 |
| `panel/apps/web/dist/index.html` | 包内预编译管理 UI | 不安装到本站面板路径；功能入口由现有插件页/资源页承接，缺项单独补最小接口 | P05、X02 |
| `panel/scripts/upgrade-bps-panel.py` | 设置面板路径并更新面板 | 不执行，不修改 `PANEL_PATH`，不改变本站整体 UI | P05、X02 |
| `plugin/dist/SHA256SUMS` | 插件发行文件完整性信息 | 沿用清单一致性核验；内部摘要不等同供应商签名 | P02 |
| `plugin/dist/bps-config` | 随包插件配置工具 | 不自动执行；保留现有插件配置、许可资料和管理资源入口，不输出授权码 | P03、X02 |
| `plugin/dist/cpa-bps.so` | 实际插件动态库 | 规范化为现有安装工件，版本化保存，保持 `cpa-bps` ID，由正式宿主加载与注册 | P01、P03、P04 |
| `plugin/dist/native-smoke` | 独立库加载烟测程序 | 上传不执行；使用本站正式 CPA 宿主的隔离验证，不以独立 smoke 代替业务链 | V01 |
| `plugin/notices/Basispoints-LGPL-3.0.txt` | 第三方许可告知 | 随原包与交付来源材料保留，不擅自改变许可条款 | X03 |
| `plugin/notices/CPA-MIT.txt` | 第三方许可告知 | 同上 | X03 |
| `plugin/notices/GHCP-UNLICENSE.txt` | 第三方许可告知 | 同上 | X03 |
| `plugin/scripts/check-library.sh` | 调用包内加载检查 | 不执行；正式宿主/实际版本与状态验证由本站候选流程负责 | P04、V01 |
| `plugin/scripts/install-unified.py` | 安装插件、可同时安装面板并改 Compose | 不执行；复用商店与唯一站点维护锁，不追加独立 Compose 链 | P03、X01 |

原包仍保留在用户指定位置。规范化的运行工件只需要指定库；说明和 notices 的交付保留不意味着执行其中脚本或复制整套面板。安装所需许可文件属于站点既有私有数据，不从历史聊天提取，不进入 Git 或日志。

## 能力边界

- 静态导入支持固定候选的身份、版本、平台及完整性判断；它不解析或解密账户材料，也不创建新插件业务身份。
- 可信来源与包内一致性分别记录。管理员对兼容未验证候选的单次确认不等同已验证，只允许尝试这次已明确目标的维护；已知不兼容仍拒绝。
- 替换插件不改变 Kilo/Codex/BPS 的客户端 Key 归属、账号调度、代理出口、额度引用或续轮规则；只有实际触及的接缝补验证。
- “库已安装、活动注册成功、商业许可有效、OAuth 可用、真实推理通过”分别有各自结果。本站不主动回退付费 API 或 Kilo credits；上游计费边界保持原未核状态，不因更新功能而宣称零费用。

## 内核发布能力映射

| 现有机制 | 网页更新承接 |
| --- | --- |
| GitHub Actions 固定发行构建 | 增补展示所需元数据，仍由本站固定版本承载定制内核；官方新版仅用于发现 |
| `mx-update` 站点登记、锁与固定版本参数 | 网页确认绑定已登记站点及固定目标；与插件写入共用站点维护权 |
| 下载、预检、停态备份、三角色切换 | 独立宿主任务调用既有更新器，不把步骤散落到浏览器请求中 |
| 内部健康、入口恢复、活动指针与轮次记录 | 页面重连读取同一任务的实际结果，HTTP 结束或 Manager 重启不判失败或重复执行 |
| 维护失败停态保全与恢复命令 | 保留恢复入口和现场，拒绝自动数据库回灌；实际现场恢复须在授权范围内 |

## 来源

- `D:/MX-CPAMP/docs/evidence/bps-plugin-update-0.5.58-20261116/P9/BINARY.json`
- `D:/MX-CPAMP/docs/evidence/bps-plugin-update-0.5.58-20261116/P9/PACKAGE-STATIC-INSPECTION.json`
- `D:/MX-CPAMP/docs/evidence/bps-plugin-update-0.5.58-20261116/A/PLUGIN-UPDATE-FACTS.zh-CN.md`
- 候选 `docs/execution/UNIFIED-UPDATE-CENTER-20261116.zh-CN.md` 与实施当前态。
