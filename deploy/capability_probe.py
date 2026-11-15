#!/usr/bin/env python3
"""候选配置策略观察与继承生产接线证据的组合消费者。"""

import hashlib
import json
import pathlib
import re
import subprocess


CHANNELS = ('kilo', 'codex', 'bps')
ROLES = {'cli-proxy-api': 'cpa', 'cpa-manager-plus': 'manager', 'oauth-login': 'worker'}
VALIDATOR = 'mx-authz-policy-v1'


class Failure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Failure(message)


def read(path):
    return json.loads(pathlib.Path(path).read_text(encoding='utf-8'))


def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


def write(path, value):
    pathlib.Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    pathlib.Path(path).chmod(0o600)


def policy_expectations(cfg):
    require(cfg.get('config-version') == 8, '策略只接受已迁移的 v8 配置')
    access = cfg.get('access', {})
    raw_keys = access.get('api-keys', [])
    require(isinstance(raw_keys, list) and raw_keys and all(isinstance(k, str) and k.strip() for k in raw_keys), '策略 Key 集合无效')
    keys = list(dict.fromkeys(k.strip() for k in raw_keys))
    scopes = {}
    rows = access.get('client-key-scopes', [])
    require(isinstance(rows, list), '策略 scope 类型无效')
    for row in rows:
        require(isinstance(row, dict) and set(row) == {'api-key', 'channel'}, '策略 scope 字段无效')
        key, scope = row['api-key'], row['channel']
        require(isinstance(key, str) and isinstance(scope, str), '策略 scope 值无效')
        key, scope = key.strip(), scope.strip()
        require(key in keys and key not in scopes and scope in CHANNELS, '策略 scope 绑定无效')
        scopes[key] = scope
    expected = []
    for index, key in enumerate(keys):
        prefix = 'key-' + str(index)
        scope = scopes.get(key, '')
        authentication = {'id': prefix + '/authenticate', 'authenticated': True}
        if scope:
            authentication['scope'] = scope
        expected.append(authentication)
        visible = []
        for channel in CHANNELS:
            allowed = channel == scope if scope else channel != 'kilo'
            expected.append({'id': prefix + '/channel/' + channel, 'allowed': allowed})
            if allowed:
                visible.append(channel)
        expected.append({'id': prefix + '/visible', 'visible': visible})
    expected += [{'id': 'invalid/authenticate', 'authenticated': False, 'error_code': 'invalid_credential'},
                 {'id': 'missing/authenticate', 'authenticated': False, 'error_code': 'no_credentials'}]
    return len(keys), expected


def validate_policy(policy, cfg, config_sha, commit):
    count, expected = policy_expectations(cfg)
    require(policy.get('schema') == 1 and policy.get('kind') == 'authz-policy-matrix' and
            policy.get('validator') == VALIDATOR, '策略观察格式不匹配')
    require(policy.get('config_sha256') == config_sha and policy.get('source_commit') == commit,
            '策略观察配置或源码绑定不匹配')
    require(policy.get('key_count') == count, '策略观察 Key 数量不匹配')
    observations = policy.get('observations')
    require(isinstance(observations, list) and len(observations) == len(expected), '策略观察缺项或多项')
    require(all(isinstance(row, dict) and isinstance(row.get('id'), str) for row in observations), '策略观察结构无效')
    actual = {row['id']: row for row in observations}
    require(len(actual) == len(observations), '策略观察重复')
    # JSON 比较保留 true 与 1 的类型区别；观察不允许额外字段掩盖结果。
    require(json.dumps(actual, sort_keys=True) == json.dumps({row['id']: row for row in expected}, sort_keys=True),
            '策略观察覆盖或授权结果不匹配')


def validate_test_run(run, requirement):
    require(run.get('exit_code') == 0 and run.get('inherited') is True and run.get('command') and
            run.get('source') and run.get('input_revision'), '继承测试原回执缺失')
    log = run.get('stdout', '')
    require(isinstance(log, str) and log, '继承测试原日志缺失')
    terminal = {}
    observed_output = log
    if requirement['format'] == 'go-json':
        pending = log.splitlines()
        while pending:
            line = pending.pop(0).strip()
            try:
                event = json.loads(line)
            except (json.JSONDecodeError, TypeError):
                continue
            if not isinstance(event, dict):
                continue
            if event.get('Test') and event.get('Action') in ('pass', 'fail', 'skip'):
                terminal[event['Test']] = event['Action']
            output = event.get('Output', '')
            observed_output += '\n' + output
            pending.extend(part.strip() for part in output.splitlines() if part.strip().startswith('{'))
    else:
        for status, name in re.findall(r'^\s*--- (PASS|FAIL|SKIP): (\S+) \(', log, re.MULTILINE):
            terminal[name] = status.lower()
        require(re.search(r'^PASS\s*$', log, re.MULTILINE), '继承测试缺少最终 PASS')
    require(requirement.get('tests') and all(terminal.get(name) == 'pass' for name in requirement['tests']),
            '继承测试目标存在缺失、失败或 skip')
    require(not any(status == 'fail' for status in terminal.values()), '继承日志含失败测试')
    for marker in requirement.get('output_markers', []):
        require(marker in observed_output, '继承测试关键观察缺失')


def validate_wiring_contract(contract, evidence):
    require(contract.get('schema') == 1 and contract.get('kind') == 'inherited-production-wiring', '生产接线合同格式无效')
    review = contract.get('incremental_review', {})
    require(review.get('status') == 'PASS' and review.get('reviewer') and review.get('receipt'), '新入口增量审查回执缺失')
    require(review.get('normal_startup_preserved') is True and review.get('source_mapping_reviewed') is True,
            '普通启动或继承映射尚未审查')
    require(contract.get('source_files') and contract.get('source_roots') and contract.get('lineage'), '生产接线源码范围缺失')
    requirements = contract.get('test_runs', {})
    runs = evidence.get('runs', {})
    require(set(requirements) == {'m10-scope', 'm10-joint', 'bps-native', 'bps-stream', 'bps-legacy'} and
            set(runs) == set(requirements), '生产接线继承覆盖不完整')
    for name, requirement in requirements.items():
        validate_test_run(runs[name], requirement)


def validate_source_mapping(root, contract, commit, command):
    rows = command(['git', 'ls-tree', '-r', commit, '--', *contract['source_roots']]).splitlines()
    actual = {}
    for row in rows:
        metadata, name = row.split('\t', 1)
        mode, kind, blob = metadata.split()
        require(kind == 'blob', '生产接线范围出现非文件输入')
        actual[name] = {'mode': mode, 'blob': blob}
    require(actual == contract['source_files'], '生产接线源码或构建输入已变，需补对应证据')


def create_wiring_binding(root, commit, destination, command):
    folder = pathlib.Path(root) / 'deploy'
    contract = read(folder / 'production-wiring.contract.json')
    evidence = read(folder / 'production-wiring.evidence.json')
    validate_wiring_contract(contract, evidence)
    validate_source_mapping(root, contract, commit, command)
    binding = {'schema': 1, 'kind': 'inherited-production-wiring', 'candidate_source_commit': commit,
               'source_mapping': 'exact-git-tree', 'contract': 'production-wiring.contract.json',
               'evidence': 'production-wiring.evidence.json', 'execution_platform': 'windows/amd64',
               'candidate_platform': 'linux/amd64', 'candidate_runtime_wiring_rerun': False}
    write(destination, binding)
    return binding


def validate_combined_receipt(receipt, evidence, cfg, images, config_sha, folder):
    folder = pathlib.Path(folder)
    release = read(folder / 'release.json')
    commit = release['commit']
    require(re.fullmatch('[0-9a-f]{40}', commit), '候选源码身份无效')
    require(receipt.get('review_type') == 'automated' and receipt.get('image_ids') == images and
            receipt.get('config_sha256') == config_sha and receipt.get('source_config_sha256') == config_sha,
            '组合回执绑定不一致')
    require(evidence.get('schema') == 1 and evidence.get('kind') == 'combined-scope-capability' and
            evidence.get('image_ids') == images, '组合证据结构无效')
    validate_policy(evidence.get('config_policy', {}), cfg, config_sha, commit)
    binding = read(folder / 'production-wiring.json')
    require(evidence.get('production_wiring') == binding and binding.get('schema') == 1 and
            binding.get('kind') == 'inherited-production-wiring' and binding.get('candidate_source_commit') == commit and
            binding.get('source_mapping') == 'exact-git-tree' and
            binding.get('contract') == 'production-wiring.contract.json' and
            binding.get('evidence') == 'production-wiring.evidence.json', '生产接线版本绑定缺失')
    validate_wiring_contract(read(folder / binding['contract']), read(folder / binding['evidence']))
    identities = evidence.get('image_identities', {})
    require(set(identities) == set(ROLES) == set(images), '组合证据镜像集合不完整')
    for service, role in ROLES.items():
        identity = identities[service]
        candidate = release['candidates'][service]
        require(identity == {'id': images[service], 'revision': commit, 'role': role, 'os': 'linux', 'architecture': 'amd64'} and
                images[service] in candidate['accepted_ids'] and candidate['revision'] == commit, '组合证据镜像身份不一致')
    require(receipt.get('scope_matrix_passed') is True and receipt.get('mode') == 'scope-enforced', '组合能力结论缺失')


def probe_command(image, config):
    config = pathlib.Path(config).resolve()
    owner = config.stat()
    return ['docker', 'run', '--rm', '--network', 'none', '--read-only',
            '--user', f'{owner.st_uid}:{owner.st_gid}', '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges', '--mount',
            'type=bind,src=' + str(config) + ',dst=/candidate.yaml,readonly',
            '--entrypoint', '/CLIProxyAPI/CLIProxyAPI', image, '--capability-probe', '--config', '/candidate.yaml']


def probe_capability(images, config, state, matrix_path, command):
    folder = pathlib.Path(matrix_path).parent
    matrix = read(matrix_path)
    require(matrix == {'schema': 1, 'validator': VALIDATOR, 'channels': list(CHANNELS),
                       'legacy_channels': ['codex', 'bps'], 'evidence': ['config_policy', 'production_wiring']}, '策略矩阵合同不匹配')
    release = read(folder / 'release.json')
    commit = release['commit']
    identities = {}
    for service, role in ROLES.items():
        rows = json.loads(command(['docker', 'image', 'inspect', images[service]]))
        require(len(rows) == 1, '候选镜像查询数量错误')
        image = rows[0]
        labels = image.get('Config', {}).get('Labels') or {}
        identity = {'id': image.get('Id'), 'revision': labels.get('org.opencontainers.image.revision'),
                    'role': labels.get('io.mx-cpamp.role'), 'os': image.get('Os'), 'architecture': image.get('Architecture')}
        require(identity == {'id': images[service], 'revision': commit, 'role': role, 'os': 'linux', 'architecture': 'amd64'},
                '候选镜像身份或平台不匹配')
        identities[service] = identity
    config_sha = digest(config)
    wrapper = 'services: {}\nx-document:\n' + ''.join('  ' + line + '\n' for line in pathlib.Path(config).read_text(encoding='utf-8').splitlines())
    cfg = json.loads(command(['docker', 'compose', '-f', '-', 'config', '--no-interpolate', '--format', 'json'], wrapper))['x-document']
    policy = json.loads(command(probe_command(images['cli-proxy-api'], config)))
    require(digest(config) == config_sha, '策略探测期间配置漂移')
    evidence = {'schema': 1, 'kind': 'combined-scope-capability', 'config_policy': policy,
                'production_wiring': read(folder / 'production-wiring.json'),
                'image_ids': images, 'image_identities': identities,
                'real_account_or_provider_inference': False}
    evidence_path = pathlib.Path(state).resolve() / 'capability-evidence.json'
    receipt = {'schema': 1, 'review': 'PASS', 'reviewed_by': VALIDATOR, 'review_type': 'automated',
               'image_ids': images, 'config_sha256': config_sha, 'source_config_sha256': config_sha,
               'mode': 'scope-enforced', 'scope_matrix_passed': True, 'runtime_evidence': str(evidence_path)}
    validate_combined_receipt(receipt, evidence, cfg, images, config_sha, folder)
    write(evidence_path, evidence)
    receipt['runtime_evidence_sha256'] = digest(evidence_path)
    receipt_path = pathlib.Path(state).resolve() / 'capability-receipt.json'
    write(receipt_path, receipt)
    return receipt_path
