# BPS 最终镜像隔离组合检查

工具只接受本地已有最终 CPA/Manager 镜像及其完整 ID、OCI revision，启动时核身份并使用 ID 启动。等待主会话提供完整 Manager 身份后执行一次；工具不构建或拉取镜像。

```text
node --check run.mjs
node --check check.mjs
node run.mjs --cpa-image mx-cpa:bps-inspection-repair-20261111 --cpa-id sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc --cpa-revision 06a2f9f121b06e8609c5f9d148213dc6ceeaea6b --manager-image MANAGER_IMAGE --manager-id MANAGER_FULL_ID --manager-revision MANAGER_FULL_REVISION
```

复用 phase2-integration 的 network none、Node checks、tmpfs、随机label及finally清理模式。checks_runner 使用已缓存镜像 `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758`，仅执行Node检查程序，不启动OAuth服务、登录进程或Worker，不配置Worker URL/secret。CPA无网络，Manager/checks共享其网络命名空间；仅localhost通信。根只读、日志none、无端口发布，配置、合成凭证、SQLite及密钥全部位于tmpfs；finally只清理本次label归属容器并核无残留。

使用无access/refresh token的合成BPS凭证，覆盖管理401、Manager实例引用、BFF credential_incomplete、未知额度、全局模型清单读不发布/人工保存注册/清空保留凭证与无关YAML、旧revision拒绝、服务端手动巡检集合1/BPS/keep/未知额度/无自动动作及记录往返。调度保持关闭，只启动手动巡检。页面草稿行为复用B线前端专项，此处验证API读与本地草稿准备不发布。

官方 access 响应解析、固定GET/headers/取消/重定向等复用CPA公开handler合成专项；工具不开放产品地址覆盖或fixture环境变量。组合检查不发真实access、登录、刷新或推理请求，不代表生产业务可用。真实执行回执另记录命令、镜像身份、退出码与清理结果。
