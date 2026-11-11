#!/usr/bin/env python3
"""只读复算候选 Compose；仅输出脱敏字段结构。"""
import copy
import importlib.util
import json
import os
import pathlib
import sys

TOOLS = '/opt/cpa-stack/mx-release-20261111-rc2/bps-final-release.py'
SERVICES = {'cli-proxy-api', 'cpa-manager-plus', 'oauth-login', 'caddy'}
FIELDS = set('name services image pull_policy volumes type source target read_only bind create_host_path propagation selinux consistency volume nocopy subpath tmpfs size mode networks default ports protocol published host_ip app_protocol environment labels command entrypoint user working_dir restart healthcheck test interval timeout retries start_period start_interval depends_on condition required init privileged security_opt cap_add cap_drop network_mode pid ipc userns_mode uts expose logging driver options secrets configs file external driver_opts ipam config subnet gateway internal attachable enable_ipv6 enable_ipv4 build context dockerfile args deploy resources limits reservations cpus memory profiles stop_grace_period stdin_open tty'.split())
MISSING = object()

def emit(**value):
    print(json.dumps(value, ensure_ascii=True, sort_keys=True), flush=True)

def describe(value, path):
    result = {'present': value is not MISSING, 'type': 'missing' if value is MISSING else type(value).__name__}
    if path[-2:] == ('bind', 'create_host_path') and type(value) is bool:
        result['value'] = value
    return result

def differences(expected, rendered):
    result = []
    def visit(left, right, path=(), hidden=False):
        if len(result) > 30 or left == right:
            return
        if type(left) is type(right) and isinstance(left, dict):
            for key in sorted(left.keys() | right.keys()):
                private = hidden or (path and path[-1] in ('environment', 'labels', 'driver_opts', 'options', 'args')) or key.startswith('x-')
                safe = not private and (key in SERVICES if path == ('services',) else key in FIELDS)
                visit(left.get(key, MISSING), right.get(key, MISSING), path + (key if safe else '<entry>',), private)
        elif type(left) is type(right) and isinstance(left, list):
            for index in range(max(len(left), len(right))):
                visit(left[index] if index < len(left) else MISSING, right[index] if index < len(right) else MISSING, path + (str(index),), hidden)
        else:
            result.append({'path': '/' + '/'.join(path), 'expected': describe(left, path), 'rendered': describe(right, path)})
    visit(expected, rendered)
    return result[:30], len(result) > 30

def diagnose(r):
    files = {name: (r.STATE / name).exists() for name in ('stage', 'ready', 'maintenance', 'complete', 'compose.bps-final.yaml')}
    stage_ok = (r.STATE / 'stage').is_file() and (r.STATE / 'stage').read_text().strip() == 'prepare_site'
    emit(code='state_observed', stage='prepare_site' if stage_ok else 'unknown', files=files)
    if not stage_ok or any(files[name] for name in ('ready', 'maintenance', 'complete')) or not files['compose.bps-final.yaml']:
        emit(code='state_not_diagnostic'); return 1
    images = r.image_records()
    merged = json.loads(r.command(r.compose() + ['config', '--format', 'json']))
    rendered = json.loads(r.command(r.compose(True) + ['config', '--format', 'json']))
    expected = copy.deepcopy(merged)
    for service in r.APPS:
        expected['services'][service].update(image=images[service], pull_policy='never')
    expected['services']['cli-proxy-api'].setdefault('volumes', []).append({'type': 'bind', 'source': str(r.ROOT / 'cpa/bps-state'), 'target': '/var/lib/cpa/bps-state', 'bind': {'create_host_path': True}})
    for value in (expected, rendered):
        for service in value['services'].values():
            service['volumes'] = sorted(service.get('volumes', []), key=lambda item: item['target'])
    diff, truncated = differences(expected, rendered)
    emit(code='compose_equal' if expected == rendered else 'candidate_merge_changed', differences=diff, truncated=truncated)
    return 0

def main():
    if os.geteuid() != 0:
        emit(code='root_required'); return 1
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location('release_readonly', TOOLS)
    r = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(r)
    return diagnose(r)

if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception:
        emit(code='diagnostic_unknown'); sys.exit(1)
