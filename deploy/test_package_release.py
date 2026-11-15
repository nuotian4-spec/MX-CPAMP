#!/usr/bin/env python3
"""发布流水线定向测试：不运行 Docker、产品或网络请求。"""

import contextlib
import copy
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import re
import subprocess
import sys
import tarfile
import tempfile
import textwrap
import types
import unittest
import urllib.error
from unittest import mock


DEPLOY = pathlib.Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('package_release', DEPLOY / 'package-release.py')
package = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(package)
CONSUMER_SPEC = importlib.util.spec_from_file_location('release_consumer', DEPLOY / 'phase2-release.py')
consumer = importlib.util.module_from_spec(CONSUMER_SPEC)
with mock.patch.dict(sys.modules, {'fcntl': types.ModuleType('fcntl')}):
    CONSUMER_SPEC.loader.exec_module(consumer)

COMMIT = '1' * 40
VERSION = 'v1.2.3'
WORKFLOW = DEPLOY.parent / '.github' / 'workflows' / 'release.yml'


def workflow_python():
    text = WORKFLOW.read_text(encoding='utf-8')
    return [textwrap.dedent(block) for block in re.findall(
        r"python3 - <<'PY'\n(.*?)^          PY$", text, re.MULTILINE | re.DOTALL)]


class LoadBoundary(Exception):
    pass


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = pathlib.Path(self.temp.name)
        self.repo = self.home / 'repo'
        self.output = self.home / 'releases'
        (self.repo / 'deploy').mkdir(parents=True)
        for name in ('kilo-release.py', 'phase2-release.py'):
            (self.repo / 'deploy' / name).write_bytes((DEPLOY / name).read_bytes())
        # 已发布入口的同等 fixture；测试不依赖当前工作树是否已落位 shell 文件。
        (self.repo / 'deploy' / 'kilo-release.sh').write_text(
            '#!/bin/sh\nset -eu\numask 077\n'
            'HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\n'
            'exec python3 "$HERE/kilo-release.py" "$@"\n', encoding='utf-8')
        self.calls = []
        self.bad_build = False
        self.inspect_arch = 'amd64'
        self.archive_arch = 'amd64'
        self.archive_commit = COMMIT
        self.config_style = 'oci'

    def fake_run(self, args, root, capture=False):
        self.calls.append(list(args))
        if args[:3] == ['git', 'rev-parse', 'HEAD']:
            return COMMIT
        if args[:2] == ['git', 'show']:
            return '1794762000'
        if args[:2] == ['git', 'status'] or args[0] == 'git':
            return ''
        if args[:2] == ['docker', 'build']:
            if self.bad_build:
                raise subprocess.CalledProcessError(1, args)
            return ''
        if args[:3] == ['docker', 'image', 'inspect']:
            image, version = args[3].split(':')
            role = next(key for key, item in package.COMPONENTS.items() if item[0] == image)
            return json.dumps([{'Id': 'sha256:' + hashlib.sha256(role.encode()).hexdigest(),
                'Os': 'linux', 'Architecture': self.inspect_arch,
                'Config': {'Labels': self.labels(role, version, COMMIT)}}])
        raise AssertionError('未预期的命令：' + repr(args))

    @staticmethod
    def labels(role, version, commit):
        return {'org.opencontainers.image.revision': commit,
                'org.opencontainers.image.version': version, 'io.mx-cpamp.role': role}

    def fake_archive(self, root, images, destination):
        records = []
        with tarfile.open(destination, 'w:gz') as archive:
            for role, image in images.items():
                version = image['ref'].split(':')[1]
                raw = json.dumps({'os': 'linux', 'architecture': self.archive_arch,
                    'config': {'Labels': self.labels(role, version, self.archive_commit)}}).encode()
                config_hash = hashlib.sha256(raw).hexdigest()
                name = ('blobs/sha256/' + config_hash if self.config_style == 'oci' else config_hash + '.json')
                member = tarfile.TarInfo(name)
                member.size = len(raw)
                archive.addfile(member, io.BytesIO(raw))
                records.append({'Config': name, 'RepoTags': [image['ref']], 'Layers': []})
            raw = json.dumps(records).encode()
            member = tarfile.TarInfo('manifest.json')
            member.size = len(raw)
            archive.addfile(member, io.BytesIO(raw))

    def build(self, version=VERSION):
        with mock.patch.object(package, 'run', side_effect=self.fake_run), \
                mock.patch.object(package, 'archive_images', side_effect=self.fake_archive), \
                contextlib.redirect_stdout(io.StringIO()):
            return package.package_release(self.repo, version, COMMIT, self.output)

    def test_complete_package_and_script_identity(self):
        directory = self.build()
        self.assertEqual({path.name for path in directory.iterdir()},
                         {package.ARCHIVE, package.MANIFEST, 'release.json', *package.SCRIPTS})
        release = json.loads((directory / 'release.json').read_text(encoding='utf-8'))
        self.assertEqual((release['version'], release['commit'], release['platform']), (VERSION, COMMIT, 'linux/amd64'))
        self.assertEqual(set(release['candidates']), {'cli-proxy-api', 'cpa-manager-plus', 'oauth-login'})
        self.assertEqual(set(release['assets']), {package.ARCHIVE, package.MANIFEST, *package.SCRIPTS})
        for name, record in release['assets'].items():
            self.assertEqual(record['sha256'], package.digest(directory / name))
            self.assertEqual(record['size'], (directory / name).stat().st_size)
        for name in package.SCRIPTS:
            self.assertEqual((directory / name).read_bytes(), (self.repo / 'deploy' / name).read_bytes())
        self.assertFalse((self.output / ('.' + VERSION + '.building')).exists())
        self.assertNotIn(str(self.repo), (directory / 'release.json').read_text(encoding='utf-8'))

    def test_three_builds_preserve_arguments_and_contexts(self):
        self.build()
        builds = [call for call in self.calls if call[:2] == ['docker', 'build']]
        self.assertEqual(len(builds), 3)
        for role, command in zip(package.COMPONENTS, builds):
            self.assertEqual(command[command.index('--platform') + 1], 'linux/amd64')
            self.assertEqual(command[-1], str(self.repo / package.COMPONENTS[role][1]))
            self.assertIn('org.opencontainers.image.revision=' + COMMIT, command)
        for argument in ('VERSION=' + VERSION, 'COMMIT=' + COMMIT, 'TARGETOS=linux', 'TARGETARCH=amd64'):
            self.assertIn(argument, builds[0])
        self.assertTrue(any(value.startswith('BUILD_DATE=') and value.endswith('Z') for value in builds[0]))
        self.assertIn('SOURCE_COMMIT=' + COMMIT, builds[1])
        self.assertIn('SOURCE_COMMIT=' + COMMIT, builds[2])

    def test_existing_version_is_not_overwritten(self):
        directory = self.build()
        original = (directory / 'release.json').read_bytes()
        self.calls.clear()
        with self.assertRaisesRegex(package.Failure, '不覆盖'):
            self.build()
        self.assertEqual((directory / 'release.json').read_bytes(), original)
        self.assertFalse(any(call[:2] == ['docker', 'build'] for call in self.calls))

    def test_different_versions_remain_separate(self):
        first = self.build()
        original = (first / 'release.json').read_bytes()
        second = self.build('v1.2.4-rc.1')
        self.assertNotEqual(first, second)
        self.assertEqual((first / 'release.json').read_bytes(), original)
        self.assertEqual(json.loads((second / 'release.json').read_text(encoding='utf-8'))['version'], 'v1.2.4-rc.1')

    def test_failed_build_has_no_completed_package(self):
        self.bad_build = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.build()
        self.assertFalse((self.output / VERSION).exists())
        self.assertFalse((self.output / ('.' + VERSION + '.building') / 'release.json').exists())

    def test_missing_script_fails_before_build(self):
        (self.repo / 'deploy' / 'kilo-release.sh').unlink()
        with self.assertRaisesRegex(package.Failure, '缺少发布脚本'):
            self.build()
        self.assertFalse(any(call[:2] == ['docker', 'build'] for call in self.calls))

    def test_invalid_version_and_commit_fail_before_commands(self):
        for version, commit in (('../v1.2.3', COMMIT), ('latest', COMMIT),
                                ('v1.2.3;echo', COMMIT), (VERSION, 'main'),
                                (VERSION, 'A' * 40)):
            with self.subTest(version=version, commit=commit), \
                    mock.patch.object(package, 'run') as command, \
                    self.assertRaises(package.Failure):
                package.validate_source(self.repo, version, commit)
            command.assert_not_called()

    def test_dirty_or_wrong_head_checkout_is_rejected(self):
        for failure in ('head', 'dirty'):
            def result(args, root, capture=False):
                if args[:3] == ['git', 'rev-parse', 'HEAD'] and failure == 'head':
                    return '2' * 40
                if args[:2] == ['git', 'status'] and failure == 'dirty':
                    return ' M tracked-file'
                return self.fake_run(args, root, capture)
            with self.subTest(failure=failure), mock.patch.object(package, 'run', side_effect=result), \
                    self.assertRaises(package.Failure):
                package.validate_source(self.repo, VERSION, COMMIT)

    def test_output_inside_source_is_rejected(self):
        with self.assertRaisesRegex(package.Failure, '源码工作区之外'):
            package.package_release(self.repo, VERSION, COMMIT, self.repo / 'dist')

    def test_inspect_wrong_platform_is_rejected(self):
        self.inspect_arch = 'arm64'
        with self.assertRaisesRegex(package.Failure, '镜像平台不匹配'):
            self.build()
        self.assertFalse((self.output / VERSION).exists())

    def test_archive_wrong_revision_is_rejected(self):
        self.archive_commit = '2' * 40
        with self.assertRaisesRegex(package.Failure, '镜像标签不匹配'):
            self.build()
        self.assertFalse((self.output / VERSION).exists())

    def test_archive_wrong_platform_is_rejected(self):
        self.archive_arch = 'arm64'
        with self.assertRaisesRegex(package.Failure, '镜像平台不匹配'):
            self.build()

    def test_both_config_paths_supply_dual_accepted_ids(self):
        for style in ('oci', 'classic'):
            self.config_style = style
            directory = self.build(VERSION + '-' + style)
            manifest = json.loads((directory / package.MANIFEST).read_text(encoding='utf-8'))
            for image in manifest['images'].values():
                self.assertEqual(len(image['accepted_ids']), 2)
                self.assertTrue(all(re.fullmatch(r'sha256:[0-9a-f]{64}', item) for item in image['accepted_ids']))

    def consume(self, directory, expectation=None):
        release = json.loads((directory / 'release.json').read_text(encoding='utf-8'))
        state = self.home / 'consumer-state'
        state.mkdir()
        with mock.patch.object(consumer, 'STATE', state), \
                mock.patch.object(consumer, 'CANDIDATES', release['candidates']), \
                mock.patch.object(consumer.subprocess, 'Popen', side_effect=LoadBoundary) as load:
            if expectation is None:
                with self.assertRaises(LoadBoundary):
                    consumer.load_bundle(('--bundle-dir', str(directory)))
                load.assert_called_once()
                self.assertEqual(load.call_args.args[0], ['docker', 'load'])
            else:
                with self.assertRaisesRegex(consumer.Failure, expectation):
                    consumer.load_bundle(('--bundle-dir', str(directory)))
                load.assert_not_called()

    def test_real_consumer_reaches_docker_load_boundary(self):
        self.consume(self.build())

    def test_real_consumer_rejects_wrong_revision(self):
        directory = self.build()
        path = directory / package.MANIFEST
        manifest = json.loads(path.read_text(encoding='utf-8'))
        manifest['images']['cpa']['revision'] = '2' * 40
        package.write_json(path, manifest)
        self.consume(directory, 'manifest_candidate')

    def test_real_consumer_rejects_wrong_accepted_ids(self):
        directory = self.build()
        path = directory / package.MANIFEST
        manifest = json.loads(path.read_text(encoding='utf-8'))
        manifest['images']['worker']['accepted_ids'] = ['sha256:' + '0' * 64]
        package.write_json(path, manifest)
        self.consume(directory, 'manifest_candidate')

    def test_real_consumer_rejects_wrong_archive_digest(self):
        directory = self.build()
        path = directory / package.MANIFEST
        manifest = json.loads(path.read_text(encoding='utf-8'))
        manifest['archive']['sha256'] = '0' * 64
        package.write_json(path, manifest)
        self.consume(directory, 'archive_digest')

    def publish(self, directory, fault=None):
        calls = []
        expected = sorted(path.name for path in directory.iterdir())
        def request(req):
            method = req.get_method()
            suffix = req.full_url.split('/repos/owner/repository', 1)[1]
            calls.append((method, suffix))
            if method == 'POST' and suffix == '/git/refs':
                if fault == 'existing_tag':
                    raise urllib.error.HTTPError(req.full_url, 422, 'tag exists', {}, None)
                result = {'ref': 'refs/tags/' + VERSION}
            elif method == 'POST' and suffix == '/releases':
                body = json.loads(req.data)
                self.assertTrue(body['draft'])
                result = {'id': 123, 'draft': True}
            elif method == 'GET' and suffix == '/releases/123':
                assets = [{'name': name, 'size': (directory / name).stat().st_size, 'state': 'uploaded'} for name in expected]
                if fault == 'missing_asset':
                    assets.pop()
                elif fault == 'wrong_size':
                    assets[0]['size'] += 1
                elif fault == 'upload_pending':
                    assets[0]['state'] = 'starter'
                result = {'assets': assets}
            elif method == 'GET' and suffix.startswith('/git/ref/tags/'):
                result = {'object': {'sha': '2' * 40 if fault == 'wrong_tag' else COMMIT}}
            elif method == 'PATCH' and suffix == '/releases/123':
                result = {'draft': False, 'html_url': 'https://example.invalid/release'}
            else:
                raise AssertionError((method, suffix))
            return io.BytesIO(json.dumps(result).encode())
        def upload(args, **kwargs):
            self.assertEqual(args[:3], ['gh', 'release', 'upload'])
            self.assertNotIn('--clobber', args)
            if fault == 'upload_failed':
                raise subprocess.CalledProcessError(1, args)
            return subprocess.CompletedProcess(args, 0)
        environment = {'VERSION': VERSION, 'COMMIT': COMMIT, 'RUNNER_TEMP': str(self.home),
                       'GITHUB_REPOSITORY': 'owner/repository', 'GITHUB_API_URL': 'https://api.example.invalid',
                       'GH_TOKEN': 'test-token'}
        with mock.patch.dict(os.environ, environment), \
                mock.patch('urllib.request.urlopen', side_effect=request), \
                mock.patch('subprocess.run', side_effect=upload), \
                contextlib.redirect_stdout(io.StringIO()):
            if fault:
                with self.assertRaises((SystemExit, subprocess.CalledProcessError, urllib.error.HTTPError)):
                    exec(compile(workflow_python()[1], str(WORKFLOW), 'exec'), {})
            else:
                exec(compile(workflow_python()[1], str(WORKFLOW), 'exec'), {})
        return calls

    def test_publish_only_after_all_remote_assets_match(self):
        calls = self.publish(self.build())
        self.assertEqual(calls[-1], ('PATCH', '/releases/123'))
        self.assertEqual(calls[0], ('POST', '/git/refs'))

    def test_publish_faults_never_publish_draft(self):
        directory = self.build()
        for fault in ('existing_tag', 'missing_asset', 'wrong_size', 'upload_pending', 'wrong_tag', 'upload_failed'):
            with self.subTest(fault=fault):
                calls = self.publish(directory, fault)
                self.assertFalse(any(method == 'PATCH' for method, _ in calls))

    def test_modified_local_asset_fails_before_tag_creation(self):
        directory = self.build()
        with (directory / 'kilo-release.sh').open('a', encoding='utf-8') as stream:
            stream.write('# changed\n')
        calls = self.publish(directory, 'local_asset_changed')
        self.assertEqual(calls, [])


class SourceContractTests(unittest.TestCase):
    def test_dockerfile_toolchain_and_embedded_web_contract(self):
        cpa = (DEPLOY.parent / 'components/cpa/Dockerfile').read_text(encoding='utf-8')
        self.assertIn('FROM golang:1.26.10-bookworm AS builder', cpa)
        self.assertIn('ENV GOTOOLCHAIN=local', cpa)
        self.assertIn('CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -buildvcs=false', cpa)
        self.assertIn('build-essential git', cpa)
        manager = (DEPLOY.parent / 'components/cpamp/Dockerfile.manager-server').read_text(encoding='utf-8')
        self.assertLess(manager.index('COPY apps/docs/package.json'), manager.index('RUN npm ci'))
        for asset in ('index.html', 'favicon.ico', 'apple-touch-icon.png'):
            self.assertIn('COPY --from=web-build /app/apps/web/dist/' + asset, manager)

    def test_workflow_uses_manual_standard_runner_without_cache_or_deploy(self):
        workflow = WORKFLOW.read_text(encoding='utf-8')
        self.assertIn('workflow_dispatch:', workflow)
        self.assertIn("if: github.ref == 'refs/heads/main'", workflow)
        self.assertEqual(workflow.count('runs-on: ubuntu-24.04'), 2)
        self.assertEqual(workflow.count('contents: write'), 1)
        self.assertEqual(workflow.count('persist-credentials: false'), 2)
        for forbidden in ('actions/cache', 'upload-artifact', 'download-artifact', '--clobber',
                          '127.0.0.1', 'ssh ', 'schedule:', '  push:'):
            self.assertNotIn(forbidden, workflow)
        blocks = workflow_python()
        self.assertEqual(len(blocks), 2)
        for block in blocks:
            compile(block, str(WORKFLOW), 'exec')


if __name__ == '__main__':
    unittest.main(verbosity=2)
