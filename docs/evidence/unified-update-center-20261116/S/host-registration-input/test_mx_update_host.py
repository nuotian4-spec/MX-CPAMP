#!/usr/bin/env python3
"""离线宿主桥专项：合成外部边界，真实文件锁、持久记录和 Unix HTTP。"""

import contextlib
import copy
import http.client
import importlib.util
import io
import json
import os
import pathlib
import socket
import socketserver
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock


DEPLOY = pathlib.Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('mx_update_host_tested', DEPLOY / 'mx-update-host.py')
h = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(h)
u = h.updater
VERSION = 'v1.2.3'
COMMIT = 'b' * 40


class UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('localhost', timeout=3)
        self.path = str(path)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


@unittest.skipUnless(os.name == 'posix' and os.geteuid() == 0, '本站锁专项需要 Linux root')
class HostTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='mx-host-test-')
        self.addCleanup(temporary.cleanup)
        self.root = pathlib.Path(temporary.name)
        for name in ('config.yaml', 'compose.yaml'):
            (self.root / name).write_text('合成测试材料\n', encoding='utf-8')
        self.site = {
            'schema': 1, 'project': 'fixture-site', 'root': str(self.root),
            'cpa_config': str(self.root / 'config.yaml'),
            'base_compose_files': [str(self.root / 'compose.yaml')],
            'active_release_pointer': str(self.root / '.mx-active-release.json'),
            'update_state_dir': str(self.root / '.mx-update-state'),
            'kilo_state': {'container': '/root/kilo-state', 'host': str(self.root / 'kilo-state')},
            'lifecycle': {'network': 'fixture_default'},
            'release_source': 'https://github.com/synthetic-offline/mx-fixture/releases/download',
            'ingress': {'service': 'caddy', 'restore_ingress': True,
                        'health_url': 'https://fixture.invalid/health', 'expected_status': 200},
        }
        self.site_path = self.root / 'site.json'
        u.write(self.site_path, self.site)
        self.old_pointer = {
            'schema': 1, 'version': 'v1.0.0', 'commit': 'a' * 40,
            'state_dir': str(self.root / '.mx-updates' / 'old'),
            'compose_files': self.site['base_compose_files'],
            'updater': str(self.root / '.mx-updates' / 'old' / 'materials' / 'mx-update.py'),
        }
        u.write(self.site['active_release_pointer'], self.old_pointer)
        self.rows = []
        self.images = {}
        for index, service in enumerate((*u.APPS, 'caddy'), 1):
            self.rows.append({
                'Id': 'old-' + service, 'Image': 'sha256:' + str(index) * 64,
                'State': {'Running': True},
                'Config': {'Labels': {
                    'com.docker.compose.project': self.site['project'],
                    'com.docker.compose.service': service,
                    'com.docker.compose.project.working_dir': str(self.root),
                    'com.docker.compose.project.config_files': ','.join(self.site['base_compose_files']),
                }},
            })
            if service in u.APPS:
                self.images[service] = 'sha256:' + str(index + 4) * 64
        self.release = {
            'schema': 1, 'version': VERSION, 'platform': 'linux/amd64', 'commit': COMMIT,
            'assets': {name: {'size': 1, 'sha256': '0' * 64} for name in u.ASSETS},
            'notes': '合成发行，仅供宿主契约测试',
        }
        self.systemd_state = 'active'
        self.stop_mode = 'stopped'
        self.subprocess = mock.patch.object(h.subprocess, 'run', side_effect=self.command).start()
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(h.urllib.request, 'urlopen', side_effect=AssertionError('测试禁止外部网络')).start()
        self.fetcher = mock.Mock(side_effect=self.fetch)
        self.launcher = mock.Mock()
        self.host = h.UpdateHost(self.site_path, fetcher=self.fetcher, launcher=self.launcher,
                                 runner=self.successful_runner)
        self.initial = self.host.current()

    def row(self, service):
        return next(row for row in self.rows
                    if row['Config']['Labels']['com.docker.compose.service'] == service)

    def command(self, args, **kwargs):
        if args[:3] == ['docker', 'ps', '-aq']:
            self.assertEqual(args[3:], ['--filter', 'label=com.docker.compose.project=' + self.site['project']])
            output = '\n'.join(row['Id'] for row in self.rows)
        elif args[:2] == ['docker', 'inspect']:
            self.assertEqual(args[2:], [row['Id'] for row in self.rows])
            output = json.dumps(self.rows)
        elif args[:2] == ['docker', 'stop']:
            self.assertEqual(args[2:], [self.row(name)['Id'] for name in ('caddy', *u.APPS)])
            if self.stop_mode == 'command_error':
                raise subprocess.CalledProcessError(1, args, stderr='合成停止命令失败')
            if self.stop_mode != 'still_running':
                for row in self.rows:
                    row['State']['Running'] = False
            output = '\n'.join(args[2:])
        elif args[:2] == ['systemctl', 'show']:
            self.assertEqual(args[2:4], ['--property=ActiveState', '--value'])
            output = self.systemd_state + '\n'
        elif args[:3] == ['systemctl', 'start', '--no-block']:
            output = ''
        elif args and args[0] == 'curl':
            self.assertEqual(args[-1], self.site['ingress']['health_url'])
            output = str(self.site['ingress']['expected_status'])
        else:
            raise AssertionError('未登记的外部命令：' + repr(args))
        return subprocess.CompletedProcess(args, 0, stdout=output, stderr='')

    def fetch(self, url):
        if url == self.site['release_source'] + '/' + VERSION + '/release.json':
            return copy.deepcopy(self.release)
        if url == 'https://api.github.com/repos/synthetic-offline/mx-fixture/releases?per_page=20':
            return [{'tag_name': VERSION, 'body': '合成本站发行'}]
        if url == 'https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest':
            return {'tag_name': 'v9.0.0'}
        raise AssertionError('未登记的发行地址：' + str(url))

    def enqueue(self):
        preview = self.host.preview(VERSION)
        return self.host.install(preview['id'])

    def state_for(self, operation):
        return self.root / '.mx-updates' / (VERSION + '-' + operation['id'])

    def successful_runner(self, site_path, site, version, state, *, confirm, fetcher, progress):
        self.assertEqual(site_path, self.site_path)
        self.assertEqual(version, VERSION)
        progress('preflight')
        confirm({'version': version,
                 'current_container_ids': {name: row['id'] for name, row in self.initial['services'].items()}})
        (state / 'maintenance').write_text('合成维护标记\n', encoding='utf-8')
        progress('backing_up')
        u.write(state / 'plan.json', {'images': self.images})
        overlay = state / 'candidate.compose.yaml'
        overlay.write_text('合成 Compose\n', encoding='utf-8')
        pointer = {
            'schema': 1, 'version': version, 'commit': COMMIT, 'state_dir': str(state),
            'compose_files': [*site['base_compose_files'], str(overlay)],
            'updater': str(state / 'materials' / 'mx-update.py'),
        }
        progress('starting')
        for service in u.APPS:
            row = self.row(service)
            row['Id'] = 'new-' + service
            row['Image'] = self.images[service]
            row['Config']['Labels']['com.docker.compose.project.config_files'] = ','.join(pointer['compose_files'])
        progress('verifying')
        u.write(state / 'completed.json', dict(pointer,
                original_caddy_id=self.initial['services']['caddy']['id'],
                ingress_restored=True, ingress_health='passed'))
        u.write(site['active_release_pointer'], pointer)
        return pointer

    def assert_error(self, code, function, *args):
        with self.assertRaises(h.RequestError) as caught:
            function(*args)
        self.assertEqual(caught.exception.code, code)
        return caught.exception

    def assert_owner(self, operation, status):
        self.assertEqual(self.host.operation(operation['id'])['status'], status)
        self.assertEqual(self.host.active()['id'], operation['id'])

    def test_manual_flock_blocks_web_install_without_enqueuing(self):
        preview = self.host.preview(VERSION)
        with u.site_lock(self.site_path, self.site):
            error = self.assert_error('update_busy', self.host.install, preview['id'])
        self.assertEqual(error.operation_id, '')
        self.assertIsNone(self.host.active())
        self.launcher.assert_not_called()
        self.assertEqual(list((self.host.state / 'operations').glob('*.json')), [])

    def test_persistent_web_owner_blocks_manual_updater_main(self):
        operation = self.enqueue()
        with mock.patch.object(u, 'run_cycle') as run_cycle, \
                mock.patch.object(sys, 'argv', ['mx-update.py', '--site', str(self.site_path), '--version', VERSION]), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(u.main(), 1)
        run_cycle.assert_not_called()
        self.assert_owner(operation, 'queued')

    def test_same_site_second_candidate_returns_existing_owner(self):
        first = self.host.preview(VERSION)
        second = self.host.preview(VERSION)
        operation = self.host.install(first['id'])
        error = self.assert_error('update_busy', self.host.install, second['id'])
        self.assertEqual(error.operation_id, operation['id'])
        self.launcher.assert_called_once_with(operation['id'])

    def test_install_is_idempotent_and_launches_once(self):
        preview = self.host.preview(VERSION)
        first = self.host.install(preview['id'])
        self.assertEqual(self.host.install(preview['id']), first)
        self.launcher.assert_called_once_with(first['id'])
        self.assertEqual(len(list((self.host.state / 'operations').glob('*.json'))), 1)

    def test_install_rejects_changed_baseline_before_launch(self):
        preview = self.host.preview(VERSION)
        self.row('caddy')['Id'] = 'changed-caddy'
        self.assert_error('runtime_baseline_changed', self.host.install, preview['id'])
        self.assertIsNone(self.host.active())
        self.launcher.assert_not_called()

    def test_late_launch_failure_does_not_overwrite_running_worker_owner(self):
        entered, finish = threading.Event(), threading.Event()
        failures = []

        def runner(*args, **kwargs):
            kwargs['progress']('preflight')
            entered.set()
            if not finish.wait(3):
                raise AssertionError('测试工作线程未收到完成信号')
            return self.successful_runner(*args, **kwargs)

        def worker(operation_id):
            try:
                self.host.run(operation_id)
            except BaseException as error:
                failures.append(error)

        thread = None

        def launch(operation_id):
            nonlocal thread
            thread = threading.Thread(target=worker, args=(operation_id,), daemon=True)
            thread.start()
            self.assertTrue(entered.wait(3))
            raise subprocess.TimeoutExpired('systemctl', 15)

        self.host.runner, self.host.launcher = runner, launch
        try:
            operation = self.enqueue()
            self.assert_owner(operation, 'installing')
        finally:
            finish.set()
            if thread:
                thread.join(5)
        self.assertFalse(thread.is_alive())
        self.assertEqual(failures, [])
        self.assertEqual(self.host.operation(operation['id'])['status'], 'completed')

    def test_launch_failure_keeps_persistent_recovery_owner(self):
        self.launcher.side_effect = subprocess.CalledProcessError(1, 'systemctl')
        operation = self.enqueue()
        self.assert_owner(operation, 'recovery_required')
        with self.assertRaises(u.Failure):
            with u.site_lock(self.site_path, self.site):
                self.fail('手工更新进入了尚未收口的站点')

    def test_launch_uses_fixed_systemd_operation_unit(self):
        self.host.launcher = self.host.launch
        operation = self.enqueue()
        command = ['systemctl', 'start', '--no-block', 'mx-update-operation@' + operation['id'] + '.service']
        self.assertIn(command, [call.args[0] for call in self.subprocess.call_args_list])

    def test_worker_binds_live_images_pointer_and_original_caddy(self):
        operation = self.enqueue()
        result = self.host.run(operation['id'])
        self.assertEqual(result['status'], 'completed')
        self.assertEqual(result['active_version'], VERSION)
        self.assertTrue(result['registered'])
        self.assertIn('finished_at', result)
        actual = self.host.current()
        self.assertEqual({name: actual['services'][name]['image'] for name in u.APPS}, self.images)
        self.assertEqual(actual['services']['caddy']['id'], self.initial['services']['caddy']['id'])
        self.assertIsNone(self.host.active())

    def test_worker_confirmation_checks_each_of_four_container_ids(self):
        for service in (*u.APPS, 'caddy'):
            with self.subTest(service=service):
                operation = self.enqueue()

                def runner(*args, **kwargs):
                    ids = {name: row['id'] for name, row in self.initial['services'].items()}
                    ids[service] = 'unexpected-container'
                    kwargs['confirm']({'version': VERSION, 'current_container_ids': ids})
                    self.fail('错误容器编号被确认')

                self.host.runner = runner
                self.assert_error('confirmed_runtime_changed', self.host.run, operation['id'])
                self.assertEqual(self.host.operation(operation['id'])['status'], 'failed')
                self.assertIsNone(self.host.active())

    def test_worker_rejects_drift_between_enqueue_and_run(self):
        operation = self.enqueue()
        self.row(u.APPS[0])['Id'] = 'other-container'
        self.assert_error('runtime_baseline_changed_or_interrupted', self.host.run, operation['id'])
        self.assertEqual(self.host.operation(operation['id'])['status'], 'failed')
        self.assertIsNone(self.host.active())

    def test_worker_rejects_changed_fixed_release(self):
        operation = self.enqueue()
        changed = dict(self.release, commit='c' * 40)

        def runner(site_path, site, version, state, **kwargs):
            kwargs['fetcher'](site, version, state)

        self.host.runner = runner
        with mock.patch.object(u, 'fetch_release', return_value=(self.root, changed)):
            self.assert_error('confirmed_release_changed', self.host.run, operation['id'])
        self.assertEqual(self.host.operation(operation['id'])['status'], 'failed')
        self.assertIsNone(self.host.active())

    def test_failure_before_maintenance_is_terminal_and_releases_owner(self):
        operation = self.enqueue()
        self.host.runner = mock.Mock(side_effect=RuntimeError('合成预检失败'))
        with self.assertRaisesRegex(RuntimeError, '合成预检失败'):
            self.host.run(operation['id'])
        result = self.host.operation(operation['id'])
        self.assertEqual(result['status'], 'failed')
        self.assertIn('finished_at', result)
        self.assertIsNone(self.host.active())
        with u.site_lock(self.site_path, self.site):
            pass

    def test_failure_after_maintenance_retains_recovery_owner(self):
        operation = self.enqueue()

        def runner(site_path, site, version, state, **kwargs):
            (state / 'maintenance').write_text('合成维护\n', encoding='utf-8')
            raise RuntimeError('合成维护后失败')

        self.host.runner = runner
        with self.assertRaisesRegex(RuntimeError, '合成维护后失败'):
            self.host.run(operation['id'])
        self.assert_owner(operation, 'recovery_required')
        self.assertNotIn('finished_at', self.host.operation(operation['id']))
        with self.assertRaises(u.Failure):
            with u.site_lock(self.site_path, self.site):
                self.fail('维护后失败丢失了持久所有者')

    def test_worker_rejects_live_image_not_bound_to_plan(self):
        operation = self.enqueue()

        def runner(*args, **kwargs):
            pointer = self.successful_runner(*args, **kwargs)
            self.row(u.APPS[0])['Image'] = 'sha256:' + 'e' * 64
            return pointer

        self.host.runner = runner
        self.assert_error('updated_runtime_not_verified', self.host.run, operation['id'])
        self.assert_owner(operation, 'recovery_required')

    def test_interrupted_does_not_overwrite_terminal_operation(self):
        for status in sorted(h.TERMINAL):
            with self.subTest(status=status):
                operation = self.enqueue()
                stored = self.host.operation(operation['id'])
                stored.update(status=status, message='已完成的原回执', finished_at='2026-11-16T00:00:00Z')
                self.host.save_operation(stored)
                self.host.interrupted(operation['id'])
                self.assertEqual(self.host.operation(operation['id']), stored)

    def test_interrupted_before_maintenance_does_not_halt_runtime(self):
        operation = self.enqueue()
        self.host.interrupted(operation['id'])
        self.assert_owner(operation, 'recovery_required')
        self.assertNotIn('halt_confirmed', self.host.operation(operation['id']))
        self.assertTrue(all(row['State']['Running'] for row in self.rows))
        self.assertFalse(any(call.args[0][:2] == ['docker', 'stop']
                             for call in self.subprocess.call_args_list))

    def test_interrupted_after_maintenance_confirms_all_four_stopped(self):
        operation = self.enqueue()
        state = self.state_for(operation)
        state.mkdir(parents=True)
        (state / 'maintenance').write_text('合成维护\n', encoding='utf-8')
        self.host.interrupted(operation['id'])
        self.assert_owner(operation, 'recovery_required')
        self.assertIs(self.host.operation(operation['id'])['halt_confirmed'], True)
        self.assertFalse(any(row['State']['Running'] for row in self.rows))
        stop_calls = [call.args[0] for call in self.subprocess.call_args_list
                      if call.args[0][:2] == ['docker', 'stop']]
        self.assertEqual(stop_calls, [['docker', 'stop', *[self.row(name)['Id'] for name in ('caddy', *u.APPS)]]])
        with self.assertRaises(u.Failure):
            with u.site_lock(self.site_path, self.site):
                self.fail('停态确认提前释放了维护所有者')

    def test_interrupted_halt_failure_retains_owner_and_negative_receipt(self):
        operation = self.enqueue()
        state = self.state_for(operation)
        state.mkdir(parents=True)
        (state / 'maintenance').write_text('合成维护\n', encoding='utf-8')
        for mode, error_type in (('command_error', 'CalledProcessError'), ('still_running', 'RequestError')):
            with self.subTest(mode=mode):
                self.stop_mode = mode
                self.host.interrupted(operation['id'])
                self.assert_owner(operation, 'recovery_required')
                stored = self.host.operation(operation['id'])
                self.assertIs(stored['halt_confirmed'], False)
                self.assertEqual(stored['halt_error_type'], error_type)
                self.assertTrue(any(row['State']['Running'] for row in self.rows))
                with self.assertRaises(u.Failure):
                    with u.site_lock(self.site_path, self.site):
                        self.fail('停态失败提前释放了维护所有者')

    def test_daemon_reconcile_preserves_live_systemd_owner(self):
        operation = self.enqueue()
        for state in ('active', 'activating', 'deactivating'):
            with self.subTest(state=state):
                self.systemd_state = state
                self.host.reconcile_active()
                self.assert_owner(operation, 'queued')
        expected = ['systemctl', 'show', '--property=ActiveState', '--value',
                    'mx-update-operation@' + operation['id'] + '.service']
        self.assertIn(expected, [call.args[0] for call in self.subprocess.call_args_list])

    def test_daemon_reconcile_marks_missing_worker_and_retains_owner(self):
        operation = self.enqueue()
        self.systemd_state = 'failed'
        self.host.reconcile_active()
        self.assert_owner(operation, 'recovery_required')
        with self.assertRaises(u.Failure):
            with u.site_lock(self.site_path, self.site):
                self.fail('中断任务的维护所有者丢失')

    def prepare_completed_recovery(self):
        operation = self.enqueue()
        state = self.state_for(operation)
        state.mkdir(parents=True)
        self.successful_runner(self.site_path, self.site, VERSION, state,
                               confirm=lambda summary: None, fetcher=None, progress=lambda stage: None)
        self.host.interrupted(operation['id'])
        # 恢复确认只在操作者已恢复服务后运行。
        for row in self.rows:
            row['State']['Running'] = True
        return operation, state

    def test_recovery_rejects_wrong_images_despite_matching_completion(self):
        operation, _ = self.prepare_completed_recovery()
        self.row(u.APPS[0])['Image'] = 'sha256:' + 'f' * 64
        self.assert_error('runtime_recovery_not_verified', self.host.recover, operation['id'])
        self.assert_owner(operation, 'recovery_required')
        self.assertFalse(any(call.args[0][0] == 'curl' for call in self.subprocess.call_args_list))

    def test_recovery_rejects_changed_original_caddy(self):
        operation, _ = self.prepare_completed_recovery()
        self.row('caddy')['Id'] = 'replacement-caddy'
        self.assert_error('runtime_recovery_not_verified', self.host.recover, operation['id'])
        self.assert_owner(operation, 'recovery_required')

    def test_recovery_rejects_missing_completion_record(self):
        operation, state = self.prepare_completed_recovery()
        (state / 'completed.json').unlink()
        self.assert_error('runtime_recovery_not_verified', self.host.recover, operation['id'])
        self.assert_owner(operation, 'recovery_required')

    def test_recovery_accepts_bound_completion_only_after_health(self):
        operation, _ = self.prepare_completed_recovery()
        result = self.host.recover(operation['id'])
        self.assertEqual(result['status'], 'completed')
        self.assertTrue(result['registered'])
        self.assertIsNone(self.host.active())
        self.assertEqual([call.args[0][-1] for call in self.subprocess.call_args_list
                          if call.args[0][0] == 'curl'], [self.site['ingress']['health_url']])

    def test_recovery_of_original_images_closes_as_failed_not_updated(self):
        operation = self.enqueue()
        self.host.interrupted(operation['id'])
        result = self.host.recover(operation['id'])
        self.assertEqual(result['status'], 'failed')
        self.assertFalse(result['registered'])
        self.assertEqual(result['active_version'], self.old_pointer['version'])
        self.assertIsNone(self.host.active())

    def start_socket(self):
        path = self.root / 'host.sock'
        server = socketserver.UnixStreamServer(str(path), h.Handler)
        path.chmod(0o600)
        server.update_host = self.host
        thread = threading.Thread(target=server.serve_forever, kwargs={'poll_interval': 0.01}, daemon=True)
        thread.start()

        def stop():
            server.shutdown()
            server.server_close()
            thread.join(3)
            self.assertFalse(thread.is_alive())

        self.addCleanup(stop)
        return path

    def post(self, path, route, body, site_id='fixture-site'):
        connection = UnixHTTPConnection(path)
        try:
            data = json.dumps(body).encode('utf-8')
            headers = ['POST ' + route + ' HTTP/1.0', 'Content-Type: application/json',
                       'Content-Length: ' + str(len(data))]
            if site_id is not None:
                headers.append('X-MX-Site-ID: ' + site_id)
            connection.connect()
            connection.sock.sendall(('\r\n'.join(headers) + '\r\n\r\n').encode('ascii') + data)
            response = http.client.HTTPResponse(connection.sock)
            response.begin()
            return response.status, json.loads(response.read())
        finally:
            connection.close()

    def test_unix_http_requires_fixed_registered_site_id(self):
        path = self.start_socket()
        self.fetcher.reset_mock()
        for site_id in (None, 'other-site'):
            with self.subTest(site_id=site_id):
                status, result = self.post(path, '/v1/discover', {}, site_id)
                self.assertEqual(status, 403)
                self.assertEqual(result['error'], 'registered_site_mismatch')
        self.fetcher.assert_not_called()
        status, result = self.post(path, '/v1/discover', {})
        self.assertEqual(status, 200)
        self.assertEqual(result['site_id'], self.site['project'])

    def test_unix_http_rejects_arbitrary_url_fields_before_fetch(self):
        path = self.start_socket()
        for route, baseline in (('/v1/discover', {}), ('/v1/preview', {'release_id': VERSION}),
                                ('/v1/install', {'candidate_id': 'not-used'})):
            for field in ('url', 'download_url', 'release_source', 'site_id'):
                with self.subTest(route=route, field=field):
                    self.fetcher.reset_mock()
                    status, result = self.post(path, route, dict(baseline, **{field: 'https://other.invalid/payload'}))
                    self.assertEqual(status, 400)
                    self.assertEqual(result['error'], 'unsupported_update_action')
                    self.fetcher.assert_not_called()
        self.launcher.assert_not_called()

    def test_unix_http_preview_install_and_idempotent_acceptance(self):
        path = self.start_socket()
        status, preview = self.post(path, '/v1/preview', {'release_id': VERSION})
        self.assertEqual(status, 200)
        self.assertEqual(preview['site_id'], self.site['project'])
        first_status, operation = self.post(path, '/v1/install', {'candidate_id': preview['id']})
        second_status, repeated = self.post(path, '/v1/install', {'candidate_id': preview['id']})
        self.assertEqual((first_status, second_status), (202, 202))
        self.assertEqual(repeated, operation)
        self.assertEqual(operation['status'], 'queued')
        self.launcher.assert_called_once_with(operation['id'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
