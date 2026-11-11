#!/usr/bin/env python3
"""固定 cpa-stack 现场的隐藏配置核验；正文仅存 root 私有目录。"""
import hashlib
import json
import os
import pathlib
import posixpath
import subprocess
import sys
import tarfile
import urllib.request
import urllib.error

ROOT = pathlib.Path('/opt/cpa-stack')
REVISIONS = {'cpa': '5c90a92eef27f450c7080bc8735c88d873fc8915', 'manager': '07572357832b69a30e3ee6c45f9f0cc2c3350e0c', 'worker': '42b04ac71df347e9c22fa35bad484c437da52d4b'}
OLD = {'cli-proxy-api': 'sha256:8377845ba3ec132c3bc41765d348e693703dc88e232d37f7a7eafc2721bff553', 'cpa-manager-plus': 'sha256:0e803f21d51aceea7b752686417bf57f40678d8068fe102ba23cb2a3afb414e1', 'caddy': 'sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d'}
MOUNTS = {
 'cli-proxy-api': {'/CLIProxyAPI/config.yaml': ('bind', str(ROOT/'cpa/config.yaml'), True), '/CLIProxyAPI/logs': ('bind', str(ROOT/'cpa/logs'), True), '/CLIProxyAPI/plugins': ('bind', str(ROOT/'cpa/plugins'), True), '/root/.cli-proxy-api': ('bind', str(ROOT/'cpa/auth'), True)},
 'cpa-manager-plus': {'/data': ('bind', str(ROOT/'manager-data'), True), '/run/secrets/cpamp-admin-key': ('bind', str(ROOT/'secrets/cpamp-admin-key'), False)},
 'caddy': {'/config': ('volume', 'cpa-stack_caddy_config', True), '/data': ('volume', 'cpa-stack_caddy_data', True), '/etc/caddy/Caddyfile': ('bind', str(ROOT/'Caddyfile'), False)},
}

def require(value, code):
    if not value:
        raise RuntimeError(code)

def read(path):
    return json.loads(pathlib.Path(path).read_text())

def write(path, value):
    pathlib.Path(path).write_text(json.dumps(value, sort_keys=True, indent=2)+'\n')
    os.chmod(path, 0o600)

def command(args):
    result = subprocess.run(args, capture_output=True, text=True)
    require(result.returncode == 0, 'command_failed_'+args[1].replace('-', '_'))
    return result.stdout

def manifest(state):
    value = read(state/'bundle/mx-cpamp-images.manifest.json')
    require(value.get('schema') == 1, 'manifest_schema')
    archive = value.get('archive', {})
    require(archive.get('file') == 'mx-cpamp-images-linux-amd64.tar.gz', 'archive_name')
    require(len(archive.get('sha256', '')) == 64, 'archive_digest')
    for role, revision in REVISIONS.items():
        image = value['images'][role]
        expected_ref = {'cpa': 'mx-cpa', 'manager': 'mx-cpamp', 'worker': 'mx-oauth-login'}[role]+':20261110-rc1'
        require(image['ref'] == expected_ref and image['revision'] == revision, 'manifest_product_'+role)
        require(image['os'] == 'linux' and image['architecture'] == 'amd64', 'manifest_platform_'+role)
        require(image['accepted_ids'] and all(i.startswith('sha256:') and len(i) == 71 for i in image['accepted_ids']), 'manifest_ids_'+role)
    return value

def verify_archive(state):
    value = manifest(state)
    digest = hashlib.sha256()
    with (state/'bundle'/value['archive']['file']).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024*1024), b''):
            digest.update(chunk)
    require(digest.hexdigest() == value['archive']['sha256'], 'archive_digest_mismatch')

def verify_images(state):
    value = manifest(state)
    for role, expected in value['images'].items():
        actual = json.loads(command(['docker', 'image', 'inspect', expected['ref']]))[0]
        require(actual['Id'] in expected['accepted_ids'], 'image_id_'+role)
        require(actual['Os'] == expected['os'] and actual['Architecture'] == expected['architecture'], 'image_platform_'+role)
        require((actual.get('Config', {}).get('Labels') or {}).get('org.opencontainers.image.revision') == expected['revision'], 'image_revision_'+role)

def compose(files):
    return ['docker', 'compose', '--project-directory', str(ROOT), '-p', 'cpa-stack'] + sum((['-f', str(p)] for p in files), [])

def snapshot(state, prefix):
    ids = []
    for service in OLD:
        matched = command(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.service='+service]).split()
        for identifier in matched:
            obj = json.loads(command(['docker', 'inspect', identifier]))[0]
            labels = obj.get('Config', {}).get('Labels') or {}
            if str(labels.get('com.docker.compose.oneoff', '')).lower() != 'true':
                ids.append(obj)
    write(state/(prefix+'-live.json'), ids)
    raw = command(compose([ROOT/'compose.yaml'])+['config', '--format', 'json'])
    write(state/(prefix+'-base.json'), json.loads(raw))
    sources = command(compose([ROOT/'compose.yaml'])+['config', '--no-env-resolution', '--format', 'json'])
    write(state/(prefix+'-sources.json'), json.loads(sources))
    # Compose 原生 YAML 解析器只处理 extension；禁用插值，避免改变配置中的 $。
    wrapper = state/'config-audit.yaml'
    text = (ROOT/'cpa/config.yaml').read_text()
    wrapper.write_text('name: cpa-config-audit\nservices: {}\nx-cpa:\n'+''.join('  '+line+'\n' for line in text.splitlines()))
    os.chmod(wrapper, 0o600)
    parsed = command(['docker', 'compose', '-f', str(wrapper), 'config', '--no-interpolate', '--format', 'json'])
    write(state/(prefix+'-cpa.json'), json.loads(parsed)['x-cpa'])

def validate(state, prefix):
    live, base, cfg = read(state/(prefix+'-live.json')), read(state/(prefix+'-base.json')), read(state/(prefix+'-cpa.json'))
    require(set(base['services']) == set(OLD), 'compose_services_changed')
    by_service = {}
    for obj in live:
        labels = obj['Config'].get('Labels') or {}
        service = labels.get('com.docker.compose.service')
        require(service not in by_service, 'multiple_service_instances')
        require(labels.get('com.docker.compose.project') == 'cpa-stack', 'other_project')
        require(labels.get('com.docker.compose.project.working_dir') == str(ROOT), 'working_dir_changed')
        require(labels.get('com.docker.compose.project.config_files') == str(ROOT/'compose.yaml'), 'compose_chain_changed')
        require(obj['State']['Running'] and obj['Image'] == OLD[service], 'baseline_state_or_image_'+service)
        mounts = {m['Destination']: (m['Type'], m.get('Name') if m['Type'] == 'volume' else m['Source'], m['RW']) for m in obj['Mounts']}
        require(mounts == MOUNTS[service], 'baseline_mounts_'+service)
        rendered = base['services'][service]
        require(rendered.get('image') == obj['Config']['Image'], 'compose_image_drift_'+service)
        planned_mounts = {}
        for mount in rendered.get('volumes', []):
            source = mount['source']
            if mount['type'] == 'volume': source = base.get('volumes', {}).get(source, {}).get('name', source)
            planned_mounts[mount['target']] = (mount['type'], source, not mount.get('read_only', False))
        require(planned_mounts == mounts, 'compose_mount_drift_'+service)
        planned_ports = {(str(p['target'])+'/'+p.get('protocol', 'tcp'), str(p['published'])) for p in rendered.get('ports', []) if p.get('published')}
        current_ports = {(target, p['HostPort']) for target, bindings in (obj['NetworkSettings'].get('Ports') or {}).items() for p in (bindings or [])}
        require(planned_ports == current_ports, 'compose_port_drift_'+service)
        actual_env = dict(item.split('=', 1) for item in obj['Config'].get('Env', []) if '=' in item)
        require(all(str(v if v is not None else '') == actual_env.get(k) for k, v in rendered.get('environment', {}).items()), 'compose_env_drift_'+service)
        if service != 'caddy':
            require(not any(obj['NetworkSettings'].get('Ports', {}).values()), 'unexpected_published_port')
        by_service[service] = obj
    require(set(by_service) == set(OLD), 'missing_service')
    require(cfg.get('config-version') == 8, 'cpa_config_version')
    auth = cfg.get('oauth', {}).get('auth-dir', cfg.get('auth-dir', '/root/.cli-proxy-api'))
    require(auth in ('/root/.cli-proxy-api', '~/.cli-proxy-api'), 'unknown_auth_path')
    kv = cfg.get('server', {}).get('kv-store', cfg.get('kv-store', {})) or {}
    raw = str(kv.get('path') or '/root/.cli-proxy-api/kv.db').strip()
    if raw.startswith('~'):
        raw = '/root/'+raw[1:].lstrip('/\\').replace('\\', '/')
    cwd = by_service['cli-proxy-api']['Config'].get('WorkingDir') or '/'
    path = posixpath.normpath(raw if raw.startswith('/') else posixpath.join(cwd, raw))
    kv_host = None
    for target, (kind, source, _) in sorted(MOUNTS['cli-proxy-api'].items(), key=lambda item: -len(item[0])):
        if path.startswith(target+'/') and kind == 'bind':
            kv_host = source+path[len(target):]
            break
    require(kv_host is not None, 'unknown_kv_persistence_path')
    args = by_service['cli-proxy-api'].get('Args') or []
    require(not any(a.lstrip('-').startswith('home-jwt') for a in args), 'home_mode_not_supported')
    manager = by_service['cpa-manager-plus']
    env = dict(item.split('=', 1) for item in manager['Config'].get('Env', []) if '=' in item)
    for key in ('USAGE_DATA_DIR', 'USAGE_DB_PATH', 'CPA_MANAGER_DATA_KEY_PATH', 'CPA_MANAGER_CONFIG'):
        if env.get(key):
            normalized = posixpath.normpath(env[key])
            require(normalized == '/data' or normalized.startswith('/data/'), 'manager_external_path_'+key)
    require(not env.get('CPA_MANAGER_DATA_KEY_FILE') or posixpath.normpath(env['CPA_MANAGER_DATA_KEY_FILE']).startswith('/data/'), 'manager_external_data_key')
    manager_config = None
    selected_config = env.get('CPA_MANAGER_CONFIG', '').strip()
    if selected_config:
        selected_config = posixpath.normpath(selected_config)
        require(selected_config.startswith('/data/'), 'manager_config_location_not_confirmed')
        manager_config = ROOT/'manager-data'/selected_config.removeprefix('/data/')
        require(manager_config.is_file(), 'manager_selected_config_missing')
        persisted = read(manager_config)
        for key in ('dataDir', 'dbPath', 'dataKeyPath', 'dataKeyFile'):
            if persisted.get(key):
                resolved = posixpath.normpath(posixpath.join(posixpath.dirname(selected_config), str(persisted[key])))
                require(resolved == '/data' or resolved.startswith('/data/'), 'manager_config_external_'+key)
    else:
        require(env.get('USAGE_DATA_DIR') or env.get('USAGE_DB_PATH'), 'manager_env_mode_not_confirmed')
        presence = command(['docker', 'exec', manager['Id'], 'sh', '-c', 'if [ -e /usr/local/bin/config.json ]; then printf present; elif [ -d /usr/local/bin ] && [ -r /usr/local/bin ]; then printf absent; else exit 2; fi'])
        require(presence == 'absent', 'manager_unmapped_default_config')
    api_keys = cfg.get('access', {}).get('api-keys', cfg.get('api-keys', []))
    require(api_keys and isinstance(api_keys[0], str) and api_keys[0], 'cpa_api_key_required_for_probe')
    tracked = [ROOT/'compose.yaml', ROOT/'cpa/config.yaml', ROOT/'Caddyfile', ROOT/'secrets/cpamp-admin-key']
    if manager_config is not None: tracked.append(manager_config)
    environment_files = [ROOT/'.env'] if (ROOT/'.env').exists() else []
    sources = read(state/(prefix+'-sources.json'))
    for service in sources['services'].values():
        for item in service.get('env_file', []):
            path = pathlib.Path(item['path'] if isinstance(item, dict) else item)
            if path.exists(): environment_files.append(path)
            else: require(isinstance(item, dict) and not item.get('required', True), 'required_env_file_missing')
    environment_files = sorted(set(environment_files))
    tracked.extend(environment_files)
    # 真正阻止切换前配置漂移；摘要被 apply 消费。
    fingerprints = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in tracked}
    return {'container_ids': {s: obj['Id'] for s, obj in by_service.items()}, 'file_digests': fingerprints, 'manager_config_mode': selected_config or 'confirmed_env_only', 'environment_files': [str(p) for p in environment_files], 'kv_host_path': kv_host, 'api_key': api_keys[0], 'base': base}

def make_override(state):
    value, base = manifest(state), read(state/'prepared-base.json')
    secret = {'source': 'mx_oauth_login_secret', 'target': 'oauth_login_secret'}
    services = {
      'cli-proxy-api': {'image': value['images']['cpa']['ref'], 'pull_policy': 'never'},
      'cpa-manager-plus': {'image': value['images']['manager']['ref'], 'pull_policy': 'never', 'environment': {'OAUTH_LOGIN_URL': 'http://oauth-login:18417', 'OAUTH_LOGIN_SECRET_FILE': '/run/secrets/oauth_login_secret'}, 'secrets': [secret]},
      'oauth-login': {'image': value['images']['worker']['ref'], 'pull_policy': 'never', 'init': True, 'restart': 'unless-stopped', 'read_only': True, 'tmpfs': ['/tmp:mode=1777,size=64m'], 'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true'], 'environment': {'OAUTH_LOGIN_SECRET_FILE': '/run/secrets/oauth_login_secret'}, 'secrets': [secret]},
    }
    networks = base['services']['cpa-manager-plus'].get('networks')
    if networks: services['oauth-login']['networks'] = {name: None for name in networks}
    write(state/'compose.mx-cpamp.yaml', {'services': services, 'secrets': {'mx_oauth_login_secret': {'file': str(ROOT/'secrets/oauth_login_secret')}}})

def check_merge(state):
    base = read(state/'prepared-base.json')
    merged = json.loads(command(compose([ROOT/'compose.yaml', state/'compose.mx-cpamp.yaml'])+['config', '--format', 'json']))
    write(state/'merged.json', merged)
    require(set(merged['services']) == set(base['services']) | {'oauth-login'}, 'merged_services')
    require(merged['services']['caddy'] == base['services']['caddy'], 'caddy_changed')
    for service in ('cli-proxy-api', 'cpa-manager-plus'):
        actual, expected = dict(merged['services'][service]), dict(base['services'][service])
        for field in ('image', 'pull_policy'): actual.pop(field, None); expected.pop(field, None)
        if service == 'cpa-manager-plus':
            env = actual.get('environment', {})
            require(env.pop('OAUTH_LOGIN_URL', None) == 'http://oauth-login:18417', 'worker_url')
            require(env.pop('OAUTH_LOGIN_SECRET_FILE', None) == '/run/secrets/oauth_login_secret', 'worker_secret_env')
            require(actual.pop('secrets', []) == expected.get('secrets', []) + [{'source': 'mx_oauth_login_secret', 'target': 'oauth_login_secret'}], 'manager_secret_mount')
            expected.pop('secrets', None)
        require(actual == expected, 'unexpected_merge_'+service)
    worker = merged['services']['oauth-login']
    require(not worker.get('ports') and not worker.get('volumes'), 'worker_exposure_or_data_mount')

def drift(state):
    snapshot(state, 'current')
    expected, actual = read(state/'plan.json'), validate(state, 'current')
    require(actual == expected, 'live_or_config_drift')
    require(read(state/'merged.json') == json.loads(command(compose([ROOT/'compose.yaml', ROOT/'compose.mx-cpamp.yaml'])+['config', '--format', 'json'])), 'override_drift')

def health(state):
    plan = read(state/'plan.json')
    worker = command(compose([ROOT/'compose.yaml', ROOT/'compose.mx-cpamp.yaml'])+['ps', '-q', 'oauth-login']).strip()
    require(worker, 'worker_missing')
    payload = {'api': plan['api_key'], 'admin': (ROOT/'secrets/cpamp-admin-key').read_text().strip()}
    program = "const fs=require('fs');let s='';process.stdin.on('data',c=>s+=c);process.stdin.on('end',async()=>{try{const k=JSON.parse(s),w=fs.readFileSync('/run/secrets/oauth_login_secret','utf8').trim();for(const [u,t,status]of [['http://cli-proxy-api:8317/v1/models',k.api,200],['http://cpa-manager-plus:18317/api/oauth-login/tasks/00000000-0000-4000-8000-000000000001',k.admin,404],['http://oauth-login:18417/health',w,200]]){const r=await fetch(u,{headers:{authorization:'Bearer '+t},signal:AbortSignal.timeout(5000)});if(r.status!==status)throw Error('probe');await r.arrayBuffer();}console.log('authenticated_probes=passed');}catch{process.exitCode=1;console.error('authenticated_probe_failed');}});"
    result = subprocess.run(['docker', 'exec', '-i', worker, 'node', '-e', program], input=json.dumps(payload), text=True, capture_output=True)
    require(result.returncode == 0, 'authenticated_probe_failed')
    caddy = plan['container_ids']['caddy']
    require(json.loads(command(['docker', 'inspect', caddy]))[0]['State']['Running'], 'caddy_not_running')
    print('authenticated_probes=passed caddy_original_container=running')

def baseline_health(state):
    plan = read(state/'plan.json')
    objects = {obj['Id']: obj for obj in read(state/'prepared-live.json')}
    cpa = objects[plan['container_ids']['cli-proxy-api']]
    manager = objects[plan['container_ids']['cpa-manager-plus']]
    shared = set(cpa['NetworkSettings']['Networks']) & set(manager['NetworkSettings']['Networks'])
    require(len(shared) == 1, 'baseline_shared_network_unknown')
    network = next(iter(shared))
    admin = (ROOT/'secrets/cpamp-admin-key').read_text().strip()
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for role, obj, port, path, key, expected_status in [('cpa', cpa, 8317, '/v1/models', plan['api_key'], 200), ('manager', manager, 18317, '/usage-service/config', admin, 200)]:
        address = obj['NetworkSettings']['Networks'][network]['IPAddress']
        require(address, 'baseline_ip_missing_'+role)
        request = urllib.request.Request('http://'+address+':'+str(port)+path, headers={'Authorization': 'Bearer '+key})
        try:
            with opener.open(request, timeout=5) as response: require(response.status == expected_status, 'baseline_auth_failed_'+role)
        except urllib.error.HTTPError as error:
            if error.code != expected_status: raise RuntimeError('baseline_auth_failed_'+role)
        except urllib.error.URLError: raise RuntimeError('baseline_probe_failed_'+role)
        if role == 'manager':
            invalid = urllib.request.Request(request.full_url, headers={'Authorization': 'Bearer '+admin+'-invalid-probe'})
            try:
                with opener.open(invalid, timeout=5): raise RuntimeError('baseline_manager_auth_not_enforced')
            except urllib.error.HTTPError as error:
                require(error.code == 401, 'baseline_manager_invalid_key_status')

def stopped(state):
    plan = read(state/'plan.json')
    for service, identifier in plan['container_ids'].items():
        running = json.loads(command(['docker', 'inspect', identifier]))[0]['State']['Running']
        require(running == (service == 'caddy'), 'unexpected_stop_state_'+service)

def main():
    action, state = sys.argv[1], pathlib.Path(sys.argv[2])
    if action == 'archive': verify_archive(state)
    elif action == 'images': verify_images(state)
    elif action == 'prepare':
        snapshot(state, 'prepared')
        write(state/'plan.json', validate(state, 'prepared'))
        baseline_health(state)
        make_override(state)
        check_merge(state)
    elif action == 'drift': drift(state)
    elif action == 'health': health(state)
    elif action == 'stopped': stopped(state)
    elif action == 'kv': print(read(state/'plan.json')['kv_host_path'])
    elif action == 'backup-env':
        with tarfile.open(state/'backup/environment-sources.tar.gz', 'w:gz') as archive:
            for path in read(state/'plan.json')['environment_files']: archive.add(path, arcname=path.lstrip('/'))
        os.chmod(state/'backup/environment-sources.tar.gz', 0o600)
    else: raise RuntimeError('unknown_action')

if __name__ == '__main__':
    try: main()
    except Exception as error:
        print('release_check_failed='+ (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
