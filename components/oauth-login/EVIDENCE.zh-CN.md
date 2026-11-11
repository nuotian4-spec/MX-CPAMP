# Worker 本地专项证据

输入基线：`22d0f16cfcd741a95dd34810aca61ea688390f10`，分支 `codex/manager-login`。本记录对应同工作树 `components/oauth-login/**`，由 B 线负责人统一提交绑定最终提交。真实 OpenAI 登录、CPA 持久导入和生产部署未执行。

2026-11-10 运行：

1. Node `C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe --test components/oauth-login/test/*.test.mjs`：exit 0，7/7 通过。覆盖 Codex/BPS 单 profile、密码/TOTP、工作区、PKCE、独立 token endpoint、额外验证无重试、错误 client 拒绝、服务鉴权、结果清理、单任务、失败脱敏、取消/超时。
2. `docker run --rm --init --network none --mount type=bind,source=D:/MX-CPAMP-worktrees/login/components/oauth-login,target=/app,readonly -w /app --entrypoint node tosub2v3-local:authorization-20261108 --test test/worker.test.mjs test/profiles.test.mjs`：exit 0，7/7 通过；真实 Linux stdin/fd3 管道与进程组内子孙进程终止通过。使用既有缓存镜像，只证明专项运行边界，不等于本组件新镜像构建成功。
3. `node --check` server/login/protocol-core/sentinel_runtime；`python -m py_compile` tls_bridge/sentinel_bridge：exit 0。
4. 官方 Node/npm 在组件目录 `npm install --package-lock-only --ignore-scripts`：exit 0，独立锁文件已生成。
5. `git diff --check -- components/oauth-login deploy docs/contracts/login-worker.md`：exit 0。
6. 通过7890代理读取PyPI包版本与依赖元数据：curl_cffi0.15.0、cffi2.0.0、pycparser2.23、certifi2026.2.25、rich15.0.0、markdown-it-py4.2.0、Pygments2.21.0、mdurl0.1.2均有实际版本回执。

## 待证

- 本组件 Dockerfile 的新镜像构建与基于新镜像的部署边界验证由 B 线统一协调。
- Python TLS/Sentinel 的真实官方网络登录未执行；此处没有真实账号或收费模型证据。
- Manager→Worker→CPA真实Interface的统一集成由 B 线负责人收口。
