#!/usr/bin/env bash
# 只读盘点现有 CPA / Manager / 同项目 Caddy，不执行服务内命令。
set -u
set -o pipefail
export LC_ALL=C

label_value() {
  docker inspect --format "{{with index .Config \"Labels\"}}{{with index . \"$2\"}}{{.}}{{end}}{{end}}" "$1" 2>/dev/null
}

path_summary() {
  local target=$1 metadata size
  printf 'path=%q ' "$target"
  if metadata=$(stat -c 'type=%F mode=%a uid=%u gid=%g bytes=%s' -- "$target" 2>/dev/null); then
    printf '%s ' "$metadata"
    size=$(du -sk -- "$target" 2>/dev/null | awk 'NR==1 {print $1}')
    printf 'disk_kib=%s\n' "${size:-unavailable}"
  else
    printf 'metadata=unavailable\n'
  fi
}

config_version() {
  local target=$1 version
  # 仅识别独立的数字字段；不解析或输出其他配置内容。
  if [[ ! -f "$target" || ! -r "$target" ]]; then
    printf 'config_version=unavailable path=%q\n' "$target"
    return
  fi
  version=$(awk '
    /^[[:space:]]*config-version[[:space:]]*:/ {
      v=$0; sub(/^[^:]*:[[:space:]]*/, "", v); sub(/[[:space:]]*#.*/, "", v);
      gsub(/[[:space:]\r]/, "", v); if (v ~ /^[0-9]+$/) print v; exit
    }
    /^[[:space:]]*"config[-_]version"[[:space:]]*:/ {
      v=$0; sub(/^[^:]*:[[:space:]]*/, "", v); sub(/[,}].*$/, "", v);
      gsub(/[[:space:]\r]/, "", v); if (v ~ /^[0-9]+$/) print v; exit
    }' "$target" 2>/dev/null)
  printf 'config_version=%s path=%q\n' "${version:-not_numeric_or_absent}" "$target"
}

if ! command -v docker >/dev/null 2>&1; then
  printf 'preflight_error=docker_missing\n'
  exit 1
fi
printf 'host_arch=%s\n' "$(uname -m)"
printf 'compose_version=%s\n' "$(docker compose version --short 2>/dev/null || printf unavailable)"
printf 'host_memory:\n'
free -m 2>/dev/null || printf 'memory=unavailable\n'
printf 'host_filesystems:\n'
df -hP 2>/dev/null || printf 'filesystems=unavailable\n'
if ! docker ps -aq >/dev/null 2>&1; then
  printf 'preflight_error=docker_unavailable\n'
  exit 1
fi

declare -A selected=() projects=()
for service in cli-proxy-api cpa-manager-plus; do
  for id in $(docker ps -aq --filter "label=com.docker.compose.service=$service" 2>/dev/null); do
    selected["$id"]=1
  done
  # 同时显示无 Compose 标签的同名容器，不把它推定为某个 Compose 项目。
  for id in $(docker ps -aq --filter "name=$service" 2>/dev/null); do
    selected["$id"]=1
  done
done
if ((${#selected[@]} == 0)); then
  printf 'discovery=no_services\n'
  exit 0
fi
for id in "${!selected[@]}"; do
  project=$(label_value "$id" com.docker.compose.project) || continue
  [[ -n "$project" && "$project" != '<no value>' ]] && projects["$project"]=1
done
printf 'discovery=found project_count=%s target_container_count=%s\n' "${#projects[@]}" "${#selected[@]}"
if ((${#projects[@]} > 1)); then printf 'selection=multiple_projects_no_automatic_choice\n'; fi
for project in "${!projects[@]}"; do
  printf 'project=%q\n' "$project"
  for id in $(docker ps -aq --filter "label=com.docker.compose.project=$project" --filter 'label=com.docker.compose.service=caddy' 2>/dev/null); do
    selected["$id"]=1
  done
done

for id in $(printf '%s\n' "${!selected[@]}" | sort); do
  printf '\ncontainer_record:\n'
  if ! docker inspect --format 'id={{.Id}} name={{.Name}} image_ref={{.Config.Image}} image_id={{.Image}} status={{.State.Status}} health={{with index .State "Health"}}{{.Status}}{{else}}not_configured{{end}}
local_model_cli_tokens={{range .Args}}{{if or (eq . "--local-model") (eq . "-local-model") (eq . "--local-model=true") (eq . "-local-model=true") (eq . "--local-model=false") (eq . "-local-model=false")}}{{.}} {{end}}{{end}}
ports={{range $port,$bindings := .NetworkSettings.Ports}}{{$port}}:{{range $bindings}}{{.HostIp}}:{{.HostPort}};{{end}} {{end}}' "$id" 2>/dev/null; then
    printf 'container_metadata=unavailable\n'
    continue
  fi
  for label_name in project service oneoff project.working_dir project.config_files; do
    value=$(label_value "$id" "com.docker.compose.$label_name")
    printf 'compose_%s=%s\n' "${label_name//project./}" "${value:-not_declared}"
  done
  image=$(docker inspect --format '{{.Image}}' "$id" 2>/dev/null)
  docker image inspect --format 'image_identity={{.Id}} os={{.Os}} arch={{.Architecture}} revision={{with index .Config "Labels"}}{{with index . "org.opencontainers.image.revision"}}{{.}}{{else}}not_declared{{end}}{{else}}not_declared{{end}}' "$image" 2>/dev/null || printf 'image_metadata=unavailable\n'
  workdir=$(label_value "$id" com.docker.compose.project.working_dir)
  [[ -n "$workdir" && "$workdir" != '<no value>' ]] && path_summary "$workdir"
  files=$(label_value "$id" com.docker.compose.project.config_files)
  IFS=',' read -r -a compose_files <<< "$files"
  for file in "${compose_files[@]}"; do
    [[ -n "$file" && "$file" != '<no value>' ]] && path_summary "$file"
  done
  while IFS=$'\t' read -r mount_type mount_name source destination writable; do
    [[ -n "$source" ]] || continue
    printf 'mount type=%s name=%q source=%q target=%q rw=%s\n' "$mount_type" "$mount_name" "$source" "$destination" "$writable"
    [[ "$source" != '-' ]] || continue
    path_summary "$source"
    case "$destination" in
      */config.yaml|*/config.yml|*/config.json) config_version "$source" ;;
      /CLIProxyAPI) [[ -d "$source" ]] && config_version "$source/config.yaml" ;;
    esac
  done < <(docker inspect --format '{{range .Mounts}}{{.Type}}{{printf "\t"}}{{with index . "Name"}}{{.}}{{else}}-{{end}}{{printf "\t"}}{{if .Source}}{{.Source}}{{else}}-{{end}}{{printf "\t%s\t%t\n" .Destination .RW}}{{end}}' "$id" 2>/dev/null)
done
printf '\npreflight_complete=read_only\n'
