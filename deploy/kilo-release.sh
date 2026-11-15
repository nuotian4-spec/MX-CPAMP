#!/bin/sh
# Kilo独立入口；start/health/rollback保持Caddy停止，不自动恢复入口。
set -eu
umask 077
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$HERE/kilo-release.py" "$@"
