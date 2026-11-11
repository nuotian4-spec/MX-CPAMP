# 固定现场上线材料验证

日期：2026-11-10。起始基线 `2e8683fa8aadb0ed4ea9cc8b6a64763f462cc6e9`，工作区干净。本线仅增加 deploy 与 deployment 说明、专项证据，未修改产品、原模板、主目录或服务器。

输入为本轮用户预检附件 `06885f47-d6ce-4348-80e6-2a9cc9bae9ac/已粘贴的文本.txt` 中的唯一 cpa-stack、完整旧镜像ID、固定挂载和原单文件 Compose。分发 manifest/正式226493978字节归档由 P9 维护；正式归档SHA256为 `89952ca57460c55cea1aa04e69c510df9bb1c6a8395777ea33cf38bee6d0fc6f`。本线实际消费同一 manifest 的三产品revision与接受ID集合，不重载正式大归档。

## 命令与结果

Linux Bash语法：

```powershell
docker run --rm --network none --mount 'type=bind,source=D:/MX-CPAMP-worktrees/login/deploy,target=/delivery,readonly' --entrypoint bash mx-cpa:integration -c 'bash -n /delivery/cpa-stack-release.sh && bash -n /delivery/cpa-stack-compose.sh && echo bash_syntax_exit=0'
```

退出0。真实Linux/Compose与交付函数边界：

```powershell
& docs/evidence/deployment/run-release-boundaries.ps1
```

退出0，`release-boundaries.log`。测试运行在缓存 Linux 工具镜像，复用缓存 Docker29镜像中的Compose5.5 CLI插件（生产5.7）；配置、路径、SQLite/WAL/密钥均为合成文件。Docker API仅连接本机测试引擎，镜像不拉取，已有产品不重建，不连接服务器。临时容器、卷以本次随机前缀定向清理并确认，`fixture_cleanup=verified`。

已通过：

- 真实 Compose 原生解析与双文件合并，Caddy配置不变、原命名卷名称不变；override先暂存；本轮发布硬链身份正确。
- 所选 Manager JSON进入文件漂移门槛；其 `/data` 内库路径改变会拒绝。实际所选另一JSON指向容器层的密钥会拒绝。
- 官方纯ENV默认分支、容器内默认JSON存在时停止；auth/默认KV、自定义plugins KV主机映射及unknown KV拒绝。
- 交付 `secret_prepare` 生成root:1000/0440并保持既有内容；错误旧权限拒绝。真实Worker UID1000通过单文件bind读取新生成secret，父目录保持0700。
- 交付确认函数穿真实Linux PTY：DEPLOY通过、取消/EOF拒绝；无TTY即使输入DEPLOY也拒绝。
- 交付停止态守卫拒绝仍运行的应用；停态备份函数实际生成0600归档、0700目录，包含SQLite/WAL/data.key/auth/默认bbolt及plugins自定义KV归档；环境来源独立备份。
- 交付 `load_bundle` 穿真实小型gzip docker-save/load和三候选镜像实际身份核证；错误被消费digest拒绝。正式归档的经典LinuxID/revision/platform证据复用P9独立大归档回执。
- 真实候选Manager：严格不存在任务GET的有效管理鉴权404、无鉴权401。真实CPA/Manager维护前探针通过；把原bootstrap文件换为无效假key时准备探针拒绝。

## 审查闭环与边界

持久override发布前完成语义合并，wrapper先安装；未完成发布仅接受本轮同inode材料恢复。Manager实际所选JSON与纯ENV路径分别判定，所选文件纳入已有摘要漂移机制。旧Manager准备探针使用config正200/负401，候选版使用严格任务GET正404/负401，均不创建任务；原bootstrap文件失效时维护前停止。

本轮只验证本地材料和合成现场，不宣称服务器已prepare或上线。下一责任人P9：整合、独审收口、公开release，交付固定整合SHA的三个同目录文件及prepare命令；用户回传prepared后再交付TTY apply。
