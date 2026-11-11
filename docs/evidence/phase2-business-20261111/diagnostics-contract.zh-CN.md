# 公开请求失败诊断

此契约只增加失败回执字段，不改变模型请求体、调用次数、失败后的停止或凭证处理。仅在已有唯一公开请求返回非200时读取该次CPA响应的`error`对象，不追加诊断请求或重试。

| 字段 | 规则 |
| --- | --- |
| `public_http_status` | CPA本次真实响应的整数HTTP状态 |
| `public_error_code` | CPA已知固定code白名单，否则`unknown` |
| `public_error_type` | `authentication_error`、`permission_error`、`rate_limit_error`、`invalid_request_error`、`server_error`，否则`unknown` |
| `public_error_category` | `local_configuration`、`model_capability`、`admission`、`upstream_http`、`transport`、`authentication`或`unknown` |
| `public_upstream_http_status` | 只从完整固定消息`BPS upstream returned HTTP NNN`提取；允许一个已知code前缀。其余为`null` |

固定code包含CPA错误构造和调度中已有的认证、配额、限流、模型、超时、内部错误、凭证准入、provider/executor及request_scoped枚举。严格按白名单比较，不回显任意code/type。

消息只匹配产品已有固定短语：未注册provider/executor或未知模型路由归本地配置；BPS明确不支持的操作、参数、历史或推理选项归模型能力；凭证等待、超时与无可用凭证归准入；明确BPS上游HTTP短语归上游HTTP；固定Go网络/上下文超时短语归transport。消息不匹配时，只按足以分类的固定code归类；泛化`internal_server_error`、`server_error`和未知消息保持`unknown`，不会从顶层500猜上游状态。

原消息、原始正文、URL、header、邮箱、token与其他字段不输出。即使错误对象中含`upstream_status`或任意数字，也不把它当作已核上游状态。已完成的旧回执没有这些字段时，不据此补写根因。

对应源码事实源为CPA的`handlers.go`错误构造、`credential_concurrency.go`准入码、`bps_capability.go`能力短语及`bps_executor.go`上游HTTP固定消息。无网络fixture验证HTTP500仍只执行一次客户端请求，秘密哨兵不回显、未知消息不被推断为上游HTTP。
