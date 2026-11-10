# 二期离线跨线集成

状态：工具准备完成，语法与合成登录进程契约通过；BFF连接引用已按最终契约适配，最终候选真实组件集成待跑。所有输入均为合成账号/Key，工具不读取真实凭据、不访问生产、不发真实登录、刷新或模型请求。

## 覆盖与边界

采用真实CPA、Manager和Worker的HTTP接口、任务管理、导入保存热注册、配置写入、账号调度与执行准入。仅将官方登录进程替换为本目录合成runner，将模型上游替换为本目录hold服务器；三组件共享`network none`命名空间，唯一通信为localhost。合成Key one/two是两份独立身份；两个合成模型共享同一份凭证的上限。

- 批次：三行成功/确定失败/成功，核逐条串行、失败后继续；latest按profile恢复已结束摘要；同batch_id返回原回执且不重放进程。
- BFF：OAuth文件上限设置、真实保存与恢复默认20；配置Key上限设置、恢复默认、runtime AuthID不变；其他凭证额度保持独立。每次PATCH先GET取得32hex的connection_ref并原样回传，确认同一连接引用持续稳定，不绕过实例绑定验证。
- CPA准入：两份配置凭证各上限1，不同模型请求分别占满；第三个请求进入公共等待，第四个等待后取消且未发送上游；取消在途请求后等待**模拟上游真实close事件**，再核额度/等待者推进；完成剩余响应后同时核上游pending与CPA active/waiting排空。

以上不替代A/B的WS、SSE、倒计时、热更新或页面专项，也不代表真实授权和生产业务可用。

## 命令

语法与无网络fixture契约：

```text
node --check run.mjs
node --check check.mjs
node --check worker.mjs
node --check fixture-runner.mjs
node verify-fixture.mjs
node verify-cleanup.mjs
```

候选镜像方式：

```text
node run.mjs --cpa-image CPA_LOCAL_IMAGE --manager-image MANAGER_LOCAL_IMAGE --worker-image WORKER_LOCAL_IMAGE --source-revision FULL_REVISION
```

已有rc1基础镜像加完整revision只读叠加方式：

```text
node run.mjs --cpa-binary ABSOLUTE_CPA_LINUX_BINARY --manager-binary ABSOLUTE_MANAGER_LINUX_BINARY --worker-source ABSOLUTE_WORKER_SRC --source-revision FULL_REVISION
```

`--preflight`仅核隔离边界、健康与管理员鉴权，不创建合成登录任务或发请求到hold上游。没有参数指向候选时默认使用rc1基础镜像，它缺本期功能，不能据此宣称整合通过。工具只使用已有镜像，不构建、不拉取。

## 输入与证据

工具记录三个基础镜像的实际完整ID；叠加时要求40位完整source revision，记录三份有效输入摘要并在结束时复核未漂移。叠加只证明本地可执行输入，不代表正式镜像或生产网络等价。

随机管理密钥、共享Worker认证secret仅通过stdin进入各容器tmpfs。无真实账号stdin接口。三组件只读根、log-driver none、无端口发布、无业务卷或Docker socket；全部配置、导入的合成凭证、Manager SQLite和任务状态在tmpfs。finally只删除本次随机label资源，并实际核无残留。输出仅阶段、固定检查码、布尔结果、必要ID/摘要，无合成token或配置Key正文。

容器预生成名称在`docker run`之前登记，CLI回执失败也进入清理。`verify-cleanup.mjs`通过CLI替身覆盖两个工具的“daemon已建容器、run返回失败”路径，要求主动删除且最终无残留；它只验证工具清理，不启动Docker或产品。

后续结果回执须绑定实际候选revision/镜像或叠加输入、执行命令、退出码和cleanup核验；产品候选未齐时保留待跑，不以此文档或fixture成功代替跨线验收。
