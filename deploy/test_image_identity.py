#!/usr/bin/env python3
"""镜像身份新增专项：真实 OCI 归档图、内存运行态，不调用 Docker 或网络。"""

import copy
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import sys
import tarfile
import tempfile
import types
import unittest
from unittest import mock


DEPLOY = pathlib.Path(__file__).resolve().parent
if os.name != 'posix':
    sys.modules.setdefault('fcntl', types.ModuleType('fcntl'))


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


c = load('identity_capability', DEPLOY / 'capability_probe.py')
p = load('identity_phase2', DEPLOY / 'phase2-release.py')
h = load('identity_host', DEPLOY / 'mx-update-host.py')
fixture = load('identity_update_fixture', DEPLOY / 'test_mx_update.py')
u, k = fixture.u, fixture.k
COMMIT = fixture.COMMIT
ARCHIVE = 'mx-cpamp-images-linux-amd64.tar.gz'
MANIFEST = 'mx-cpamp-images.manifest.json'


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def image_materials(folder, fault=None):
    """所有 descriptor、config、manifest 和发行摘要均来自本次实际归档字节。"""
    release = fixture.materials(folder, {s: 'sha256:' + '0' * 64 for s in c.ROLES})
    blobs, records, candidates, configs, manifests, indexes = {}, [], {}, {}, {}, {}

    def blob(value, media_type):
        raw = encoded(value)
        sha = hashlib.sha256(raw).hexdigest()
        blobs['blobs/sha256/' + sha] = raw
        return {'mediaType': media_type, 'digest': 'sha256:' + sha, 'size': len(raw)}

    layer_raw = b'identity-fixture-layer'
    layer_sha = hashlib.sha256(layer_raw).hexdigest()
    blobs['blobs/sha256/' + layer_sha] = layer_raw
    layer = {'mediaType': 'application/vnd.oci.image.layer.v1.tar', 'digest': 'sha256:' + layer_sha,
             'size': len(layer_raw)}
    roots = []
    for service, role in c.ROLES.items():
        config = {'architecture': 'amd64', 'os': 'linux', 'rootfs': {'type': 'layers', 'diff_ids': [layer['digest']]},
                  'config': {'Labels': {'org.opencontainers.image.revision': COMMIT, 'io.mx-cpamp.role': role}}}
        if service == 'cli-proxy-api' and fault == 'role':
            config['config']['Labels']['io.mx-cpamp.role'] = 'worker'
        if service == 'cli-proxy-api' and fault == 'platform':
            config['architecture'] = 'arm64'
        config_descriptor = blob(config, 'application/vnd.oci.image.config.v1+json')
        configs[service] = config_descriptor['digest']
        candidates[service] = {'role': role, 'ref': 'fixture:' + role, 'revision': COMMIT,
                               'accepted_ids': [config_descriptor['digest']]}
        if service == 'cli-proxy-api' and fault == 'accepted_config':
            candidates[service]['accepted_ids'] = ['sha256:' + 'f' * 64]
        records.append({'Config': 'blobs/sha256/' + config_descriptor['digest'][7:],
                        'RepoTags': [candidates[service]['ref']], 'Layers': ['blobs/sha256/' + layer_sha]})
        selected_config = config_descriptor
        if service == 'cli-proxy-api' and fault == 'manifest_config':
            other = copy.deepcopy(config)
            other['config']['Labels']['io.mx-cpamp.role'] = 'unregistered'
            selected_config = blob(other, 'application/vnd.oci.image.config.v1+json')
        manifest = {'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.manifest.v1+json',
                    'config': selected_config, 'layers': [layer]}
        manifest_descriptor = blob(manifest, manifest['mediaType'])
        manifests[service] = manifest_descriptor['digest']
        index = {'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.index.v1+json',
                 'manifests': [manifest_descriptor]}
        index_descriptor = blob(index, index['mediaType'])
        indexes[service] = index_descriptor['digest']
        roots.extend([manifest_descriptor, index_descriptor])
    mixed = blob({'schemaVersion': 2, 'manifests': roots[::2]}, 'application/vnd.oci.image.index.v1+json')
    roots.append(mixed)
    orphan = blob({'schemaVersion': 2, 'config': {'mediaType': 'application/vnd.oci.image.config.v1+json',
                   'digest': configs['cli-proxy-api'], 'size': len(blobs['blobs/sha256/' + configs['cli-proxy-api'][7:]])},
                   'layers': [layer], 'annotations': {'fixture': 'unreachable'}},
                  'application/vnd.oci.image.manifest.v1+json')
    if fault == 'descriptor_size':
        roots[0] = dict(roots[0], size=roots[0]['size'] + 1)
    if fault == 'descriptor_digest':
        roots[0] = dict(roots[0], digest='sha256:' + 'e' * 64)
    blobs['manifest.json'] = encoded(records)
    blobs['index.json'] = encoded({'schemaVersion': 2, 'manifests': roots})
    with tarfile.open(folder / ARCHIVE, 'w:gz') as archive:
        for name, raw in blobs.items():
            item = tarfile.TarInfo(name)
            item.size = len(raw)
            archive.addfile(item, io.BytesIO(raw))
    archive_raw = (folder / ARCHIVE).read_bytes()
    manifest = {'schema': 1, 'archive': {'file': ARCHIVE, 'sha256': hashlib.sha256(archive_raw).hexdigest()},
                'images': {candidate['role']: dict(candidate, os='linux', architecture='amd64')
                           for candidate in candidates.values()}}
    (folder / MANIFEST).write_bytes(encoded(manifest))
    release['candidates'] = candidates
    for name in (ARCHIVE, MANIFEST):
        raw = (folder / name).read_bytes()
        release['assets'][name] = {'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
    u.write(folder / 'release.json', release)
    return {'release': release, 'configs': configs, 'images': manifests, 'indexes': indexes,
            'mixed': mixed['digest'], 'orphan': orphan['digest']}


class ImageIdentityTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='mx-image-identity-')
        self.addCleanup(temporary.cleanup)
        self.root = pathlib.Path(temporary.name)
        self.folder = self.root / 'materials'
        self.data = image_materials(self.folder)

    def test_config_manifest_and_single_role_index_keep_original_allowlist(self):
        before = copy.deepcopy(self.data['release']['candidates'])
        for service, candidate in before.items():
            for image in (self.data['configs'][service], self.data['images'][service], self.data['indexes'][service]):
                self.assertTrue(c.accepts_image_id(image, candidate, self.folder))
            self.assertTrue(c.same_image_identity(self.data['images'][service], self.data['configs'][service], candidate, self.folder))
        self.assertEqual(u.read(self.folder / 'release.json')['candidates'], before)

    def test_unreachable_and_multirole_ids_are_not_accepted(self):
        candidate = self.data['release']['candidates']['cli-proxy-api']
        for image in (self.data['orphan'], self.data['mixed'], self.data['images']['oauth-login']):
            self.assertFalse(c.accepts_image_id(image, candidate, self.folder))
            self.assertFalse(c.same_image_identity(self.data['images']['cli-proxy-api'], image, candidate, self.folder))

    def test_wrong_role_platform_or_accepted_config_is_rejected(self):
        for fault in ('role', 'platform', 'accepted_config'):
            with self.subTest(fault=fault):
                folder = self.root / fault
                data = image_materials(folder, fault)
                with self.assertRaises(c.Failure):
                    c.accepts_image_id(data['images']['cli-proxy-api'], data['release']['candidates']['cli-proxy-api'], folder)

    def test_manifest_pointing_to_another_config_is_not_accepted(self):
        folder = self.root / 'another-config'
        data = image_materials(folder, 'manifest_config')
        self.assertFalse(c.accepts_image_id(data['images']['cli-proxy-api'], data['release']['candidates']['cli-proxy-api'], folder))

    def test_descriptor_digest_and_size_are_verified(self):
        for fault in ('descriptor_digest', 'descriptor_size'):
            with self.subTest(fault=fault):
                folder = self.root / fault
                data = image_materials(folder, fault)
                with self.assertRaises(c.Failure):
                    c.accepts_image_id(data['images']['cli-proxy-api'], data['release']['candidates']['cli-proxy-api'], folder)

    def test_archive_and_manifest_assets_are_verified(self):
        for name in (ARCHIVE, MANIFEST):
            with self.subTest(asset=name):
                folder = self.root / ('changed-' + name)
                data = image_materials(folder)
                with (folder / name).open('ab') as stream:
                    stream.write(b' ')
                with self.assertRaises(c.Failure):
                    c.accepts_image_id(data['images']['cli-proxy-api'], data['release']['candidates']['cli-proxy-api'], folder)

    def test_phase2_first_membership_returns_actual_executable_ids(self):
        def command(args, data=None):
            service = next(s for s, row in self.data['release']['candidates'].items() if row['ref'] == args[-1])
            return json.dumps([{'Id': self.data['images'][service], 'Os': 'linux', 'Architecture': 'amd64',
                                'Config': {'Labels': {'org.opencontainers.image.revision': COMMIT}}}])
        with mock.patch.object(p, 'STATE', self.root), mock.patch.object(p, 'CANDIDATES', self.data['release']['candidates']), mock.patch.object(p, 'command', command):
            self.assertEqual(p.image_records(), self.data['images'])
            self.assertTrue(p.runtime_image_matches('cli-proxy-api', self.data['images']['cli-proxy-api'], self.data['configs']['cli-proxy-api']))

    def test_capability_second_membership_keeps_actual_receipt_ids(self):
        cfg = {'config-version': 8, 'access': {'api-keys': ['fixture-key'], 'client-key-scopes': []}}
        count, observations = c.policy_expectations(cfg)
        images = self.data['images']
        config_sha = 'a' * 64
        receipt = {'review_type': 'automated', 'image_ids': images, 'config_sha256': config_sha,
                   'source_config_sha256': config_sha, 'scope_matrix_passed': True, 'mode': 'scope-enforced'}
        evidence = {'schema': 1, 'kind': 'combined-scope-capability', 'image_ids': images,
                    'config_policy': {'schema': 1, 'kind': 'authz-policy-matrix', 'validator': c.VALIDATOR,
                                      'config_sha256': config_sha, 'source_commit': COMMIT,
                                      'key_count': count, 'observations': observations},
                    'production_wiring': u.read(self.folder / 'production-wiring.json'),
                    'image_identities': {s: {'id': image, 'revision': COMMIT, 'role': c.ROLES[s],
                                            'os': 'linux', 'architecture': 'amd64'} for s, image in images.items()}}
        c.validate_combined_receipt(receipt, evidence, cfg, images, config_sha, self.folder)
        bad = copy.deepcopy(evidence)
        bad['image_identities']['oauth-login']['role'] = 'manager'
        with self.assertRaises(c.Failure):
            c.validate_combined_receipt(receipt, bad, cfg, images, config_sha, self.folder)

    def test_host_completion_and_recovery_identity_comparison(self):
        plan = {'images': self.data['images']}
        services = {s: {'image': image} for s, image in self.data['configs'].items()}
        self.assertTrue(h.candidate_runtime_images_match(self.root, plan, services))
        services['cli-proxy-api']['image'] = self.data['configs']['oauth-login']
        self.assertFalse(h.candidate_runtime_images_match(self.root, plan, services))


class ContainerIdentityCycleTests(unittest.TestCase):
    def test_start_checked_health_and_runtime_use_same_archive_binding(self):
        setup = fixture.UpdateTests(methodName='runTest')
        setup.setUp()
        self.addCleanup(setup.doCleanups)
        state = setup.root / '.mx-updates' / 'identity-round'
        state.mkdir()
        folder = state / 'materials'
        data = image_materials(folder)
        setup.b.images = data['images']
        setup.b.runtime_image_matches = lambda service, executable, observed: c.same_image_identity(
            executable, observed, data['release']['candidates'][service], folder)
        original_command = setup.b.command
        def command(args, stdin=None):
            result = original_command(args, stdin)
            if args[:2] == ['docker', 'compose'] and 'up' in args:
                for service in u.APPS:
                    setup.b.live[service]['Image'] = data['configs'][service]
            return result
        setup.b.command = command
        created = []
        def factory(spec, backend):
            setup.b.spec = spec
            release = k.Release(spec, backend)
            created.append(release)
            return release
        library = types.SimpleNamespace(backend=lambda spec: setup.b, Release=factory, read=k.read,
                                        scope_keys=k.scope_keys, digest=k.digest)
        loader = lambda name, path: library if name == 'mx_update_kilo' else c
        with mock.patch.object(k, '__file__', str(folder / 'kilo-release.py')), mock.patch.object(k.time, 'sleep'):
            result = u.run_cycle(setup.site_path, setup.site, 'v1.2.3', state, confirm=setup.confirm,
                                 fetcher=lambda *args: (folder, data['release']), loader=loader)
            self.assertEqual(result, u.read(setup.site['active_release_pointer']))
            self.assertEqual(u.read(state / 'plan.json')['images'], data['images'])
            self.assertEqual(u.read(state / 'capability-receipt.json')['image_ids'], data['images'])
            self.assertEqual((state / 'stage').read_text().strip(), 'completed')
            setup.confirm.assert_called_once()
            release = created[0]
            plan, active = u.read(state / 'plan.json'), u.read(state / 'active.json')
            release.checked_health(plan, active, ingress_stopped=False)
            release.verify_current_storage(plan, setup.b.snapshot())
            for service in u.APPS:
                self.assertEqual(setup.b.live[service]['Image'], data['configs'][service])
                self.assertEqual(setup.b.live[service]['Config']['Image'], data['images'][service])
            live = setup.b.live['cli-proxy-api']
            old_source = live['Mounts'][0]['Source']
            live['Mounts'][0]['Source'] = str(setup.root / 'drifted')
            with self.assertRaises(k.Failure):
                release.checked_health(plan, active, ingress_stopped=False)
            live['Mounts'][0]['Source'] = old_source
            live['Config']['Image'] = data['images']['oauth-login']
            with self.assertRaises(k.Failure):
                release.checked_health(plan, active, ingress_stopped=False)


if __name__ == '__main__':
    unittest.main(verbosity=2)
