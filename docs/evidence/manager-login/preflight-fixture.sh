#!/usr/bin/env bash
# 在 Linux 上端到端执行交付脚本；Docker 元数据来自隔离的合成现场。
set -euo pipefail
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
export FIXTURE_ROOT=$root
mkdir -p "$root/bin" "$root/alpha/auths" "$root/beta/auths"
for project in alpha beta; do
  printf 'config-version: 8\napi-keys: [SENTINEL_CONFIG_SECRET]\n' > "$root/$project/config.yaml"
  printf '{"refresh_token":"SENTINEL_ACCOUNT_SECRET"}\n' > "$root/$project/auths/account.json"
  printf 'services: {}\n' > "$root/$project/compose.yaml"
  printf 'services: {}\n' > "$root/$project/override.yaml"
done
cat > "$root/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "$FIXTURE_ROOT/docker.calls"
if [[ $1 == compose && $2 == version ]]; then printf '2.40.0\n'; exit; fi
if [[ $1 == ps ]]; then
  [[ $FIXTURE_CASE == none ]] && exit 0
  if [[ $FIXTURE_CASE == standalone ]]; then
    [[ "$*" == *name=cli-proxy-api* ]] && printf 'standalone\n'
    exit 0
  fi
  case "$*" in
    *com.docker.compose.project=alpha*) printf 'caddya\n' ;;
    *com.docker.compose.project=beta*) printf 'caddyb\n' ;;
    *cli-proxy-api*) printf 'cpaa\n'; [[ $FIXTURE_CASE == multi ]] && printf 'cpab\n' ;;
    *cpa-manager-plus*) printf 'maga\nsetupa\n'; [[ $FIXTURE_CASE == multi ]] && printf 'magb\n' ;;
  esac
  exit 0
fi
if [[ $1 == image && $2 == inspect ]]; then
  [[ "$*" != *'.Config.Labels'* ]] || exit 97
  revision=fixture
  [[ "${@: -1}" == *standalone ]] && revision=not_declared
  printf 'image_identity=%s os=linux arch=amd64 revision=%s\n' "${@: -1}" "$revision"
  exit 0
fi
if [[ $1 == inspect ]]; then
  format=${3:-}; id=${@: -1}; project=alpha
  [[ $id == *b ]] && project=beta
  service=cli-proxy-api; oneoff=False
  [[ $id == mag* || $id == setup* ]] && service=cpa-manager-plus
  [[ $id == caddy* ]] && service=caddy
  [[ $id == setup* ]] && oneoff=True
  base=$FIXTURE_ROOT/$project
  if [[ $id == standalone && $format == *'com.docker.compose.'* ]]; then
    [[ $format != *'.Config.Labels'* ]] || exit 97
    exit 0
  fi
  # 一旦交付脚本读取原始 Env 或完整 inspect，哨兵会进入其输出并使验证失败。
  case "$format" in
    *'.Config.Env'*|'') printf 'SENTINEL_ENV_SECRET\n' ;;
    '{{.Image}}') printf 'sha256:fixture-%s\n' "$id" ;;
    *'range .Mounts'*) printf 'bind\t-\t%s\t/CLIProxyAPI/config.yaml\tfalse\nvolume\tfixture-%s-auth\t%s\t/root/.cli-proxy-api\ttrue\n' "$base/config.yaml" "$project" "$base/auths" ;;
    *'id={{.Id}}'*)
      printf 'id=%s name=/%s image_ref=fixture image_id=sha256:fixture-%s status=running health=healthy\n' "$id" "$id" "$id"
      printf 'local_model_cli_tokens=--local-model\nports=8317/tcp:127.0.0.1:8317\n'
      ;;
    *'project.working_dir'*) printf '%s\n' "$base" ;;
    *'project.config_files'*) printf '%s/compose.yaml,%s/override.yaml\n' "$base" "$base" ;;
    *'com.docker.compose.service'*) printf '%s\n' "$service" ;;
    *'com.docker.compose.oneoff'*) printf '%s\n' "$oneoff" ;;
    *'com.docker.compose.project'*) printf '%s\n' "$project" ;;
    *) printf 'unsupported_fixture_inspect\n' >&2; exit 97 ;;
  esac
  exit 0
fi
printf 'forbidden_docker_command\n' > "$FIXTURE_ROOT/forbidden"
exit 97
DOCKER
chmod +x "$root/bin/docker"
export PATH="$root/bin:$PATH"
bash -n /checks/readonly-preflight.sh
printf 'bash_syntax_exit=0\n'
for scenario in none single multi standalone; do
  export FIXTURE_CASE=$scenario
  bash /checks/readonly-preflight.sh > "$root/$scenario.stdout"
  if grep -Eq 'SENTINEL_(ENV|CONFIG|ACCOUNT)_SECRET' "$root/$scenario.stdout"; then
    printf 'secret_leak=%s\n' "$scenario"; exit 1
  fi
  case "$scenario" in
    none) grep -q 'discovery=no_services' "$root/$scenario.stdout" ;;
    single)
      grep -q 'project_count=1 target_container_count=3' "$root/$scenario.stdout"
      grep -q 'compose_oneoff=True' "$root/$scenario.stdout"
      grep -q 'compose_service=caddy' "$root/$scenario.stdout"
      grep -q 'config_version=8' "$root/$scenario.stdout"
      grep -q 'override.yaml' "$root/$scenario.stdout"
      grep -q 'mount type=volume name=fixture-alpha-auth' "$root/$scenario.stdout"
      grep -q 'mount type=bind name=-' "$root/$scenario.stdout"
      ;;
    multi)
      grep -q 'project_count=2 target_container_count=5' "$root/$scenario.stdout"
      grep -q 'selection=multiple_projects_no_automatic_choice' "$root/$scenario.stdout"
      grep -q 'compose_project=beta' "$root/$scenario.stdout"
      ;;
    standalone)
      grep -q 'project_count=0 target_container_count=1' "$root/$scenario.stdout"
      grep -q 'compose_project=not_declared' "$root/$scenario.stdout"
      grep -q 'arch=amd64 revision=not_declared' "$root/$scenario.stdout"
      ;;
  esac
  printf 'scenario=%s exit=0 secret_sentinels=absent\n' "$scenario"
done
test ! -e "$root/forbidden"
printf 'preflight_fixture_exit=0\n'
