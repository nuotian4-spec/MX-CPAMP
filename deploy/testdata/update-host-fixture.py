#!/usr/bin/env python3
"""联合专项夹具：真实宿主进程/Unix HTTP/updater，合成 Docker 与发行边界。"""

import importlib.util
import json
import os
import pathlib
import socketserver
import subprocess
import sys
import time
import types
from unittest import mock

DEPLOY = pathlib.Path(__file__).resolve().parents[1]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


f = load('joint_updater_fixtures', DEPLOY / 'test_mx_update.py')
h = load('joint_host_product', DEPLOY / 'mx-update-host.py')
u = h.updater


def runtime(b, site):
    pointer = u.read(site['active_release_pointer']) if pathlib.Path(site['active_release_pointer']).exists() else None
    return {'version': pointer['version'] if pointer else '', 'pointer': pointer,
            'services': {name: {'id': row['Id'], 'image': row['Image'], 'running': row['State']['Running'],
                         'compose_files': row['Config']['Labels']['com.docker.compose.project.config_files'].split(',')}
                         for name, row in b.live.items()}}


def make_host(root):
    metadata = u.read(root / 'fixture.json')
    site = metadata['site']
    release = metadata['release']
    b = f.UpdateDocker(metadata['backend_spec'], root)

    def fetch(url):
        if url.endswith('/releases?per_page=20'):
            return [{'tag_name': release['version'], 'body': '离线合成固定发行'}]
        if url.endswith('/releases/latest'):
            return {'tag_name': 'v8.0.73'}
        return release

    def fetch_local(site, version, destination):
        deadline = time.monotonic() + 15
        while not (root / 'continue').exists():
            if time.monotonic() > deadline:
                raise RuntimeError('联合专项未释放维护门闩')
            time.sleep(0.005)
        folder = destination / 'materials'
        f.materials(folder, b.images)
        u.write(folder / 'release.json', release)
        return folder, release

    u.fetch_release = fetch_local

    def launch(operation_id):
        with (root / 'worker.log').open('ab') as output:
            process = subprocess.Popen([sys.executable, str(pathlib.Path(__file__).resolve()), 'worker', str(root), operation_id],
                                       stdout=output, stderr=output, start_new_session=True)
        u.write(root / 'worker-process.json', {'pid': process.pid, 'daemon_pid': os.getpid()})

    def runner(site_path, site, version, state, **kwargs):
        def factory(spec, backend):
            b.spec = spec
            return f.k.Release(spec, backend)
        library = types.SimpleNamespace(backend=lambda spec: b, Release=factory, read=f.k.read,
                                        scope_keys=f.k.scope_keys, digest=f.k.digest)
        loader = lambda name, path: library if name == 'mx_update_kilo' else f.c
        with mock.patch.object(f.k, '__file__', str(state / 'materials/kilo-release.py')), mock.patch.object(f.k.time, 'sleep'):
            pointer = u.run_cycle(site_path, site, version, state, loader=loader, **kwargs)
        u.write(root / 'runtime.json', runtime(b, site))
        u.write(root / 'updater-proof.json', {'worker_pid': os.getpid(), 'stage': (state / 'stage').read_text().strip(),
                'backup_exists': (state / 'backup/data.tar.gz').is_file(), 'original_caddy_id': b.live['caddy']['Id'],
                'boundary': 'synthetic Docker and release transport; real updater lifecycle and filesystem'})
        return pointer

    return h.UpdateHost(root / 'site.json', fetcher=fetch, inspector=lambda site: u.read(root / 'runtime.json'),
                        launcher=launch, runner=runner)


def serve(ready):
    fixture = f.UpdateTests()
    fixture.setUp()
    root, site, b = fixture.root, fixture.site, fixture.b
    site['release_source'] = 'https://github.com/synthetic-fixture/mx/releases/download'
    site['update_state_dir'] = str(root / '.mx-update-state')
    u.write(root / 'site.json', site)
    release = f.materials(root / 'seed-materials', b.images)
    release['assets'] = {name: release['assets'].get(name, {'size': 1, 'sha256': '0' * 64}) for name in u.ASSETS}
    u.write(root / 'fixture.json', {'site': site, 'release': release, 'backend_spec': b.spec})
    u.write(root / 'runtime.json', runtime(b, site))
    host = make_host(root)
    socket = root / 'fixture.sock'
    with socketserver.UnixStreamServer(str(socket), h.Handler) as server:
        server.update_host = host
        u.write(ready, {'root': str(root), 'socket': str(socket), 'site_id': site['project'], 'pid': os.getpid()})
        server.serve_forever()


if __name__ == '__main__':
    os.umask(0o077)
    if sys.argv[1] == 'serve':
        serve(pathlib.Path(sys.argv[2]))
    else:
        make_host(pathlib.Path(sys.argv[2])).run(sys.argv[3])
