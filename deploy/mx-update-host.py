#!/usr/bin/env python3
"""本站网页更新桥：固定发行、共享站点锁、持久任务和独立宿主执行。"""

import argparse
import datetime
import http.server
import importlib.util
import json
import os
import pathlib
import re
import socketserver
import subprocess
import sys
import urllib.parse
import urllib.request
import uuid


HERE = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('mx_update_host_runner', HERE / 'mx-update.py')
updater = importlib.util.module_from_spec(spec)
spec.loader.exec_module(updater)
TERMINAL = {'completed', 'completed_disabled', 'failed', 'license_unverified', 'license_error'}


class RequestError(Exception):
    def __init__(self, code, status=409, operation_id=''):
        self.code, self.status, self.operation_id = code, status, operation_id
        super().__init__(code)


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00', 'Z')


def fetch_json(url):
    request = urllib.request.Request(url, headers={'Accept': 'application/json', 'User-Agent': 'MX-CPAMP-Update-Center'})
    with urllib.request.urlopen(request, timeout=20) as response:
        data = response.read((1 << 20) + 1)
    if len(data) > 1 << 20:
        raise RequestError('release_metadata_too_large', 502)
    return json.loads(data)


def docker_json(arguments):
    result = subprocess.run(['docker', *arguments], check=True, text=True, capture_output=True, timeout=30)
    return json.loads(result.stdout)


def inspect_runtime(site):
    result = subprocess.run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + site['project']],
                            check=True, text=True, capture_output=True, timeout=15)
    ids = result.stdout.split()
    if not ids:
        raise RequestError('registered_runtime_not_found')
    rows = docker_json(['inspect', *ids])
    services = {}
    for row in rows:
        labels = row.get('Config', {}).get('Labels', {})
        name = labels.get('com.docker.compose.service')
        if name not in (*updater.APPS, 'caddy'):
            continue
        if name in services or labels.get('com.docker.compose.project.working_dir') != site['root']:
            raise RequestError('registered_runtime_mismatch')
        files = labels.get('com.docker.compose.project.config_files', '').split(',')
        services[name] = {'id': row['Id'], 'image': row['Image'], 'running': row['State']['Running'], 'compose_files': files}
    if set(services) != {*updater.APPS, 'caddy'} or not all(row['running'] for row in services.values()):
        raise RequestError('registered_runtime_not_healthy')
    pointer_path = pathlib.Path(site['active_release_pointer'])
    pointer = updater.read(pointer_path) if pointer_path.exists() else None
    if pointer:
        if pointer.get('schema') != 1 or any(services[name]['compose_files'] != pointer.get('compose_files') for name in updater.APPS):
            raise RequestError('active_pointer_runtime_mismatch')
    return {'version': pointer.get('version', '') if pointer else '', 'pointer': pointer, 'services': services}


def candidate_runtime_images_match(state, plan, services):
    if all(services[name]['image'] == plan['images'][name] for name in updater.APPS):
        return True
    folder = pathlib.Path(state) / 'materials'
    selected = importlib.util.spec_from_file_location('mx_host_image_identity', folder / 'capability_probe.py')
    identity = importlib.util.module_from_spec(selected)
    selected.loader.exec_module(identity)
    candidates = updater.read(folder / 'release.json')['candidates']
    return all(identity.same_image_identity(plan['images'][name], services[name]['image'], candidates[name], folder)
               for name in updater.APPS)


def halt_registered_runtime(site):
    result = subprocess.run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + site['project']],
                            check=True, text=True, capture_output=True, timeout=15)
    ids = result.stdout.split()
    if not ids:
        raise RequestError('registered_runtime_not_found')
    rows = docker_json(['inspect', *ids])
    selected = {}
    for row in rows:
        labels = row.get('Config', {}).get('Labels', {})
        name = labels.get('com.docker.compose.service')
        if name not in (*updater.APPS, 'caddy'):
            continue
        if (name in selected or labels.get('com.docker.compose.project') != site['project'] or
                labels.get('com.docker.compose.project.working_dir') != site['root']):
            raise RequestError('registered_runtime_mismatch')
        selected[name] = row['Id']
    if set(selected) != {*updater.APPS, 'caddy'}:
        raise RequestError('registered_runtime_incomplete')
    subprocess.run(['docker', 'stop', *[selected[name] for name in ('caddy', *updater.APPS)]],
                   check=True, text=True, capture_output=True, timeout=90)
    stopped = docker_json(['inspect', *selected.values()])
    if any(row['State']['Running'] for row in stopped):
        raise RequestError('interrupted_runtime_not_stopped')


class UpdateHost:
    def __init__(self, site_path, *, fetcher=fetch_json, inspector=inspect_runtime, launcher=None, runner=None):
        self.site_path = pathlib.Path(site_path).resolve()
        self.site = updater.read(self.site_path)
        updater.validate_site(self.site, self.site_path)
        self.site_id = self.site['project']
        if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,63}', self.site_id):
            raise RequestError('invalid_site_id')
        self.state = pathlib.Path(self.site.get('update_state_dir', str(pathlib.Path(self.site['root']) / '.mx-update-state')))
        if not self.state.is_absolute() or not self.state.is_relative_to(pathlib.Path(self.site['root'])):
            raise RequestError('invalid_update_state_directory')
        self.site['update_state_dir'] = str(self.state)
        for name in ('operations', 'core-previews'):
            (self.state / name).mkdir(mode=0o700, parents=True, exist_ok=True)
        self.fetcher, self.inspector = fetcher, inspector
        self.launcher = launcher or self.launch
        self.runner = runner or updater.run_cycle

    def operation(self, operation_id):
        if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,95}', operation_id):
            raise RequestError('invalid_operation_id', 400)
        return updater.read(self.state / 'operations' / (operation_id + '.json'))

    def save_operation(self, operation):
        updater.write(self.state / 'operations' / (operation['id'] + '.json'), operation)

    def active(self):
        path = self.state / 'active.json'
        if not path.exists():
            return None
        return self.operation(updater.read(path)['id'])

    def current(self):
        return self.inspector(self.site)

    def discover(self):
        output = {'site_id': self.site_id, 'registered': True, 'current_version': '', 'official_version': '',
                  'site_version': '', 'releases': [], 'blockers': []}
        try:
            output['current_version'] = self.current()['version']
        except Exception:
            output['blockers'].append('registered_runtime_not_verified')
        active = self.active()
        if active:
            output['active_operation'] = self.public_operation(active)
        else:
            records = [updater.read(path) for path in (self.state / 'operations').glob('core-*.json')]
            if records:
                output['active_operation'] = self.public_operation(max(records, key=lambda row: row.get('started_at', '')))
        source = urllib.parse.urlparse(self.site['release_source'])
        parts = source.path.strip('/').split('/')
        if source.hostname == 'github.com' and len(parts) == 4 and parts[-2:] == ['releases', 'download']:
            try:
                releases = self.fetcher('https://api.github.com/repos/' + '/'.join(parts[:2]) + '/releases?per_page=20')
                for release in releases:
                    tag = release.get('tag_name', '')
                    if release.get('draft') or release.get('prerelease'):
                        continue
                    try:
                        updater.validate_version(tag)
                    except Exception:
                        continue
                    output['releases'].append({'id': tag, 'version': tag, 'notes': str(release.get('body', ''))[:6000]})
                if output['releases']:
                    output['site_version'] = output['releases'][0]['version']
            except Exception:
                output['blockers'].append('site_release_discovery_unavailable')
        else:
            output['blockers'].append('site_release_discovery_unavailable')
        try:
            official = self.fetcher('https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest')
            output['official_version'] = str(official.get('tag_name', ''))
        except Exception:
            pass
        return output

    def preview(self, release_id):
        updater.validate_version(release_id)
        active = self.active()
        if active and active['status'] not in TERMINAL:
            raise RequestError('update_busy', operation_id=active['id'])
        baseline = self.current()
        release = self.fetcher(self.site['release_source'].rstrip('/') + '/' + release_id + '/release.json')
        if release.get('version') != release_id or release.get('schema') != 1 or release.get('platform') != 'linux/amd64' or set(release.get('assets', {})) != updater.ASSETS:
            raise RequestError('invalid_fixed_site_release', 422)
        candidate_id = str(uuid.uuid4())
        public = {'id': candidate_id, 'site_id': self.site_id, 'version': release_id, 'current_version': baseline['version'],
                  'maintenance': ['停止本站 CPA、Manager 与登录服务', '完整停态备份后切换固定镜像', '健康通过后恢复原 Caddy；失败不自动回灌数据库'],
                  'notes': str(release.get('notes', '')), 'installable': True, 'blockers': []}
        updater.write(self.state / 'core-previews' / (candidate_id + '.json'),
                      {'preview': public, 'baseline': baseline, 'release': release, 'site_id': self.site_id})
        return public

    def candidate(self, candidate_id):
        if str(uuid.UUID(candidate_id)) != candidate_id:
            raise RequestError('invalid_candidate_id', 400)
        value = updater.read(self.state / 'core-previews' / (candidate_id + '.json'))
        if value.get('site_id') != self.site_id:
            raise RequestError('candidate_site_mismatch')
        return value

    def install(self, candidate_id):
        candidate = self.candidate(candidate_id)
        operation_id = 'core-' + candidate_id
        previous = self.state / 'operations' / (operation_id + '.json')
        if previous.exists():
            return self.public_operation(self.operation(operation_id))
        try:
            with updater.site_lock(self.site_path, self.site):
                if previous.exists():
                    return self.public_operation(self.operation(operation_id))
                if self.current() != candidate['baseline']:
                    raise RequestError('runtime_baseline_changed')
                operation = {'id': operation_id, 'type': 'core', 'target_version': candidate['preview']['version'],
                             'status': 'queued', 'stage': 'queued', 'active_version': candidate['baseline']['version'],
                             'registered': False, 'license_status': 'not_applicable', 'message': '本站维护任务已受理，等待宿主执行',
                             'started_at': now(), 'candidate_id': candidate_id, 'baseline': candidate['baseline']}
                self.save_operation(operation)
                updater.write(self.state / 'active.json', {'id': operation_id})
        except (BlockingIOError, updater.Failure):
            active = self.active()
            raise RequestError('update_busy', operation_id=active['id'] if active else '')
        try:
            self.launcher(operation_id)
        except Exception:
            self.interrupted(operation_id)
        return self.public_operation(self.operation(operation_id))

    def launch(self, operation_id):
        subprocess.run(['systemctl', 'start', '--no-block', 'mx-update-operation@' + operation_id + '.service'],
                       check=True, capture_output=True, timeout=15)

    def interrupted(self, operation_id):
        try:
            with updater.site_lock(self.site_path, self.site, operation_id=operation_id):
                operation = self.operation(operation_id)
                if operation['status'] in TERMINAL:
                    return
                active = self.active()
                if not active or active['id'] != operation_id or operation['type'] != 'core':
                    raise RequestError('operation_owner_mismatch')
                operation.update(status='recovery_required', stage='recovery_required',
                                 message='宿主任务已中断；保留维护所有者与备份，核实现场后执行恢复确认')
                state = pathlib.Path(self.site['root']) / '.mx-updates' / (operation['target_version'] + '-' + operation_id)
                if (state / 'maintenance').exists():
                    try:
                        halt_registered_runtime(self.site)
                        operation['halt_confirmed'] = True
                    except Exception as error:
                        operation['halt_confirmed'] = False
                        operation['halt_error_type'] = type(error).__name__
                        operation['message'] = '宿主维护中断且停态尚未确认；保留所有者，请立即核查四服务现场'
                self.save_operation(operation)
        except BlockingIOError:
            # A running worker owns the file lock and publishes its own outcome.
            return

    def reconcile_active(self):
        active = self.active()
        if not active or active['type'] != 'core' or active['status'] in TERMINAL:
            return
        unit = 'mx-update-operation@' + active['id'] + '.service'
        result = subprocess.run(['systemctl', 'show', '--property=ActiveState', '--value', unit],
                                check=True, text=True, capture_output=True, timeout=15)
        if result.stdout.strip() not in ('active', 'activating', 'deactivating'):
            self.interrupted(active['id'])

    @staticmethod
    def public_operation(operation):
        fields = ('id', 'type', 'target_version', 'status', 'stage', 'active_version', 'registered', 'license_status', 'message', 'started_at', 'finished_at')
        return {name: operation[name] for name in fields if name in operation}

    def run(self, operation_id):
        operation = self.operation(operation_id)
        if operation['type'] != 'core' or operation['status'] in TERMINAL:
            raise RequestError('operation_not_runnable')
        candidate = self.candidate(operation['candidate_id'])
        state = pathlib.Path(self.site['root']) / '.mx-updates' / (operation['target_version'] + '-' + operation_id)
        with updater.site_lock(self.site_path, self.site, operation_id=operation_id):
            active = self.active()
            if not active or active['id'] != operation_id:
                raise RequestError('operation_owner_mismatch')
            try:
                if state.exists() or self.current() != candidate['baseline']:
                    raise RequestError('runtime_baseline_changed_or_interrupted')
                state.mkdir(mode=0o700, parents=True)
                def fetch_fixed(site, version, destination):
                    folder, release = updater.fetch_release(site, version, destination)
                    if release != candidate['release']:
                        raise RequestError('confirmed_release_changed')
                    return folder, release
                def confirm_fixed(summary):
                    expected = {name: row['id'] for name, row in candidate['baseline']['services'].items()}
                    if summary['version'] != operation['target_version'] or summary['current_container_ids'] != expected:
                        raise RequestError('confirmed_runtime_changed')
                def progress(stage):
                    operation['status'] = 'reloading' if stage in ('starting', 'verifying') else 'installing'
                    operation['stage'] = stage
                    operation['message'] = '本站固定发行维护进行中'
                    self.save_operation(operation)
                pointer = self.runner(self.site_path, self.site, operation['target_version'], state,
                                      confirm=confirm_fixed, fetcher=fetch_fixed, progress=progress)
                actual = self.current()
                plan = updater.read(state / 'plan.json')
                if (actual['version'] != pointer['version'] or actual['pointer'] != pointer or
                        not candidate_runtime_images_match(state, plan, actual['services']) or
                        actual['services']['caddy']['id'] != candidate['baseline']['services']['caddy']['id']):
                    raise RequestError('updated_runtime_not_verified')
                operation.update(status='completed', stage='completed', active_version=pointer['version'], registered=True,
                                 message='固定版本已生效，入口健康通过；未执行真实模型请求', finished_at=now())
                self.save_operation(operation)
                (self.state / 'active.json').unlink()
                return operation
            except BaseException:
                maintenance = (state / 'maintenance').exists()
                operation.update(status='recovery_required' if maintenance else 'failed', stage='recovery_required' if maintenance else 'failed',
                                 message='维护中断，保留备份及所有者，按恢复手册核查' if maintenance else '维护前检查失败，运行状态未切换')
                if not maintenance:
                    operation['finished_at'] = now()
                self.save_operation(operation)
                if not maintenance:
                    (self.state / 'active.json').unlink(missing_ok=True)
                raise

    def recover(self, operation_id):
        operation = self.operation(operation_id)
        candidate = self.candidate(operation['candidate_id'])
        with updater.site_lock(self.site_path, self.site, operation_id=operation_id):
            if not self.active() or self.active()['id'] != operation_id:
                raise RequestError('operation_owner_mismatch')
            current = self.current()
            same_images = all(current['services'][name]['image'] == candidate['baseline']['services'][name]['image'] and
                              current['services'][name]['compose_files'] == candidate['baseline']['services'][name]['compose_files'] for name in (*updater.APPS, 'caddy'))
            old_restored = current['pointer'] == candidate['baseline']['pointer'] and same_images
            state = pathlib.Path(self.site['root']) / '.mx-updates' / (operation['target_version'] + '-' + operation_id)
            completed = updater.read(state / 'completed.json') if (state / 'completed.json').exists() else None
            plan = updater.read(state / 'plan.json') if (state / 'plan.json').exists() else None
            new_completed = bool(completed and plan and current['version'] == operation['target_version'] and current['pointer'] and
                                 current['pointer']['state_dir'] == str(state) and
                                 all(completed.get(key) == current['pointer'].get(key) for key in ('schema', 'version', 'commit', 'state_dir', 'compose_files', 'updater')) and
                                 completed.get('commit') == candidate['release']['commit'] and
                                 completed.get('ingress_restored') is True and completed.get('ingress_health') == 'passed' and
                                 current['services']['caddy']['id'] == completed.get('original_caddy_id') == candidate['baseline']['services']['caddy']['id'] and
                                  candidate_runtime_images_match(state, plan, current['services']))
            if not (old_restored or new_completed):
                raise RequestError('runtime_recovery_not_verified')
            result = subprocess.run(['curl','--fail','--silent','--show-error','--max-time','15','--output','/dev/null','--write-out','%{http_code}',self.site['ingress']['health_url']],
                                    check=True,text=True,capture_output=True,timeout=20)
            if result.stdout != str(self.site['ingress']['expected_status']):
                raise RequestError('recovery_health_not_verified')
            operation.update(status='completed' if new_completed else 'failed', stage='recovered', registered=new_completed,
                             active_version=current['version'], finished_at=now(), message='人工恢复后的容器、活动指针与入口健康已核实；未回灌数据库')
            self.save_operation(operation)
            (self.state / 'active.json').unlink()
            return operation


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        host = self.server.update_host
        status = 200
        try:
            if self.headers.get('X-MX-Site-ID') != host.site_id:
                raise RequestError('registered_site_mismatch', 403)
            length = int(self.headers.get('Content-Length', '0'))
            if length < 0 or length > 4096:
                raise RequestError('invalid_request_size', 413)
            body = json.loads(self.rfile.read(length) or b'{}')
            if not isinstance(body, dict):
                raise RequestError('invalid_request',400)
            if self.path == '/v1/discover' and not body:
                output = host.discover()
            elif self.path == '/v1/preview' and set(body) == {'release_id'}:
                output = host.preview(body['release_id'])
            elif self.path == '/v1/install' and set(body) == {'candidate_id'}:
                output = host.install(body['candidate_id'])
                status = 202
            else:
                raise RequestError('unsupported_update_action',400)
        except RequestError as error:
            status = error.status
            output = {'error':error.code, 'operation_id':error.operation_id}
        except Exception:
            status, output = 503, {'error':'registered_update_host_error'}
        data = json.dumps(output,ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header('Content-Type','application/json')
        self.send_header('Content-Length',str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site',required=True,type=pathlib.Path)
    group=parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--serve',action='store_true')
    group.add_argument('--run')
    group.add_argument('--recover')
    group.add_argument('--interrupted')
    args=parser.parse_args()
    updater.require(os.name=='posix' and os.geteuid()==0,'宿主桥需要 Linux root')
    os.umask(0o077)
    host=UpdateHost(args.site)
    if args.run:
        host.run(args.run)
    elif args.recover:
        host.recover(args.recover)
    elif args.interrupted:
        host.interrupted(args.interrupted)
    else:
        host.reconcile_active()
        directory=pathlib.Path('/run/mx-update')
        directory.mkdir(mode=0o700,exist_ok=True)
        socket=directory/(host.site_id+'.sock')
        socket.unlink(missing_ok=True)
        with socketserver.UnixStreamServer(str(socket),Handler) as server:
            socket.chmod(0o600)
            server.update_host=host
            server.serve_forever()


if __name__=='__main__':
    main()
