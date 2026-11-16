#!/usr/bin/env python3
"""固定版本日常更新；站点首次登记，一次确认，失败保全。"""

import argparse
import contextlib
import datetime
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import time
import urllib.parse
import uuid


HERE = pathlib.Path(__file__).resolve().parent
APPS = ('cli-proxy-api', 'cpa-manager-plus', 'oauth-login')
ROLES = dict(zip(APPS, ('cpa', 'manager', 'worker')))
IMAGE_REPOSITORIES = dict(zip(APPS, ('mx-cpa', 'mx-cpamp', 'mx-oauth-login')))
ASSETS = {'mx-cpamp-images-linux-amd64.tar.gz', 'mx-cpamp-images.manifest.json',
          'kilo-release.py', 'kilo-release.sh', 'phase2-release.py',
          'mx-update.py', 'mx-update.sh', 'capability_probe.py', 'capability-matrix.json',
          'production-wiring.json', 'production-wiring.contract.json', 'production-wiring.evidence.json',
          'capability-image-evidence.json', 'mx-update.site.example.json', 'README.update.zh-CN.md'}


class Failure(Exception):
    pass


def require(value, message):
    if not value:
        raise Failure(message)


def read(path):
    return json.loads(pathlib.Path(path).read_text(encoding='utf-8'))


def write(path, value):
    path = pathlib.Path(path)
    temporary = path.with_name(path.name + '.new')
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    temporary.chmod(0o600)
    os.replace(temporary, path)


def digest(path):
    result = hashlib.sha256()
    with pathlib.Path(path).open('rb') as stream:
        for data in iter(lambda: stream.read(1024 * 1024), b''):
            result.update(data)
    return result.hexdigest()


def load_module(name, path):
    specification = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def validate_site(site, path):
    require(site.get('schema') == 1 and site.get('project'), '站点 schema 或项目缺失')
    root = pathlib.Path(site['root'])
    require(root.is_absolute() and root.is_dir(), '站点 root 必须为存在的绝对目录')
    for field in ('cpa_config', 'active_release_pointer'):
        selected = pathlib.Path(site[field])
        require(selected.is_absolute() and selected.is_relative_to(root), '站点路径必须位于 root：' + field)
    require(pathlib.Path(site['cpa_config']).is_file(), '站点 CPA 配置缺失')
    base = site.get('base_compose_files')
    require(isinstance(base, list) and base and len(base) == len(set(base)), '站点基础 Compose 链缺失或重复')
    require(all(pathlib.Path(name).is_absolute() and pathlib.Path(name).is_file() for name in base), '站点基础 Compose 文件缺失')
    require(site.get('lifecycle', {}).get('network'), '站点受管网络缺失')
    require(set(site.get('kilo_state', {})) == {'container', 'host'}, '站点 Kilo 状态位置缺失')
    ingress = site.get('ingress', {})
    require(ingress.get('service') == 'caddy' and ingress.get('restore_ingress') is True,
            '站点须明确授权健康后恢复原 Caddy')
    health = urllib.parse.urlparse(ingress.get('health_url', ''))
    require(health.scheme in ('https', 'http') and health.hostname and not health.username,
            '站点入口健康地址无效')
    require(isinstance(ingress.get('expected_status'), int), '站点入口预期状态缺失')
    source = urllib.parse.urlparse(site.get('release_source', ''))
    require(source.scheme == 'https' and source.hostname and not source.username and
            not source.query and not source.fragment and source.path.rstrip('/').endswith('/releases/download'),
            '发行来源必须是固定 HTTPS Release 下载前缀')
    require(path.is_absolute() and path.is_file(), '站点登记文件缺失')


@contextlib.contextmanager
def site_lock(site_path, site, operation_id=None):
    require(os.name == 'posix' and os.geteuid() == 0, '真实更新入口需要 Linux root')
    os.umask(0o077)
    require(site_path.stat().st_uid == 0 and site_path.stat().st_mode & 0o777 == 0o600,
            '站点文件需要 root 所有及 0600 权限')
    import fcntl
    with (pathlib.Path(site['root']) / '.mx-update.lock').open('a') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        state = pathlib.Path(site.get('update_state_dir', str(pathlib.Path(site['root']) / '.mx-update-state')))
        active_path = state / 'active.json'
        if active_path.exists():
            owner = read(active_path).get('id')
            require(isinstance(owner, str) and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,95}', owner),
                    '维护所有者记录无效，需要人工核实运行状态')
            previous = read(state / 'operations' / (owner + '.json'))
            terminal = previous.get('status') in ('completed', 'completed_disabled', 'failed', 'license_unverified', 'license_error')
            require(terminal or operation_id == owner, '站点维护尚未收口：' + owner)
        yield


def validate_version(version):
    require(len(version) <= 128 and re.fullmatch(
        r'v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', version),
        '只接受固定版本，不接受 latest 或分支')


def fetch_release(site, version, state):
    validate_version(version)
    folder = state / 'materials'
    folder.mkdir(mode=0o700)
    base = site['release_source'].rstrip('/') + '/' + version
    def download(name):
        destination = folder / name
        subprocess.run(['curl', '--fail', '--silent', '--show-error', '--location',
                        '--proto', '=https', '--proto-redir', '=https',
                        '--output', str(destination), base + '/' + name], check=True)
        destination.chmod(0o600)
        return destination
    release = read(download('release.json'))
    require(release.get('schema') == 1 and release.get('version') == version and
            release.get('platform') == 'linux/amd64' and
            re.fullmatch('[0-9a-f]{40}', release.get('commit', '')), '发行身份不匹配')
    require(set(release.get('assets', {})) == ASSETS, '发行未包含完整的一键更新材料')
    for name in sorted(ASSETS):
        path = download(name)
        expected = release['assets'][name]
        require(path.stat().st_size == expected.get('size') and digest(path) == expected.get('sha256'),
                '发行资产校验失败：' + name)
    manifest = read(folder / 'mx-cpamp-images.manifest.json')
    require(manifest.get('schema') == 1 and set(manifest.get('images', {})) == set(ROLES.values()), '镜像清单角色不完整')
    require(manifest.get('archive', {}).get('file') == 'mx-cpamp-images-linux-amd64.tar.gz' and
            manifest['archive'].get('sha256') == release['assets']['mx-cpamp-images-linux-amd64.tar.gz']['sha256'],
            '镜像归档绑定不一致')
    candidates = {}
    for service, role in ROLES.items():
        image = manifest['images'][role]
        require(image.get('revision') == release['commit'] and image.get('os') == 'linux' and
                image.get('architecture') == 'amd64' and image.get('ref') == IMAGE_REPOSITORIES[service] + ':' + version,
                '候选镜像版本、平台或源码绑定不一致')
        ids = image.get('accepted_ids')
        require(isinstance(ids, list) and ids and all(re.fullmatch(r'sha256:[0-9a-f]{64}', item) for item in ids),
                '候选镜像 ID 集合无效')
        candidates[service] = {'role': role, 'ref': image['ref'], 'revision': image['revision'], 'accepted_ids': ids}
    require(release.get('candidates') == candidates, '发行候选映射与镜像清单不一致')
    return folder, release


def current_chain(site, facts):
    chains = [facts[service]['compose_files'] for service in APPS]
    require(all(isinstance(chain, list) and chain and all(chain) for chain in chains), '应用 Compose 链缺失')
    chain = max(chains, key=len)
    require(all(chain[:len(item)] == item for item in chains), '应用 Compose 链存在冲突')
    require(len(chain) == len(set(chain)) and all(pathlib.Path(name).is_absolute() and pathlib.Path(name).is_file() for name in chain),
            '当前 Compose 链包含重复或缺失文件')
    base = site['base_compose_files']
    require(chain[:len(base)] == base, '当前应用链未保留站点基础 Compose 链')
    require(all(facts[service]['working_dir'] == site['root'] for service in APPS), '当前 Compose 项目目录不匹配')
    pointer = pathlib.Path(site['active_release_pointer'])
    if pointer.exists():
        active = read(pointer)
        require(active.get('schema') == 1 and all(active.get('compose_files') == item for item in chains), '活动指针与现场应用链不一致')
    return chain


def preflight_space(plan, state):
    total = 0
    for raw in plan['sources']['paths']:
        source = pathlib.Path(raw)
        if not source.exists():
            require(plan['sources'].get('new_state') and raw == plan['sources']['state_host'], '备份来源缺失')
            continue
        if source.is_file():
            total += source.stat().st_size
        elif source.is_dir():
            for directory, _, files in os.walk(source):
                total += sum((pathlib.Path(directory) / name).stat().st_size for name in files)
    required = total + 16 * 1024 * 1024
    require(shutil.disk_usage(state).free >= required, '备份可用空间不足')
    check = state / '.backup-write-check'
    with check.open('xb') as stream:
        stream.write(b'write-check')
    check.unlink()
    return {'source_bytes_estimate': total, 'required_free_bytes': required, 'stopped_backup_complete': False}


def preflight_pointer(site):
    parent = pathlib.Path(site['active_release_pointer']).parent
    require(parent.is_dir(), '活动指针父目录缺失')
    source = parent / ('.mx-pointer-check-' + uuid.uuid4().hex)
    target = source.with_suffix('.renamed')
    try:
        with source.open('xb') as stream:
            stream.write(b'atomic-write-check')
        os.replace(source, target)
    finally:
        source.unlink(missing_ok=True)
        target.unlink(missing_ok=True)


def ingress_health(site, backend):
    ingress = site['ingress']
    for attempt in range(10):
        try:
            status = backend.command(['curl', '--silent', '--show-error', '--max-time', '10',
                '--output', '/dev/null', '--write-out', '%{http_code}', ingress['health_url']])
            if status == str(ingress['expected_status']):
                return
        except Exception:
            pass
        if attempt < 9:
            time.sleep(2)
    raise Failure('入口健康未达到站点登记判据')


def confirm_update(summary):
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    require(input('将停止应用、做停态备份、切换镜像并在健康后恢复原 Caddy。输入 UPDATE 确认：').strip() == 'UPDATE',
            '更新未确认')


def run_cycle(site_path, site, version, state, confirm=confirm_update, fetcher=fetch_release,
              loader=load_module, space_check=preflight_space, check_ingress=ingress_health, progress=None):
    release_runner = None
    phase = 'preflight'
    report = progress or (lambda stage: None)
    try:
        report('preflight')
        folder, release = fetcher(site, version, state)
        library = loader('mx_update_kilo', folder / 'kilo-release.py')
        capability = loader('mx_update_capability', folder / 'capability_probe.py')
        spec = {'schema': 1, 'input_id': version + ':' + release['commit'] + ':' + state.name,
                'root': site['root'], 'project': site['project'], 'state_dir': str(state),
                'cpa_config': site['cpa_config'], 'kilo_state': site['kilo_state'],
                'lifecycle': site['lifecycle'], 'candidates': release['candidates']}
        backend = library.backend(spec)
        live = backend.snapshot()
        facts = backend.facts(live)
        spec['compose_files'] = current_chain(site, facts)
        spec['baseline'] = facts
        protected = list(site.get('protected_files', [])) + [str(site_path)]
        protected += [str(path) for path in folder.iterdir() if path.is_file() and path.suffix in ('.py', '.sh', '.json')]
        if pathlib.Path(site['active_release_pointer']).exists():
            protected.append(site['active_release_pointer'])
        spec['protected_files'] = sorted(set(protected))
        write(state / 'spec.json', spec)
        release_runner = library.Release(spec, backend)
        release_runner.prepare(folder)
        plan = library.read(state / 'plan.json')
        release_runner.topology(plan, plan['base'], backend.snapshot(), ingress_stopped=False)
        release_runner.authenticated_health(live)
        space = space_check(plan, state)
        preflight_pointer(site)
        candidate = state / 'cpa-config.yaml'
        receipt = capability.probe_capability(plan['images'], candidate, state, folder / 'capability-matrix.json', backend.command)
        cfg = backend.yaml_document(candidate)
        release_runner.capability(receipt, candidate, cfg, plan['images'], library.scope_keys(cfg),
                                  'scope-enforced', library.digest(candidate))
        write(state / 'preflight.json', {'version': version, 'commit': release['commit'], 'images': plan['images'],
             'capability_receipt': str(receipt), 'space': space, 'status': 'ready'})
        confirm({'version': version, 'commit': release['commit'], 'images': plan['images'],
                 'current_container_ids': {name: fact['id'] for name, fact in facts.items()},
                 'backup': str(state / 'backup'), 'restore_original_caddy': facts['caddy']['id']})
        confirmed = True
        def already_confirmed():
            require(confirmed, '本轮尚未确认')
        phase = 'apply'
        report('backing_up')
        release_runner.apply(already_confirmed)
        phase = 'start'
        report('starting')
        release_runner.lifecycle('start', capability_path=receipt, confirm=already_confirmed)
        phase = 'complete'
        report('verifying')
        active = release_runner.complete_update(lambda: check_ingress(site, backend))
        pointer = {'schema': 1, 'version': version, 'commit': release['commit'], 'state_dir': str(state),
                   'compose_files': active['compose_files'], 'updater': str(folder / 'mx-update.py')}
        write(state / 'completed.json', dict(pointer, original_caddy_id=facts['caddy']['id'],
              ingress_restored=True, ingress_health='passed', inference_business_acceptance='not_run',
              publication='active_pointer_is_completion_authority'))
        release_runner.mark('completed')
        write(pathlib.Path(site['active_release_pointer']), pointer)
        return pointer
    except (Exception, KeyboardInterrupt) as error:
        maintenance = (state / 'maintenance').exists()
        halted = None
        halt_error = None
        if maintenance and release_runner is not None:
            try:
                if phase != 'start' or isinstance(error, KeyboardInterrupt):
                    release_runner.halt()
                halted = not any(item['State']['Running'] for item in release_runner.b.snapshot().values())
            except Exception as cleanup_error:
                halted = False
                halt_error = type(cleanup_error).__name__
            release_runner.mark('update_failed')
        write(state / 'update-failure.json', {'phase': phase, 'maintenance_started': maintenance,
              'halt_confirmed': halted, 'halt_error_type': halt_error, 'error_type': type(error).__name__,
              'automatic_rollback': False})
        raise


def managed_command(site, arguments):
    active = read(site['active_release_pointer'])
    files = active.get('compose_files')
    require(isinstance(files, list) and files and all(pathlib.Path(name).is_file() for name in files), '活动 Compose 链缺失')
    command = ['docker', 'compose', '--project-directory', site['root'], '-p', site['project']]
    for path in files:
        command += ['-f', path]
    return command + arguments


def active_updater(site):
    path = pathlib.Path(site['active_release_pointer'])
    if not path.exists():
        return None
    pointer = read(path)
    require(pointer.get('schema') == 1, '活动指针 schema 错误')
    state = pathlib.Path(pointer['state_dir'])
    require(state.is_absolute() and state.is_relative_to(pathlib.Path(site['root']) / '.mx-updates'), '活动轮次目录越界')
    updater = state / 'materials' / 'mx-update.py'
    require(str(updater) == pointer.get('updater'), '活动更新入口与轮次不一致')
    release = read(updater.parent / 'release.json')
    require(release.get('version') == pointer.get('version') and release.get('commit') == pointer.get('commit'), '活动发行身份漂移')
    expected = release.get('assets', {}).get('mx-update.py', {})
    require(updater.is_file() and updater.stat().st_size == expected.get('size') and digest(updater) == expected.get('sha256'), '活动更新入口材料漂移')
    return updater.resolve()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site', required=True, type=pathlib.Path)
    parser.add_argument('--version')
    parser.add_argument('--compose', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    state = None
    try:
        site_path = args.site.resolve()
        site = read(site_path)
        validate_site(site, site_path)
        selected = None if site.get('update_state_dir') else active_updater(site)
        if selected is not None and selected != pathlib.Path(__file__).resolve():
            os.execv(sys.executable, [sys.executable, str(selected), *sys.argv[1:]])
        with site_lock(site_path, site):
            if args.compose is not None:
                require(args.compose and args.version is None, '--compose 需要命令且不与 --version 混用')
                return subprocess.run(managed_command(site, args.compose), check=False).returncode
            require(args.version is not None, '更新需要 --version')
            validate_version(args.version)
            parent = pathlib.Path(site['root']) / '.mx-updates'
            parent.mkdir(mode=0o700, exist_ok=True)
            require(parent.stat().st_uid == 0 and parent.stat().st_mode & 0o777 == 0o700, '更新目录需要 root 所有及 0700 权限')
            run_id = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
            state = parent / (args.version + '-' + run_id)
            state.mkdir(mode=0o700)
            pointer = run_cycle(site_path, site, args.version, state)
            print('更新完成：' + pointer['version'] + '；原 Caddy 已恢复；本轮未执行真实模型推理验收。')
            return 0
    except (Exception, KeyboardInterrupt) as error:
        print('更新结束：' + type(error).__name__ + ('；私有材料：' + str(state) if state else ''), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
