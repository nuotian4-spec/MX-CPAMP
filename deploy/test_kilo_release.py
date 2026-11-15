"""仅使用临时合成目录和内存Docker替身，不连接Docker或目标主机。"""
import copy
import contextlib
import gzip
import importlib.util
import io
import json
import pathlib
import posixpath
import sys
import tarfile
import tempfile
import types
import unittest
from unittest import mock

loader = importlib.util.spec_from_file_location('kilo_release', pathlib.Path(__file__).with_name('kilo-release.py'))
k = importlib.util.module_from_spec(loader)
loader.loader.exec_module(k)


class FakeDocker:
    def __init__(self, spec, root):
        self.spec, self.root, self.calls = spec, root, []
        self.images = {s: item['accepted_ids'][0] for s, item in spec['candidates'].items()}
        self.cfg = {'config-version': 8, 'oauth': {'auth-dir': '/root/.cli-proxy-api'}, 'access': {'api-keys': ['fixture-legacy'], 'client-key-scopes': []}}
        self.live = {}
        for service in [*spec['candidates'], 'caddy']:
            mounts = []
            if service == 'cli-proxy-api':
                mounts = [{'Type': 'bind', 'Source': str(root / 'auth'), 'Destination': '/root/.cli-proxy-api', 'RW': True}, {'Type': 'bind', 'Source': spec['cpa_config'], 'Destination': '/CLIProxyAPI/config.yaml', 'RW': True}, {'Type': 'bind', 'Source': str(root / 'bps-state'), 'Destination': '/root/bps-state', 'RW': True}]
            if service == 'cpa-manager-plus':
                mounts = [{'Type': 'volume', 'Source': str(root / 'manager-volume'), 'Destination': '/data', 'RW': True}]
            self.live[service] = {'Id': service + '-container', 'Image': service + '-old-id', 'Config': {'Image': service + '-old-ref', 'WorkingDir': '/CLIProxyAPI', 'Env': ['HOME=/root']}, 'Args': [], 'Mounts': mounts, 'State': {'Running': True}}
        self.base = {'services': {s: {'image': c['Config']['Image'], 'volumes': [{'type': m['Type'], 'source': m['Source'], 'target': m['Destination']} for m in c['Mounts']]} for s, c in self.live.items()}}
        self.existing_paths = set()
        self.probes = []
        self.documents = {}

    @staticmethod
    def resolve(path, cwd):
        return posixpath.normpath(path if path.startswith('/') else posixpath.join(cwd, path))

    def snapshot(self): return copy.deepcopy(self.live)

    def facts(self, live):
        return {s: {'id': c['Id'], 'image_id': c['Image'], 'image_ref': c['Config']['Image'], 'working_dir': (c['Config'].get('Labels') or {}).get('com.docker.compose.project.working_dir', self.spec['root']), 'compose_files': (c['Config'].get('Labels') or {}).get('com.docker.compose.project.config_files', ','.join(self.spec['compose_files'])).split(','), 'mounts': c['Mounts']} for s, c in live.items()}

    def yaml_document(self, path):
        return copy.deepcopy(self.cfg if str(path) == self.spec['cpa_config'] else self.documents.get(str(path), {'services': {}}))

    def compose_matches_live(self, base, live):
        k.require(base == self.base, '合成Compose与基线不符')

    def load_bundle(self, source):
        self.calls.append(['合成加载包'] + source)
        return self.images.copy()

    def image_records(self): return self.images.copy()

    def command(self, args, data=None):
        self.calls.append(args)
        if args[1] == 'exec':
            script = args[5]
            if script == 'test ! -e "$1" && printf absent':
                path = args[7]
            elif script in ['test ! -e /usr/local/bin/config.json && printf absent', 'test ! -e /run/secrets/cpa_data_key && printf absent']:
                path = script.split()[3]
            else:
                raise AssertionError('意外exec脚本：' + repr(args))
            self.probes.append((args[2], path))
            return '' if (args[2], path) in self.existing_paths else 'absent'
        if args[1] == 'inspect':
            return json.dumps([next(c for c in self.live.values() if c['Id'] == args[2])])
        if 'stop' in args:
            self.live[args[-1]]['State']['Running'] = False
            return ''
        if 'config' in args:
            result = copy.deepcopy(self.base)
            if str(pathlib.Path(self.spec['state_dir']) / 'compose.kilo.yaml') in args:
                override = k.read(pathlib.Path(self.spec['state_dir']) / 'compose.kilo.yaml')
                for service, changes in override['services'].items():
                    volumes = changes.pop('volumes', [])
                    result['services'][service].update(changes)
                    result['services'][service]['volumes'] += volumes
            return json.dumps(result)
        raise AssertionError('意外命令：' + repr(args))


class KiloReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='kilo-release-fixture-')
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        for name in ['auth', 'manager-volume', 'release-state', 'bps-state']:
            (self.root / name).mkdir()
        for name in ['compose.yaml', 'compose.extra.yaml', 'config.yaml', 'auth/kilo-fixture.json', 'manager-volume/usage.sqlite', 'manager-volume/usage.sqlite-wal', 'manager-volume/data.key']:
            (self.root / name).write_text('合成数据', encoding='utf-8')
        self.spec = {'schema': 1, 'input_id': 'fixture-only', 'root': str(self.root), 'project': 'fixture', 'state_dir': str(self.root / 'release-state'), 'compose_files': [str(self.root / 'compose.yaml'), str(self.root / 'compose.extra.yaml')], 'cpa_config': str(self.root / 'config.yaml'), 'kilo_state': {'container': '/root/kilo-state', 'host': str(self.root / 'kilo-state')}, 'candidates': {s: {'role': role, 'ref': s + ':fixture', 'revision': 'fixture-input', 'accepted_ids': ['sha256:' + str(i) * 64]} for i, (s, role) in enumerate([('cli-proxy-api', 'cpa'), ('cpa-manager-plus', 'manager'), ('oauth-login', 'worker')], 1)}, 'baseline': {'placeholder': {}}}
        self.b = FakeDocker(self.spec, self.root)
        self.spec['baseline'] = self.b.facts(self.b.live)
        self.release = k.Release(self.spec, self.b)

    def prepare(self): self.release.prepare(self.root / 'synthetic-bundle')

    def test_state_path_default_explicit_and_auth_rejection(self):
        self.assertEqual(k.state_directory({'oauth': {'auth-dir': '~/auth'}}), ('/root/auth', '/root/kilo-state'))
        self.assertEqual(k.state_directory({'kilo': {'state-dir': '../state'}})[1], '/state')
        with self.assertRaises(k.Failure): k.state_directory({'kilo': {'state-dir': '/root/.cli-proxy-api/state'}})

    def test_parameters_and_complete_compose_chain(self):
        args = k.compose(self.spec, '/fixture/last.yaml')
        self.assertEqual([args[i + 1] for i, value in enumerate(args) if value == '-f'], self.spec['compose_files'] + ['/fixture/last.yaml'])
        bad = copy.deepcopy(self.spec); bad['compose_files'] = []
        with self.assertRaises(k.Failure): k.validate_spec(bad)

    def test_service_baseline_accepts_six_six_four_history(self):
        files = [str(self.root / ('compose-%d.yaml' % index)) for index in range(6)]
        self.spec['compose_files'] = files
        for service, count in [('cli-proxy-api', 6), ('cpa-manager-plus', 6), ('oauth-login', 4), ('caddy', 4)]:
            self.b.live[service]['Config']['Labels'] = {'com.docker.compose.project.working_dir': self.spec['root'], 'com.docker.compose.project.config_files': ','.join(files[:count])}
        self.spec['baseline'] = self.b.facts(self.b.live)
        facts = self.release.check_baseline(self.b.snapshot())
        self.assertEqual({s: len(facts[s]['compose_files']) for s in k.APPS}, {'cli-proxy-api': 6, 'cpa-manager-plus': 6, 'oauth-login': 4})
        for field, value in [('com.docker.compose.project.working_dir', '/other-project'), ('com.docker.compose.project.config_files', ','.join(files[:3]))]:
            with self.subTest(field=field):
                live = self.b.snapshot()
                live['oauth-login']['Config']['Labels'][field] = value
                with self.assertRaises(k.Failure): self.release.check_baseline(live)
        changed = self.b.snapshot()
        changed['oauth-login']['Id'] = 'unregistered-worker'
        with self.assertRaises(k.Failure): self.release.check_baseline(changed)

    def test_prepare_mount_and_backup_coverage(self):
        self.prepare()
        plan = k.read(self.release.state / 'plan.json')
        self.assertIn(self.spec['kilo_state']['host'], plan['sources']['paths'])
        self.assertIn(str(self.root / 'manager-volume'), plan['sources']['paths'])
        override = k.read(self.release.state / 'compose.kilo.yaml')
        self.assertEqual(override['services']['cli-proxy-api']['volumes'][0]['target'], '/root/kilo-state')
        self.assertEqual(k.read(self.release.state / 'cpa-config.yaml')['upstream']['kilo']['state-dir'], '/root/kilo-state')
        self.assertFalse(any('stop' in c or 'up' in c for c in self.b.calls))

    def test_unmounted_existing_state_blocks_prepare(self):
        self.b.existing_paths.add(('cli-proxy-api-container', '/root/kilo-state'))
        with self.assertRaisesRegex(k.Failure, '^未挂载Kilo状态已存在，先保全$'): self.prepare()
        self.assertIn(('cli-proxy-api-container', '/CLIProxyAPI/.env'), self.b.probes)
        self.assertIn(('cli-proxy-api-container', '/root/kilo-state'), self.b.probes)
        self.assertFalse(any('合成加载包' in c for c in self.b.calls))
        self.assertFalse(pathlib.Path(self.spec['kilo_state']['host']).exists())

    def test_unmounted_absent_state_prepares_without_creating_host_state(self):
        self.prepare()
        self.assertIn(('cli-proxy-api-container', '/root/kilo-state'), self.b.probes)
        self.assertTrue(k.read(self.release.state / 'plan.json')['sources']['new_state'])
        self.assertFalse(pathlib.Path(self.spec['kilo_state']['host']).exists())

    def test_cpa_env_gate_is_distinct_from_unmounted_state_gate(self):
        self.b.existing_paths.add(('cli-proxy-api-container', '/CLIProxyAPI/.env'))
        with self.assertRaisesRegex(k.Failure, '^CPA容器内额外env来源待核$'): self.prepare()
        self.assertNotIn(('cli-proxy-api-container', '/root/kilo-state'), self.b.probes)

    def test_apply_backup_preserves_full_data_and_stays_stopped(self):
        self.prepare()
        result = self.release.apply(lambda: None)
        self.assertIn('installed_stopped', result)
        with tarfile.open(self.release.state / 'backup/data.tar.gz') as archive:
            names = archive.getnames()
        for suffix in ['usage.sqlite', 'usage.sqlite-wal', 'data.key', 'kilo-fixture.json', 'kilo-state']:
            self.assertTrue(any(n.endswith(suffix) for n in names), suffix)
        self.assertTrue(self.b.live['caddy']['State']['Running'])
        self.assertTrue(all(not self.b.live[s]['State']['Running'] for s in self.spec['candidates']))
        self.assertFalse(any('up' in c or 'start' in c for c in self.b.calls))

    def test_backup_failure_never_starts_and_blocks_retry(self):
        self.prepare()
        def fail(plan): raise k.Failure('合成备份失败')
        self.release.backup = fail
        with self.assertRaises(k.Failure): self.release.apply(lambda: None)
        with self.assertRaises(k.Failure): self.release.apply(lambda: None)
        self.assertTrue(all(not self.b.live[s]['State']['Running'] for s in self.spec['candidates']))
        self.assertFalse(any('up' in c or 'start' in c for c in self.b.calls))

    def test_protected_source_drift_blocks_before_stop(self):
        self.prepare(); (self.root / 'compose.extra.yaml').write_text('合成漂移', encoding='utf-8')
        with self.assertRaises(k.Failure): self.release.apply(lambda: None)
        self.assertFalse(any('stop' in c for c in self.b.calls))

    def test_existing_mapped_state_is_preserved_and_covered(self):
        host = pathlib.Path(self.spec['kilo_state']['host']); (host / 'bindings').mkdir(parents=True)
        (host / 'bindings/fixture.json').write_text('合成绑定', encoding='utf-8')
        (host / 'kilo-proxy-state.json').write_text('合成回收站', encoding='utf-8')
        self.b.live['cli-proxy-api']['Mounts'].append({'Type': 'bind', 'Source': str(host), 'Destination': '/root/kilo-state', 'RW': True})
        self.b.base['services']['cli-proxy-api']['volumes'].append({'type': 'bind', 'source': str(host), 'target': '/root/kilo-state'})
        self.spec['baseline'] = self.b.facts(self.b.live)
        self.prepare(); self.release.apply(lambda: None)
        with tarfile.open(self.release.state / 'backup/data.tar.gz') as archive:
            names = archive.getnames()
        self.assertTrue(any(n.endswith('bindings/fixture.json') for n in names))
        self.assertTrue(any(n.endswith('kilo-proxy-state.json') for n in names))
        self.assertEqual((host / 'bindings/fixture.json').read_text(encoding='utf-8'), '合成绑定')

    def test_manager_unmapped_database_blocks_prepare(self):
        self.b.live['cpa-manager-plus']['Config']['Env'].append('USAGE_DB_PATH=/unmounted/usage.sqlite')
        with self.assertRaises(k.Failure): self.prepare()
        self.assertFalse(any('合成加载包' in c for c in self.b.calls))

    def test_override_relative_env_file_uses_project_directory(self):
        nested = self.root / 'overrides'; nested.mkdir()
        override = nested / 'service.yaml'; override.write_text('合成覆盖文件', encoding='utf-8')
        env = self.root / 'service.env'; env.write_text('FIXTURE=project-root\n', encoding='utf-8')
        wrong = nested / 'service.env'; wrong.write_text('FIXTURE=wrong-directory\n', encoding='utf-8')
        self.spec['compose_files'].append(str(override))
        self.spec['baseline'] = self.b.facts(self.b.live)
        self.b.documents[str(override)] = {'services': {'cpa-manager-plus': {'env_file': ['service.env']}}}
        self.prepare()
        paths = k.read(self.release.state / 'plan.json')['sources']['paths']
        self.assertIn(str(env), paths)
        self.assertNotIn(str(wrong), paths)
        self.release.apply(lambda: None)
        with tarfile.open(self.release.state / 'backup/data.tar.gz') as archive:
            self.assertEqual(archive.extractfile(k.archive_name(env)).read(), env.read_bytes())
            self.assertNotIn(k.archive_name(wrong), archive.getnames())

    def test_manager_single_file_database_mount_rejected(self):
        external = self.root / 'external-db'; (external / 'usage-archives').mkdir(parents=True)
        (external / 'usage.sqlite').write_text('合成数据库', encoding='utf-8')
        manager = self.b.live['cpa-manager-plus']
        manager['Config']['Env'].append('USAGE_DB_PATH=/external-db/usage.sqlite')
        manager['Mounts'] += [{'Type': 'bind', 'Source': str(external / 'usage.sqlite'), 'Destination': '/external-db/usage.sqlite', 'RW': True}, {'Type': 'bind', 'Source': str(external / 'usage-archives'), 'Destination': '/external-db/usage-archives', 'RW': True}]
        self.spec['baseline'] = self.b.facts(self.b.live)
        with self.assertRaisesRegex(k.Failure, '^Manager数据库须整目录持久挂载，拒绝单文件DB映射$'): self.prepare()
        self.assertFalse(any('合成加载包' in c or 'stop' in c for c in self.b.calls))

    def test_manager_external_database_directory_preserves_wal_shm_and_key(self):
        external = self.root / 'external-db'; (external / 'usage-archives').mkdir(parents=True)
        for name in ['usage.sqlite', 'usage.sqlite-wal', 'usage.sqlite-shm', 'data.key']:
            (external / name).write_text('合成外置数据:' + name, encoding='utf-8')
        manager = self.b.live['cpa-manager-plus']
        manager['Config']['Env'] += ['USAGE_DB_PATH=/external-db/usage.sqlite', 'CPA_MANAGER_DATA_KEY_PATH=/external-db/data.key']
        manager['Mounts'].append({'Type': 'bind', 'Source': str(external), 'Destination': '/external-db', 'RW': True})
        self.b.base['services']['cpa-manager-plus']['volumes'].append({'type': 'bind', 'source': str(external), 'target': '/external-db'})
        self.spec['baseline'] = self.b.facts(self.b.live)
        self.prepare(); self.release.apply(lambda: None)
        with tarfile.open(self.release.state / 'backup/data.tar.gz') as archive:
            for name in ['usage.sqlite', 'usage.sqlite-wal', 'usage.sqlite-shm', 'data.key']:
                self.assertEqual(archive.extractfile(k.archive_name(external / name)).read().decode('utf-8'), '合成外置数据:' + name)

    def test_legacy_manifest_consumer_validates_real_fixture(self):
        legacy_path = pathlib.Path(__file__).with_name('phase2-release.py')
        spec = importlib.util.spec_from_file_location('kilo_legacy_fixture', legacy_path)
        legacy = importlib.util.module_from_spec(spec)
        with mock.patch.dict(sys.modules, {'fcntl': types.SimpleNamespace()}): spec.loader.exec_module(legacy)
        legacy.STATE = self.root / 'legacy-state'; legacy.STATE.mkdir()
        legacy.CANDIDATES = self.spec['candidates']
        source = self.root / 'bundle-source'; source.mkdir()
        archive = source / 'mx-cpamp-images-linux-amd64.tar.gz'
        archive.write_bytes(gzip.compress(b'fixture-image-archive'))
        manifest = {'schema': 1, 'archive': {'file': archive.name, 'sha256': k.digest(archive)}, 'images': {item['role']: {**item, 'os': 'linux', 'architecture': 'amd64'} for item in self.spec['candidates'].values()}}
        k.write(source / 'mx-cpamp-images.manifest.json', manifest)
        def inspect(args):
            item = next(item for item in self.spec['candidates'].values() if item['ref'] == args[-1])
            return json.dumps([{'Id': item['accepted_ids'][0], 'Os': 'linux', 'Architecture': 'amd64', 'Config': {'Labels': {'org.opencontainers.image.revision': item['revision']}}}])
        process = mock.Mock(); process.stdin = io.BytesIO(); process.wait.return_value = 0
        with mock.patch.object(legacy, 'command', inspect), mock.patch.object(legacy.subprocess, 'Popen', return_value=process):
            self.assertEqual(legacy.load_bundle(['--bundle-dir', str(source)]), self.b.images)
        legacy.STATE = self.root / 'bad-state'; legacy.STATE.mkdir()
        manifest['archive']['sha256'] = '0' * 64; k.write(source / 'mx-cpamp-images.manifest.json', manifest)
        with mock.patch.object(legacy.subprocess, 'Popen') as forbidden:
            with self.assertRaises(legacy.Failure): legacy.load_bundle(['--bundle-dir', str(source)])
            forbidden.assert_not_called()

    def test_scope_unknown_empty_keys_and_unrevoked_keys_stay_blocked(self):
        evidence = {'client_ingress': 'blocked', 'runtime_evidence': 'fixture-matrix', 'mode': 'revoke-scoped-keys', 'invalid_key_rejected': True, 'channel_matrix_passed': True}
        for keys in [[], ['scoped-key'], ['scoped-key', 'legacy']]:
            with self.assertRaises(k.Failure): k.rollback_scope_gate(evidence, {'access': {'api-keys': keys}}, ['scoped-key'])
        self.assertIn('继续保持入口阻断', k.rollback_scope_gate(evidence, {'access': {'api-keys': ['legacy']}}, ['scoped-key']))
        evidence['mode'] = 'unknown'
        with self.assertRaises(k.Failure): k.rollback_scope_gate(evidence, {}, ['scoped-key'])


class LifecycleDocker(FakeDocker):
    def __init__(self, spec, root):
        super().__init__(spec, root)
        self.network = 'fixture_default'
        self.health_ok = True
        self.extra_endpoint = False
        self.cfg['access'] = {'api-keys': ['fixture-kilo', ' fixture-codex ', 'fixture-bps', 'fixture-legacy'], 'client-key-scopes': [{'api-key': 'fixture-kilo', 'channel': 'kilo'}, {'api-key': 'fixture-codex', 'channel': 'codex'}, {'api-key': 'fixture-bps', 'channel': 'bps'}]}
        self.cfg['unrelated'] = {'keep': [1, '原样']}
        (root / 'admin-key').write_text('fixture-admin-only', encoding='utf-8')
        (root / 'worker-key').write_text('fixture-worker-only', encoding='utf-8')
        for service, container in self.live.items():
            container['HostConfig'] = {'NetworkMode': self.network, 'PortBindings': {}}
            container['NetworkSettings'] = {'Networks': {self.network: {}}, 'Ports': {}}
            container['Config']['Labels'] = {'com.docker.compose.project.config_files': ','.join(spec['compose_files'])}
            if service == 'cpa-manager-plus':
                container['Config']['Env'].append('CPA_MANAGER_ADMIN_KEY_FILE=/run/secrets/admin-key')
                container['Mounts'].append({'Type': 'bind', 'Source': str(root / 'admin-key'), 'Destination': '/run/secrets/admin-key', 'RW': False})
            if service == 'oauth-login':
                container['Config']['Env'].append('OAUTH_LOGIN_SECRET_FILE=/run/secrets/oauth_login_secret')
                container['Mounts'].append({'Type': 'bind', 'Source': str(root / 'worker-key'), 'Destination': '/run/secrets/oauth_login_secret', 'RW': False})
        self.base = self.render_live()

    def render_live(self):
        return {'services': {s: {'image': c['Config']['Image'], 'volumes': [{'type': m['Type'], 'source': m['Source'], 'target': m['Destination'], **({'read_only': True} if not m['RW'] else {})} for m in c['Mounts']]} for s, c in self.live.items()}}

    def yaml_document(self, path):
        if str(path) == self.spec['cpa_config']:
            try: return k.read(path)
            except json.JSONDecodeError: return copy.deepcopy(self.cfg)
        return super().yaml_document(path)

    def compose_matches_live(self, rendered, live):
        for service, candidate in rendered['services'].items():
            k.require(candidate['image'] == live[service]['Config']['Image'], '合成运行镜像漂移')
            expected = sorted([(v['source'], v['target']) for v in candidate.get('volumes', [])])
            k.require(expected == sorted((v['Source'], v['Destination']) for v in live[service]['Mounts']), '合成运行挂载漂移')

    def command(self, args, data=None):
        if args[:3] == ['docker', 'network', 'inspect']:
            self.calls.append(args)
            endpoints = {c['Id']: {} for c in self.live.values() if c['State']['Running']}
            if self.extra_endpoint: endpoints['outside-client'] = {}
            return json.dumps([{'Driver': 'bridge', 'Scope': 'local', 'Labels': {'com.docker.compose.project': self.spec['project']}, 'Containers': endpoints, 'Options': {}}])
        if args[:3] == ['docker', 'image', 'inspect']:
            self.calls.append(args)
            return json.dumps([{'Id': args[-1], 'Os': 'linux', 'Architecture': 'amd64'}])
        if args[:3] == ['docker', 'exec', '-i']:
            self.calls.append(args)
            return 'ok' if self.health_ok else 'failed'
        if len(args) > 1 and args[1] == 'compose' and ('config' in args or 'up' in args):
            self.calls.append(args)
            rendered = copy.deepcopy(self.base)
            files = [args[i + 1] for i, value in enumerate(args) if value == '-f']
            for file in files:
                if pathlib.Path(file).name not in ['compose.kilo.yaml', 'compose.rollback.yaml']: continue
                for service, changes in k.read(file)['services'].items():
                    changes = copy.deepcopy(changes)
                    volumes = changes.pop('volumes', [])
                    rendered['services'][service].update(changes)
                    rendered['services'][service]['volumes'] += volumes
            if 'config' in args: return json.dumps(rendered)
            for service in k.APPS:
                value = rendered['services'][service]
                live = self.live[service]
                if live['Image'] == value['image'] and '--force-recreate' not in args:
                    live['State']['Running'] = True
                    continue
                live['Id'] = service + '-candidate' if 'compose.rollback.yaml' not in [pathlib.Path(f).name for f in files] else service + '-rollback'
                live['Config']['Image'] = value['image']; live['Image'] = value['image']
                live['Config']['Labels']['com.docker.compose.project.config_files'] = ','.join(files)
                live['Mounts'] = [{'Type': v['type'], 'Source': v['source'], 'Destination': v['target'], 'RW': not v.get('read_only', False)} for v in value.get('volumes', [])]
                live['State']['Running'] = True
            return ''
        return super().command(args, data)


class KiloLifecycleTests(unittest.TestCase):
    def setUp(self):
        KiloReleaseTests.setUp(self)
        self.spec['lifecycle'] = {'network': 'fixture_default'}
        self.b = LifecycleDocker(self.spec, self.root)
        self.spec['baseline'] = self.b.facts(self.b.live)
        self.release = k.Release(self.spec, self.b)
        self.plan_path = self.root / 'operator-plan.json'
        k.write(self.plan_path, self.spec)
        self.release.prepare(self.root / 'synthetic-bundle')
        self.release.apply(lambda: None)
        self.new_config = k.read(self.spec['cpa_config'])

    def proof(self, rollback=False, revoke=False, content=None):
        images = {s: self.spec['baseline'][s]['image_id'] for s in k.APPS} if rollback else self.b.images
        cfg = content or self.new_config
        if revoke: cfg, _, _ = k.revoke_scope_keys(cfg)
        path = self.root / ('rollback-proof.json' if rollback else 'start-proof.json')
        config_path = self.root / 'proof-config.json'; k.write(config_path, cfg)
        evidence = self.root / 'fixture-matrix.log'; evidence.write_text('合成隔离矩阵证据，仅测试用', encoding='utf-8')
        k.write(path, {'schema': 1, 'review': 'PASS', 'reviewed_by': 'fixture-reviewer', 'image_ids': images, 'source_config_sha256': k.digest(self.spec['cpa_config']), 'config_sha256': k.digest(config_path), 'mode': 'revoke-scoped-keys' if revoke else 'scope-enforced', 'runtime_evidence': str(evidence), 'runtime_evidence_sha256': k.digest(evidence), 'scope_matrix_passed': True, 'invalid_key_rejected': True, 'channel_matrix_passed': True})
        return path

    def cli(self, action, proof=None, revoke=False):
        args = [action, '--plan', str(self.plan_path)]
        if proof: args += ['--capability', str(proof)]
        if revoke: args += ['--revoke-scoped-keys']
        with mock.patch.object(k, 'backend', return_value=self.b), mock.patch.object(k, 'operation_state', side_effect=lambda _: contextlib.nullcontext()), mock.patch.object(k, 'confirm_lifecycle'), mock.patch.object(k.time, 'sleep'):
            k.main(args)

    def assert_stopped(self):
        self.assertTrue(all(not c['State']['Running'] for c in self.b.live.values()))
        self.assertFalse(any('up' in c and 'caddy' in c for c in self.b.calls))

    def assert_unsorted_mounts_allowed(self, action):
        targets = [v['target'] for v in self.b.base['services']['cli-proxy-api']['volumes']]
        self.assertNotEqual(targets, sorted(targets))
        try:
            self.release.lifecycle(action, self.proof(rollback=action == 'rollback'), confirm=lambda: None)
        except k.Failure as error:
            self.assert_stopped()
            self.assertFalse(any('up' in call for call in self.b.calls))
            print(json.dumps({'action': action, 'error': str(error), 'up_called': False, 'stage': (self.release.state / 'stage').read_text().strip()}, ensure_ascii=False))
            raise
        self.assertTrue(any('up' in call for call in self.b.calls))
        self.assertFalse(self.b.live['caddy']['State']['Running'])
        self.assertEqual((self.release.state / 'stage').read_text().strip(), 'candidate_healthy_blocked' if action == 'start' else 'rollback_healthy_blocked')

    def test_start_accepts_unsorted_mounts(self):
        self.assert_unsorted_mounts_allowed('start')

    def test_rollback_accepts_unsorted_mounts(self):
        self.assert_unsorted_mounts_allowed('rollback')

    def test_lifecycle_accepts_omitted_empty_volumes(self):
        for action in ['start', 'rollback']:
            with self.subTest(action=action):
                self.setUp()
                self.assertEqual(k.read(self.release.state / 'plan.json')['rendered']['services']['caddy']['volumes'], [])
                original = self.b.command
                def omitted(args, data=None):
                    raw = original(args, data)
                    if len(args) > 1 and args[1] == 'compose' and 'config' in args:
                        document = json.loads(raw)
                        document['services']['caddy'].pop('volumes')
                        return json.dumps(document)
                    return raw
                with mock.patch.object(self.b, 'command', side_effect=omitted):
                    self.release.lifecycle(action, self.proof(rollback=action == 'rollback'), confirm=lambda: None)
                self.assertTrue(any('up' in call for call in self.b.calls))
                self.assertFalse(self.b.live['caddy']['State']['Running'])
                self.assertEqual((self.release.state / 'stage').read_text().strip(), 'candidate_healthy_blocked' if action == 'start' else 'rollback_healthy_blocked')

    def test_compose_value_drift_rejected_before_up(self):
        for action in ['start', 'rollback']:
            for field in ['source', 'target', 'type', 'read_only', 'bind', 'image', 'environment', 'command']:
                with self.subTest(action=action, field=field):
                    self.setUp()
                    self.b.base['services']['cli-proxy-api']['command'] = ['alpha', 'beta']
                    plan = k.read(self.release.state / 'plan.json')
                    plan['base']['services']['cli-proxy-api']['command'] = ['alpha', 'beta']
                    plan['rendered']['services']['cli-proxy-api']['command'] = ['alpha', 'beta']
                    k.write(self.release.state / 'plan.json', plan)
                    original = self.b.command
                    def changed(args, data=None):
                        raw = original(args, data)
                        if len(args) > 1 and args[1] == 'compose' and 'config' in args:
                            document = json.loads(raw)
                            service = document['services']['cli-proxy-api']
                            if field in ['source', 'target', 'type', 'read_only', 'bind']:
                                service['volumes'][0][field] = {'source': '/different-source', 'target': '/different-target', 'type': 'volume', 'read_only': True, 'bind': {'create_host_path': True}}[field]
                            else:
                                service[field] = {'image': 'different-image', 'environment': {'CHANGED': 'true'}, 'command': ['beta', 'alpha']}[field]
                            return json.dumps(document)
                        return raw
                    with mock.patch.object(self.b, 'command', side_effect=changed):
                        with self.assertRaisesRegex(k.Failure, '启动Compose渲染漂移|回退改变了镜像之外的Compose字段'):
                            self.release.lifecycle(action, self.proof(rollback=action == 'rollback'), confirm=lambda: None)
                    self.assert_stopped()
                    self.assertFalse(any('up' in call for call in self.b.calls))

    def test_cli_start_and_health_keep_caddy_stopped(self):
        self.cli('start', self.proof())
        self.assertEqual((self.release.state / 'stage').read_text().strip(), 'candidate_healthy_blocked')
        self.assertFalse(self.b.live['caddy']['State']['Running'])
        self.assertTrue(all(self.b.live[s]['State']['Running'] for s in k.APPS))
        self.cli('health')
        stop = next(i for i, c in enumerate(self.b.calls) if 'stop' in c and c[-1] == 'caddy')
        up = next(i for i, c in enumerate(self.b.calls) if 'up' in c)
        self.assertLess(stop, up)

    def test_cli_start_recreates_reused_worker_with_candidate_chain(self):
        worker = self.b.live['oauth-login']
        old_id = worker['Id']
        worker['Image'] = self.b.images['oauth-login']
        worker['Config']['Image'] = self.b.images['oauth-login']
        worker['Config']['Labels']['com.docker.compose.project.config_files'] = ','.join(self.spec['compose_files'][:1])
        self.cli('start', self.proof())
        up = next(args for args in self.b.calls if 'up' in args)
        self.assertIn('--force-recreate', up)
        self.assertEqual(up[-3:], k.APPS)
        self.assertNotEqual(self.b.live['oauth-login']['Id'], old_id)
        expected = self.spec['compose_files'] + [str(self.release.state / 'compose.kilo.yaml')]
        self.assertEqual(self.b.live['oauth-login']['Config']['Labels']['com.docker.compose.project.config_files'].split(','), expected)
        self.cli('health')
        self.assertFalse(self.b.live['caddy']['State']['Running'])

    def test_cli_start_config_drift_stops_all_before_up(self):
        proof = self.proof()
        cfg = copy.deepcopy(self.new_config); cfg['unrelated']['changed'] = True; k.write(self.spec['cpa_config'], cfg)
        with self.assertRaisesRegex(k.Failure, '候选配置漂移'): self.cli('start', proof)
        self.assert_stopped(); self.assertFalse(any('up' in c for c in self.b.calls))

    def test_cli_health_failure_stops_apps_and_ingress(self):
        self.cli('start', self.proof()); self.b.health_ok = False
        with self.assertRaisesRegex(k.Failure, '内网只读鉴权健康失败'): self.cli('health')
        self.assert_stopped()

    def test_cli_unknown_scope_capability_stops_before_up(self):
        proof = self.proof(); receipt = k.read(proof); receipt['scope_matrix_passed'] = False; k.write(proof, receipt)
        with self.assertRaisesRegex(k.Failure, '隔离矩阵尚未通过'): self.cli('start', proof)
        self.assert_stopped(); self.assertFalse(any('up' in c for c in self.b.calls))

    def test_cli_scope_image_binding_mismatch_stops(self):
        proof = self.proof(); receipt = k.read(proof); receipt['image_ids']['cli-proxy-api'] = 'wrong'; k.write(proof, receipt)
        with self.assertRaisesRegex(k.Failure, '镜像或配置绑定不符'): self.cli('start', proof)
        self.assert_stopped()

    def test_cli_topology_direct_port_or_extra_endpoint_rejected(self):
        for kind in ['port', 'endpoint']:
            with self.subTest(kind=kind):
                self.setUp()
                if kind == 'port': self.b.live['cli-proxy-api']['HostConfig']['PortBindings'] = {'8317/tcp': [{'HostPort': '8317'}]}
                else: self.b.extra_endpoint = True
                with self.assertRaisesRegex(k.Failure, '直达端口|额外旁接容器'): self.cli('start', self.proof())
                self.assert_stopped(); self.assertFalse(any('up' in c for c in self.b.calls))

    def test_cli_rollback_revoke_preserves_data_and_legacy_key(self):
        self.cli('start', self.proof())
        current = copy.deepcopy(self.new_config); current['unrelated']['after-start'] = '保留新配置'
        k.write(self.spec['cpa_config'], current)
        (self.root / 'auth/new-after-start.json').write_text('当前新凭据', encoding='utf-8')
        (self.root / 'manager-volume/usage.sqlite-wal').write_text('新WAL数据', encoding='utf-8')
        before_auth = (self.root / 'auth/new-after-start.json').read_bytes()
        proof = self.proof(rollback=True, revoke=True, content=current)
        output = io.StringIO()
        with mock.patch('sys.stdout', output): self.cli('rollback', proof, revoke=True)
        self.assertIn('Key数量=3', output.getvalue())
        for key in ['fixture-kilo', 'fixture-codex', 'fixture-bps', 'fixture-legacy']:
            self.assertNotIn(key, output.getvalue())
        cfg = k.read(self.spec['cpa_config'])
        self.assertEqual(cfg['access']['api-keys'], ['fixture-legacy'])
        self.assertNotIn('client-key-scopes', cfg['access'])
        expected = copy.deepcopy(current); expected['access'] = {'api-keys': ['fixture-legacy']}
        self.assertEqual(cfg, expected)
        self.assertEqual((self.root / 'auth/new-after-start.json').read_bytes(), before_auth)
        self.assertEqual((self.root / 'manager-volume/usage.sqlite-wal').read_text(encoding='utf-8'), '新WAL数据')
        with tarfile.open(self.release.state / 'backup-rollback-current/data.tar.gz') as archive:
            self.assertEqual(archive.extractfile(k.archive_name(self.root / 'auth/new-after-start.json')).read(), before_auth)
        self.assertFalse(self.b.live['caddy']['State']['Running'])
        self.assertEqual((self.release.state / 'stage').read_text().strip(), 'rollback_healthy_blocked')

    def test_cli_rollback_revoke_empty_rejected_and_preserved(self):
        cfg = copy.deepcopy(self.new_config); cfg['access']['api-keys'] = ['fixture-kilo', 'fixture-codex', 'fixture-bps']
        k.write(self.spec['cpa_config'], cfg)
        before = pathlib.Path(self.spec['cpa_config']).read_bytes()
        proof = self.proof(rollback=True)
        with self.assertRaisesRegex(k.Failure, '撤销后无非scope有效Key'): self.cli('rollback', proof, revoke=True)
        self.assertEqual(pathlib.Path(self.spec['cpa_config']).read_bytes(), before)
        self.assert_stopped(); self.assertFalse(any('up' in c for c in self.b.calls))

    def test_cli_rollback_without_scope_evidence_keeps_current_config(self):
        proof = self.proof(rollback=True); receipt = k.read(proof); receipt['review'] = 'PENDING'; k.write(proof, receipt)
        before = pathlib.Path(self.spec['cpa_config']).read_bytes()
        with self.assertRaisesRegex(k.Failure, '缺少已审scope能力回执'): self.cli('rollback', proof)
        self.assertEqual(pathlib.Path(self.spec['cpa_config']).read_bytes(), before)
        self.assert_stopped()

    def test_cli_rollback_storage_or_mount_drift_rejected_before_backup(self):
        for kind in ['storage', 'mount', 'v8-kilo', 'v8-bps', 'bps-enabled']:
            with self.subTest(kind=kind):
                self.setUp()
                if kind == 'storage':
                    cfg = copy.deepcopy(self.new_config); cfg['server'] = {'kv-store': {'path': '/new-data/kv.db'}}
                    k.write(self.spec['cpa_config'], cfg)
                elif kind.startswith('v8-') or kind == 'bps-enabled':
                    cfg = copy.deepcopy(self.new_config)
                    if kind == 'v8-kilo': cfg['upstream']['kilo']['state-dir'] = '/other/kilo'
                    else: cfg['upstream']['bps'] = {'protocol': {'state-dir': '/other/bps'} if kind == 'v8-bps' else {'enabled': False}}
                    k.write(self.spec['cpa_config'], cfg)
                else:
                    self.b.live['cpa-manager-plus']['Mounts'][0]['Source'] = str(self.root / 'untracked-volume')
                with self.assertRaisesRegex(k.Failure, '存储路径漂移|运行挂载漂移'):
                    self.cli('rollback', self.proof(rollback=True))
                self.assert_stopped()
                self.assertFalse((self.release.state / 'backup-rollback-current').exists())

    def test_cli_health_actual_image_or_proof_drift_stops_all(self):
        for kind in ['image', 'proof']:
            with self.subTest(kind=kind):
                self.setUp(); proof = self.proof(); self.cli('start', proof)
                if kind == 'image': self.b.live['cli-proxy-api']['Image'] = 'other-actual-image'
                else:
                    receipt = k.read(proof); receipt['reviewed_by'] = 'other-reviewer'; k.write(proof, receipt)
                with self.assertRaisesRegex(k.Failure, '运行镜像或服务状态不符|已审scope回执或证据漂移'):
                    self.cli('health')
                self.assert_stopped()

    def test_cli_missing_capability_stops_all(self):
        self.cli('start', self.proof())
        with self.assertRaisesRegex(k.Failure, 'scope能力回执'): self.cli('rollback')
        self.assert_stopped()

    def test_cli_start_health_failure_stops_all(self):
        self.b.health_ok = False
        with self.assertRaisesRegex(k.Failure, '内网只读鉴权健康失败'): self.cli('start', self.proof())
        self.assert_stopped()
        self.assertEqual((self.release.state / 'stage').read_text().strip(), 'start_failed')

    def test_cli_start_compose_or_mount_drift_rejected_before_up(self):
        for kind in ['compose', 'mount']:
            with self.subTest(kind=kind):
                self.setUp()
                if kind == 'compose': self.b.base['services']['cli-proxy-api']['environment'] = {'UNEXPECTED': '1'}
                else: self.b.live['cli-proxy-api']['Mounts'][0]['Source'] = str(self.root / 'other-auth')
                with self.assertRaisesRegex(k.Failure, '启动Compose渲染漂移|运行挂载漂移'):
                    self.cli('start', self.proof())
                self.assert_stopped(); self.assertFalse(any('up' in c for c in self.b.calls))


class KiloStorageV8Tests(unittest.TestCase):
    setUp = KiloReleaseTests.setUp
    prepare = KiloReleaseTests.prepare

    def test_formal_storage_fixtures(self):
        path = pathlib.Path(__file__).resolve().parent.parent / 'docs/evidence/kilo-native-20261112/m10-release-materials/release03-fix01/config-fixtures.json'
        for case in k.read(path):
            with self.subTest(name=case['name']):
                self.assertEqual(k.storage_config(case['config']), case['expected'])

    def test_prepare_canonical_state_keeps_effective_path(self):
        self.b.cfg['kilo'] = {'state-dir': '/old/ignored', 'proxy-limit': 5}
        self.b.cfg['upstream'] = {'kilo': {'state-dir': '/custom/kilo', 'proxy-limit': 7}, 'bps': {'protocol': {'state-dir': '/custom/bps'}}}
        self.spec['kilo_state']['container'] = '/custom/kilo'
        mount = next(m for m in self.b.live['cli-proxy-api']['Mounts'] if m['Destination'] == '/root/bps-state')
        mount['Destination'] = '/custom/bps'
        self.spec['baseline'] = self.b.facts(self.b.live)
        for item in self.b.base['services']['cli-proxy-api']['volumes']:
            if item['target'] == '/root/bps-state': item['target'] = '/custom/bps'
        self.prepare()
        cfg = k.read(self.release.state / 'cpa-config.yaml')
        self.assertEqual(cfg['upstream']['kilo'], {'state-dir': '/custom/kilo', 'proxy-limit': 7})
        self.assertEqual(cfg['kilo'], {'proxy-limit': 5})
        self.assertEqual(k.state_directory(cfg)[1], '/custom/kilo')

    def test_default_bps_unmounted_existing_or_enabled_absent_rejected(self):
        for exists, enabled in [(True, True), (False, True), (False, False)]:
            with self.subTest(exists=exists, enabled=enabled):
                self.setUp()
                if not enabled: self.b.cfg['upstream'] = {'bps': {'protocol': {'enabled': False}}}
                self.b.live['cli-proxy-api']['Mounts'] = [m for m in self.b.live['cli-proxy-api']['Mounts'] if m['Destination'] != '/root/bps-state']
                self.spec['baseline'] = self.b.facts(self.b.live)
                if exists: self.b.existing_paths.add(('cli-proxy-api-container', '/root/bps-state'))
                with self.assertRaisesRegex(k.Failure, '既有BPS状态未持久化|BPS状态须持久挂载'):
                    self.prepare()
                self.assertFalse(any('合成加载包' in call for call in self.b.calls))

    def test_default_bps_mapped_data_in_stopped_backup(self):
        saved = self.root / 'bps-state/default-existing.json'; saved.write_text('当前BPS状态', encoding='utf-8')
        self.prepare(); self.release.apply(lambda: None)
        with tarfile.open(self.release.state / 'backup/data.tar.gz') as archive:
            self.assertEqual(archive.extractfile(k.archive_name(saved)).read(), saved.read_bytes())


if __name__ == '__main__':
    unittest.main(verbosity=2)
