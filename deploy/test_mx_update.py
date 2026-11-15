#!/usr/bin/env python3
"""只运行新更新路径的合成专项；不访问 Docker、服务器或真实账号。"""

import copy
import importlib.util
import json
import pathlib
import shutil
import sys
import tempfile
import types
import unittest
from unittest import mock


DEPLOY = pathlib.Path(__file__).resolve().parent


def load(name, path):
    specification = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


u = load('mx_update_tested', DEPLOY / 'mx-update.py')
c = load('mx_capability_tested', DEPLOY / 'capability_probe.py')
old = load('mx_release_fixture_helpers', DEPLOY / 'test_kilo_release.py')
k = old.k
COMMIT = '1' * 40


def fixture_contract():
    names = ('m10-scope', 'm10-joint', 'bps-native', 'bps-stream', 'bps-legacy')
    contract = {'schema': 1, 'kind': 'inherited-production-wiring', 'source_roots': ['fixture'],
                'source_files': {'fixture/main.go': {'mode': '100644', 'blob': 'a' * 40}},
                'lineage': ['synthetic-test-only'],
                'incremental_review': {'status': 'PASS', 'reviewer': 'synthetic-test-only',
                    'receipt': 'synthetic-test-only', 'normal_startup_preserved': True, 'source_mapping_reviewed': True},
                'test_runs': {name: {'format': 'go-verbose', 'tests': ['TestFixture']} for name in names}}
    evidence = {'schema': 1, 'runs': {name: {'exit_code': 0, 'inherited': True, 'source': 'synthetic-test-only',
                'input_revision': 'synthetic-test-only', 'command': ['fixture'],
                'stdout': '--- PASS: TestFixture (0.01s)\nPASS\n'} for name in names}}
    return contract, evidence


def materials(folder, images):
    folder.mkdir(parents=True)
    for name in ('mx-update.py', 'capability_probe.py', 'capability-matrix.json', 'kilo-release.py', 'phase2-release.py'):
        shutil.copyfile(DEPLOY / name, folder / name)
    (folder / 'kilo-release.sh').write_text('#!/bin/sh\n', encoding='utf-8')
    contract, evidence = fixture_contract()
    u.write(folder / 'production-wiring.contract.json', contract)
    u.write(folder / 'production-wiring.evidence.json', evidence)
    u.write(folder / 'production-wiring.json', {'schema': 1, 'kind': 'inherited-production-wiring',
        'candidate_source_commit': COMMIT, 'source_mapping': 'exact-git-tree',
        'contract': 'production-wiring.contract.json', 'evidence': 'production-wiring.evidence.json'})
    release = {'schema': 1, 'version': 'v1.2.3', 'commit': COMMIT, 'platform': 'linux/amd64',
        'candidates': {service: {'role': role, 'ref': 'fixture:' + role, 'revision': COMMIT,
                                'accepted_ids': [images[service]]} for service, role in c.ROLES.items()},
        'assets': {'mx-update.py': {'size': (folder / 'mx-update.py').stat().st_size, 'sha256': u.digest(folder / 'mx-update.py')}}}
    u.write(folder / 'release.json', release)
    return release


class UpdateDocker(old.LifecycleDocker):
    def __init__(self, spec, root):
        super().__init__(spec, root)
        self.recreations = 0
        self.probe_fault = None
        for container in self.live.values():
            container['Config']['Labels']['com.docker.compose.project.working_dir'] = str(root)

    def yaml_document(self, path):
        if pathlib.Path(path).name == 'cpa-config.yaml':
            return u.read(path)
        return super().yaml_document(path)

    def command(self, args, data=None):
        if args[:3] == ['docker', 'image', 'inspect']:
            self.calls.append(args)
            service = next(name for name, image in self.images.items() if image == args[-1])
            return json.dumps([{'Id': args[-1], 'Os': 'linux', 'Architecture': 'amd64', 'Config': {'Labels': {
                'org.opencontainers.image.revision': COMMIT, 'io.mx-cpamp.role': c.ROLES[service]}}}])
        if args[:2] == ['docker', 'run']:
            self.calls.append(args)
            path = pathlib.Path(args[args.index('--mount') + 1].split('src=', 1)[1].split(',dst=', 1)[0])
            cfg = u.read(path)
            count, observations = c.policy_expectations(cfg)
            policy = {'schema': 1, 'kind': 'authz-policy-matrix', 'validator': c.VALIDATOR,
                      'source_commit': COMMIT, 'config_sha256': u.digest(path), 'key_count': count, 'observations': observations}
            if self.probe_fault:
                self.probe_fault(policy)
            return json.dumps(policy)
        if args[:2] == ['docker', 'compose'] and '-' in args and data:
            self.calls.append(args)
            return json.dumps({'x-document': json.loads('\n'.join(line[2:] for line in data.splitlines()[2:]))})
        if args[:2] == ['docker', 'start']:
            self.calls.append(args)
            next(item for item in self.live.values() if item['Id'] == args[2])['State']['Running'] = True
            return ''
        if args[0] == 'curl':
            self.calls.append(args)
            return '200'
        result = super().command(args, data)
        if 'up' in args:
            self.recreations += 1
            for service in u.APPS:
                self.live[service]['Id'] += '-' + str(self.recreations)
        return result


class UpdateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='mx-update-fixture-')
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        for name in ('auth', 'manager-volume', 'bps-state', '.mx-updates'):
            (self.root / name).mkdir()
        for name in ('compose.yaml', 'compose.extra.yaml', 'config.yaml', 'auth/fixture.json',
                     'manager-volume/usage.sqlite', 'manager-volume/usage.sqlite-wal', 'manager-volume/data.key'):
            (self.root / name).write_text('fixture', encoding='utf-8')
        self.site = {'schema': 1, 'root': str(self.root), 'project': 'fixture',
            'base_compose_files': [str(self.root / 'compose.yaml'), str(self.root / 'compose.extra.yaml')],
            'active_release_pointer': str(self.root / '.mx-active-release.json'), 'cpa_config': str(self.root / 'config.yaml'),
            'kilo_state': {'container': '/root/kilo-state', 'host': str(self.root / 'kilo-state')},
            'lifecycle': {'network': 'fixture_default'}, 'release_source': 'https://example.invalid/repo/releases/download',
            'ingress': {'service': 'caddy', 'restore_ingress': True, 'health_url': 'https://example.invalid/', 'expected_status': 200}}
        spec = dict(self.site, state_dir=str(self.root / '.mx-updates' / 'round-1'),
                    compose_files=self.site['base_compose_files'], candidates={service: {'role': role,
                    'ref': 'fixture:' + role, 'revision': COMMIT, 'accepted_ids': ['sha256:' + str(index) * 64]}
                    for index, (service, role) in enumerate(c.ROLES.items(), 1)})
        self.b = UpdateDocker(spec, self.root)
        u.write(self.root / 'config.yaml', self.b.cfg)
        self.site_path = self.root / 'site.json'
        u.write(self.site_path, self.site)
        self.confirm = mock.Mock()
        self.counter = 0

    def run_cycle(self, **kwargs):
        self.counter += 1
        state = self.root / '.mx-updates' / ('round-' + str(self.counter))
        state.mkdir()
        self.state = state
        folder = state / 'materials'
        release = materials(folder, self.b.images)
        def factory(spec, backend):
            self.b.spec = spec
            return k.Release(spec, backend)
        library = types.SimpleNamespace(backend=lambda spec: self.b, Release=factory, read=k.read,
                                        scope_keys=k.scope_keys, digest=k.digest)
        loader = lambda name, path: library if name == 'mx_update_kilo' else c
        with mock.patch.object(k, '__file__', str(folder / 'kilo-release.py')), mock.patch.object(k.time, 'sleep'):
            return u.run_cycle(self.site_path, self.site, 'v1.2.3', state, confirm=self.confirm,
                fetcher=lambda *args: (folder, release), loader=loader, **kwargs)

    def test_success_once_confirmation_and_only_original_ingress(self):
        original = self.b.live['caddy']['Id']
        pointer = self.run_cycle()
        self.confirm.assert_called_once()
        self.assertEqual([args for args in self.b.calls if args[:2] == ['docker', 'start']], [['docker', 'start', original]])
        self.assertEqual(u.read(self.site['active_release_pointer']), pointer)
        self.assertTrue(all(item['State']['Running'] for item in self.b.live.values()))
        self.assertEqual(u.active_updater(self.site), pathlib.Path(pointer['updater']).resolve())
        self.assertEqual(u.managed_command(self.site, ['ps'])[-1], 'ps')
        self.assertEqual((self.state / 'stage').read_text().strip(), 'completed')
        self.assertTrue((self.state / 'backup/data.tar.gz').is_file())

    def test_next_round_discovers_current_ids_and_compose_chain(self):
        first = self.run_cycle()
        ids = {name: self.b.live[name]['Id'] for name in u.APPS}
        second = self.run_cycle()
        spec = u.read(self.state / 'spec.json')
        self.assertEqual({name: spec['baseline'][name]['id'] for name in u.APPS}, ids)
        self.assertEqual(spec['compose_files'], first['compose_files'])
        self.assertEqual(second['compose_files'][:-1], first['compose_files'])
        self.assertEqual(self.confirm.call_count, 2)

    def test_preflight_matrix_failure_never_stops(self):
        self.b.probe_fault = lambda evidence: evidence['observations'].pop()
        with self.assertRaises(c.Failure):
            self.run_cycle()
        self.confirm.assert_not_called()
        self.assertFalse(any('stop' in call or 'up' in call or 'start' in call for call in self.b.calls))
        self.assertFalse((self.state / 'maintenance').exists())

    def test_preflight_space_failure_never_stops(self):
        with self.assertRaises(OSError):
            self.run_cycle(space_check=mock.Mock(side_effect=OSError('fixture-full')))
        self.assertFalse(any('stop' in call or 'up' in call for call in self.b.calls))

    def test_cancel_confirmation_never_stops(self):
        self.confirm.side_effect = u.Failure('synthetic-cancelled')
        with self.assertRaises(u.Failure):
            self.run_cycle()
        self.assertFalse(any('stop' in call or 'up' in call for call in self.b.calls))

    def test_material_drift_before_apply_never_stops(self):
        def change(summary):
            (self.state / 'materials' / 'capability_probe.py').write_text('fixture-drift', encoding='utf-8')
        self.confirm.side_effect = change
        with self.assertRaises(k.Failure):
            self.run_cycle()
        self.assertFalse(any('stop' in call or 'up' in call for call in self.b.calls))

    def test_pointer_parent_preflight_never_stops(self):
        self.site['active_release_pointer'] = str(self.root / 'missing' / 'pointer.json')
        with self.assertRaises(u.Failure):
            self.run_cycle()
        self.assertFalse(any('stop' in call for call in self.b.calls))

    def test_interrupt_during_start_halts_all_without_rollback(self):
        with mock.patch.object(k.Release, 'lifecycle', side_effect=KeyboardInterrupt), self.assertRaises(KeyboardInterrupt):
            self.run_cycle()
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))
        self.assertFalse(pathlib.Path(self.site['active_release_pointer']).exists())
        self.assertFalse(any('compose.rollback.yaml' in ' '.join(call) for call in self.b.calls))
        self.assertTrue(u.read(self.state / 'update-failure.json')['halt_confirmed'])

    def test_apply_backup_failure_halts_and_preserves_current_config(self):
        before = (self.root / 'config.yaml').read_bytes()
        with mock.patch.object(k.Release, 'backup', side_effect=OSError('fixture-backup')), self.assertRaises(OSError):
            self.run_cycle()
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))
        self.assertEqual((self.root / 'config.yaml').read_bytes(), before)
        self.assertFalse(any('up' in call or 'start' in call for call in self.b.calls))

    def test_start_failure_does_not_open_ingress(self):
        original = self.b.command
        def command(args, data=None):
            if 'up' in args:
                raise OSError('fixture-recreate')
            return original(args, data)
        with mock.patch.object(self.b, 'command', side_effect=command), self.assertRaises(OSError):
            self.run_cycle()
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))
        self.assertFalse(any('start' in call for call in self.b.calls))

    def test_ingress_health_failure_halts_without_publishing(self):
        with self.assertRaises(OSError):
            self.run_cycle(check_ingress=mock.Mock(side_effect=OSError('fixture-ingress-health')))
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))
        self.assertFalse(pathlib.Path(self.site['active_release_pointer']).exists())
        self.assertTrue((self.state / 'backup/data.tar.gz').is_file())

    def test_pointer_publication_failure_halts_without_new_pointer(self):
        original = u.write
        def write(path, value):
            if str(path) == self.site['active_release_pointer']:
                raise OSError('fixture-publish')
            original(path, value)
        with mock.patch.object(u, 'write', side_effect=write), self.assertRaises(OSError):
            self.run_cycle()
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))
        self.assertEqual((self.state / 'stage').read_text().strip(), 'update_failed')

    def test_first_round_supports_prefix_chains_and_rejects_conflicts(self):
        facts = self.b.facts(self.b.live)
        facts['oauth-login']['compose_files'] = facts['oauth-login']['compose_files'][:1]
        self.assertEqual(u.current_chain(self.site, facts), self.site['base_compose_files'])
        facts['oauth-login']['compose_files'] = list(reversed(self.site['base_compose_files']))
        with self.assertRaises(u.Failure):
            u.current_chain(self.site, facts)

    def test_active_updater_rejects_drift(self):
        pointer = self.run_cycle()
        pathlib.Path(pointer['updater']).write_text('drift', encoding='utf-8')
        with self.assertRaises(u.Failure):
            u.active_updater(self.site)

    def test_complete_does_not_start_changed_caddy(self):
        original = k.Release.complete_update
        def complete(release, ingress_check):
            self.b.live['caddy']['Id'] = 'different-caddy'
            return original(release, ingress_check)
        with mock.patch.object(k.Release, 'complete_update', complete), self.assertRaises(k.Failure):
            self.run_cycle()
        self.assertFalse(any('start' in call for call in self.b.calls))


class PortProjectionDocker(UpdateDocker):
    def __init__(self, spec, root):
        super().__init__(spec, root)
        with mock.patch.dict(sys.modules, {'fcntl': types.ModuleType('fcntl')}):
            self.phase2 = load('mx_real_phase2_facts', DEPLOY / 'phase2-release.py')
        self.bindings = {'80/tcp': [{'HostIp': '', 'HostPort': '80'}],
                         '443/tcp': [{'HostIp': '', 'HostPort': '443'}]}
        self.running_ports = {'80/tcp': [{'HostIp': '0.0.0.0', 'HostPort': '80'}, {'HostIp': '::', 'HostPort': '80'}],
                              '443/tcp': [{'HostIp': '0.0.0.0', 'HostPort': '443'}, {'HostIp': '::', 'HostPort': '443'}],
                              '443/udp': None, '2019/tcp': None}
        self.restored_ports = copy.deepcopy(self.running_ports)
        self.live['caddy']['HostConfig']['PortBindings'] = copy.deepcopy(self.bindings)
        self.live['caddy']['HostConfig']['RestartPolicy'] = {'Name': 'unless-stopped', 'MaximumRetryCount': 0}
        self.live['caddy']['NetworkSettings']['Ports'] = copy.deepcopy(self.running_ports)
        self.base['services']['caddy']['ports'] = [
            {'target': 80, 'published': '80', 'protocol': 'tcp'},
            {'target': 443, 'published': '443', 'protocol': 'tcp'}]

    def facts(self, live):
        return self.phase2.facts(live)

    def command(self, args, data=None):
        result = super().command(args, data)
        if 'stop' in args and args[-1] == 'caddy':
            self.live['caddy']['NetworkSettings']['Ports'] = {}
        if args[:2] == ['docker', 'start']:
            self.live['caddy']['NetworkSettings']['Ports'] = copy.deepcopy(self.restored_ports)
        return result


class CaddyPortProjectionTests(unittest.TestCase):
    run_cycle = UpdateTests.run_cycle

    def setUp(self):
        UpdateTests.setUp(self)
        self.b = PortProjectionDocker(self.b.spec, self.root)

    def test_stopped_projection_changes_and_full_running_facts_restore(self):
        ingress = mock.Mock()
        original = k.Release.complete_update
        stopped = []
        def complete(release, ingress_check):
            fact = self.b.facts(self.b.snapshot())['caddy']
            stopped.append(fact)
            self.assertEqual(fact['ports'], {})
            self.assertEqual(fact['host_config']['PortBindings'], self.b.bindings)
            return original(release, ingress_check)
        with mock.patch.object(k.Release, 'complete_update', complete):
            self.run_cycle(check_ingress=ingress)
        self.assertEqual(len(stopped), 1)
        ingress.assert_called_once()
        plan = u.read(self.state / 'plan.json')
        self.assertEqual(self.b.facts(self.b.snapshot())['caddy'], plan['facts']['caddy'])
        self.assertEqual([args for args in self.b.calls if args[:2] == ['docker', 'start']],
                         [['docker', 'start', plan['facts']['caddy']['id']]])
        self.assertEqual((self.state / 'stage').read_text().strip(), 'completed')

    def test_stable_port_binding_change_rejected_before_start(self):
        original = k.Release.complete_update
        def complete(release, ingress_check):
            self.b.live['caddy']['HostConfig']['PortBindings']['443/tcp'][0]['HostPort'] = '8443'
            return original(release, ingress_check)
        with mock.patch.object(k.Release, 'complete_update', complete), self.assertRaisesRegex(k.Failure, '稳定配置漂移'):
            self.run_cycle()
        self.assertFalse(any(args[:2] == ['docker', 'start'] for args in self.b.calls))
        self.assertEqual(u.read(self.state / 'update-failure.json')['phase'], 'complete')
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))

    def test_wrong_running_port_projection_rejected_after_start(self):
        self.b.restored_ports['443/tcp'][0]['HostPort'] = '8443'
        ingress = mock.Mock()
        with self.assertRaisesRegex(k.Failure, '恢复状态不符'):
            self.run_cycle(check_ingress=ingress)
        ingress.assert_not_called()
        self.assertEqual(len([args for args in self.b.calls if args[:2] == ['docker', 'start']]), 1)
        self.assertEqual(u.read(self.state / 'update-failure.json')['phase'], 'complete')
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))
        self.assertFalse(pathlib.Path(self.site['active_release_pointer']).exists())

    def test_original_container_id_change_rejected_before_start(self):
        original = k.Release.complete_update
        def complete(release, ingress_check):
            self.b.live['caddy']['Id'] = 'different-caddy'
            return original(release, ingress_check)
        with mock.patch.object(k.Release, 'complete_update', complete), self.assertRaisesRegex(k.Failure, 'Caddy容器身份漂移'):
            self.run_cycle()
        self.assertFalse(any(args[:2] == ['docker', 'start'] for args in self.b.calls))
        self.assertTrue(all(not item['State']['Running'] for item in self.b.live.values()))


class PolicyTests(unittest.TestCase):
    def setUp(self):
        self.cfg = {'config-version': 8, 'access': {'api-keys': ['k', 'c', 'b', 'l'], 'client-key-scopes': [
            {'api-key': key, 'channel': channel} for key, channel in zip(['k', 'c', 'b'], c.CHANNELS)]}}
        count, rows = c.policy_expectations(self.cfg)
        self.policy = {'schema': 1, 'kind': 'authz-policy-matrix', 'validator': c.VALIDATOR,
            'source_commit': COMMIT, 'config_sha256': '2' * 64, 'key_count': count, 'observations': rows}

    def test_exact_observation_set_accepts(self):
        c.validate_policy(self.policy, self.cfg, '2' * 64, COMMIT)
        self.assertEqual(len(self.policy['observations']), 22)

    def test_missing_duplicate_or_wrong_observation_rejected(self):
        for action in (lambda x: x.pop(), lambda x: x.__setitem__(1, x[0]),
                       lambda x: x[2].update(allowed=True), lambda x: x[0].update(authenticated=1)):
            rows = copy.deepcopy(self.policy)
            action(rows['observations'])
            with self.subTest(action=action), self.assertRaises(c.Failure):
                c.validate_policy(rows, self.cfg, '2' * 64, COMMIT)

    def test_config_and_revision_binding_rejected(self):
        for sha, commit in [('3' * 64, COMMIT), ('2' * 64, '2' * 40)]:
            with self.assertRaises(c.Failure):
                c.validate_policy(self.policy, self.cfg, sha, commit)

    def test_policy_alone_never_satisfies_wiring(self):
        contract, evidence = fixture_contract()
        c.validate_wiring_contract(contract, evidence)
        contract['incremental_review']['status'] = 'PENDING'
        with self.assertRaises(c.Failure):
            c.validate_wiring_contract(contract, evidence)

    def test_inherited_missing_skip_or_failure_rejected(self):
        for value in ('', '--- SKIP: TestFixture (0.01s)\nPASS\n', '--- FAIL: TestFixture (0.01s)\nFAIL\n'):
            contract, evidence = fixture_contract()
            evidence['runs']['bps-native']['stdout'] = value
            with self.assertRaises(c.Failure):
                c.validate_wiring_contract(contract, evidence)

    def test_json_log_decodes_observation_output(self):
        run = {'exit_code': 0, 'inherited': True, 'command': 'fixture', 'source': 'fixture', 'input_revision': 'fixture',
               'stdout': '{"Action":"output","Test":"TestFixture","Output":"Core -\\u003e Host POST0"}\n'
                         '{"Action":"pass","Test":"TestFixture"}\n'}
        c.validate_test_run(run, {'format': 'go-json', 'tests': ['TestFixture'], 'output_markers': ['Core -> Host POST0']})

    def test_source_change_and_new_source_rejected(self):
        contract, _ = fixture_contract()
        valid = '100644 blob ' + 'a' * 40 + '\tfixture/main.go\n'
        c.validate_source_mapping('.', contract, COMMIT, lambda _: valid)
        for changed in [valid.replace('a' * 40, 'b' * 40), valid + '100644 blob ' + 'a' * 40 + '\tfixture/new.go\n']:
            with self.assertRaises(c.Failure):
                c.validate_source_mapping('.', contract, COMMIT, lambda _: changed)

    def test_docker_probe_isolated_and_only_candidate_mount(self):
        command = c.probe_command('sha256:' + '1' * 64, pathlib.Path(__file__))
        self.assertEqual(command[command.index('--network') + 1], 'none')
        self.assertEqual(command.count('--mount'), 1)
        self.assertIn('--read-only', command)
        self.assertIn('--capability-probe', command)
        self.assertTrue(command[command.index('--mount') + 1].endswith(',readonly'))


class ProbeProcessTests(unittest.TestCase):
    def test_candidate_owner_is_probe_process_user(self):
        for uid, gid in ((0, 0), (1001, 1001)):
            with self.subTest(uid=uid), mock.patch.object(pathlib.Path, 'stat', return_value=types.SimpleNamespace(st_uid=uid, st_gid=gid)):
                command = c.probe_command('synthetic-image', pathlib.Path(__file__))
            self.assertEqual(command[command.index('--user') + 1], f'{uid}:{gid}')
            self.assertEqual(command[command.index('--cap-drop') + 1], 'ALL')
            self.assertEqual(command[command.index('--network') + 1], 'none')
            self.assertIn('--read-only', command)
            self.assertEqual(command.count('--mount'), 1)
            self.assertTrue(command[command.index('--mount') + 1].endswith(',readonly'))

    def test_ci_process_summary_only_emits_bounded_codes(self):
        smoke = load('mx_ci_process_diagnostic', DEPLOY / 'test_capability_image.py')
        result = types.SimpleNamespace(returncode=1, stdout='synthetic-private-value',
                                       stderr='synthetic-private-value\ncapability_probe_config_unreadable\n')
        summary = smoke.process_summary(result)
        self.assertIn('exit=1', summary)
        self.assertIn('code=capability_probe_config_unreadable', summary)
        self.assertNotIn('synthetic-private-value', summary)
        result.stderr = 'synthetic-private-value'
        self.assertIn('code=unclassified_process_failure', smoke.process_summary(result))
        with mock.patch.object(smoke.subprocess, 'run', return_value=result):
            with self.assertRaises(smoke.probe.Failure) as failure:
                smoke.verify_image('synthetic-image', COMMIT, pathlib.Path('unused-evidence.json'))
        self.assertIn('code=unclassified_process_failure', str(failure.exception))
        self.assertNotIn('synthetic-private-value', str(failure.exception))


class DownloadTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='mx-download-fixture-')
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.data = {name: b'synthetic-material' for name in u.ASSETS}
        images = {role: {'ref': u.IMAGE_REPOSITORIES[service] + ':v1.2.3', 'revision': COMMIT,
                  'os': 'linux', 'architecture': 'amd64', 'accepted_ids': ['sha256:' + str(index) * 64]}
                  for index, (service, role) in enumerate(u.ROLES.items(), 1)}
        self.data['mx-cpamp-images.manifest.json'] = json.dumps({'schema': 1,
             'archive': {'file': 'mx-cpamp-images-linux-amd64.tar.gz',
                         'sha256': c.hashlib.sha256(self.data['mx-cpamp-images-linux-amd64.tar.gz']).hexdigest()},
             'images': images}).encode()
        self.release = {'schema': 1, 'version': 'v1.2.3', 'commit': COMMIT, 'platform': 'linux/amd64',
            'assets': {name: {'size': len(raw), 'sha256': c.hashlib.sha256(raw).hexdigest()} for name, raw in self.data.items()},
            'candidates': {service: dict(role=role, **{key: images[role][key] for key in ('ref', 'revision', 'accepted_ids')})
                           for service, role in u.ROLES.items()}}

    def fetch(self, state):
        self.data['release.json'] = json.dumps(self.release).encode()
        def download(args, **kwargs):
            self.assertIn('--proto-redir', args)
            self.assertTrue(args[-1].startswith('https://example.invalid/repo/releases/download/v1.2.3/'))
            pathlib.Path(args[args.index('--output') + 1]).write_bytes(self.data[args[-1].rsplit('/', 1)[-1]])
        with mock.patch.object(u.subprocess, 'run', side_effect=download):
            return u.fetch_release({'release_source': 'https://example.invalid/repo/releases/download'}, 'v1.2.3', state)

    def test_fixed_release_assets_and_image_bindings(self):
        folder, release = self.fetch(self.root)
        self.assertEqual(set(path.name for path in folder.iterdir()), u.ASSETS | {'release.json'})
        self.assertEqual(release['commit'], COMMIT)

    def test_missing_asset_or_wrong_digest_rejected(self):
        original = copy.deepcopy(self.release)
        for index, change in enumerate(('missing', 'digest')):
            self.release = copy.deepcopy(original)
            if change == 'missing':
                self.release['assets'].pop('production-wiring.json')
            else:
                self.release['assets']['mx-update.py']['sha256'] = '0' * 64
            state = self.root / str(index)
            state.mkdir()
            with self.assertRaises(u.Failure):
                self.fetch(state)


if __name__ == '__main__':
    unittest.main(verbosity=2)
