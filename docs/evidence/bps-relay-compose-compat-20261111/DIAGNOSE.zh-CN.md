# 候选 Compose 只读差异诊断

2026-11-11。诊断仅导入现场 `/opt/cpa-stack/mx-release-20261111-rc2/bps-final-release.py`，不执行发布 main。要求 root、既有 STATE 为 `prepare_site`、无 ready/maintenance/complete 且已有候选 Compose。调用原 `image_records()` 检查候选身份，用原双文件链和既有三文件链执行 `config --format json`，按原 prepare 重算 expected 并排序 volumes。读取结果全部保留在内存，不写文件、不 load、不下载、不停止或创建服务。

输出仅含固定 code、白名单 stage、文件存在性，以及最多30个脱敏差异路径、存在性、类型。环境变量名、labels、扩展键及未知字段匿名为 `<entry>`；除 `bind.create_host_path` 的布尔值外，不输出实际值。异常统一输出 `diagnostic_unknown`，不输出原异常或 Docker 原始输出。

## 本地专项

输入：交付工具 `da4c11ee5ad800663938bbc01390c6502e878c58` 工作树原文件与本目录诊断；原工具未修改。缓存 Linux runner `sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48`，Compose 2.33.0。容器断网、根文件系统只读、仅 `/tmp` 为临时合成现场；未挂 Docker socket。测试仅替换 ROOT/STATE/PROJECT 和候选镜像事实，调用原 command/compose 和实际诊断函数。

运行命令：

```powershell
docker run --rm --network none --read-only --tmpfs /tmp --mount type=bind,source=D:/MX-CPAMP/docs/evidence/bps-relay-compose-compat-20261111,target=/checks,readonly --mount type=bind,source=D:/MX-CPAMP/deploy,target=/delivery,readonly --entrypoint python3 sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48 -B /checks/test-diagnose.py
```

退出0，原始输出见 `diagnose.log`。实际两份合成 Compose 覆盖：候选省略 bind 时报告 `/services/cli-proxy-api/volumes/0/bind` 缺失；显式 `create_host_path: true` 时完整相等；source 改变时只报告 `/source` 类型相同、值有差异。三个调用均核全部合成文件的字节、mtime、mode 前后相同；秘密哨兵、source 值、临时绝对路径均未输出。另核动态键匿名及30项上限，Python compile 与 Linux `sh -n` heredoc 通过。

完整复制入口以 `python3 -B - <<'PY'` 包裹诊断原文，并以独立 `PY` 结束。其 Linux Bash 语法专项使用缓存 `mx-cpa-buildenv:bps-tools-images-20261111` 的 `/bin/bash -n`，断网、只读、无挂载，退出0：

```powershell
$script=Get-Content -LiteralPath docs/evidence/bps-relay-compose-compat-20261111/diagnose-merge.py -Raw
$here="python3 -B - <<'PY'`n"+$script+"`nPY`n"
$here | docker run --rm --network none --read-only -i --entrypoint /bin/bash mx-cpa-buildenv:bps-tools-images-20261111 -n
```

## 证据边界

Compose 2.33.0 保留显式 true；省略 bind 的反例证明结构差异会触发严格比较，未证明 OVH Compose 5.7.0 的实际差异就是该字段。5.7现场原因仍待本只读诊断回执；没有再次执行 prepare，也未操作生产或发起官方请求。下一责任人：主会话向用户贴诊断原文，回传后按真实字段差异决定最小修复。
