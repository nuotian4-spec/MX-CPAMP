"""真实 Linux/Compose 与交付函数的最小边界，不连接生产。"""
import copy
import gzip
import hashlib
import importlib.util
import json
import os
import pathlib
import pty
import select
import subprocess
import tarfile
import time

spec = importlib.util.spec_from_file_location('release', '/delivery/cpa-stack-release.py')
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)
root, state = r.ROOT, r.ROOT/'.mx-upgrade-20261110-rc1'
state.mkdir(parents=True, mode=0o700)
(state/'bundle').mkdir(mode=0o700)
for name in ('manager-data', 'cpa/auth', 'cpa/logs', 'cpa/plugins', 'secrets'):
    (root/name).mkdir(parents=True, exist_ok=True)
for name, body in {'manager-data/config.json': json.dumps({'dataDir': '/data', 'dbPath': '/data/usage.sqlite'}), 'manager-data/usage.sqlite': 'sqlite-fixture', 'manager-data/usage.sqlite-wal': 'wal-fixture', 'manager-data/data.key': 'KEY_SENTINEL', 'cpa/auth/account.json': 'TOKEN_SENTINEL', 'cpa/auth/kv.db': 'bolt-fixture', 'Caddyfile': 'example.invalid {}', 'secrets/cpamp-admin-key': 'FAKE_ADMIN_'+'a'*40, '.env': 'KEEP=ENV_SENTINEL\n', 'manager.env': 'RETAINED=ENV_FILE_SENTINEL\n'}.items():
    (root/name).write_text(body)
cfg = {'config-version': 8, 'oauth': {'auth-dir': '/root/.cli-proxy-api'}, 'server': {'host': '0.0.0.0', 'port': 8317, 'kv-store': {}}, 'management': {'allow-remote': True, 'secret-key': 'FAKE_MANAGEMENT_'+'c'*40, 'disable-control-panel': True, 'disable-auto-update-panel': True}, 'access': {'api-keys': ['FAKE_API_'+'b'*40]}}
(root/'cpa/config.yaml').write_text(json.dumps(cfg))
services = {}
refs = {'cli-proxy-api': 'eceasy/cli-proxy-api:v8.0.63', 'cpa-manager-plus': 'seakee/cpa-manager-plus:v1.14.10', 'caddy': 'caddy:2.10.2-alpine'}
for service, mounts in r.MOUNTS.items():
    volumes = []
    for target, (kind, source, rw) in mounts.items():
        volumes.append({'type': kind, 'source': source if kind == 'bind' else source.removeprefix('cpa-stack_'), 'target': target, 'read_only': not rw})
    services[service] = {'image': refs[service], 'volumes': volumes, 'networks': {'default': None}}
services['cpa-manager-plus'].update(environment={'CPA_MANAGER_CONFIG': '/data/config.json', 'USAGE_DATA_DIR': '/data', 'USAGE_DB_PATH': '/data/usage.sqlite'}, env_file=[str(root/'manager.env')])
services['caddy']['ports'] = ['80:80', '443:443']
base = {'services': services, 'volumes': {'caddy_data': {'name': 'cpa-stack_caddy_data'}, 'caddy_config': {'name': 'cpa-stack_caddy_config'}}, 'networks': {'default': {'name': 'cpa-stack_default'}}}
(root/'compose.yaml').write_text(json.dumps(base))
actual_command = r.command
default_config_presence = 'absent'
rendered = json.loads(actual_command(r.compose([root/'compose.yaml'])+['config', '--format', 'json']))
live = {}
for service in services:
    model = rendered['services'][service]
    mounts = [{'Type': kind, 'Source': source if kind == 'bind' else '/var/lib/docker/volumes/'+source+'/_data', 'Name': source if kind == 'volume' else '', 'Destination': target, 'RW': rw} for target, (kind, source, rw) in r.MOUNTS[service].items()]
    live[service] = {'Id': 'fixture-'+service, 'Image': r.OLD[service], 'Args': [], 'State': {'Running': True}, 'Config': {'Image': refs[service], 'WorkingDir': '/CLIProxyAPI', 'Env': [k+'='+str(v) for k,v in model.get('environment', {}).items()], 'Labels': {'com.docker.compose.project': 'cpa-stack', 'com.docker.compose.service': service, 'com.docker.compose.oneoff': 'False', 'com.docker.compose.project.working_dir': str(root), 'com.docker.compose.project.config_files': str(root/'compose.yaml')}}, 'Mounts': mounts, 'NetworkSettings': {'Ports': {'80/tcp': [{'HostPort': '80'}], '443/tcp': [{'HostPort': '443'}]} if service == 'caddy' else {}}}

def fixture_command(args):
    if args[:2] == ['docker', 'ps']:
        service = args[-1].split('=')[-1]
        return service+'\n' if service in live else ''
    if args[:2] == ['docker', 'inspect']:
        key = args[-1].removeprefix('fixture-')
        return json.dumps([live[key]])
    if args[:3] == ['docker', 'exec', 'fixture-cpa-manager-plus']:
        return default_config_presence
    return actual_command(args)

r.command = fixture_command
manifest = json.loads(pathlib.Path('/release/mx-cpamp-images.manifest.json').read_text())
r.write(state/'bundle/mx-cpamp-images.manifest.json', manifest)
r.snapshot(state, 'prepared')
plan = r.validate(state, 'prepared')
r.write(state/'plan.json', plan)
r.make_override(state)
r.check_merge(state)
assert not (root/'compose.mx-cpamp.yaml').exists()
assert set(plan['environment_files']) == {str(root/'.env'), str(root/'manager.env')}
assert plan['kv_host_path'] == str(root/'cpa/auth/kv.db')
assert r.read(state/'merged.json')['volumes']['caddy_data']['name'] == 'cpa-stack_caddy_data'
print('real_compose_merge=passed caddy_and_named_volumes=preserved staged_override_only=true')
os.link(state/'compose.mx-cpamp.yaml', root/'compose.mx-cpamp.yaml')
assert os.path.samefile(state/'compose.mx-cpamp.yaml', root/'compose.mx-cpamp.yaml')
r.drift(state)
original_manager_config = (root/'manager-data/config.json').read_text()
(root/'manager-data/config.json').write_text(json.dumps({'dataDir': '/data', 'dbPath': '/data/another.sqlite'}))
try:
    r.drift(state)
except RuntimeError as error:
    assert str(error) == 'live_or_config_drift'
else: raise AssertionError('manager_selected_json_drift_missed')
(root/'manager-data/config.json').write_text(original_manager_config)
print('published_override_same_inode_and_selected_manager_json_drift=passed')

def rejected(fn, code):
    try: fn()
    except RuntimeError as error:
        assert str(error) == code, str(error)
    else: raise AssertionError('expected_rejection_'+code)

saved = copy.deepcopy(r.read(state/'prepared-cpa.json'))
bad = copy.deepcopy(saved); bad['server']['kv-store']['path'] = '/tmp/unknown.db'
r.write(state/'prepared-cpa.json', bad)
rejected(lambda: r.validate(state, 'prepared'), 'unknown_kv_persistence_path')
r.write(state/'prepared-cpa.json', saved)
selected = root/'manager-data/selected.json'
selected.write_text(json.dumps({'dataKeyPath': '/tmp/missed.key'}))
live['cpa-manager-plus']['Config']['Env'] = [e.replace('CPA_MANAGER_CONFIG=/data/config.json', 'CPA_MANAGER_CONFIG=/data/selected.json') for e in live['cpa-manager-plus']['Config']['Env']]
# 保持渲染和真实Env一致，只测已选择JSON的持久路径。
changed = r.read(state/'prepared-base.json'); changed['services']['cpa-manager-plus']['environment']['CPA_MANAGER_CONFIG'] = '/data/selected.json'; r.write(state/'prepared-base.json', changed)
r.write(state/'prepared-live.json', list(live.values()))
rejected(lambda: r.validate(state, 'prepared'), 'manager_config_external_dataKeyPath')
live['cpa-manager-plus']['Config']['Env'] = [e.replace('CPA_MANAGER_CONFIG=/data/selected.json', 'CPA_MANAGER_CONFIG=/data/config.json') for e in live['cpa-manager-plus']['Config']['Env']]
r.snapshot(state, 'prepared')
print('unknown_kv_and_selected_manager_config_rejections=passed')
saved_base = r.read(state/'prepared-base.json')
saved_live = r.read(state/'prepared-live.json')
env_base = copy.deepcopy(saved_base); env_base['services']['cpa-manager-plus']['environment'].pop('CPA_MANAGER_CONFIG')
env_live = copy.deepcopy(saved_live)
for obj in env_live:
    if obj['Id'] == 'fixture-cpa-manager-plus': obj['Config']['Env'] = [v for v in obj['Config']['Env'] if not v.startswith('CPA_MANAGER_CONFIG=')]
r.write(state/'prepared-base.json', env_base); r.write(state/'prepared-live.json', env_live)
assert r.validate(state, 'prepared')['manager_config_mode'] == 'confirmed_env_only'
default_config_presence = 'present'
rejected(lambda: r.validate(state, 'prepared'), 'manager_unmapped_default_config')
default_config_presence = 'absent'
r.write(state/'prepared-base.json', saved_base); r.write(state/'prepared-live.json', saved_live)
print('real_default_env_mode_and_unknown_container_json_rejection=passed')
custom = copy.deepcopy(saved); custom['server']['kv-store']['path'] = '/CLIProxyAPI/plugins/custom.db'
(root/'cpa/config.yaml').write_text(json.dumps(custom))
(root/'cpa/plugins/custom.db').write_text('custom-bbolt-fixture')
r.snapshot(state, 'prepared')
plan = r.validate(state, 'prepared'); r.write(state/'plan.json', plan)
assert plan['kv_host_path'] == str(root/'cpa/plugins/custom.db')

library = pathlib.Path('/delivery/cpa-stack-release.sh').read_text().rsplit('main "$@"', 1)[0]
pathlib.Path('/tmp/release-library.sh').write_text(library)
prefix = '. /tmp/release-library.sh; HELPER=/delivery/cpa-stack-release.py; '
def shell(body, **kwargs):
    return subprocess.run(['sh', '-c', prefix+body], capture_output=True, text=True, **kwargs)
result = shell('secret_prepare')
assert result.returncode == 0, result.stderr
secret = root/'secrets/oauth_login_secret'
assert secret.stat().st_uid == 0 and secret.stat().st_gid == 1000 and secret.stat().st_mode & 0o777 == 0o440
original_secret = secret.read_bytes()
assert shell('secret_prepare').returncode == 0 and secret.read_bytes() == original_secret
assert shell('secret_prepare verify').returncode == 0
secret.chmod(0o600)
assert shell('secret_prepare verify').returncode != 0 and secret.stat().st_mode & 0o777 == 0o600
secret.chmod(0o440)
volume = os.environ['FIXTURE_SECRET_VOLUME']
secret_mount = actual_command(['docker', 'volume', 'inspect', '--format', '{{.Mountpoint}}', volume]).strip()+'/oauth_login_secret'
worker = subprocess.run(['docker', 'run', '--rm', '--network', 'none', '--mount', 'type=bind,source='+secret_mount+',target=/run/secrets/oauth_login_secret,readonly', '--entrypoint', 'node', 'mx-oauth-login:20261110-rc1', '-e', "const f=require('fs');if(f.readFileSync('/run/secrets/oauth_login_secret','utf8').trim().length<32)process.exit(1);console.log('worker_uid1000_secret_read=passed')"], capture_output=True, text=True)
assert worker.returncode == 0, worker.stderr
print('secret_root1000_0440_and_actual_worker_read=passed existing_secret=preserved')

assert shell('confirm_deploy', input='DEPLOY\n').returncode != 0
for text, expected in [('DEPLOY\n', 0), ('CANCEL\n', 1), ('\x04', 1)]:
    master, slave = pty.openpty()
    process = subprocess.Popen(['sh', '-c', prefix+'confirm_deploy'], stdin=slave, stdout=slave, stderr=slave)
    os.close(slave); time.sleep(.1); os.write(master, text.encode()); process.wait(timeout=3); os.close(master)
    assert process.returncode == expected
print('real_pty_confirm_cancel_eof_and_no_tty=passed')

rejected(lambda: r.stopped(state), 'unexpected_stop_state_cli-proxy-api')
live['cli-proxy-api']['State']['Running'] = False; live['cpa-manager-plus']['State']['Running'] = False
r.stopped(state)
result = shell('backup_data')
assert result.returncode == 0, result.stderr
assert (state/'backup').stat().st_mode & 0o777 == 0o700
with tarfile.open(state/'backup/data.tar.gz') as archive:
    for entry in ('manager-data/usage.sqlite', 'manager-data/usage.sqlite-wal', 'manager-data/data.key', 'cpa/auth/kv.db'): assert entry in archive.getnames()
for file in (state/'backup').iterdir(): assert file.stat().st_mode & 0o777 == 0o600
with tarfile.open(state/'backup/custom-kv.tar.gz') as archive: assert str(root/'cpa/plugins/custom.db').lstrip('/') in archive.getnames()
print('backup_stopped_state_guard_and_sqlite_wal_key_bbolt_permissions=passed')
print('custom_kv_plugins_host_mapping_and_actual_archive=passed')

# 小归档穿真实 load_bundle；正式226MB归档由P9的独立Linux回执覆盖。
archive = state/'bundle/mx-cpamp-images-linux-amd64.tar.gz'
save = subprocess.Popen(['docker', 'image', 'save', 'python:3.13-alpine'], stdout=subprocess.PIPE)
with gzip.open(archive, 'wb') as out:
    while chunk := save.stdout.read(1024*1024): out.write(chunk)
assert save.wait() == 0
manifest['archive']['sha256'] = hashlib.sha256(archive.read_bytes()).hexdigest()
r.write(state/'bundle/mx-cpamp-images.manifest.json', manifest)
assert shell('load_bundle').returncode == 0
manifest['archive']['sha256'] = '0'*64
r.write(state/'bundle/mx-cpamp-images.manifest.json', manifest)
assert shell('load_bundle').returncode != 0
print('real_gzip_docker_load_and_consumed_digest_rejection=passed')

name = os.environ['FIXTURE_PREFIX']+'-manager'
cpa_name = name+'-cpa'
try:
    (root/'secrets/probe-cpa-management').write_text('FAKE_CPA_MANAGEMENT_ONLY')
    subprocess.run(['docker', 'run', '-d', '--name', name, '--network', 'bridge', '--mount', 'type=volume,source='+volume+',target=/run/secrets,readonly', '-e', 'CPA_MANAGER_ADMIN_KEY_FILE=/run/secrets/cpamp-admin-key', '-e', 'CPA_MANAGER_CONFIG=/data/config.json', '-e', 'CPA_UPSTREAM_URL=http://127.0.0.1:8317', '-e', 'CPA_MANAGEMENT_KEY_FILE=/run/secrets/probe-cpa-management', 'mx-cpamp:20261110-rc1'], check=True, capture_output=True)
    for _ in range(30):
        probe = subprocess.run(['docker', 'exec', name, 'sh', '-c', 'key=$(cat /run/secrets/cpamp-admin-key); wget -S -O /dev/null --header "Authorization: Bearer $key" http://127.0.0.1:18317/api/oauth-login/tasks/00000000-0000-4000-8000-000000000001'], capture_output=True, text=True)
        if '404' in probe.stderr: break
        time.sleep(.2)
    assert '404' in probe.stderr
    unauthorized = subprocess.run(['docker', 'exec', name, 'wget', '-S', '-O', '/dev/null', 'http://127.0.0.1:18317/api/oauth-login/tasks/00000000-0000-4000-8000-000000000001'], capture_output=True, text=True)
    assert '401' in unauthorized.stderr
    print('actual_manager_readonly_task_probe_authenticated404_unauthorized401=passed')
    probe_cfg = copy.deepcopy(cfg); probe_cfg['server']['kv-store']['disabled'] = True; probe_cfg['oauth']['auth-dir'] = '/tmp/probe-auth'
    (root/'secrets/probe-cpa.json').write_text(json.dumps(probe_cfg))
    subprocess.run(['docker', 'run', '-d', '--name', cpa_name, '--network', 'bridge', '--mount', 'type=volume,source='+volume+',target=/state,readonly', '--entrypoint', '/CLIProxyAPI/CLIProxyAPI', 'mx-cpa:20261110-rc1', '--config', '/state/probe-cpa.json', '--local-model'], check=True, capture_output=True)
    current = json.loads(actual_command(['docker', 'inspect', name, cpa_name]))
    probe_plan = copy.deepcopy(plan)
    probe_plan['container_ids']['cpa-manager-plus'] = current[0]['Id']; probe_plan['container_ids']['cli-proxy-api'] = current[1]['Id']
    r.write(state/'plan.json', probe_plan); r.write(state/'prepared-live.json', current)
    for attempt in range(30):
        try: r.baseline_health(state); break
        except RuntimeError:
            if attempt == 29: raise
            time.sleep(.2)
    admin_file = root/'secrets/cpamp-admin-key'; admin_original = admin_file.read_text(); admin_file.write_text('FAKE_STALE_BOOTSTRAP_KEY')
    rejected(lambda: r.baseline_health(state), 'baseline_auth_failed_manager')
    admin_file.write_text(admin_original)
    print('actual_prepare_baseline_probes_and_stale_admin_key_rejection=passed')
finally:
    subprocess.run(['docker', 'rm', '-f', name], check=True, capture_output=True)
    subprocess.run(['docker', 'rm', '-f', cpa_name], capture_output=True)
print('release_boundaries_exit=0')
