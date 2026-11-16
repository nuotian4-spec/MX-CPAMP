#!/usr/bin/env python3
"""宿主桥首次接入专项：临时系统目录、真实文件锁及合成系统命令。"""

import configparser
import contextlib
import copy
import importlib.util
import io
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


DEPLOY = pathlib.Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('mx_update_install_tested', DEPLOY / 'mx-update-host-install.py')
i = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(i)
u = i.updater


@unittest.skipUnless(os.name == 'posix' and os.geteuid() == 0, '首次接入专项需要 Linux root')
class InstallTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='mx-install-test-')
        self.addCleanup(temporary.cleanup)
        self.workspace = pathlib.Path(temporary.name)
        self.root = self.workspace / 'site'
        self.system_root = self.workspace / 'system'
        self.root.mkdir()
        self.system_root.mkdir()
        for name in ('compose.yaml', 'compose.env.yaml', 'legacy.override.yaml'):
            (self.root / name).write_text('合成 Compose 文件\n', encoding='utf-8')
        self.config_bytes = b'fixture: original-config\nstate: preserve\n'
        (self.root / 'config.yaml').write_bytes(self.config_bytes)
        self.site = {
            'schema': 1, 'root': str(self.root), 'project': 'fixture-install',
            'cpa_config': str(self.root / 'config.yaml'),
            'base_compose_files': [str(self.root / 'compose.yaml'), str(self.root / 'compose.env.yaml')],
            'active_release_pointer': str(self.root / '.mx-active-release.json'),
            'kilo_state': {'container': '/root/kilo-state', 'host': str(self.root / 'kilo-state')},
            'lifecycle': {'network': 'fixture_default'},
            'release_source': 'https://fixture.invalid/repo/releases/download',
            'ingress': {'service': 'caddy', 'restore_ingress': True,
                        'health_url': 'https://fixture.invalid/health', 'expected_status': 200},
        }
        self.original_chain = [*self.site['base_compose_files'], str(self.root / 'legacy.override.yaml')]
        self.original_pointer = {
            'schema': 1, 'version': 'v1.2.3', 'commit': 'a' * 40,
            'compose_files': self.original_chain, 'state_dir': str(self.root / '.mx-updates' / 'original'),
            'updater': str(self.root / '.mx-updates' / 'original' / 'materials' / 'mx-update.py'),
        }
        self.site_path = self.root / 'site.json'
        u.write(self.site_path, self.site)
        u.write(self.site['active_release_pointer'], self.original_pointer)
        self.original_site_bytes = self.site_path.read_bytes()
        self.original_pointer_bytes = pathlib.Path(self.site['active_release_pointer']).read_bytes()
        self.lock_path = self.root / '.mx-update.lock'
        self.lock_bytes = b'existing-site-lock\n'
        self.lock_path.write_bytes(self.lock_bytes)
        self.lock_path.chmod(0o600)
        self.live = {
            name: {'id': 'original-' + name, 'image': 'sha256:' + str(index) * 64,
                   'compose_files': copy.deepcopy(self.original_chain), 'running': True}
            for index, name in enumerate((*u.APPS, 'caddy'), 1)
        }
        self.initial_live = copy.deepcopy(self.live)
        self.fault = None
        self.halt_fails = False
        self.health_status = '200'
        self.commands = mock.patch.object(i, 'command', side_effect=self.command).start()
        self.inspector = mock.patch.object(i.host_module, 'inspect_runtime', side_effect=self.runtime).start()
        self.docker_json = mock.patch.object(i.host_module, 'docker_json', side_effect=self.inspect_cpa).start()
        self.halt = mock.patch.object(i.host_module, 'halt_registered_runtime', side_effect=self.stop_runtime).start()
        mock.patch.object(i.subprocess, 'run', side_effect=AssertionError('测试禁止真实外部命令')).start()
        mock.patch.object(i.time, 'sleep').start()
        self.addCleanup(mock.patch.stopall)

    def runtime(self, site):
        self.assertEqual(site['root'], str(self.root))
        self.assertEqual(site['project'], self.site['project'])
        if not all(row['running'] for row in self.live.values()):
            raise i.host_module.RequestError('registered_runtime_not_healthy')
        pointer_path = pathlib.Path(site['active_release_pointer'])
        pointer = u.read(pointer_path) if pointer_path.exists() else None
        return {'version': pointer['version'] if pointer else '',
                'pointer': pointer, 'services': copy.deepcopy(self.live)}

    def inspect_cpa(self, arguments):
        self.assertEqual(arguments, ['inspect', self.live['cli-proxy-api']['id']])
        return [{'Config': {'User': 'root'}, 'Mounts': [
            {'Destination': '/mx-update.lock', 'Source': str(self.lock_path)},
            {'Destination': '/mx-update-state', 'Source': str(self.root / '.mx-update-state')},
            {'Destination': '/run/mx-update', 'Source': '/run/mx-update'},
        ]}]

    def command(self, arguments):
        if arguments == ['systemctl', '--version']:
            return 'systemd 合成版本'
        if arguments == ['systemctl', 'daemon-reload']:
            return ''
        if arguments == ['systemctl', 'enable', '--now', 'mx-update-host.service']:
            if self.fault == 'enable':
                raise subprocess.CalledProcessError(1, arguments)
            return ''
        if arguments[:2] == ['docker', 'compose']:
            self.assertEqual(arguments[2:6], ['--project-directory', str(self.root), '-p', self.site['project']])
            offset, chain = 6, []
            while arguments[offset] == '-f':
                chain.append(arguments[offset + 1])
                offset += 2
            action = arguments[offset:]
            if action == ['config', '--quiet']:
                return ''
            self.assertEqual(action, ['up', '-d', '--no-build', '--pull', 'never', '--no-deps', '--force-recreate', *u.APPS])
            if self.fault == 'up':
                raise subprocess.CalledProcessError(1, arguments)
            for name in u.APPS:
                self.live[name].update(id='bridged-' + name, compose_files=chain, running=True)
            return ''
        if arguments[:2] == ['docker', 'stop']:
            self.assertEqual(arguments[2:], [self.initial_live[name]['id'] for name in ('caddy', *u.APPS)])
            for row in self.live.values():
                row['running'] = False
            return ''
        if arguments[:2] == ['docker', 'start']:
            self.assertEqual(arguments[2:], [self.initial_live['caddy']['id']])
            self.live['caddy']['running'] = True
            return ''
        if arguments[0] == 'curl':
            self.assertEqual(arguments[-1], self.site['ingress']['health_url'])
            return self.health_status
        raise AssertionError('未登记的系统命令：' + repr(arguments))

    def stop_runtime(self, site):
        self.assertEqual(site['root'], str(self.root))
        self.assertEqual(site['project'], self.site['project'])
        if self.halt_fails:
            raise subprocess.CalledProcessError(1, ['docker', 'stop', 'fixture'])
        for row in self.live.values():
            row['running'] = False

    def install(self, confirmed=True):
        with contextlib.redirect_stdout(io.StringIO()):
            i.install(self.site_path, confirmed, system_root=self.system_root)

    def backup(self):
        backups = list((self.root / '.mx-update-bootstrap').iterdir())
        self.assertEqual(len(backups), 1)
        return backups[0]

    def owner(self):
        state = self.root / '.mx-update-state'
        active = u.read(state / 'active.json')
        return u.read(state / 'operations' / (active['id'] + '.json'))

    def tree_contents(self):
        return {str(path.relative_to(self.workspace)):
                ('link', os.readlink(path)) if path.is_symlink() else
                ('file', path.read_bytes()) if path.is_file() else ('directory', None)
                for path in self.workspace.rglob('*')}

    def failed_install(self):
        self.fault = 'up'
        with self.assertRaises(subprocess.CalledProcessError):
            self.install()
        self.fault = None
        return self.backup()

    def restore_original(self, backup):
        shutil.copy2(backup / 'site.json', self.site_path)
        shutil.copy2(backup / 'config.yaml', self.site['cpa_config'])
        shutil.copy2(backup / 'active-pointer.json', self.site['active_release_pointer'])
        self.live = copy.deepcopy(self.initial_live)

    def test_without_confirmation_changes_nothing(self):
        before = self.tree_contents()
        with self.assertRaisesRegex(u.Failure, 'confirm-maintenance'):
            self.install(False)
        self.assertEqual(self.tree_contents(), before)
        for boundary in (self.commands, self.inspector, self.docker_json, self.halt):
            boundary.assert_not_called()

    def test_other_registered_site_is_rejected_without_replacing_registry(self):
        registry = self.system_root / 'etc/mx-update/site.json'
        registry.parent.mkdir(parents=True)
        u.write(registry, {'root': '/other-site', 'project': 'other-project'})
        before = self.tree_contents()
        with self.assertRaisesRegex(u.Failure, '另一站点'):
            self.install()
        self.assertEqual(self.tree_contents(), before)
        self.commands.assert_not_called()
        self.inspector.assert_not_called()

    def test_existing_lock_keeps_same_inode_and_contents(self):
        with self.lock_path.open('rb') as existing:
            original_inode = os.fstat(existing.fileno()).st_ino
            self.install()
            self.assertEqual(self.lock_path.stat().st_ino, original_inode)
            self.assertEqual(os.fstat(existing.fileno()).st_ino, original_inode)
            self.assertEqual(existing.read(), self.lock_bytes)
        self.assertEqual(self.lock_path.read_bytes(), self.lock_bytes)
        self.assertEqual(self.lock_path.stat().st_mode & 0o777, 0o600)

    def test_manual_site_lock_blocks_recreation(self):
        with u.site_lock(self.site_path, self.site):
            with self.assertRaises(BlockingIOError):
                self.install()
        self.assertFalse((self.root / 'compose.mx-update-host.json').exists())
        self.assertFalse((self.root / '.mx-update-state' / 'active.json').exists())
        self.assertFalse(any(call.args[0][:2] in (['docker', 'stop'], ['docker', 'compose'])
                             for call in self.commands.call_args_list))

    def test_all_apps_recreate_full_chain_and_original_caddy_is_preserved(self):
        self.install()
        next_chain = [*self.site['base_compose_files'], str(self.root / 'compose.mx-update-host.json'),
                      self.original_chain[-1]]
        for name in u.APPS:
            self.assertEqual(self.live[name]['compose_files'], next_chain)
            self.assertEqual(self.live[name]['image'], self.initial_live[name]['image'])
            self.assertNotEqual(self.live[name]['id'], self.initial_live[name]['id'])
        self.assertEqual(self.live['caddy']['id'], self.initial_live['caddy']['id'])
        self.assertEqual(self.live['caddy']['compose_files'], self.original_chain)
        up_calls = [call.args[0] for call in self.commands.call_args_list
                    if call.args[0][:2] == ['docker', 'compose'] and 'up' in call.args[0]]
        self.assertEqual(len(up_calls), 1)
        self.assertEqual(up_calls[0][-len(u.APPS):], list(u.APPS))
        self.assertEqual(u.read(self.site['active_release_pointer'])['compose_files'], next_chain)
        self.assertEqual(u.read(self.site_path)['base_compose_files'], next_chain[:-1])
        self.halt.assert_not_called()

    def test_configuration_site_and_original_pointer_are_backed_up(self):
        self.install()
        backup = self.backup()
        self.assertEqual((backup / 'site.json').read_bytes(), self.original_site_bytes)
        self.assertEqual((backup / 'config.yaml').read_bytes(), self.config_bytes)
        self.assertEqual((backup / 'active-pointer.json').read_bytes(), self.original_pointer_bytes)
        self.assertEqual(pathlib.Path(self.site['cpa_config']).read_bytes(), self.config_bytes)
        before = u.read(backup / 'before.json')
        self.assertEqual(before['original_chain'], self.original_chain)
        self.assertFalse(before['database_restore'])

    def test_existing_system_materials_are_preserved_before_replacement(self):
        originals = {}
        for folder, names in (('usr/local/lib/mx-update', i.MATERIALS), ('etc/systemd/system', i.UNITS)):
            target = self.system_root / folder
            target.mkdir(parents=True, exist_ok=True)
            for name in names:
                originals[name] = ('原安装材料：' + name + '\n').encode('utf-8')
                (target / name).write_bytes(originals[name])
        self.install()
        backup = self.backup()
        for name, data in originals.items():
            self.assertEqual((backup / ('previous-' + name)).read_bytes(), data)
        for name in i.MATERIALS:
            installed = self.system_root / 'usr/local/lib/mx-update' / name
            self.assertEqual(installed.read_bytes(), (DEPLOY / name).read_bytes())
            self.assertEqual(installed.stat().st_mode & 0o777, 0o700)
        for name in i.UNITS:
            installed = self.system_root / 'etc/systemd/system' / name
            self.assertEqual(installed.read_bytes(), (DEPLOY / name).read_bytes())
            self.assertEqual(installed.stat().st_mode & 0o777, 0o644)

    def test_success_registers_site_and_clears_active_owner(self):
        self.install()
        registry = self.system_root / 'etc/mx-update/site.json'
        self.assertTrue(registry.is_symlink())
        self.assertEqual(registry.resolve(), self.site_path)
        state = self.root / '.mx-update-state'
        self.assertFalse((state / 'active.json').exists())
        operations = list((state / 'operations').glob('*.json'))
        self.assertEqual(len(operations), 1)
        operation = u.read(operations[0])
        self.assertEqual((operation['type'], operation['status']), ('integration', 'completed'))
        self.assertIn('finished_at', operation)
        completed = u.read(self.backup() / 'completed.json')
        self.assertEqual(completed['health'], 'passed')
        self.assertFalse(completed['database_restore'])
        overlay = u.read(self.root / 'compose.mx-update-host.json')['services']['cli-proxy-api']
        self.assertEqual(overlay['environment']['MX_UPDATE_SITE_ID'], self.site['project'])
        self.assertEqual(overlay['environment']['MX_UPDATE_LOCK_PATH'], '/mx-update.lock')
        self.assertEqual((self.system_root / 'run/mx-update').stat().st_mode & 0o777, 0o700)
        with u.site_lock(self.site_path, u.read(self.site_path)):
            pass

    def test_registry_used_as_site_stays_regular_file_without_self_link(self):
        registry = self.system_root / 'etc/mx-update/site.json'
        registry.parent.mkdir(parents=True)
        shutil.copy2(self.site_path, registry)
        self.site_path = registry
        self.install()
        self.assertTrue(registry.is_file())
        self.assertFalse(registry.is_symlink())
        self.assertEqual(registry.resolve(), registry)
        self.assertEqual(registry.stat().st_mode & 0o777, 0o600)
        self.assertEqual(u.read(registry)['project'], self.site['project'])
        self.assertEqual(u.read(registry)['update_state_dir'], str(self.root / '.mx-update-state'))
        self.assertEqual((self.backup() / 'site.json').read_bytes(), self.original_site_bytes)
        self.assertFalse((self.root / '.mx-update-state' / 'active.json').exists())

    def test_failure_after_stop_retains_recovery_owner_and_halts(self):
        backup = self.failed_install()
        self.halt.assert_called_once()
        operation = self.owner()
        self.assertEqual((operation['type'], operation['status']), ('integration', 'recovery_required'))
        self.assertNotIn('finished_at', operation)
        self.assertFalse(any(row['running'] for row in self.live.values()))
        self.assertEqual(u.read(backup / 'recovery-required.json')['status'], 'recovery_required')
        self.assertFalse((backup / 'completed.json').exists())
        with self.assertRaises(u.Failure):
            with u.site_lock(self.site_path, u.read(self.site_path)):
                self.fail('首次接入失败丢失了维护所有者')

    def test_halt_failure_still_persists_recovery_owner(self):
        self.halt_fails = True
        backup = self.failed_install()
        self.halt.assert_called_once()
        self.assertEqual(self.owner()['status'], 'recovery_required')
        self.assertEqual(u.read(backup / 'recovery-required.json')['status'], 'recovery_required')
        with self.assertRaises(u.Failure):
            with u.site_lock(self.site_path, u.read(self.site_path)):
                self.fail('停态确认失败丢失了所有者')

    def test_failed_ingress_health_halts_and_does_not_publish_success(self):
        self.health_status = '503'
        with self.assertRaisesRegex(u.Failure, '入口健康'):
            self.install()
        self.halt.assert_called_once()
        self.assertEqual(self.owner()['status'], 'recovery_required')
        self.assertFalse((self.backup() / 'completed.json').exists())
        self.assertFalse(any(row['running'] for row in self.live.values()))

    def test_recover_rejects_wrong_image_chain_or_pointer_before_health(self):
        backup = self.failed_install()
        for change in ('image', 'app_chain', 'caddy_chain', 'pointer'):
            with self.subTest(change=change):
                self.restore_original(backup)
                if change == 'image':
                    self.live[u.APPS[0]]['image'] = 'sha256:' + 'f' * 64
                elif change == 'app_chain':
                    self.live[u.APPS[0]]['compose_files'] = self.original_chain[:-1]
                elif change == 'caddy_chain':
                    self.live['caddy']['compose_files'] = self.original_chain[:-1]
                else:
                    u.write(self.site['active_release_pointer'], dict(self.original_pointer, version='v9.9.9'))
                self.commands.reset_mock()
                with self.assertRaisesRegex(u.Failure, '尚未恢复'):
                    i.recover(self.site_path, backup)
                self.assertEqual(self.owner()['status'], 'recovery_required')
                self.commands.assert_not_called()

    def test_recover_requires_healthy_ingress_before_releasing_owner(self):
        backup = self.failed_install()
        self.restore_original(backup)
        self.health_status = '503'
        with self.assertRaisesRegex(u.Failure, '恢复入口健康'):
            i.recover(self.site_path, backup)
        self.assertEqual(self.owner()['status'], 'recovery_required')

    def test_recover_original_runtime_closes_failed_operation_without_database_restore(self):
        backup = self.failed_install()
        operation_id = self.owner()['id']
        self.restore_original(backup)
        self.commands.reset_mock()
        with contextlib.redirect_stdout(io.StringIO()):
            i.recover(self.site_path, backup)
        state = self.root / '.mx-update-state'
        self.assertFalse((state / 'active.json').exists())
        operation = u.read(state / 'operations' / (operation_id + '.json'))
        self.assertEqual((operation['status'], operation['stage']), ('failed', 'recovered'))
        self.assertIn('finished_at', operation)
        self.assertEqual(pathlib.Path(self.site['cpa_config']).read_bytes(), self.config_bytes)
        self.assertEqual(len(self.commands.call_args_list), 1)
        self.assertEqual(self.commands.call_args.args[0][0], 'curl')
        with u.site_lock(self.site_path, u.read(self.site_path)):
            pass

    def test_cli_does_not_expose_test_system_root(self):
        with mock.patch.object(i, 'install') as install, \
                mock.patch.object(sys, 'argv', ['mx-update-host-install.py', '--site', str(self.site_path), '--confirm-maintenance']):
            i.main()
            install.assert_called_once_with(self.site_path, True)
        with mock.patch.object(i, 'install') as install, \
                mock.patch.object(sys, 'argv', ['mx-update-host-install.py', '--site', str(self.site_path), '--system-root', str(self.system_root)]), \
                contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as failure:
                i.main()
            self.assertEqual(failure.exception.code, 2)
            install.assert_not_called()


class UnitFileTests(unittest.TestCase):
    def unit(self, name):
        parser = configparser.ConfigParser(interpolation=None)
        parser.optionxform = str
        parser.read(DEPLOY / name, encoding='utf-8')
        return parser

    def test_worker_has_exec_stop_post_and_is_independent_of_manager(self):
        unit = self.unit('mx-update-operation@.service')
        self.assertEqual(unit['Service']['ExecStart'],
                         '/usr/bin/python3 /usr/local/lib/mx-update/mx-update-host.py --site /etc/mx-update/site.json --run %i')
        self.assertEqual(unit['Service']['ExecStopPost'],
                         '/usr/bin/python3 /usr/local/lib/mx-update/mx-update-host.py --site /etc/mx-update/site.json --interrupted %i')
        self.assertEqual(unit['Service']['Type'], 'oneshot')
        self.assertEqual(unit['Service']['User'], 'root')
        self.assertEqual(unit['Service']['KillMode'], 'control-group')
        self.assertEqual(unit['Service']['Restart'], 'no')
        self.assertEqual(unit['Unit']['Requires'], 'docker.service')
        self.assertNotIn('manager', ' '.join(value.lower() for value in unit['Unit'].values()))

    def test_host_unit_uses_fixed_registry_and_preserves_runtime_directory(self):
        unit = self.unit('mx-update-host.service')
        self.assertEqual(unit['Service']['ExecStart'],
                         '/usr/bin/python3 /usr/local/lib/mx-update/mx-update-host.py --site /etc/mx-update/site.json --serve')
        self.assertEqual(unit['Service']['RuntimeDirectory'], 'mx-update')
        self.assertEqual(unit['Service']['RuntimeDirectoryMode'], '0700')
        self.assertEqual(unit['Service']['RuntimeDirectoryPreserve'], 'yes')
        self.assertEqual(unit['Service']['UMask'], '0077')
        self.assertNotIn('manager', ' '.join(value.lower() for value in unit['Unit'].values()))


if __name__ == '__main__':
    unittest.main(verbosity=2)
