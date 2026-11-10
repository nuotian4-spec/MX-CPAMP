# 来源与许可

- `src/vendor/protocol-core.mjs`、`src/vendor/sentinel_bridge.py`、`src/vendor/sentinel_runtime.cjs`：从 `poxiao33/toSub2` 固定提交 `8548397e89bf80e508eda64a87e0d556d43abc84` 提取；原作者 poxiao33，MIT。原许可保留在 `licenses/tosub2-MIT.txt`。
- `src/profiles.mjs` 的 Excel 单 profile PKCE、密码/TOTP与工作区编排参考 Sub2API 供体 `tools/openai_excel_password_flow.mjs`、`tools/openai_excel_oauth_adapter.py`。供体基线 `c46cd611d2391f68d51a6bc08c34ab1f8b16ceaf`，文件含本地未提交改动；提取输入为该只读工作树当次文件。该衍生适配文件采用 LGPL-3.0，原许可保留在 `licenses/Sub2API-LGPL-3.0.txt`。
- 原供体 CLI、磁盘 checkpoint、Sub2API API/数据库、注册账号、修改密码、手机号绑定、自动重登与 Cloudflare solver 未纳入本组件。
- jsdom 与其依赖由独立 npm 锁文件固定；curl_cffi 与 Python 依赖由 requirements.lock 固定。依赖各自许可不由上述许可替代。

## 运行界限

Worker 只执行当次密码/TOTP授权，CPA 仍是凭据保存、刷新与推理调度的唯一责任方。额外验证返回 needs_user_action；没有自动重试。真实 OpenAI 登录和 BPS 手工 OAuth 回调兑换未在本地假上游测试中验证。
