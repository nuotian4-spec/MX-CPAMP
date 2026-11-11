"""缓存 Linux/Compose 的只读诊断专项，不执行发布 main。"""
import contextlib
import copy
import importlib.util
import io
import json
import pathlib
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module

d = load('diagnostic', '/checks/diagnose-merge.py')
r = load('release_readonly', '/delivery/bps-final-release.py')
root = pathlib.Path(tempfile.mkdtemp(prefix='mx-compose-diagnosis-'))
r.ROOT = root; r.STATE = root / 'state'; r.STATE.mkdir(); r.PROJECT = 'mx-compose-diagnosis'
(r.STATE / 'stage').write_text('prepare_site\n')
images = {service: 'synthetic:fixed' for service in r.APPS}
r.image_records = lambda: copy.deepcopy(images)
sentinel = 'SECRET_SENTINEL_DIAG_73ea'
base = {'services': {service: {'image': 'synthetic:old', 'environment': {sentinel: sentinel}, 'labels': {sentinel: sentinel}, 'x-' + sentinel: {sentinel: sentinel}} for service in r.APPS + ['caddy']}}
(root / 'compose.yaml').write_text(json.dumps(base))
(root / 'compose.mx-cpamp.yaml').write_text('{"services":{}}')

def run_case(name, mount, expected_code, expected_path=None):
    override = {'services': {service: {'image': images[service], 'pull_policy': 'never'} for service in r.APPS}}
    override['services']['cli-proxy-api']['volumes'] = [mount]
    (r.STATE / 'compose.bps-final.yaml').write_text(json.dumps(override))
    before = {str(p): (p.read_bytes(), p.stat().st_mtime_ns, p.stat().st_mode) for p in root.rglob('*') if p.is_file()}
    output = io.StringIO()
    with contextlib.redirect_stdout(output):
        assert d.diagnose(r) == 0
    after = {str(p): (p.read_bytes(), p.stat().st_mtime_ns, p.stat().st_mode) for p in root.rglob('*') if p.is_file()}
    assert before == after, 'diagnostic_wrote_files'
    raw = output.getvalue(); assert sentinel not in raw and str(root) not in raw and 'SOURCE_SENTINEL' not in raw
    result = json.loads(raw.splitlines()[-1]); assert result['code'] == expected_code
    if expected_path:
        assert expected_path in [row['path'] for row in result['differences']], result
    print(json.dumps({'case': name, 'passed': True, 'zero_file_writes': True, 'secret_absent': True, 'result': result}))

mount = {'type': 'bind', 'source': str(root / 'cpa/bps-state'), 'target': '/var/lib/cpa/bps-state'}
run_case('missing_bind_default', copy.deepcopy(mount), 'candidate_merge_changed', '/services/cli-proxy-api/volumes/0/bind')
mount['bind'] = {'create_host_path': True}
run_case('explicit_true_equal', copy.deepcopy(mount), 'compose_equal')
mount['source'] = '/synthetic/SOURCE_SENTINEL_NOT_FOR_OUTPUT'
run_case('source_drift', copy.deepcopy(mount), 'candidate_merge_changed', '/services/cli-proxy-api/volumes/0/source')
left = {'services': {'cli-proxy-api': {'environment': {sentinel: 'a'}, 'labels': {sentinel: 'a'}, 'x-' + sentinel: {sentinel: 'a'}}}}
right = copy.deepcopy(left)
for value in right['services']['cli-proxy-api'].values():
    value[sentinel] = 'b'
safe = json.dumps(d.differences(left, right)); assert sentinel not in safe and '<entry>' in safe
diff, truncated = d.differences({'environment': {str(i): 1 for i in range(40)}}, {'environment': {str(i): 2 for i in range(40)}})
assert len(diff) == 30 and truncated
print(json.dumps({'case': 'anonymous_keys_and_limit', 'passed': True, 'limit': len(diff), 'truncated': truncated}))
shell = "python3 -B - <<'PY'\n" + pathlib.Path('/checks/diagnose-merge.py').read_text() + '\nPY\n'
check = subprocess.run(['sh', '-n'], input=shell, text=True, capture_output=True)
assert check.returncode == 0, 'heredoc_syntax_failed'
compile(pathlib.Path('/checks/diagnose-merge.py').read_text(), 'diagnose-merge.py', 'exec')
print(json.dumps({'case': 'linux_python_and_sh_heredoc_syntax', 'passed': True}))
print('command_exit=0')
