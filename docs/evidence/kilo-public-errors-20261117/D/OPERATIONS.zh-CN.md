# Kilo 错误呈现候选使用与交付说明

日期：2026-11-17。适用：本次Kilo公开错误脱敏候选。当前基线为 `67445a35ba450468948447492b560c204afa7321`，不是包含本修复的新发布提交。用户已授权本轮受控入仓、提交推送与新固定版本发布；18路径回迁已核实，`v2026.11.17-rc.1`发布实施中，新固定提交待实际生成。生产维护、真实登录或模型调用尚未授权，本文服务器命令仅作后续操作材料。

## 使用说明

### 三类客户端Key与凭据角色

本项目“三类Key”指 **Kilo、Codex、BPS三类客户端推理Key**，由既有 `access.api-keys` 和 `access.client-key-scopes` 关联。每个scope只能是 `kilo`、`codex` 或 `bps`，并引用一个已登记的客户端Key；重复绑定或无效通道会被拒绝。无scope的旧Key不进入Kilo。

日常客户端继续使用对应通道的现有Key和现有模型入口。本修复不要求换Key、重建账号或清空会话。以下凭据用途不可混用：

| 凭据角色 | 用途 | 本次处理 |
| --- | --- | --- |
| Kilo / Codex / BPS客户端Key | 调用本站相应推理通道 | 保留原通道归属，不输出到诊断记录 |
| Manager / CPA管理凭据 | 读取管理事实、配置和维护操作 | 仍走原管理鉴权，推理Key不替代它 |
| Kilo上游API Key / OAuth材料 | 上游账号授权 | 保留已有账号材料，不从历史聊天取用 |
| 独立额度access token | 只用于已确认的额度来源绑定 | 不作为推理Key或管理Key，不写入回执 |

静态依据：`components/cpa/internal/config/client_key_scopes.go:8-30`、`components/cpa/internal/channelscope/scope.go:21-27`、`docs/execution/UNIFIED-UPDATE-USER-GUIDE.zh-CN.md`。

### 账号、代理、额度与恢复

- 账号沿用现有API Key导入或设备授权入口，保留稳定账号引用及手工停用意图。页面恢复只补尚未完成的步骤，不重发已成功授权。
- 手工出口、共享池和固定IP自动独占池保留原规则。提高累计账号数N不自动恢复已退休IP；退出自动模式不等于释放已有绑定或解除额度限制。错误脱敏不增加直连兜底或自动切换出口。
- 已存在的Codex额度绑定和独立额度材料继续使用。额度的“未知、过期、来源变化”不被当成“可用”；来源额度不等于Kilo实时权益、插件商业许可或实际可计费余额。
- 内部明确 `subscription_sharing_usage_limit_exceeded` 才归入现有Kilo额度耗尽恢复路径；真实上游403并明确 `account_restricted` 才归入账号限制。公开提示“访问受限”本身不触发改账号、改身份或解封操作。
- 既有额度恢复仍核对账号、手工停用、身份/来源版本和恢复时间。公开错误改名不会重置这些条件；失败请求不自动重放。

静态依据：`KiloAutoEgress.tsx:144`、`KiloEgressFields.tsx:201-230`、`KiloAddAccount.tsx:271-279`；这三个组件位于 `components/cpamp/apps/web/src/features/kilo/`。后端依据为 `components/cpa/sdk/cliproxy/auth/kilo_recovery.go:85-97,350-418`。

### 看到新错误怎么处理

使用当前实现确定的公开码及提示，不从用户文案反推上游状态：限流提示等待，明确额度/授权/访问条件提示联系管理员，输入问题修正请求，未知失败提供现有请求ID。管理员通过同一请求ID查询真实HTTP状态、内部机器码和是否已开始输出；公开回执不附账号、代理、令牌或原始提示词。

流已经开始时只能发失败终态，不再改写已发送的HTTP状态，也不补成功帧。不要因为客户端收到一次错误就重放整段历史或反复重新登录。

## 第三方样例的验收边界

样例中的 `request_failed` / `api_error` 是通用呈现，`sequence_number:2` 是事件序号，`param:null` 没有字段定位。本次目标是用合成输入覆盖本站可能产生同类失败帧的路径，并验证修复后可区分类别及隐藏品牌；这不证明第三方使用本站版本，也不证明其具体限制原因。

若第三方维护方希望确认原因，最小输入是同一请求的真实上游HTTP状态、机器code/type、输出是否已开始和关联ID。无需完整会话、密码或令牌，也不要求用户在自己的服务器寻找第三方请求。

## 本地构建与候选输入

准确产品输入为 `../A/frozen-inputs.json` 的18个CPA路径（13修改、5新增）；实际命令及覆盖结论见 `../A/EXECUTION-RECEIPT-20261117.zh-CN.md`，最终放行以S/V回执为准。输入未变的证据复用，不重复全量登录、BPS或更新套件。

正式提交文档与证据以`../CURRENT.zh-CN.md`的9文件白名单为准。本文引用的原始构建结果、命令文本、专项日志、overlay和临时二进制为本地保留材料，不是本次提交资产；正式执行、审查、验收回执保留对应结论。

本地构建已完成，真实回执为 `../A/native-build-result.txt` 的 `NATIVE_BUILD_RC=0` 和 `../A/linux-build-result.txt` 的 `LINUX_BUILD_RC=0`：

| 目标 | 产物绝对路径 | 字节数 |
| --- | --- | --- |
| Windows amd64 | `C:/Users/子羽/AppData/Local/Temp/mx-kilo-public-errors-build-20261117-054341/cli-proxy-api.exe` | 108816896 |
| Linux amd64 | `C:/Users/子羽/AppData/Local/Temp/mx-kilo-public-errors-build-20261117-054341/CLIProxyAPI-linux-amd64` | 106605672 |

这些是候选二进制，不是已发布的组合镜像包。输入未变时复用构建回执；仅在代码变化后按实际受影响范围补验证。

实际使用的Go为 `C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe`，本地测试使用 `GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`CGO_ENABLED=0`。下列是未来需要构建变化后的候选时的命令模板，不要求现在重跑：

```powershell
Set-Location 'C:/Users/子羽/.codex/worktrees/kilo-public-errors-20261117/MX-CPAMP/components/cpa'
# 仅在需要重新构建变化后的候选时运行；使用本轮执行记录中的已验证Go路径。
& $env:MX_GO_EXE build -o $env:MX_CPA_BUILD_OUTPUT ./cmd/server
```

必须先将 `MX_GO_EXE` 和 `MX_CPA_BUILD_OUTPUT` 设置为实际工具与新输出路径；本段不改变系统Go或现有产物。Linux部署仍需既有发行流程生成 `linux/amd64` 三镜像组合，本地编译成功不等于Linux镜像或生产可用。

## 固定版本发布材料

当前本轮独立审查与本地验收已通过，用户已授权提交、推送和固定版本发布，18路径已受控回迁。发布版本确定为`v2026.11.17-rc.1`；A完成入main与推送后，由S核实际新固定提交及发布工作区，随后运行既有Actions。当前尚未取得发布成功回执，旧RC4不作为本次修复版本。

复用 `.github/workflows/release.yml`，其输入是新固定版本与已进入main的40位提交SHA。经授权后的本机PowerShell命令：

```powershell
$env:http_proxy='http://127.0.0.1:7890'
$env:https_proxy='http://127.0.0.1:7890'
$Version=$env:MX_APPROVED_RELEASE_VERSION
$Commit=$env:MX_APPROVED_RELEASE_COMMIT
if ([string]::IsNullOrWhiteSpace($Version) -or $Version -eq 'v2026.11.16-rc.4') {
    throw '请填写本次已批准的新固定版本；旧RC4不包含此修复。'
}
if ($Commit -notmatch '^[0-9a-f]{40}$' -or $Commit -eq '67445a35ba450468948447492b560c204afa7321') {
    throw '请填写包含本次修复且已进入main的新提交。'
}
gh workflow run release.yml --ref main -f "version=$Version" -f "commit=$Commit"
```

发布流程验证main提交归属、目标版本不存在、固定源码与组合接线证据，构建三镜像并发布16项资产。若变更命中既有production-wiring合同冻结输入，只补相应受影响证据并重新绑定，不把旧证据直接改名。`deploy/package-release.py:79-109` 和 `.github/workflows/release.yml` 是实际入口；没有新增发布脚本。

## 经授权后的生产更新

需要生产维护授权、已发布新固定版本、现场站点登记有效且不存在未收口维护任务。以既有 `/opt/cpa-stack/site.json` 为站点登记，不重新安装宿主桥，不手动上传候选包。以下是服务器上的既有入口；`MX_APPROVED_RELEASE_VERSION` 必须来自已核验的新Release：

```sh
: "${MX_APPROVED_RELEASE_VERSION:?请先设置本次已批准的新固定版本}"
test "$MX_APPROVED_RELEASE_VERSION" != v2026.11.16-rc.4 || exit 1
sudo /opt/cpa-stack/bin/mx-update.sh \
  --site /opt/cpa-stack/site.json \
  --version "$MX_APPROVED_RELEASE_VERSION"
```

更新摘要须显示本次版本、提交、镜像、现有容器、备份路径和原Caddy ID；操作者确认一次 `UPDATE` 后进入维护。它会重建CPA、Manager、登录服务，并在健康通过后恢复原Caddy。入口检查按现有实现最多10次，每次是登记健康URL查询，不是模型推理。成功回执仍明确 `inference_business_acceptance=not_run`。

查看当前组合使用实际活动指针中的完整Compose链：

```sh
sudo /opt/cpa-stack/bin/mx-update.sh \
  --site /opt/cpa-stack/site.json --compose ps
```

依据：`deploy/mx-update.py:216-228,237-325`。不要只用一份基础Compose覆盖运行组合。

## 停态备份与回退材料

### 备份

更新在停止应用后，按本轮 `plan.json` 的 `sources.paths` 收集持久材料，生成 `<本轮state>/backup/data.tar.gz` 与 `backup/receipt.json`。收据包含实际路径、归档字节数、摘要及 `stopped:true`；路径集合由现场解析，不根据旧聊天猜测。保留站点配置、完整Compose链、活动发行指针及新旧镜像。备份存在不等于已经恢复。

备份由更新入口自动执行，无需再跑一轮独立备份。填写本轮实际state目录后，可只读查看既有收据的非秘密字段：

```sh
: "${MX_UPDATE_ROUND:?请先设置本轮真实state目录绝对路径}"
sudo python3 -I -B - "$MX_UPDATE_ROUND/backup/receipt.json" <<'MX_BACKUP_RECEIPT_RO'
import json, pathlib, sys
record = json.loads(pathlib.Path(sys.argv[1]).read_text())
print(json.dumps({"stopped": record.get("stopped"),
                  "archive_bytes": record.get("bytes"),
                  "recorded_sha256": record.get("sha256")}, ensure_ascii=False))
MX_BACKUP_RECEIPT_RO
```

这只展示已有收据，不读取归档内容、不显示配置/凭据、不重新计算摘要，也不代替实际恢复核验。

依据：`deploy/kilo-release.py:330-370`。本修复没有数据库迁移；也不授权把备份整库回灌。

### 回退与恢复

`mx-update.sh` 没有 `--rollback` 参数，`mx-update-host.py --recover` 只核实已经完成的恢复事实并收口，不替代回退。不要删除active、维护锁或原任务目录来“解锁”。

按实际停点选一次受控操作：

| 现场状态 | 保留材料 | 下一动作 |
| --- | --- | --- |
| 停服前预检失败，原服务未改变 | `update-failure.json`、原容器、候选材料 | 修正确定的预检问题；确认原服务未被改变后才决定是否发起新轮次 |
| 维护开始后失败或中断 | 同轮plan、backup、stage、实际容器与完整Compose链 | 先确认实际停态；按同轮原镜像/配置决定恢复基线或继续候选，不盲目重跑更新 |
| 需要回到升级前镜像 | 原镜像ID、原完整Compose链、当前持久数据的保全、原配置与指针 | 经授权先保全当前数据，再按核实后的原链恢复三应用；数据回灌须另行审查 |
| 原基线或新候选已经恢复且内部/入口健康可核 | 实际容器、镜像、Compose链、活动指针和任务记录 | 用原任务恢复确认入口收口，保留失败与恢复事实 |

如选择 `kilo-release.py rollback`，先核实际阶段属于其支持集合、原镜像仍在、同轮scope能力回执有效且回退配置与数据兼容。该入口会先保全当前数据，回退后仍保持Caddy停止，入口恢复另行处理；不能拿新版本能力回执证明旧镜像支持相同scope。当前CLI更新失败统一可标为 `update_failed`，不应强改stage去满足底层rollback条件。具体现场路径在维护轮次产生后绑定，本文不预填旧轮次路径。

#### 仅限支持阶段的镜像回退模板

适用阶段仅为 `installed_stopped`、`candidate_healthy_blocked`、`start_failed`、`health_failed`。`update_failed`、`completed`及其它阶段不套用本模板；不要修改stage绕过检查。

前置条件：已有本次回退维护授权；核过同轮原镜像、完整Compose链、存储兼容与停态备份；`backup-rollback-current` 尚未存在；回退能力回执绑定的是待恢复的原镜像与本轮配置/scope事实。三项路径必须显式填写为已核验材料：

- `MX_ROLLBACK_SCRIPT`：本轮实际 `kilo-release.py`，保留同目录依赖；不是随意下载的另一个版本。
- `MX_ROLLBACK_PLAN`：当时传给 `--plan` 的**输入spec**文件，顶层包含 `state_dir`。日常updater轮次是 `<本轮state>/spec.json`；它不是程序生成的 `<本轮state>/plan.json`。
- `MX_ROLLBACK_CAPABILITY`：针对原镜像、当前配置和scope策略核验的回退能力回执；不是候选启动回执的简单复制。

在真实交互终端执行，看到提示后手工输入 `ROLLBACK`。不要通过管道输入确认，也不要把这段包进 `sudo bash <<EOF`，否则不满足TTY要求：

```sh
(
  set -eu
  : "${MX_ROLLBACK_SCRIPT:?请先设置本轮实际kilo-release.py绝对路径}"
  : "${MX_ROLLBACK_PLAN:?请先设置本轮实际输入spec文件绝对路径}"
  : "${MX_ROLLBACK_CAPABILITY:?请先设置绑定原镜像的回退能力回执绝对路径}"
  sudo test -f "$MX_ROLLBACK_SCRIPT"
  sudo test -f "$MX_ROLLBACK_PLAN"
  sudo test -f "$MX_ROLLBACK_CAPABILITY"
  stage=$(sudo python3 -I -B -c \
    'import json,pathlib,sys; spec=json.loads(pathlib.Path(sys.argv[1]).read_text()); print((pathlib.Path(spec["state_dir"])/"stage").read_text().strip())' \
    "$MX_ROLLBACK_PLAN")
  case "$stage" in
    installed_stopped|candidate_healthy_blocked|start_failed|health_failed) ;;
    *) printf '阶段%s不支持本模板；保留现场，勿修改stage。\n' "$stage" >&2; exit 1 ;;
  esac
  sudo env -u DOCKER_CONTEXT -u DOCKER_TLS_VERIFY -u DOCKER_CERT_PATH \
    DOCKER_HOST=unix:///var/run/docker.sock \
    python3 -I -B "$MX_ROLLBACK_SCRIPT" rollback \
    --plan "$MX_ROLLBACK_PLAN" --capability "$MX_ROLLBACK_CAPABILITY"
)
```

模板不带 `--revoke-scoped-keys`，不会把撤销客户端Key混入镜像回退。现有实现保留当前持久数据和配置语义，额外保全当前材料，再以原镜像ID重建三应用；不是数据库回灌。通过时应停在 `rollback_healthy_blocked`，Caddy仍停止，后续入口恢复和活动指针/原任务收口须依据同轮事实处理。失败不重复调用模板。参数、阶段及TTY依据：`deploy/kilo-release.py:545-614,630-670`。

现有宿主核心任务在现场恢复完成后的收口命令：

```sh
: "${MX_CORE_OPERATION_ID:?请先设置本次实际core任务编号}"
sudo python3 /usr/local/lib/mx-update/mx-update-host.py \
  --site /etc/mx-update/site.json \
  --recover "$MX_CORE_OPERATION_ID"
```

该命令仅适用于具有宿主core任务编号的维护。CLI独立轮次以该轮材料和活动指针处理，不伪造core任务编号。依据：`deploy/README.update-host.zh-CN.md`、`deploy/kilo-release.py:540-614`、`deploy/mx-update.py:299-314`、`deploy/mx-update-host.py:340-369`。

## 已知限制与计费边界

- 脱敏针对普通客户端失败输出，不隐藏管理员已鉴权诊断中的通道名，也不修改正常模型回答和用户文字。
- Kilo专用Key的WebSocket仍保持既有拒绝；本次不新增传输能力或修复BPS输入兼容。
- 真实供应商限制不能用合成测试证明；本地通过不等于上线、账号可用、额度恢复或计费为零。
- 本次错误呈现不改变既有计费策略与付费回退选择。外部供应商计费与策略以其当前文档及账户约定为准；本地测试不证明零收费，也不证明第三方请求经过本站相同链路。
- 官方参考链接；本轮未重新核验当前页面：<https://kilo.ai/docs/gateway/api-reference>、<https://kilo.ai/docs/gateway/usage-and-billing>。不以未经当前核实的费率、额度或状态映射作为验收事实。
- 10个custom-tool续接用例在原提交同样返回409，属于既有续接功能缺口。本轮保留原成功断言，不宣称该功能已修复，也不把它计为公开错误呈现新增回归。对照见 `../S/BASELINE-CUSTOM-TOOL-CONTROL-20261117.zh-CN.md`。
- 18路径主仓回迁已完成；本轮提交、推送与v2026.11.17-rc.1固定版本发布实施中，尚无发布成功回执。生产维护和真实客户端验收未授权、未执行，不写整体完成。

当前责任人：A执行受控提交、推送及固定版本发布，S核远端与发布合同；D将本文及功能映射、CURRENT交A取件。下一责任人：主会话核实际发布回执；生产维护和真实验收仍待授权。Goal工具登记仍为blocked，未记录为active。
