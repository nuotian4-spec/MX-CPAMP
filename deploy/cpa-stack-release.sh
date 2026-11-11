#!/usr/bin/env bash
# 仅适用于已盘点的 /opt/cpa-stack；失败保留阶段与材料，不自动回灌数据。
set -eu
umask 077
ROOT=/opt/cpa-stack
STATE=$ROOT/.mx-upgrade-20261110-rc1
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
HELPER=$HERE/cpa-stack-release.py
stage=initial
fail() { printf 'release_failed stage=%s code=%s\n' "$stage" "$1" >&2; exit 1; }
mark() { stage=$1; printf '%s\n' "$stage" > "$STATE/stage"; printf 'stage=%s\n' "$stage"; }
compose_base() { docker compose --project-directory "$ROOT" -p cpa-stack -f "$ROOT/compose.yaml" "$@"; }
compose_both() { docker compose --project-directory "$ROOT" -p cpa-stack -f "$ROOT/compose.yaml" -f "$ROOT/compose.mx-cpamp.yaml" "$@"; }
load_bundle() {
  python3 "$HELPER" archive "$STATE"
  gzip -t "$STATE/bundle/mx-cpamp-images-linux-amd64.tar.gz"
  # FIFO 不落未压缩副本；分别检查解压与 load 退出码。
  mkfifo "$STATE/images.pipe"
  gzip -dc "$STATE/bundle/mx-cpamp-images-linux-amd64.tar.gz" > "$STATE/images.pipe" & unzip_pid=$!
  if docker image load < "$STATE/images.pipe" > "$STATE/image-load.log" 2>&1; then load_code=0; else load_code=$?; fi
  if wait "$unzip_pid"; then unzip_code=0; else unzip_code=$?; fi
  rm -f "$STATE/images.pipe"
  [ "$load_code" = 0 ] && [ "$unzip_code" = 0 ] || fail image_load
  python3 "$HELPER" images "$STATE"
}
secret_prepare() {
  install -d -o root -g root -m 0700 "$ROOT/secrets"
  if [ ! -e "$ROOT/secrets/oauth_login_secret" ]; then
    [ "${1:-create}" = create ] || fail worker_secret_missing
    python3 -c 'import secrets; print(secrets.token_hex(32))' > "$ROOT/secrets/oauth_login_secret"
    chown root:1000 "$ROOT/secrets/oauth_login_secret"
    chmod 0440 "$ROOT/secrets/oauth_login_secret"
  fi
  [ "$(stat -c '%u:%g:%a' "$ROOT/secrets/oauth_login_secret")" = '0:1000:440' ] || fail worker_secret_permissions
  [ "$(wc -c < "$ROOT/secrets/oauth_login_secret")" -ge 32 ] || fail worker_secret_length
}
confirm_deploy() {
  [ -t 0 ] || fail tty_required
  printf '仅切换 CPA/Manager 并新增内部 Worker；Caddy 不停。输入 DEPLOY：'
  IFS= read -r answer || fail confirmation_eof
  [ "$answer" = DEPLOY ] || fail confirmation_cancelled
}
backup_data() {
  backup=$STATE/backup
  install -d -o root -g root -m 0700 "$backup"
  # 已停服务后整体复制 Manager 小目录与 CPA auth，覆盖 SQLite/WAL/data.key/默认 bbolt。
  tar -C "$ROOT" -czf "$backup/data.tar.gz" manager-data cpa/auth cpa/config.yaml compose.yaml Caddyfile secrets/cpamp-admin-key secrets/oauth_login_secret
  [ ! -f "$ROOT/.env" ] || cp -p "$ROOT/.env" "$backup/original.env"
  python3 "$HELPER" backup-env "$STATE"
  kv_path=$(python3 "$HELPER" kv "$STATE")
  case "$kv_path" in
    "$ROOT/cpa/auth/"*) : ;;
    *) [ ! -e "$kv_path" ] || tar -C / -czf "$backup/custom-kv.tar.gz" "${kv_path#/}" ;;
  esac
  chmod 0600 "$backup/"*
  tar -tzf "$backup/data.tar.gz" > "$backup/contents.txt"
  chmod 0600 "$backup/contents.txt"
}
main() {
  [ "$(id -u)" = 0 ] || fail root_required
  for tool in docker python3 gzip tar flock; do command -v "$tool" >/dev/null || fail missing_$tool; done
  [ -f "$ROOT/compose.yaml" ] || fail fixed_site_missing
  install -d -o root -g root -m 0700 "$STATE"
  exec 9> "$STATE/operation.lock"
  flock -n 9 || fail operation_in_progress
  trap 'code=$?; if [ "$code" != 0 ]; then printf "release_failed stage=%s exit=%s state=%s\n" "$stage" "$code" "$STATE" >&2; fi' EXIT
  case "${1:-}" in
    prepare)
      [ ! -f "$STATE/ready" ] && [ ! -f "$STATE/complete" ] || fail already_prepared_or_complete
      if [ -e "$ROOT/compose.mx-cpamp.yaml" ]; then
        [ "$STATE/compose.mx-cpamp.yaml" -ef "$ROOT/compose.mx-cpamp.yaml" ] || fail unknown_existing_override
        mark recovering_prepared_publication
        python3 "$HELPER" images "$STATE"
        python3 "$HELPER" drift "$STATE"
        cmp -s "$HERE/cpa-stack-compose.sh" "$ROOT/cpa-compose" || fail installed_entry_changed
        : > "$STATE/ready"
        mark prepared
        printf 'prepared=%s maintenance_started=no\n' "$STATE"
        return
      fi
      mark prepare_bundle
      install -d -o root -g root -m 0700 "$STATE/bundle"
      case "${2:-}" in
        --bundle-dir)
          cp -- "$3/mx-cpamp-images.manifest.json" "$STATE/bundle/"
          cp -- "$3/mx-cpamp-images-linux-amd64.tar.gz" "$STATE/bundle/" ;;
        --release-url)
          command -v curl >/dev/null || fail curl_missing
          for file in mx-cpamp-images.manifest.json mx-cpamp-images-linux-amd64.tar.gz; do curl -fLsS --proto '=https' "$3/$file" -o "$STATE/bundle/$file"; done ;;
        *) fail bundle_source_required ;;
      esac
      chmod 0600 "$STATE/bundle/"*
      mark prepare_load
      load_bundle
      mark prepare_site
      [ ! -e "$ROOT/compose.mx-cpamp.yaml" ] || fail override_already_exists
      secret_prepare
      python3 "$HELPER" prepare "$STATE"
      install -o root -g root -m 0700 "$HERE/cpa-stack-compose.sh" "$ROOT/cpa-compose"
      mark publishing_prepared
      ln "$STATE/compose.mx-cpamp.yaml" "$ROOT/compose.mx-cpamp.yaml"
      : > "$STATE/ready"
      mark prepared
      printf 'prepared=%s maintenance_started=no\n' "$STATE" ;;
    apply)
      [ -f "$STATE/ready" ] && [ ! -f "$STATE/complete" ] || fail not_prepared
      previous_stage=$(cat "$STATE/stage")
      case "$previous_stage" in prepared|awaiting_confirmation|pre_switch_drift_check) : ;; *) fail maintenance_already_started ;; esac
      mark awaiting_confirmation
      confirm_deploy
      mark pre_switch_drift_check
      secret_prepare verify
      python3 "$HELPER" images "$STATE"
      python3 "$HELPER" drift "$STATE"
      mark stopping_apps
      compose_base stop -t 30 cpa-manager-plus cli-proxy-api > "$STATE/stop.log" 2>&1
      python3 "$HELPER" stopped "$STATE"
      mark backing_up
      backup_data
      mark starting_candidates
      compose_both up -d --no-deps --no-build --pull never oauth-login cli-proxy-api cpa-manager-plus > "$STATE/up.log" 2>&1
      mark authenticated_health
      attempts=0
      until python3 "$HELPER" health "$STATE" > "$STATE/health.log" 2>&1; do attempts=$((attempts+1)); [ "$attempts" -lt 30 ] || fail health_failed; sleep 2; done
      : > "$STATE/complete"
      mark complete
      printf 'release_complete state=%s backup=%s\n' "$STATE" "$STATE/backup" ;;
    *) fail usage_prepare_or_apply ;;
  esac
}
main "$@"
