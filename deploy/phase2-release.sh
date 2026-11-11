#!/usr/bin/env bash
# 二期独立发布入口；状态和秘密只在服务器私有目录保存。
set -eu
umask 077
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$HERE/phase2-release.py" "$@"
