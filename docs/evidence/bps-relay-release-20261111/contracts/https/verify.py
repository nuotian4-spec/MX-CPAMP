"""原 ImageRelay 库与真实 Caddy TLS 的离线接线专项。"""
import json
import pathlib
import shutil
import subprocess
import tempfile
import time
import uuid

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[4]
CPA = ROOT / 'components/cpa'
PREFIX = 'bps-https-' + uuid.uuid4().hex[:10]
BUILD = 'sha256:75195664cd12258c72160cf4121c931ae4176719d6545a9e5f35fabfe2abbc55'
CADDY = 'sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d'
source_dir = CPA / 'cmd' / ('https_fixture_' + PREFIX.replace('-', '_'))
fixture = PREFIX + '-relay'
caddy = PREFIX + '-caddy'
network = PREFIX + '-network'


def run(args, check=True):
    result = subprocess.run(args, capture_output=True, timeout=120)
    if check: assert result.returncode == 0, '离线专项命令失败'
    return result


def wait_file(path):
    for _ in range(100):
        if path.exists(): return
        time.sleep(.1)
    raise AssertionError('离线测试回执未就绪')


try:
    assert not source_dir.exists()
    source_dir.mkdir()
    shutil.copyfile(HERE / 'main.go', source_dir / 'main.go')
    with tempfile.TemporaryDirectory(prefix=PREFIX) as temporary:
        exchange = pathlib.Path(temporary)
        compiled = run(['docker', 'run', '--rm', '--pull', 'never', '--network', 'none', '--mount', 'type=bind,source=' + str(CPA) + ',target=/app,readonly', '--mount', 'type=bind,source=' + str(exchange) + ',target=/exchange', '-w', '/app', '-e', 'GOTOOLCHAIN=local', '-e', 'GOPROXY=off', '-e', 'GOSUMDB=off', '--entrypoint', 'go', BUILD, 'build', '-o', '/exchange/fixture', './cmd/' + source_dir.name])
        print('actual_library_fixture_offline_compile=passed exit=0', flush=True)
        run(['docker', 'network', 'create', '--internal', network])
        run(['docker', 'run', '-d', '--pull', 'never', '--network', network, '--network-alias', 'relay-fixture', '--name', fixture, '--mount', 'type=bind,source=' + str(exchange) + ',target=/exchange', '--entrypoint', '/exchange/fixture', BUILD])
        wait_file(exchange / 'ready.json')
        manifest = json.loads((exchange / 'ready.json').read_text())
        config = '''{
 auto_https disable_redirects
}
https://relay.example.com:8443 {
 tls internal
 @relay {
  method GET HEAD
  path /api/bps-images/*
 }
 route {
  handle @relay {
   reverse_proxy relay-fixture:8317
  }
  handle {
   respond 404
  }
 }
}
'''
        (exchange / 'Caddyfile').write_text(config, encoding='utf-8', newline='\n')
        run(['docker', 'run', '-d', '--pull', 'never', '--network', network, '--network-alias', 'relay.example.com', '--name', caddy, '--mount', 'type=bind,source=' + str(exchange / 'Caddyfile') + ',target=/etc/caddy/Caddyfile,readonly', CADDY])
        root_ca = exchange / 'root.crt'
        for _ in range(100):
            copied = run(['docker', 'cp', caddy + ':/data/caddy/pki/authorities/local/root.crt', str(root_ca)], check=False)
            if copied.returncode == 0: break
            time.sleep(.1)
        assert root_ca.exists()
        client = run(['docker', 'run', '--rm', '--pull', 'never', '--network', network, '--mount', 'type=bind,source=' + str(exchange) + ',target=/exchange,readonly', '--mount', 'type=bind,source=' + str(HERE) + ',target=/checks,readonly', '--entrypoint', 'python3', 'sha256:7984cf56ceaddebe4fefeb75d64000b9f45d8b77b11a09b24524da6cdc3af0c8', '/checks/client.py'])
        print(client.stdout.decode(), end='', flush=True)
        run(['docker', 'kill', '--signal', 'TERM', fixture])
        exit_code = run(['docker', 'wait', fixture]).stdout.decode().strip()
        assert exit_code == '0'
        wait_file(exchange / 'counts.json')
        counts = json.loads((exchange / 'counts.json').read_text())
        assert counts == {'GET_valid': 1, 'HEAD_valid': 1, 'GET_unknown': 2, 'GET_directory': 1}
        print('post_delete_options_state_management_not_forwarded=passed', flush=True)
        print('actual_relay_received_get_valid=1 head_valid=1 get_unknown=2 get_directory=1 other=0', flush=True)
        print('fixture_process_exit=0', flush=True)
finally:
    for name in [caddy, fixture]: run(['docker', 'rm', '-fv', name], check=False)
    run(['docker', 'network', 'rm', network], check=False)
    if source_dir.exists():
        assert source_dir.resolve().parent == (CPA / 'cmd').resolve()
        assert source_dir.name.startswith('https_fixture_bps_https_')
        shutil.rmtree(source_dir)
    assert not run(['git', '-C', str(ROOT), 'status', '--porcelain', '--', 'components/cpa']).stdout.strip()
    assert not run(['docker', 'ps', '-aq', '--filter', 'name=' + PREFIX]).stdout.strip()
    assert not run(['docker', 'network', 'ls', '-q', '--filter', 'name=' + network]).stdout.strip()
print('owned_containers_network_temporary_sources_and_ca_cleanup=verified')
print('product_git_diff_and_status=clean')
print('official_model_requests=0 production_requests=0 external_domain_connections=0')
print('exit=0')
