"""只加载真实投影函数，验证未批准字段与秘密字符串不会进入输出。"""
import ast
import json
from pathlib import Path

source = Path(__file__).with_name('worker.py').read_text(encoding='utf-8')
tree = ast.parse(source)
names = {'NUMBERS', 'BOOLEANS', 'ROOT_KEYS', 'AUTH_KEYS', 'WINDOW', 'RATE', 'QUOTA_SCHEMA',
         'kind', 'fields', 'quota_fields'}
nodes = [node for node in tree.body if
         isinstance(node, ast.Import) and all(alias.name in {'math', 're'} for alias in node.names) or
         isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id in names for target in node.targets) or
         isinstance(node, ast.FunctionDef) and node.name in names]
namespace = {}
exec(compile(ast.Module(body=nodes, type_ignores=[]), 'actual-projection', 'exec'), namespace)
secret = 'DO_NOT_EMIT_SECRET_MARKER'
root = {
    'unknown_root_' + secret: secret,
    'quota': {'remaining': 0, 'resets_at': 0, 'nested_identity_' + secret: {'account_id': secret}},
    'rate_limit': {'allowed': False, 'primary_window': {
        'used_percent': 0, 'reset_at': 0, 'access_token_' + secret: secret}},
    'credits': {'balance': '0', 'identity_' + secret: secret},
    'model_catalog': {'models': [{'id': secret, secret: secret}]},
    'usage_limit': {'resets_at': '2026-11-11T10:13:43Z', 'plan_type': secret},
}
projected = {'root_fields': namespace['fields'](root), 'quota_fields': namespace['quota_fields'](root),
             'unknown_root_field_count': len(set(root) - namespace['ROOT_KEYS'])}
encoded = json.dumps(projected)
assert secret not in encoded
assert 'nested_identity_' not in encoded and 'access_token_' not in encoded
assert '2026-11-11T10:13:43Z' not in encoded
items = {item['field']: item for item in projected['quota_fields']['fields']}
assert items['quota.remaining']['value'] == 0
assert items['rate_limit.primary_window.reset_at']['value'] == 0
assert items['rate_limit.allowed']['value'] is False
assert items['credits.balance']['value'] == 0
assert items['usage_limit.resets_at'] == {'field': 'usage_limit.resets_at', 'type': 'string'}
assert projected['unknown_root_field_count'] == 1
assert projected['quota_fields']['unknown_field_count'] == 3
print(json.dumps({'projection_negative_cases': 'passed', 'unknown_names_and_strings_hidden': True,
                  'zero_and_reset_preserved': True, 'official_requests': 0}))
