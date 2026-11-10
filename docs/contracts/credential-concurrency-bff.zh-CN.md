# 凭证并发 Manager 接口

`GET /api/credential-concurrency`使用Manager Admin鉴权并禁缓存。Local模式返回CPA脱敏并发快照，并追加`connection_ref`：32位随机十六进制连接引用。引用由Manager在内存生成，不从URL或密钥派生，不包含上游地址和管理密钥。相同连接的连续读取复用引用；读取时连接发生变化返回409，页面应刷新。

Home保持`{"mode":"home","credentials":[]}`，不展示本地计数或编辑。

`PATCH /api/credential-concurrency`请求：

```json
{"auth_id":"AUTH_ID","concurrency_limit":20,"connection_ref":"读取快照时返回的连接引用"}
```

`concurrency_limit`为1至2147483647的整数；null恢复默认。引用缺失或格式无效返回400；未知、重启后失效、与当前连接不符返回409。后端在读取待修改凭证前核验连接引用，在写入前再次核验，并保留URL/管理密钥中途漂移检查。客户端不提供CPA目标地址或路径。

OAuth文件仍按auth_id写fields；配置Key仍按auth_index唯一映射并使用ETag/If-Match。页面表单与connection_ref绑定，轮询发现连接改变时重新创建表单，避免复用原实例的未保存编辑值。
