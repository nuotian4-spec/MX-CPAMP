# BPS 最终镜像隔离组合检查

工具只接受本地已有最终 CPA/Manager 镜像及其完整 ID、OCI revision，启动时核身份并使用 ID 启动。等待主会话提供完整 Manager 身份后执行一次；工具不构建或拉取镜像。

```text
node --check run.mjs
node --check check.mjs
node run.mjs --cpa-image mx-cpa:bps-inspection-repair-20261111 --cpa-id sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc --cpa-revision 06a2f9f121b06e8609c5f9d148213dc6ceeaea6b --manager-image MANAGER_IMAGE --manager-id MANAGER_FULL_ID --manager-revision MANAGER_FULL_REVISION
```

复用 phase2-integration 的 network none、Node checks、tmpfs、随机label及finally清理模式。checks_runner 使用已缓存镜像 `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758`，仅执行Node检查程序，不启动OAuth服务、登录进程或Worker，不配置Worker URL/secret。CPA无网络，Manager/checks共享其网络命名空间；仅localhost通信。根只读、日志none、无端口发布，配置、合成凭证、SQLite及密钥全部位于tmpfs；finally只清理本次label归属容器并核无残留。

使用无access/refresh token的合成BPS凭证，覆盖管理401、Manager实例引用、BFF credential_incomplete、未知额度、全局模型清单读不发布/人工保存注册/清空保留凭证与无关YAML、旧revision拒绝、服务端手动巡检集合1/BPS/keep/未知额度/无自动动作及记录往返。仅巡检scheduler保持关闭，只启动手动巡检；collector沿用环境托管的原输入，不修改其启停或连接设置。页面草稿行为复用B线前端专项，此处验证API读与本地草稿准备不发布。

官方 access 响应解析、固定GET/headers/取消/重定向等复用CPA公开handler合成专项；工具不开放产品地址覆盖或fixture环境变量。组合检查不发真实access、登录、刷新或推理请求，不代表生产业务可用。真实执行回执另记录命令、镜像身份、退出码与清理结果。

## 配置写入边界

首轮真实组合的access和models阶段通过，随后`inspection_config`失败并完成清理。首轮回执由主会话保留为`ATTEMPT-1.jsonl`，不覆盖或改写；该回执未记录HTTP状态或错误正文。

定向源码比对确认，旧工具把GET返回的`collector.enabled=true`改成false。当前fixture以`cpaUpstreamUrl`和`managementKeyFile`提供环境托管连接；Manager `ManagerConfigConnectionDiffers`把collector enabled纳入受托管字段，`managerconfig.Service.Update`拒绝此变更，对应HTTP409及稳定错误码`connection_env_managed`。这是工具构造越过配置边界，非BPS巡检产品缺陷；HTTP类别来自源码路径，待主会话新一轮真实组合核验。

工具现在原样保留collector，只修改`codexInspection`。配置PUT的诊断仅输出HTTP数字及白名单错误码；不输出原始config、Key或上游正文。修改后本线仅执行`node --check run.mjs`、`node --check check.mjs`及`git diff --check`，实际退出码均为0。下一步由主会话使用同一组最终镜像重跑组合，不重建相同产品。
