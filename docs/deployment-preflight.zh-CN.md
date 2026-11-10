# 上线前只读预检

在现有 OVH 服务器的 SSH 终端运行固定提交中的脚本：

```bash
set -o pipefail
curl -fsSL 'https://raw.githubusercontent.com/nuotian4-spec/MX-CPAMP/0f12bdd81daad620d2d1d3624523d707fd575242/deploy/readonly-preflight.sh' | sudo bash
```

已在服务器检出该提交时，也可直接执行：

```bash
bash /实际检出目录/deploy/readonly-preflight.sh
```

不依赖当前目录。回传完整输出，由部署负责人核对实际 Compose 项目、working_dir、完整 config_files 链、常驻和 one-off 实例、镜像ID/架构/revision、挂载Type/Name/Source/Target/RW、健康与容量，再准备匹配现况的发布命令。发现多个项目会逐个列明，不自动选择；无目标服务明确输出 `discovery=no_services`。`compose_oneoff=True` 是一次性容器，不作为常驻运行实例。同名独立容器或镜像缺少标签时字段为 `not_declared`，不丢弃镜像ID和架构。

脚本仅调用本机 Docker 的读取命令及 `stat/du/uname/free/df`，不联网、不进入容器、不执行 Compose config、不读 `.env`、不创建备份、不写文件，也不拉镜像或修改服务。挂载与 Compose 文件只给路径、权限及容量；仅对明确挂载的 config.yaml/yml/json 或 `/CLIProxyAPI/config.yaml` 提取数字 `config-version`。缺权限、无字段或非数字会明确标记，不输出正文。

输出不包含完整 inspect、环境变量、账号内容、API Key、密码或 token。`local_model_cli_tokens` 仅保留六种明确的 `--local-model`/`-local-model` 参数写法；空值只表示未观察到这些参数。该参数控制模型目录来源，不能据此判定整体运行模式或升级兼容性。域名、密钥和数据的保留方案待真实预检回执后确定。
