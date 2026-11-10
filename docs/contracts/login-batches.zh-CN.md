# 批量登录接口

Manager Admin 鉴权、`Cache-Control: no-store`。密码和 TOTP 只进入当次内存队列及 Worker stdin；响应不含原邮箱、密码、种子、令牌或原始上游错误。

- `POST /api/oauth-login/batches`：`{batch_id, profile, items:[{line_no,email,password,totp_secret}]}`。UUID由客户端生成；同进程保留期内同编号去重，不重放授权。
- `GET /api/oauth-login/batches/active`：当前 CPA 连接上的活动批次或 `null`。
- `GET /api/oauth-login/batches/:id`：脱敏逐行状态。
- `DELETE /api/oauth-login/batches/:id`：取消未执行条目及正在登录的条目；已经导入中的条目等待真实结果，已成功条目不撤回。

每批最多100行，正文最多1MiB，每行最多16KiB。格式按首个与末个 `----` 分隔，保留密码空格和中间分隔符。坏行、重复邮箱跳过；一批只选Codex或BPS，单批独占Manager登录通道。普通登录失败和额外交互只结束该行；下一行在Worker停止及授权结果清理确认后开始。

接收时冻结CPA连接；每行创建前和凭证导入前复核。每行独立获取导入锁，不持有整批导入锁。`import_unconfirmed`、`worker_stop_unconfirmed`、`worker_cleanup_unconfirmed`、`worker_unavailable`、`cpa_target_changed`、`import_busy`中断批次并清空剩余材料。清理失败不抹去已经确认保存的成功结果。

等待材料从接收起最多30分钟，后台定时清理，不依赖轮询；已启动条目沿用25分钟上限。页面离开仅停轮询；重新进入查询活动批次。服务重启不续跑，404不触发重交。终态脱敏摘要保留1小时。批次状态为running/cancelling/completed/cancelled/interrupted；逐行另含pending/skipped与原单任务终态。

BPS OAuth页面提供生成、打开及复制授权链接；回调提交ACK只表示接收，状态查询确认CPA保存注册后才发账号更新事件。
