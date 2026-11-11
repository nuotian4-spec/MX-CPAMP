"""临时 Worker 内的一次性 BPS 额度来源读取，仅接受 prepare/run。"""
import json
import math
import re
import sys
from curl_cffi import requests

ACCESS = 'https://bps.openai.com/basispoints/api/responses/access?include_models=true'
USAGE = 'https://chatgpt.com/backend-api/wham/usage'
PROXY = 'http://host.docker.internal:7890'
AGENT = 'codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal'
CLIENT = 'app_fnr0pYvVwwFDocDumLG3H2Bp'
NUMBERS = {
    'used_percent', 'remaining_percent', 'limit_window_seconds', 'reset_after_seconds',
    'reset_at', 'resets_at', 'resets_in_seconds', 'limit', 'remaining', 'used', 'total',
    'balance', 'credits', 'usage_limit', 'remaining_credits', 'total_credits', 'total_available',
}
BOOLEANS = {'allowed', 'limit_reached', 'has_credits', 'unlimited'}
ERRORS = {
    'invalid_api_key', 'invalid_token', 'token_expired', 'unauthorized', 'authentication_error',
    'insufficient_permissions', 'access_denied', 'forbidden', 'rate_limit_exceeded',
    'usage_limit_reached', 'usage_not_included', 'invalid_authentication', 'missing_scope',
    'insufficient_scope', 'usage_policy_lookup_unavailable', 'usage_policy_dependency_missing',
    'usage_policy_context_missing',
}
ROOT_KEYS = {'allowed', 'model_catalog', 'plan_type', 'rate_limit', 'code_review_rate_limit',
             'additional_rate_limits', 'credits', 'quota', 'usage_limit', 'error', 'detail'}
AUTH_KEYS = {'type', 'profile', 'client_id', 'access_token', 'account_id', 'expired'}
WINDOW = {key: None for key in ('used_percent', 'limit_window_seconds', 'reset_after_seconds', 'reset_at')}
RATE = {'allowed': None, 'limit_reached': None, 'primary_window': WINDOW, 'secondary_window': WINDOW}
QUOTA_SCHEMA = {
    'rate_limit': RATE, 'code_review_rate_limit': RATE,
    'additional_rate_limits': [{'rate_limit': RATE}],
    'credits': {'has_credits': None, 'unlimited': None, 'balance': None},
    'quota': {key: None for key in ('limit', 'remaining', 'used', 'total', 'resets_at', 'resets_in_seconds')},
    'usage_limit': {'plan_type': None, 'resets_at': None, 'resets_in_seconds': None},
    'error': {'plan_type': None, 'resets_at': None, 'resets_in_seconds': None},
}


def emit(value):
    print(json.dumps(value, separators=(',', ':'), allow_nan=False), flush=True)


def kind(value):
    if value is None:
        return 'null'
    if isinstance(value, bool):
        return 'boolean'
    if isinstance(value, (int, float)):
        return 'number'
    if isinstance(value, str):
        return 'string'
    return 'array' if isinstance(value, list) else 'object'


def fields(root, allowed=ROOT_KEYS):
    return [{'name': key, 'type': kind(root[key])} for key in sorted(allowed) if key in root]


def quota_fields(root):
    result = []
    unknown = 0

    def visit(value, schema, location=''):
        nonlocal unknown
        if isinstance(value, dict) and isinstance(schema, dict):
            for key, child in value.items():
                if key not in schema:
                    if location:
                        unknown += 1
                    continue
                child_path = (location + '.' if location else '') + key
                item = {'field': child_path, 'type': kind(child)}
                if schema[key] is None:
                    if key in NUMBERS and isinstance(child, (int, float)) and not isinstance(child, bool) and math.isfinite(child):
                        item['value'] = child
                    elif key in NUMBERS and isinstance(child, str) and re.fullmatch(r'-?[0-9]+(?:\.[0-9]+)?', child) and len(child) < 40:
                        item['value'] = float(child)
                    elif key in BOOLEANS and isinstance(child, bool):
                        item['value'] = child
                result.append(item)
                visit(child, schema[key], child_path)
        elif isinstance(value, list) and isinstance(schema, list):
            for child in value[:32]:
                visit(child, schema[0], location + '[]')

    visit(root, QUOTA_SCHEMA)
    return {'fields': result, 'unknown_field_count': unknown}


def error_code(root):
    error = root.get('error')
    code = error.get('code') if isinstance(error, dict) else None
    return code if code in ERRORS else ('other' if isinstance(code, str) else None)


def wipe(value):
    if isinstance(value, dict):
        for child in value.values():
            wipe(child)
        value.clear()
    elif isinstance(value, list):
        for child in value:
            wipe(child)
        value.clear()
    elif isinstance(value, bytearray):
        value[:] = b'\0' * len(value)


raw = bytearray()
documents = []
selected = None
headers = {}
session = response = body = parsed = None
attempts = {'access': 0, 'wham': 0}
failure = None
try:
    control = json.loads(sys.stdin.buffer.readline())
    mode = control.get('mode')
    if mode not in ('prepare', 'run'):
        raise ValueError('mode_invalid')
    raw = bytearray(sys.stdin.buffer.read())
    for part in raw.split(b'\0'):
        if not part:
            continue
        try:
            document = json.loads(part)
            if not isinstance(document, dict):
                raise ValueError('credential_structure')
            documents.append(document)
        finally:
            wipe(part)
    matches = [doc for doc in documents if doc.get('type') == 'bps']
    emit({'stage': 'credential_inventory', 'auth_file_count': len(documents), 'bps_count': len(matches),
          'known_provider_counts': {provider: sum(doc.get('type') == provider for doc in documents)
                                    for provider in ('bps', 'codex')},
          'auth_root_fields': [fields(doc, AUTH_KEYS) for doc in documents]})
    if len(matches) != 1:
        raise ValueError('bps_count_not_one')
    selected = matches[0]
    if selected.get('profile') != 'excel' or selected.get('client_id') != CLIENT:
        raise ValueError('profile_client_mismatch')
    if any(not isinstance(selected.get(key), str) or not selected[key].strip() or
           any(c in selected[key] for c in '\r\n') for key in ('access_token', 'account_id')):
        raise ValueError('credential_incomplete')
    emit({'stage': 'prepare', 'status': 'passed', 'auth_file_count': len(documents),
          'bps_count': 1, 'profile_client_match': True, 'dependency': 'curl_cffi',
          'auth_field_types': {key: kind(selected.get(key)) for key in
                               ('type', 'profile', 'client_id', 'access_token', 'account_id', 'expired')},
          'official_requests': 0})
    if mode == 'run':
        session = requests.Session(trust_env=False)
        headers = {'Authorization': 'Bearer ' + selected['access_token'],
                   'chatgpt-account-id': selected['account_id'], 'x-openai-account-id': selected['account_id'],
                   'x-basispoints-auth-mode': 'chatgpt', 'Content-Type': 'application/json',
                   'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Origin': 'https://bps.openai.com'}
        for key, value in {'client-agent-profile': 'excel', 'client-editor': 'excel', 'client-host': 'office',
                           'client-platform': 'excel', 'client-platform-class': 'PC',
                           'client-product': 'basispoints-excel-plugin', 'client-runtime': 'desktop',
                           'office-host': 'Excel', 'office-platform': 'PC'}.items():
            headers['x-openai-internal-basispoints-' + key] = value
        for stage, url in (('access', ACCESS), ('wham', USAGE)):
            if stage == 'wham':
                headers.clear()
                headers.update({'Authorization': 'Bearer ' + selected['access_token'],
                                'Chatgpt-Account-Id': selected['account_id'],
                                'Content-Type': 'application/json', 'User-Agent': AGENT})
            attempts[stage] += 1
            response = session.get(url, headers=headers, proxies={'http': PROXY, 'https': PROXY},
                                   timeout=20, allow_redirects=False, max_redirects=0, verify=True)
            body = bytearray(response.content)
            record = {'stage': stage, 'http': response.status_code}
            if len(body) > 2 * 1024 * 1024:
                emit(record)
                raise ValueError('response_too_large')
            try:
                parsed = json.loads(body)
            except (ValueError, UnicodeError):
                emit(record)
                raise ValueError('invalid_response')
            if not isinstance(parsed, dict):
                emit(record)
                raise ValueError('invalid_response')
            record.update({'root_fields': fields(parsed), 'quota_fields': quota_fields(parsed),
                           'unknown_root_field_count': len(set(parsed) - ROOT_KEYS),
                           'error_code': error_code(parsed)})
            if stage == 'access':
                catalog = parsed.get('model_catalog')
                models = catalog.get('models') if isinstance(catalog, dict) else None
                valid = parsed.get('allowed') is True and isinstance(models, list) and all(
                    isinstance(model, dict) and isinstance(model.get('id'), str) for model in models)
                record.update({'allowed': parsed.get('allowed') if isinstance(parsed.get('allowed'), bool) else None,
                               'model_count': len(models) if isinstance(models, list) else None,
                               'structure_valid': valid})
            else:
                valid = isinstance(parsed.get('rate_limit'), dict)
                record['structure_valid'] = valid
            emit(record)
            if response.status_code != 200:
                raise ValueError('http_not_200')
            if not valid:
                raise ValueError('invalid_response')
            wipe(parsed)
            parsed = None
            wipe(body)
            body = None
            response.content = b''
            response.close()
            response = None
            session.cookies.clear()
except Exception as error:
    codes = {'mode_invalid', 'credential_structure', 'bps_count_not_one', 'profile_client_mismatch',
             'credential_incomplete', 'response_too_large', 'invalid_response', 'http_not_200'}
    failure = str(error) if isinstance(error, ValueError) and str(error) in codes else 'transport_or_local_error'
finally:
    headers.clear()
    wipe(parsed)
    wipe(body)
    wipe(documents)
    wipe(raw)
    selected = parsed = body = raw = None
    if response is not None:
        response.content = b''
        response.close()
    if session is not None:
        session.cookies.clear()
        session.headers.clear()
        session.close()
    emit({'stage': 'complete', 'status': 'stopped' if failure else 'passed', 'error_code': failure,
          'attempted_gets': attempts, 'refresh_requests': 0, 'inference_requests': 0,
          'sensitive_buffers_cleared': True})
    if failure:
        sys.exit(1)
