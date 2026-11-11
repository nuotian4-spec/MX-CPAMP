# BPS状态跨容器重建专项

日期：2026-11-11。源码基线 `9eb2ec9e7f45876304f91ca3cb7f8fce2423d706`。仅新增本目录的本地验收工具与证据，没有产品、部署工具、SQL/schema或生产配置修改。

## 固定输入

| 角色 | 实际镜像ID | OCI revision |
|---|---|---|
| CPA | `sha256:a4428de0eeda96d76cfa5733eed190ea8b45cd49083bf6b881f4bc9dfc1f5933` | `ef4d287ea3298a7680f8fc21e82db77417bbe634` |
| 合成HTTPS上游/测试客户端 | `sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758` | `95dd9ca2c16d5469c43ce7c910f38ff271af45e3` |

两镜像启动前校验ID与revision，以完整不可变ID运行，`--pull never`；没有构建或下载。测试客户端只使用Worker镜像中现成Node，不启动登录Worker。

本机工具为Node `C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`、OpenSSL `C:/Program Files/Git/usr/bin/openssl.exe`、Docker Compose `5.3.1`。合成证书在唯一临时目录生成，SAN为替身DNS名称；仅本轮CPA设置 `SSL_CERT_FILE`，没有把证书、代理或合成身份植入生产。Compose网络 `internal=true`，无发布端口；真实BPS域名仅作为该内部网络中HTTPS替身的别名，不访问外网。

## 命令与结果

在工作树根目录执行：

```powershell
$env:http_proxy='http://127.0.0.1:7890'; $env:https_proxy='http://127.0.0.1:7890'
& 'C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe' docs/evidence/bps-state-durability-20261111/run.mjs
```

最终执行exit0，[白名单结果](run.log)。检验通过真实Compose与固定CPA二进制，不是替换Docker的状态机：

1. 显式 `bps-protocol.state-dir=/var/lib/cpa/bps-state`，单独命名卷；凭据扫描目录为 `/state/auths`。固定合成凭据及无外部副作用ping经过 `/v1/responses`、真实调度、Executor及协议桥接，首轮HTTP200写出一个绑定文件。
2. 实际文件模式600。递归枚举整个状态卷，仅有一个 `bindings/call_bps_*.json`；内容严格七字段：`auth_id/account_id/subject/call_id/native_id/caller/expires`。聊天、参数、access/refresh token秘密哨兵未进入记录。
3. 用同一完整Compose定义执行 `up --force-recreate --no-deps cpa`。新旧完整容器ID不同；实际inspect的卷Source、Target、RW一致，原记录内容保持。
4. CPA重建后，客户端回传完整历史和原公开call_id，第二轮HTTP200。仅内网HTTPS替身确认原native call_id恢复、同一合成账号和认证标记，最终pong；全程合成上游请求恰好2次。
5. 错误state-dir置于auth扫描树内：原CPA明确输出配置拒绝并退出。该CLI实际退出码为0；测试依据明确拒绝诊断和已退出进程判断，不将exit0等同服务可用。
6. 另一个真实Compose override将同一Target错挂到新空卷。本轮交付的 `guard()` 读取实际容器inspect，因Source不符拒绝，未再向合成上游发请求；独立挂着原卷的观测端确认原绑定未被改写或删除。没有自动修正或回灌。
7. 清理使用本轮完整Compose链（包括错挂反例override），仅针对唯一project/label删除本轮容器、卷和网络；再按精确label核无残留。临时目录先验证绝对父目录及本轮随机前缀，再递归删除。最终 `cleanup.verified=true`。

## 已知边界与保留记录

- 这是固定镜像下的真实持久文件/Compose生命周期验证，但身份、HTTPS服务和模型回包均为合成；不代表真实BPS跨重启业务、OAuth、模型能力或OVH现场验收。
- 发布门禁应核实际running、即时认证健康及配置拒绝诊断，不能仅凭启动命令exit0。没有扩展修复CLI退出码。
- [initial.log](initial.log)保留首轮记录：重建与续轮已通过，错误目录因原测试要求非零退出而未通过；未将该记录改称产品缺陷已修。
- [second.log](second.log)保留一次Compose启动命令失败及确认清理的回执，原因为unknown。
- [pre-cleanup-fix.log](pre-cleanup-fix.log)保留边界全部通过但清理未确认的回执：只按base定义down漏了override中的空测试卷。该卷随后按精确名称和归属label核验后清理；最终脚本清理携带完整Compose链，末次执行确认无残留。

全部记录与最终源码仅含合成材料。没有真实登录、刷新、模型调用、生产连接、推送或部署。独立审查由父会话另行收取；本线不自称独审通过。下一责任人：独审席核本目录证据与清理边界，主会话再整合到发布材料。
