# 最终镜像构建回执

日期：2026-11-11。构建目录：`D:/MX-CPAMP`。两项构建均退出0，完整输出和退出码保存在本目录。

| 产物 | OCI revision | 本地完整镜像ID |
|---|---|---|
| `mx-cpa:bps-inspection-repair-20261111` | `06a2f9f121b06e8609c5f9d148213dc6ceeaea6b` | `sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc` |
| `mx-cpamp:bps-inspection-repair-20261111` | `de1c4356cd80363c6b28384fdf3e3a704380ec26` | `sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429` |

`CPA-IMAGE.json`及`MANAGER-IMAGE.json`来自构建后实际Docker inspect。两镜像均为Linux amd64，运行时代理环境数量0；代理只用于本机构建，不写入生产配置。Worker产品输入未变，沿用既有镜像。

## CPA

构建输入为上述revision，使用`components/cpa/Dockerfile`及原构建链，包含CGO的`cmd/server`编译。执行与输出见`CPA-BUILD.log`、`CPA-BUILD.exit`。后续Manager及文档整合不改变`components/cpa`，经目录比较退出0，复用该产物。

## Manager

构建绑定主线`de1c4356cd80363c6b28384fdf3e3a704380ec26`；`components/cpamp`与已审源产品`875dd976c036583dd525b1a10fda07af7b9a43b1`比较无差异、退出0。后续专项证据提交仅在docs下，产品输入等价。

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
docker build --progress plain --build-arg 'HTTP_PROXY=http://host.docker.internal:7890' --build-arg 'HTTPS_PROXY=http://host.docker.internal:7890' --build-arg 'NO_PROXY=localhost,127.0.0.1,::1' --build-arg VERSION=bps-inspection-repair-20261111 --build-arg SOURCE_COMMIT=de1c4356cd80363c6b28384fdf3e3a704380ec26 -t mx-cpamp:bps-inspection-repair-20261111 -f components/cpamp/Dockerfile.manager-server components/cpamp
```

`MANAGER-BUILD.log`记录`tsc && vite build`和Manager Go编译均通过，`MANAGER-BUILD.exit`为0。构建包含的类型检查不另重复。npm使用既有lock安装，报告7项moderate及1项high依赖审计提示；本轮未修改依赖。Vite的native配置预告警告不影响本次构建。

构建通过只确认产物可生成；隔离组合、真实官方接口及生产验收分别记录。
