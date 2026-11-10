# BPS工具与图片最终候选构建

日期：2026-11-11。两个产品执行线已整合，产品固定输入为 `2f51beb7c8f9ed7ecb3c91562fe94824192c6990`。本轮是本地候选，无推送、生产操作或SQL/schema迁移。

| 组件 | 镜像 | 实际ID | 源码revision |
|---|---|---|---|
| CPA | `mx-cpa:bps-tools-images-20261111` | `sha256:064349bcaf66620cbe3e4770b2caf5b833ff2cb8dfd1b1c2f47c6789c452a766` | `2f51beb7c8f9ed7ecb3c91562fe94824192c6990` |
| Manager | `mx-cpamp:bps-tools-images-20261111` | `sha256:6732b8d85cfade68fce21ae286ede773d8151a397c64eedbea0b4618d4b6cd73` | `bd573d0439a925d811e25aeeb575ac91feb5796d` |
| Worker | `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

CPA和Manager各执行一次原Dockerfile最终构建，Version均为 `bps-tools-images-20261111`，exit0。CPA传入COMMIT/BUILD_DATE及OCI revision，Manager传入SOURCE_COMMIT并写入二进制与标签。Manager包含 `tsc && vite build` 后嵌入Go服务。日志：[CPA](cpa-build.log)、[Manager](manager-build.log)。

Manager在其产品输入冻结后先行构建；随后只修改CPA及证据。已用 `git diff --exit-code bd573d0 HEAD -- components/cpamp` 确认Manager输入未变。Worker产品目录相对95dd无diff，继续复用，不重建未变组件。候选运行镜像均未包含构建代理环境变量。

从同一Manager输入导出 `web-build` 阶段，复用缓存执行页面专项，没有重建Manager产物。`BPSSettingsCard.test.tsx`三项与 `bpsSettings.test.ts`一项全部通过，exit0；覆盖默认native、关图提示、非法HTTPS零写入、切换实例引用与固定API路径。[阶段导出](web-build-stage.log)、[测试日志](web-tests.log)。react-test-renderer弃用提示仍由现有依赖产生，不影响四项结果。

从相同CPA参数导出builder为 `mx-cpa-buildenv:bps-tools-images-20261111`，全部构建输入复用，exit0，[导出日志](buildenv.log)。独立执行席在固定builder镜像运行七包Linux竞争检测，exit0，见[竞争检测](LINUX-RACE.zh-CN.md)。

固定三组件[无凭证预检](preflight.jsonl)exit0：实际镜像ID和revision、健康、管理鉴权、只读根、tmpfs、关闭容器日志、无公开端口及收尾清理通过，harness和输入未漂移。预检没有发起登录、刷新或模型请求，不替代业务验收或OVH网络验证。

实际构建附带本机7890代理和Docker可达的构建代理参数。仓库日志副本只裁剪行尾空白及末尾空行，原始日志保留于 `C:/Users/子羽/.codex/artifacts/mx-bps-tools-images-*-20261111.log`。各镜像保留自己的真实revision，不统一重标未重建组件。
