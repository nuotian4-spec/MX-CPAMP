"""只验证本轮 Caddy 摘要和交付原文；固定缓存镜像，不访问生产。"""
import ast
import copy
import json
import pathlib
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[3]
DELIVERY = ROOT / 'deploy/phase2-readonly-preflight.sh'
BASE = 'db97e37f177a9223130d95035cda57ef831b112d'
SENTINEL = 'SECRET_SENTINEL_CADDY_20261111'
IMAGE = 'caddy:2.10.2-alpine'
SHELL_IMAGE = 'postgres:17-alpine'


def command(args, data=None, clean_stderr=False):
    result = subprocess.run(args, input=data.encode('utf-8') if data is not None else None, capture_output=True)
    assert result.returncode == 0, '专项命令退出非零'
    if clean_stderr: assert not result.stderr, '专项命令存在标准错误输出'
    return result.stdout.decode('utf-8')


def embedded(source):
    return source.split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0]


source = DELIVERY.read_text(encoding='utf-8').replace('\r\n', '\n')
tree = ast.parse(embedded(source))
summary = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'caddy_skeleton')
namespace = {}
exec(compile(ast.Module(body=[summary], type_ignores=[]), str(DELIVERY), 'exec'), namespace)
summarize = namespace['caddy_skeleton']

# 除纯函数与一处 emit 扩展外，全部预检逻辑与旧证据输入相同。
baseline = command(['git', 'show', BASE + ':deploy/phase2-readonly-preflight.sh'])
unchanged = copy.deepcopy(tree)
unchanged.body = [node for node in unchanged.body if not (isinstance(node, ast.FunctionDef) and node.name == 'caddy_skeleton')]
for node in ast.walk(unchanged):
    if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == 'emit' and node.args and isinstance(node.args[0], ast.Constant) and node.args[0].value == 'caddy_routes':
        node.keywords = [kw for kw in node.keywords if kw.arg is not None]
assert ast.dump(unchanged) == ast.dump(ast.parse(embedded(baseline)))
print('unchanged_preflight_io_auth_activity_ast=passed')

guard = [False]


def deny_io(event, args):
    if guard[0] and (event == 'open' or event.startswith(('socket.', 'subprocess.', 'os.remove', 'os.rename', 'os.mkdir', 'os.rmdir', 'os.chmod'))):
        raise AssertionError('摘要函数触发 I/O')


sys.addaudithook(deny_io)


def observed(adapted):
    guard[0] = True
    try:
        result = summarize(adapted)
    finally:
        guard[0] = False
    encoded = json.dumps(result, ensure_ascii=True, separators=(',', ':'))
    assert SENTINEL not in encoded and 'private.invalid' not in encoded
    return result


def adapted(config):
    return json.loads(command(['docker', 'run', '--rm', '-i', '--pull', 'never', '--network', 'none', IMAGE, 'caddy', 'adapt', '--config', '/dev/stdin', '--adapter', 'caddyfile'], config))


relay = '''handle @relay {
 reverse_proxy cli-proxy-api:8317
}'''
api = '''handle /api/* {
 reverse_proxy cpa-manager-plus:18317
}'''
v1 = '''handle /v1/* {
 reverse_proxy cli-proxy-api:8317
}'''


def config(handles):
    return ''':8080 {
 @relay {
  path /api/bps-images/*
  method GET HEAD
 }
 route {
''' + '\n'.join(handles) + '\n }\n}\n'


def routes(result):
    found = []

    def visit(value):
        if isinstance(value, dict):
            if 'match' in value and 'handle' in value:
                found.append(value)
            for child in value.values():
                visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)
    visit(result)
    return found


examples = []
for name, handles, expected in [('relay_before_api', [relay, api, v1], ['relay', 'api', 'v1']), ('relay_after_api', [api, relay, v1], ['api', 'relay', 'v1'])]:
    result = observed(adapted(config(handles)))
    matched = [route for route in routes(result) if route['match'] and route['match'][0].get('paths') != ['any']]
    assert [route['match'][0]['paths'][0] for route in matched] == expected
    assert next(route for route in matched if route['match'][0]['paths'] == ['relay'])['match'][0]['methods'] == ['GET', 'HEAD']
    assert len({route['group'] for route in matched}) == 1 and isinstance(matched[0]['group'], int)
    assert result['skeleton_status'] == 'observed' and result['skeleton_truncated'] is False
    all_targets = [target for route in routes(result) for handler in route['handle'] for target in handler.get('targets', [])]
    assert all_targets == ['cpa', 'manager', 'cpa'] if name == 'relay_before_api' else all_targets == ['manager', 'cpa', 'cpa']
    examples.append(dict(case=name, **result))
print('real_caddy_adapt_before_after_order_get_head_targets_groups=passed')

unknown_config = '''http://private.invalid:8080 {
 @relay {
  path /api/bps-images/*
  method GET HEAD
  header X-Secret SECRET
  query token=SECRET
 }
 route {
  handle @relay {
   rewrite * /SECRET
   header Authorization SECRET
   reverse_proxy SECRET:8317
  }
  handle /api/* {
   reverse_proxy cpa-manager-plus:18317
  }
 }
}
'''.replace('SECRET', SENTINEL)
unknown_result = observed(adapted(unknown_config))
unknown_routes = routes(unknown_result)
assert unknown_result['skeleton_status'] == 'unknown'
assert any(route['terminal'] is True for route in unknown_routes)
assert any(matcher.get('other_matchers') == 'unknown' for route in unknown_routes for matcher in route['match'])
assert any(handler.get('type') == 'rewrite' and handler.get('details') == 'unknown' for route in unknown_routes for handler in route['handle'])
assert any(handler.get('targets') == ['unknown'] for route in unknown_routes for handler in route['handle'])
examples.append(dict(case='nested_unknown_matcher_rewrite_target', **unknown_result))
print('real_caddy_adapt_nested_terminal_unknown_matcher_rewrite=passed')
print('host_header_query_rewrite_upstream_secret_sentinel_redaction=passed')

unknown_shape = {'apps': {'http': {'servers': {'SECRET': {'routes': [{'match': [{'path': [SENTINEL], 'method': ['POST'], 'expression': SENTINEL}], 'handle': [{'handler': SENTINEL}], 'group': SENTINEL, 'terminal': SENTINEL}]}}}}}
assert observed(unknown_shape)['skeleton_status'] == 'unknown'
assert observed({'apps': []})['skeleton_status'] == 'unknown'
oversized = {'apps': {'http': {'servers': {'srv': {'routes': [{}] * 200}}}}}
assert observed(oversized)['skeleton_truncated'] is True
deep = {'handle': []}
for _ in range(20):
    deep = {'handle': [{'handler': 'subroute', 'routes': [deep]}]}
assert observed({'apps': {'http': {'servers': {'srv': {'routes': [deep]}}}}})['skeleton_truncated'] is True
print('unknown_types_paths_methods_and_size_depth_bounds=passed')
print('summary_audit_no_io_no_http_no_writes=passed')

# 检查用户实际粘贴的外层原文；仅 bash -n，不运行预检。
wrapper = "sudo bash <<'CPA_PREFLIGHT_READONLY_20261111'\n" + source + '\nCPA_PREFLIGHT_READONLY_20261111\n'
command(['docker', 'run', '--rm', '-i', '--pull', 'never', '--network', 'none', '--entrypoint', 'bash', SHELL_IMAGE, '-n'], wrapper, clean_stderr=True)
print('linux_outer_heredoc_bash_n=passed')
command(['docker', 'run', '--rm', '-i', '--pull', 'never', '--network', 'none', '--entrypoint', 'bash', SHELL_IMAGE, '-n'], source, clean_stderr=True)
print('linux_delivery_bash_n=passed')
for image in [IMAGE, SHELL_IMAGE]:
    image_id = command(['docker', 'image', 'inspect', image, '--format', '{{.Id}}']).strip()
    print('cached_image=' + image + ' id=' + image_id)
(HERE / 'caddy-skeleton.example.jsonl').write_text(''.join(json.dumps(example, ensure_ascii=True, separators=(',', ':')) + '\n' for example in examples), encoding='utf-8')
print('preflight_execution_count=0')
print('production_requests=0')
print('http_post_count=0')
print('exit=0')
