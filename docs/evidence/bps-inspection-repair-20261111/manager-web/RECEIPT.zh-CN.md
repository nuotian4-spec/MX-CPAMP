# Manager / Web 本地候选回执

日期：2026-11-11，北京时间。产品输入：`875dd976c036583dd525b1a10fda07af7b9a43b1`。基线：`5d431b9be0828f94576d1807e1cc87b822cc1350`。分支：`codex/bps-models-inspection`。

## 实现

- `affae3c1ca5f8284ebd9a15f39546d0625a70645`：BPS目标规范化、可组合目标、管理员BFF、浏览器及服务端独立只读探针。
- `c357ed531dae8819011304f5ca0b98c8e70b3e26`：BPS全局模型草稿与人工保存、逐凭证官方权限和注册目录分别展示、固定凭证展示时的实例引用、保存未确认后的锁写与局部刷新。
- `875dd976c036583dd525b1a10fda07af7b9a43b1`：缺少BPS管理能力时保留普通账号列表，BPS无绑定引用时禁止查询。

BPS探针只消费CPA专用access结果，动作始终为`keep`、`autoRecoverEligible=false`、`usedPercent=null`；错误状态不继承旧健康状态，未知provider不进入Codex探针。完整模型目录只在页面内存，历史仅保存权限、状态码、时间、稳定错误摘要。默认目标仍为Codex。

模型配置仅通过现有v8路径替换`oauth.providers.bps.models`，携带全局ETag和Manager实例引用；其他YAML字段及逐凭证禁用规则保留。发现和一键选用不发布，保存空草稿才清空全局目录。当前凭证未获官方允许的已发布模型显示权限不匹配；未查显示未确认。

## 专项证据

全部命令在本工作树执行。Node：`C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/node-v22.23.4-win-x64/node.exe`；Go：`C:/Users/子羽/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.windows-amd64/bin/go.exe`。

| 检查 | 输入及命令 | 结果 |
|---|---|---|
| Go模型、BFF及巡检 | `c357ed5`对应Go输入；在`components/cpamp/apps/manager-server`运行`go test ./internal/model ./internal/service/cpa ./internal/service/credentialconcurrency ./internal/service/codexinspection ./internal/http/controller/bpssettings -run 'BPS\|BPSTarget\|TargetTypes\|CombinedTargets' -count=1`；设置`GOTOOLCHAIN=local`、`GOPROXY=off` | exit 0，五包通过；cpa包自身无匹配测试，客户端由BFF及巡检专项穿过 |
| Web目标、两端结果契约、本地历史、模型UI及账号规则 | `c357ed5`产品输入；在`components/cpamp/apps/web`运行`node ../../node_modules/vitest/vitest.mjs run src/features/monitoring/model/bpsInspectionProbe.test.ts src/features/monitoring/model/bpsInspectionSession.test.ts src/features/system/components/BPSModelsPanel.test.tsx src/services/api/bpsAccess.test.ts src/features/monitoring/components/InspectionConfigFields.test.tsx src/features/accounts/components/accountDetail/AccountModelsTab.test.tsx` | 2026-11-11 15:56，exit 0，6文件53例通过 |
| Web类型 | 同上类型输入，`node ../../node_modules/typescript/bin/tsc --noEmit` | exit 0 |
| 全部变更Web文件lint | `c357ed5`；`git diff 5d431b9be0828f94576d1807e1cc87b822cc1350 --name-only -- '*.ts' '*.tsx'`列出的Web路径，去掉`components/cpamp/apps/web/`前缀后作为`node ../../node_modules/eslint/bin/eslint.js <路径列表> --max-warnings 0`参数 | exit 0 |
| Web格式 | 实际集合为本轮全部新增/变更Web `*.ts`、`*.tsx`，由`git diff 5d431b9be0828f94576d1807e1cc87b822cc1350 --name-only`与当时未跟踪文件合并去重，并明确排除`components/cpamp/apps/web/src/features/accounts/AccountsPage.tsx`；对其余路径执行`node ../../node_modules/prettier/bin/prettier.cjs --check <路径列表>`。`AccountsPage.tsx`仅新增一行业务参数，原排版复用不重排，未纳入该格式检查 | 其余变更文件exit 0；AccountsPage原排版复用 |
| 最终兼容补丁 | `875dd97`；`node ../../node_modules/vitest/vitest.mjs run src/services/api/bpsCredentialSnapshot.test.ts src/features/monitoring/model/bpsInspectionSession.test.ts src/features/system/components/BPSModelsPanel.test.tsx` | 2026-11-11 15:59，exit 0，3文件8例通过 |
| 最终兼容lint/格式 | `875dd97`；`node ../../node_modules/eslint/bin/eslint.js src/services/api/bpsCredentialSnapshot.ts src/services/api/bpsCredentialSnapshot.test.ts --max-warnings 0`；对这两个文件执行Prettier | exit 0 |
| Go格式与diff | 对全部新增/变更Go文件执行`gofmt -l`；`git diff --check` | exit 0，gofmt无输出 |

专项覆盖：allowed真/假、401/403/429及坏响应不产生自动动作；BPS单选/混选不走Codex接口；SQLite结果及浏览器历史的未知额度往返；人工禁用保持；目录不进入历史；发现/草稿不保存；保存和清空仅改模型字段；两凭证权限不同；旧页面引用遇实例变化拒绝；保存后读取失败锁写，局部刷新保留草稿；旧CPA缺BPS管理路由时普通账号列表仍可读。

`c357ed5`前最后仅将两个effect清理中的数字ref自增改为固定代际赋值，未改变类型接口；该文件单独补跑lint及4例UI测试，均exit 0。`875dd97`仅变更绑定读取的能力缺失分支与对应测试；Go输入、TS返回类型及其余UI输入未变，复用上述类型、Go和未受影响Web证据。相关浏览器会话与模型UI8例已定向补跑。

依赖复用：本工作树`components/cpamp/package-lock.json`与`oauth-input-diagnostics`工作树相同；使用其既有`components/cpamp/node_modules`和Web依赖目录的junction。未联网安装或修改lock。

## 审查与交付边界

独立只读代码/安全审查已核最终`875dd976c036583dd525b1a10fda07af7b9a43b1`，已放行整合构建。当前候选不新增SQL/schema/table，巡检沿用现有SQLite记录；仅管理员保存模型清单会写现有CPA配置。

本席未运行真实access、推理、登录、刷新或线上巡检，未读取真实凭据或截图，未push/deploy。最终Manager镜像构建、CPA配套镜像及无token合成凭证的隔离组合验收由主会话统一执行；本回执不宣称在线接口实测或生产业务验收通过。

精确接口与组合命令见同目录`INTERFACES.zh-CN.md`。下一责任人：主会话整合与构建验收。
