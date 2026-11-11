# CPA 官方账号用量候选

基线 `f4dd43e4c217ca6eb7762ca4d4f747665280657c`，分支 `codex/bps-account-usage-cpa`。产品输入与本回执位于同一提交；主会话按交付完整 SHA 合入。

新增 v8 独立只读 `bps/credentials/usage`。精确 BPS 凭证、管理鉴权、固定 Wham GET/来源 UA、有效代理、最多5秒父预算、无重定向，身份/token/有效出口漂移丢弃结果。只返回官方账号主窗口；不定义 BPS 独占或共享额度桶。完整契约：`docs/contracts/bps-account-usage-v8.zh-CN.md`。

## 专项证据

工作目录 `components/cpa`，本机已存在工具链 `C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe`；`GOPROXY=off`、`GOTOOLCHAIN=local`。

```powershell
$env:GOPROXY='off'
$env:GOTOOLCHAIN='local'
& 'C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe' test ./internal/api -run '^TestBPSAccountUsage' -count=1 -v
```

exit 0，详 `usage-go-test.log`、`usage-go-test.exit`。5个公开接口专项组、26个数据/HTTP场景、9个传输/快照场景、2个取消阶段及更短父预算通过；覆盖管理鉴权、精确引用与选中token/headers、无额外请求、原存储零写入、401/403/429/重定向、身份与出口漂移、真实脱敏数值17/2592000/1796977220、合法0、reset-only、两窗未知、非法类型/范围/非finite、额外字段隔离。

`gofmt` 与 `git diff --check` 通过。旧 access 实现未改，`quota:null` 契约继续保留。未重复旧 access 全量专项。

## 边界

全部新增专项通过内存 RoundTripper 与合成管理凭证执行，未访问真实官方接口、登录、刷新、推理或真实账号。无凭证/配置写入、SQL/schema/迁移、push/deploy。此回执证明 CPA 本地专项；最终 CPA/Manager 镜像及断网组合由主会话统一构建和执行。
