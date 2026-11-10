# 二期最终本地镜像与Linux竞争检测

日期：2026-11-11。完整产品输入：`95dd9ca2c16d5469c43ce7c910f38ff271af45e3`。整合时分别按组件比较CPA与已审 `ee9a266`、Manager/Worker与已审 `8da0131`，产品输入完全一致；没有新增SQL/schema文件。

| 镜像 | 实际完整ID | 构建回执 |
|---|---|---|
| `mx-cpa:phase2-local-20261111` | `sha256:0bb5d7dbfc44d4822577fa4ae9f76aa81e0b5a2fd7afae2168c5ae53cbd1f47a` | `cpa-build.log`，exit0 |
| `mx-cpamp:phase2-local-20261111` | `sha256:de4775de2a5345c199ff96034d647b0076b0791a30bb8ed04143497ca35c7429` | `manager-build.log`，exit0 |
| `mx-oauth-login:phase2-local-20261111` | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `worker-build.log`，exit0 |

三个镜像的 `org.opencontainers.image.revision` 均为上述完整提交。CPA内嵌Commit与Manager内嵌SourceCommit通过原Dockerfile构建参数写入同一提交。Manager构建包含最终Web的 `tsc && vite build`，产物 `dist/index.html` 6,506.23kB，随后复制进Go embed并编译；未复用首期页面。

构建均从 `D:/MX-CPAMP` 执行原Dockerfile，命令主体：

```text
docker build --build-arg VERSION=phase2-local-20261111 --build-arg COMMIT=95dd9ca2c16d5469c43ce7c910f38ff271af45e3 --build-arg BUILD_DATE=2026-11-11 --label org.opencontainers.image.revision=95dd9ca2c16d5469c43ce7c910f38ff271af45e3 -t mx-cpa:phase2-local-20261111 -f components/cpa/Dockerfile components/cpa
docker build --build-arg VERSION=phase2-local-20261111 --build-arg SOURCE_COMMIT=95dd9ca2c16d5469c43ce7c910f38ff271af45e3 -t mx-cpamp:phase2-local-20261111 -f components/cpamp/Dockerfile.manager-server components/cpamp
docker build --build-arg SOURCE_COMMIT=95dd9ca2c16d5469c43ce7c910f38ff271af45e3 -t mx-oauth-login:phase2-local-20261111 components/oauth-login
```

实际命令附带 `--progress plain`，本机CLI使用7890代理，构建参数使用Docker可达的同一代理；参数未写入产品配置。构建后直接inspect确认三个运行镜像均无构建代理环境项，避免把本地地址带入生产。

## Linux race

以相同参数将CPA的 `builder` 目标导出为 `mx-cpa-buildenv:phase2-local-20261111`；全部编译层命中缓存，没有重复产品构建。见 `buildenv.log`。利用该镜像已有GCC/CGO和模块缓存，在断网容器执行：

```text
docker run --rm --network none -e GOTOOLCHAIN=local -e GOPROXY=off -e CGO_ENABLED=1 --entrypoint go mx-cpa-buildenv:phase2-local-20261111 test -race ./sdk/cliproxy/auth ./internal/runtime/executor -run '^(TestCredentialConcurrency|TestCodexDuplexConcurrency)' -count=1 -timeout=180s
```

退出0；auth包1.126s、executor包1.165s，无race报告。原始输出：`linux-race.log`。覆盖新增准入共享状态与WS真实loopback生命周期；不是全仓race或真实账号压测。

本轮仅本地构建与定向检测，没有推送、生产操作或数据库迁移。隔离跨线、真实业务、手工OAuth和生产状态分别以对应回执为准。
