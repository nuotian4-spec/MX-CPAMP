#!/usr/bin/env python3
"""安装本站更新桥；一次显式维护确认，保留配置、完整应用链与恢复材料。"""

import argparse
import copy
import datetime
import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import time


HERE = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('mx_update_bootstrap_host', HERE / 'mx-update-host.py')
host_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host_module)
updater = host_module.updater
MATERIALS = ('mx-update.py', 'mx-update-host.py', 'mx-update-host-install.py')
UNITS = ('mx-update-host.service', 'mx-update-operation@.service')


def command(arguments):
    return subprocess.run(arguments, check=True, text=True, capture_output=True).stdout.strip()


def compose(site, chain, arguments):
    result = ['docker', 'compose', '--project-directory', site['root'], '-p', site['project']]
    for path in chain:
        result += ['-f', path]
    return command(result + arguments)


def overlay(site):
    root = pathlib.Path(site['root'])
    return {'services': {'cli-proxy-api': {
        'environment': {'MX_UPDATE_LOCK_PATH': '/mx-update.lock', 'MX_UPDATE_STATE_DIR': '/mx-update-state',
                        'MX_UPDATE_SOCKET': '/run/mx-update/' + site['project'] + '.sock', 'MX_UPDATE_SITE_ID': site['project']},
        'volumes': [
            {'type': 'bind', 'source': str(root / '.mx-update.lock'), 'target': '/mx-update.lock'},
            {'type': 'bind', 'source': str(root / '.mx-update-state'), 'target': '/mx-update-state'},
            {'type': 'bind', 'source': '/run/mx-update', 'target': '/run/mx-update', 'read_only': True},
        ],
    }}}


def install(site_path, confirmed, *, system_root=pathlib.Path('/')):
    updater.require(os.name == 'posix' and os.geteuid() == 0, '首次接入需要 Linux root')
    updater.require(confirmed, '需要 --confirm-maintenance 显式授权首次完整应用维护')
    os.umask(0o077)
    site_path = pathlib.Path(site_path).resolve()
    site = updater.read(site_path)
    updater.validate_site(site, site_path)
    root = pathlib.Path(site['root'])
    registry = system_root / 'etc/mx-update/site.json'
    if registry.exists():
        registered = updater.read(registry)
        updater.require(registered['root'] == site['root'] and registered['project'] == site['project'],
                        '宿主已经登记另一站点；拒绝覆盖或跨站更新')
    updater.require(all((HERE / name).is_file() for name in (*MATERIALS, *UNITS)), '安装材料不完整')
    command(['systemctl', '--version'])
    runtime = host_module.inspect_runtime(site)
    chains = [runtime['services'][name]['compose_files'] for name in updater.APPS]
    updater.require(all(chain == chains[0] for chain in chains), '首次接入要求三应用完整 Compose 链一致')
    original_chain = chains[0]
    base = site['base_compose_files']
    updater.require(original_chain[:len(base)] == base, '运行链未保留站点基础 Compose 链')
    cpa = host_module.docker_json(['inspect', runtime['services']['cli-proxy-api']['id']])[0]
    updater.require(cpa['Config'].get('User', '') in ('', '0', 'root', '0:0', 'root:root'), 'CPA 必须以 root 访问本站 0600 锁与宿主 socket')
    target = root / 'compose.mx-update-host.json'
    updater.require(str(target) not in original_chain and not target.exists(), '站点桥接已准备；请按其原备份与回执收口，勿重复接入')
    next_site = copy.deepcopy(site)
    next_site['update_state_dir'] = str(root / '.mx-update-state')
    next_site['base_compose_files'] = base + [str(target)]
    next_chain = next_site['base_compose_files'] + original_chain[len(base):]
    run_id = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    backup = root / '.mx-update-bootstrap' / run_id
    backup.mkdir(parents=True, mode=0o700)
    operation_id = 'bootstrap-' + run_id
    with updater.site_lock(site_path, site):
        updater.require(host_module.inspect_runtime(site) == runtime, '确认前现场发生变化')
        for name, source in [('site.json', site_path), ('config.yaml', pathlib.Path(site['cpa_config']))]:
            shutil.copy2(source, backup / name)
        pointer_path = pathlib.Path(site['active_release_pointer'])
        if pointer_path.exists():
            shutil.copy2(pointer_path, backup / 'active-pointer.json')
        updater.write(backup / 'before.json', {'site_path': str(site_path), 'runtime': runtime,
                      'original_chain': original_chain, 'new_chain': next_chain, 'database_restore': False, 'operation_id': operation_id})
        (root / '.mx-update-state').mkdir(mode=0o700, exist_ok=True)
        (root / '.mx-update-state' / 'operations').mkdir(mode=0o700, exist_ok=True)
        runtime_directory = system_root / 'run/mx-update'
        runtime_directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        for folder in (root / '.mx-update-state', runtime_directory):
            folder.chmod(0o700)
        destination = system_root / 'usr/local/lib/mx-update'
        destination.mkdir(mode=0o700, parents=True, exist_ok=True)
        for name in MATERIALS:
            output = destination / name
            if output.exists():
                shutil.copy2(output, backup / ('previous-' + name))
            shutil.copy2(HERE / name, output)
            output.chmod(0o700)
        for name in UNITS:
            output = system_root / 'etc/systemd/system' / name
            output.parent.mkdir(parents=True, exist_ok=True)
            if output.exists():
                shutil.copy2(output, backup / ('previous-' + name))
            shutil.copy2(HERE / name, output)
            output.chmod(0o644)
        updater.write(target, overlay(next_site))
        compose(site, next_chain, ['config', '--quiet'])
        stopped = False
        operation = {'id': operation_id, 'type': 'integration', 'status': 'installing', 'stage': 'bootstrap',
                     'started_at': host_module.now(), 'message': '首次宿主桥接维护中'}
        operation_path = root / '.mx-update-state' / 'operations' / (operation_id + '.json')
        updater.write(operation_path, operation)
        updater.write(root / '.mx-update-state' / 'active.json', {'id': operation_id})
        try:
            stopped = True
            command(['docker', 'stop', runtime['services']['caddy']['id'],
                     *[runtime['services'][name]['id'] for name in updater.APPS]])
            updater.write(backup / 'maintenance.json', {'stopped': True, 'database_restore': False})
            updater.write(site_path, next_site)
            registry.parent.mkdir(mode=0o700, exist_ok=True)
            if registry != site_path:
                if registry.exists() or registry.is_symlink():
                    registry.unlink()
                registry.symlink_to(site_path)
            if runtime['pointer']:
                pointer = copy.deepcopy(runtime['pointer'])
                pointer['compose_files'] = next_chain
                updater.write(pointer_path, pointer)
            command(['systemctl', 'daemon-reload'])
            command(['systemctl', 'enable', '--now', 'mx-update-host.service'])
            compose(next_site, next_chain, ['up', '-d', '--no-build', '--pull', 'never', '--no-deps', '--force-recreate', *updater.APPS])
            command(['docker', 'start', runtime['services']['caddy']['id']])
            actual = host_module.inspect_runtime(next_site)
            updater.require(all(actual['services'][name]['image'] == runtime['services'][name]['image'] and
                                actual['services'][name]['compose_files'] == next_chain for name in updater.APPS),
                            '首次接入后的应用镜像或完整 Compose 链不符')
            updater.require(actual['services']['caddy']['id'] == runtime['services']['caddy']['id'], '原 Caddy 身份漂移')
            cpa = host_module.docker_json(['inspect', actual['services']['cli-proxy-api']['id']])[0]
            mounts = {item['Destination']: item['Source'] for item in cpa['Mounts']}
            updater.require(mounts.get('/mx-update.lock') == str(root / '.mx-update.lock') and
                            mounts.get('/mx-update-state') == str(root / '.mx-update-state') and
                            mounts.get('/run/mx-update') == '/run/mx-update', 'CPA 桥接挂载不匹配')
            health = ''
            for attempt in range(15):
                try:
                    health = command(['curl', '--fail', '--silent', '--show-error', '--max-time', '10',
                                      '--output', '/dev/null', '--write-out', '%{http_code}', site['ingress']['health_url']])
                    if health == str(site['ingress']['expected_status']):
                        break
                except subprocess.CalledProcessError:
                    pass
                time.sleep(2)
            updater.require(health == str(site['ingress']['expected_status']), '首次接入入口健康未通过')
            updater.write(backup / 'completed.json', {'status': 'registered', 'site_id': site['project'],
                          'runtime': actual, 'health': 'passed', 'database_restore': False})
            operation.update(status='completed', stage='completed', finished_at=host_module.now())
            updater.write(operation_path, operation)
            (root / '.mx-update-state' / 'active.json').unlink()
            print('本站宿主桥已登记；完整应用链与入口健康通过。备份：' + str(backup))
        except BaseException:
            if stopped:
                try:
                    host_module.halt_registered_runtime(next_site)
                finally:
                    updater.write(backup / 'recovery-required.json', {'status': 'recovery_required', 'database_restore': False})
                    operation.update(status='recovery_required', stage='recovery_required')
                    updater.write(operation_path, operation)
            raise


def recover(site_path, backup):
    updater.require(os.name == 'posix' and os.geteuid() == 0, '恢复确认需要 Linux root')
    site_path, backup = pathlib.Path(site_path).resolve(), pathlib.Path(backup).resolve()
    site = updater.read(site_path)
    updater.validate_site(site, site_path)
    updater.require(backup.is_relative_to(pathlib.Path(site['root']) / '.mx-update-bootstrap'), '恢复材料目录越界')
    before = updater.read(backup / 'before.json')
    updater.require(before['site_path'] == str(site_path), '恢复站点与安装登记不符')
    operation_id = before['operation_id']
    with updater.site_lock(site_path, site, operation_id=operation_id):
        state = pathlib.Path(site['root']) / '.mx-update-state'
        updater.require(updater.read(state / 'active.json')['id'] == operation_id, '恢复所有者不符')
        current = host_module.inspect_runtime(site)
        original = before['runtime']
        updater.require(current['pointer'] == original['pointer'] and all(
            current['services'][name]['image'] == original['services'][name]['image'] and
            current['services'][name]['compose_files'] == original['services'][name]['compose_files']
            for name in (*updater.APPS, 'caddy')), '原配置、完整 Compose 链和镜像尚未恢复')
        status = command(['curl', '--fail', '--silent', '--show-error', '--max-time', '15', '--output', '/dev/null',
                          '--write-out', '%{http_code}', site['ingress']['health_url']])
        updater.require(status == str(site['ingress']['expected_status']), '恢复入口健康不符')
        operation = updater.read(state / 'operations' / (operation_id + '.json'))
        operation.update(status='failed', stage='recovered', finished_at=host_module.now(), message='原运行链恢复已确认；未回灌数据库')
        updater.write(state / 'operations' / (operation_id + '.json'), operation)
        (state / 'active.json').unlink()
        print('原运行链与入口健康已恢复，维护所有者已收口。')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site', required=True, type=pathlib.Path)
    parser.add_argument('--confirm-maintenance', action='store_true')
    parser.add_argument('--recover-bootstrap', type=pathlib.Path)
    args = parser.parse_args()
    if args.recover_bootstrap:
        recover(args.site, args.recover_bootstrap)
    else:
        install(args.site, args.confirm_maintenance)


if __name__ == '__main__':
    main()
