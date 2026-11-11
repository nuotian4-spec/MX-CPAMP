"""固定候选与离线配置契约专项；不访问官方或生产。"""
import ast
import copy
import json
import os
import pathlib
import posixpath
import subprocess
import sys
import tempfile
import time
import types
import uuid

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[3]
BASE = 'bab1afb07e3cf25e78394b6cb959aaa5999b31bd'
IMAGE = 'mx-cpa:bps-quota-identity-20261111'
REVISION = 'b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c'
KEY = 'SYNTHETIC_BPS_CONTRACT_20261111'
rows = []
CANDIDATE_ONLY = '--candidate-only' in sys.argv


def command(args, data=None, check=True):
    result = subprocess.run(args, input=data.encode() if data is not None else None, capture_output=True, timeout=180)
    if check: assert result.returncode == 0, '专项命令退出非零'
    return result


def case(name, root=None, canonical=None, expected=None, **extra):
    cfg = {'config-version': 8, 'server': {'port': 8317}, 'management': {'disable-control-panel': True, 'disable-auto-update-panel': True}, 'access': {'api-keys': [KEY]}, 'oauth': {'auth-dir': '/fixture/auth'}}
    if root is not None: cfg['bps-protocol'] = root
    if canonical is not None: cfg['upstream'] = {'bps': {'protocol': canonical}}
    cfg.update(extra)
    result = {'name': name, 'config': cfg, 'expected': dict(image_transport='native', image_relay_base_url='', enabled=True, images_enabled=True, state='/fixture/bps-state')}
    result['expected'].update(expected or {})
    return result


cases = [
    case('root', {'image-transport': 'relay', 'image-relay-base-url': 'https://root.example.invalid', 'state-dir': '/fixture/root-state', 'images-enabled': False}, expected={'image_transport': 'relay', 'image_relay_base_url': 'https://root.example.invalid', 'state': '/fixture/root-state', 'images_enabled': False}),
    case('v8', canonical={'image-transport': 'relay', 'image-relay-base-url': 'https://v8.example.invalid', 'state-dir': '/fixture/v8-state', 'enabled': False}, expected={'image_transport': 'relay', 'image_relay_base_url': 'https://v8.example.invalid', 'state': '/fixture/v8-state', 'enabled': False}),
    case('mixed', {'image-transport': 'native', 'image-relay-base-url': 'https://root.example.invalid', 'state-dir': '/fixture/root-state', 'enabled': True, 'images-enabled': False}, {'image-transport': 'relay', 'image-relay-base-url': 'https://v8.example.invalid', 'enabled': False}, expected={'image_transport': 'relay', 'image_relay_base_url': 'https://v8.example.invalid', 'state': '/fixture/root-state', 'enabled': False, 'images_enabled': False}),
    case('null_leaves', {'image-transport': 'relay', 'image-relay-base-url': 'https://root.example.invalid', 'state-dir': '/fixture/root-state', 'enabled': False, 'images-enabled': False}, {key: None for key in ['image-transport', 'image-relay-base-url', 'state-dir', 'enabled', 'images-enabled']}),
    case('empty_leaves', {'image-transport': 'relay', 'image-relay-base-url': 'https://root.example.invalid', 'state-dir': '/fixture/root-state'}, {'image-transport': '', 'image-relay-base-url': '', 'state-dir': ''}),
    case('trailing_slash', canonical={'image-transport': 'relay', 'image-relay-base-url': 'https://v8.example.invalid/'}, expected={'image_transport': 'relay', 'image_relay_base_url': 'https://v8.example.invalid/'}),
    case('relative_state', canonical={'state-dir': ' ./private/../state '}, expected={'state': '@CWD@/state'}),
    case('home_state', canonical={'state-dir': ' ~/private-state '}, expected={'state': '@HOME@/private-state'}),
    case('auth_priority', expected={'state': '/fixture/v8/bps-state'}, **{'auth-dir': '/fixture/root/auth', 'oauth': {'auth-dir': '/fixture/v8/auth'}}),
    case('auth_null', expected={'state': '@HOME@/bps-state'}, **{'auth-dir': '/fixture/root/auth', 'oauth': {'auth-dir': None}}),
    case('root_null', **{'bps-protocol': None}),
    dict(case('parent_null', **{'upstream': {'bps': {'protocol': None}}}), reject=True),
]

source = (ROOT / 'deploy/phase2-readonly-preflight.sh').read_text(encoding='utf-8').replace('\r\n', '\n')


def embedded(text):
    return text.split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0]


tree = ast.parse(embedded(source))
helpers = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in ['effective_bps_protocol', 'bps_container_path', 'host_path', 'safe']]
main = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'main')
start = next(i for i, node in enumerate(main.body) if isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name) and node.targets[0].id == 'oauth')
end = next(i for i, node in enumerate(main.body) if isinstance(node, ast.Expr) and isinstance(node.value, ast.Call) and isinstance(node.value.func, ast.Name) and node.value.func.id == 'emit' and node.value.args and isinstance(node.value.args[0], ast.Constant) and node.value.args[0].value == 'bps_state')
ns = {'os': types.SimpleNamespace(path=posixpath), 're': __import__('re'), 'config': True, 'path_info': lambda path: {'exists': 'unknown'}, 'emit': lambda kind, **kw: rows.append(dict(kind=kind, **kw))}
exec(compile(ast.Module(body=helpers, type_ignores=[]), 'preflight', 'exec'), ns)
block = compile(ast.Module(body=main.body[start:end + 1], type_ignores=[]), 'preflight-bps-block', 'exec')
container = {'Config': {'WorkingDir': '/CLIProxyAPI', 'User': '', 'Env': []}, 'Mounts': [{'Type': 'bind', 'Source': '/fixture-host', 'Destination': '/fixture'}]}
for tc in ([] if CANDIDATE_ONLY else cases):
    ns.update(cfg=copy.deepcopy(tc['config']), cpa=container)
    exec(block, ns)
    row = rows[-1]
    if tc.get('reject'):
        assert row['transport'] == 'unknown' and row['relay_origin_configured'] is None and row['container_path'] == 'unknown'
    else:
        exp = tc['expected']
        assert row['transport'] == exp['image_transport'] and row['relay_origin_configured'] == bool(exp['image_relay_base_url'])
        assert row['container_path'] == exp['state'].replace('@CWD@', '/CLIProxyAPI').replace('@HOME@', '/root')
    assert KEY not in json.dumps(row)
    row['case'] = tc['name']
if not CANDIDATE_ONLY: print('preflight_effective_root_v8_mixed_null_empty_auth_home_cwd=passed')

# 仅替换 BPS 取值片段；其他 I/O、认证、活动统计与已审基线保持 AST 等价。
baseline = ast.parse(embedded(command(['git', 'show', BASE + ':deploy/phase2-readonly-preflight.sh']).stdout.decode()))
base_main = next(node for node in baseline.body if isinstance(node, ast.FunctionDef) and node.name == 'main')
base_start = next(i for i, node in enumerate(base_main.body) if isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name) and node.targets[0].id == 'auth_path')
base_end = next(i for i, node in enumerate(base_main.body) if isinstance(node, ast.Expr) and isinstance(node.value, ast.Call) and isinstance(node.value.func, ast.Name) and node.value.func.id == 'emit' and node.value.args and isinstance(node.value.args[0], ast.Constant) and node.value.args[0].value == 'bps_state')
unchanged = copy.deepcopy(tree)
unchanged.body = [node for node in unchanged.body if not (isinstance(node, ast.FunctionDef) and node.name in ['effective_bps_protocol', 'bps_container_path'])]
new_main = next(node for node in unchanged.body if isinstance(node, ast.FunctionDef) and node.name == 'main')
new_main.body[start:end + 1] = copy.deepcopy(base_main.body[base_start:base_end + 1])
if not CANDIDATE_ONLY:
    assert ast.dump(unchanged) == ast.dump(baseline)
    print('unchanged_other_preflight_auth_io_activity_ast=passed')
if '--preflight-only' in sys.argv:
    (HERE / 'preflight.example.jsonl').write_text(''.join(json.dumps(row, ensure_ascii=True, separators=(',', ':')) + '\n' for row in rows), encoding='utf-8')
    print('preflight_requests=0 writes=0 post_requests=0')
    print('exit=0')
    sys.exit(0)

info = json.loads(command(['docker', 'image', 'inspect', IMAGE]).stdout)[0]
assert info['Config']['Labels']['org.opencontainers.image.revision'] == REVISION
print('candidate_image=' + info['Id'] + ' revision=' + REVISION)

prefix = 'bps-contract-' + uuid.uuid4().hex[:10]
with tempfile.TemporaryDirectory(prefix=prefix) as temporary:
    temporary = pathlib.Path(temporary)
    (temporary / 'cases.json').write_text(json.dumps(cases), encoding='utf-8')
    (temporary / 'overlay.json').write_text(json.dumps({'Replace': {'/app/internal/config/zz_bps_relay_contract_test.go': '/evidence/config_contract_test.go'}}), encoding='utf-8')
    if not CANDIDATE_ONLY:
        loader = command(['docker', 'run', '--rm', '--pull', 'never', '--network', 'none', '--mount', 'type=bind,source=' + str(ROOT / 'components/cpa') + ',target=/app,readonly', '--mount', 'type=bind,source=' + str(HERE) + ',target=/evidence,readonly', '--mount', 'type=bind,source=' + str(temporary) + ',target=/checks,readonly', '-w', '/app', '-e', 'GOTOOLCHAIN=local', '-e', 'GOPROXY=off', '-e', 'GOSUMDB=off', '--entrypoint', 'go', 'mx-cpa-buildenv:bps-tools-images-20261111', 'test', '-overlay', '/checks/overlay.json', './internal/config', '-run', '^TestBPSRelayEffectiveContract$', '-count=1', '-v'])
        (HERE / 'loader.log').write_text(loader.stdout.decode(), encoding='utf-8')
        print('actual_linux_config_loader_and_state_directory_12_cases=passed')
    else:
        assert 'PASS' in (HERE / 'loader.log').read_text(encoding='utf-8')
        print('config_loader_and_preflight_prior_pass=reused')
    for tc in cases:
        if tc.get('reject') or tc['name'] not in ['root', 'v8', 'mixed', 'null_leaves', 'trailing_slash']:
            continue
        name = prefix + '-' + tc['name']
        config_file = temporary / (tc['name'] + '.json')
        config_file.write_text(json.dumps(tc['config']), encoding='utf-8')
        try:
            command(['docker', 'run', '-d', '--pull', 'never', '--network', 'none', '--name', name, '--mount', 'type=bind,source=' + str(config_file) + ',target=/CLIProxyAPI/config.yaml', '-e', 'MANAGEMENT_PASSWORD=' + KEY, IMAGE, './CLIProxyAPI', '--config', '/CLIProxyAPI/config.yaml', '--local-model'])
            client = '''import json,sys,time,urllib.request,urllib.error
key=sys.stdin.read()
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for attempt in range(40):
 try:
  request=urllib.request.Request('http://127.0.0.1:8317/v8/management/bps/settings',headers={'Authorization':'Bearer '+key})
  with opener.open(request,timeout=2) as response:
   value=json.load(response)
   print(json.dumps({'status':response.status,'body':{field:value[field] for field in ['image_transport','image_relay_base_url','enabled','images_enabled']}}));break
 except urllib.error.HTTPError as error:
  print(json.dumps({'status':error.code}));sys.exit(1)
 except (OSError,urllib.error.URLError):time.sleep(.15)
else:sys.exit(2)
'''
            request = command(['docker', 'run', '--rm', '-i', '--pull', 'never', '--network', 'container:' + name, '--entrypoint', 'python3', 'python:3.13-alpine', '-c', client], KEY)
            response = json.loads(request.stdout)
            assert response['status'] == 200
            result = response['body']
            expected = tc['expected']
            for field in ['image_transport', 'image_relay_base_url', 'enabled', 'images_enabled']:
                assert result[field] == expected[field], '候选 Settings 字段不符'
            assert 'state-dir' not in result and 'state_dir' not in result
            print('candidate_startup_settings_get_' + tc['name'] + '=passed http_status=200')
        finally:
            command(['docker', 'rm', '-f', name], check=False)
    assert not command(['docker', 'ps', '-aq', '--filter', 'name=' + prefix]).stdout.strip()
print('fixture_cleanup=verified')
if rows: (HERE / 'preflight.example.jsonl').write_text(''.join(json.dumps(row, ensure_ascii=True, separators=(',', ':')) + '\n' for row in rows), encoding='utf-8')
print('preflight_requests=0 model_requests=0 external_network_success=0 production_requests=0')
print('exit=0')
