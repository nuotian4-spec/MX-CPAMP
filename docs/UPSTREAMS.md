# 上游来源与开发基线

记录日期：2026-11-10（北京时间）。固定基线已导入，定制实现与验证在独立分支进行。

| 对象 | 来源 | 选定版本 / 完整提交 | 状态 |
|---|---|---|---|
| 发布仓库 | https://github.com/nuotian4-spec/MX-CPAMP | 初始 main：`cae2b599e864153e95c241b3070a7336dcddfad0` | 已核远端、已克隆 |
| CPA | https://github.com/router-for-me/CLIProxyAPI | v8.0.63，`6db7d4b839d86c5b24e17bbe73bcd4db18ef1b1a` | 原样导入 `components/cpa`；导入提交 `be8fae57583f75237c5c90be1924eeab84de0f0f` |
| CPAMP | https://github.com/seakee/CPA-Manager-Plus | v1.14.10，`a67ed8834a914ab24c36d2490fa6bb6d4166724c` | 原样导入 `components/cpamp`；导入提交 `2291001f5daba395ef2db74b87006a217ddd382a` |
| Sub2API 登录/BPS 供体 | 本地独立供体工作树，不整包入仓 | HEAD `c46cd611d2391f68d51a6bc08c34ab1f8b16ceaf` | 有未提交修改，仅按选定文件提取 |
| 登录协议依赖 | https://github.com/poxiao33/toSub2 | `8548397e89bf80e508eda64a87e0d556d43abc84` | 必要协议模块已提取；独立 npm/Python 锁文件和镜像 |

## 导入原则

1. CPA、CPAMP 分别从固定基线导入普通目录，保留各自源码相对路径、锁文件和许可，不携带 `.git`。
2. 两份上游基线分别提交，再记录定制修改，避免把来源导入与功能实现混成一个不可追溯提交。
3. 供体 HEAD 不包含其全部工作区输入。提取前核对选定文件及真实 diff，逐文件记录，不搬入未知改动。
4. 不导入 Sub2API 数据库、旧任务、日志、凭据或运行目录；不接续旧登录实验。
5. 登录供体与依赖保留原许可和必要来源声明；当前根目录不统一声明所有组件为 MIT。
6. 组件源码加入后，核对其原 `.gitignore` 与本仓库规则，避免遗漏必要的受许可文件、模型资料、测试与构建输入。
7. Docker 构建上下文限于组件；各组件 `.dockerignore` 另行排除真实运行数据，不能依赖 `.gitignore` 代替。

## 定制来源与验证

CPA 与 CPAMP 原样导入分别核对了 2296、1572 个上游跟踪文件，blob 与 mode 无差异。共同实施基线为 `22d0f16cfcd741a95dd34810aca61ea688390f10`。

登录协议的逐文件来源与许可见 [NOTICE](../components/oauth-login/NOTICE.zh-CN.md)，保留 toSub2 MIT 与 Sub2API LGPL-3.0 原文。供体含未提交输入的事实已记录，不将供体 HEAD 描述为全部输入。

实现证据见 [CPA 回执](evidence/cpa-runtime/README.zh-CN.md)、[Manager/Worker 回执](evidence/manager-login/RECEIPT.zh-CN.md)。本地验证、推送、镜像发布与生产部署分开记录；来源提交不替代业务验收。
