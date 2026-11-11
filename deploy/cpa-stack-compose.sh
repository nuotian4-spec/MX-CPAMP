#!/usr/bin/env bash
# 后续管理固定使用原配置与持久 override，保持原项目名。
set -eu
cd /opt/cpa-stack
exec docker compose --project-directory /opt/cpa-stack -p cpa-stack -f /opt/cpa-stack/compose.yaml -f /opt/cpa-stack/compose.mx-cpamp.yaml "$@"
