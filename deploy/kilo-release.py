#!/usr/bin/env python3
"""Kilo发布：准备、备份、入口停止下启动与代码回退；不自动恢复入口。"""
import argparse
import copy
import contextlib
import hashlib
import importlib.util
import json
import os
import pathlib
import posixpath
import select
import shutil
import sys
import tarfile
import time

APPS = ['oauth-login', 'cpa-manager-plus', 'cli-proxy-api']


class Failure(Exception):
    pass


def require(value, message):
    if not value:
        raise Failure(message)


def read(path):
    return json.loads(pathlib.Path(path).read_text(encoding='utf-8'))


def write(path, value):
    path = pathlib.Path(path)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    path.chmod(0o600)


def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


def archive_name(path):
    path = pathlib.Path(path)
    return path.as_posix().removeprefix(path.drive).lstrip('/')


def inside(path, root):
    return path == root or path.startswith(root.rstrip('/') + '/')


def config_value(cfg, current, legacy, default):
    def lookup(path, canonical):
        value = cfg
        for part in path.split('.'):
            if value is None and not canonical:
                return False, None
            require(isinstance(value, dict), '存储配置节点须为映射:' + path)
            if part not in value:
                return False, None
            value = value[part]
        return True, value
    present, value = lookup(current, True)
    if not present:
        present, value = lookup(legacy, False)
    return value if present and value is not None else default


def resolve_storage_path(value, cwd, home):
    value = value.strip()
    if value == '~' or value.startswith(('~/', '~\\')):
        value = home + '/' + value[1:].lstrip('/\\')
    return posixpath.normpath(value if value.startswith('/') else posixpath.join(cwd, value))


def state_directory(cfg, cwd='/CLIProxyAPI', home='/root'):
    selected = storage_config(cfg)
    auth = resolve_storage_path(selected['auth-dir'], cwd, home)
    state = resolve_storage_path(selected['kilo-state-dir'] or posixpath.join(posixpath.dirname(auth), 'kilo-state'), cwd, home)
    require(not inside(state, auth), 'Kilo状态目录位于凭证发现目录内')
    return auth, state


def validate_spec(spec):
    require(spec.get('schema') == 1 and spec.get('input_id'), '发布计划缺少schema或输入标识')
    for key in ['root', 'state_dir', 'cpa_config']:
        require(pathlib.Path(spec[key]).is_absolute(), '发布计划路径须为绝对路径')
    root, state = pathlib.Path(spec['root']), pathlib.Path(spec['state_dir'])
    require(state.is_relative_to(root) and state != root, '发布状态目录须位于工作目录内')
    require(spec.get('project') and isinstance(spec.get('compose_files'), list) and spec['compose_files'], '完整Compose链缺失')
    require(len(set(spec['compose_files'])) == len(spec['compose_files']), 'Compose链含重复路径')
    for name in spec['compose_files']:
        require(pathlib.Path(name).is_absolute() and pathlib.Path(name).is_file(), 'Compose文件缺失')
    require(pathlib.Path(spec['cpa_config']).is_file(), 'CPA配置文件缺失')
    require(set(spec['candidates']) == {'cli-proxy-api', 'cpa-manager-plus', 'oauth-login'}, '三角色候选缺失')
    for service, role in [('cli-proxy-api', 'cpa'), ('cpa-manager-plus', 'manager'), ('oauth-login', 'worker')]:
        image = spec['candidates'][service]
        require(image.get('role') == role and image.get('ref') and image.get('revision') and image.get('accepted_ids'), '候选镜像标识缺失')
    target, host = spec['kilo_state']['container'], pathlib.Path(spec['kilo_state']['host'])
    require(target.startswith('/') and posixpath.normpath(target) == target and target != '/', 'Kilo容器目录无效')
    require(host.is_absolute() and not state.is_relative_to(host) and not host.is_relative_to(state), 'Kilo持久目录与发布状态目录冲突')
    require(spec.get('baseline'), '现场基线未填写')


def normalize_compose_volumes(document):
    document = copy.deepcopy(document)
    for service in document['services'].values():
        service['volumes'] = sorted(service.get('volumes', []), key=lambda mount: mount['target'])
    return document


def compose(spec, extra=None):
    args = ['docker', 'compose', '--project-directory', spec['root'], '-p', spec['project']]
    for name in spec['compose_files'] + ([str(extra)] if extra else []):
        args += ['-f', name]
    return args


def map_path(container, path):
    for mount in sorted(container['Mounts'], key=lambda m: -len(m['Destination'])):
        target = mount['Destination'].rstrip('/')
        if inside(path, target):
            require(mount['Type'] in ['bind', 'volume'], '持久挂载类型未支持')
            return pathlib.Path(mount['Source'] + path[len(target):])
    return None


def rollback_scope_gate(evidence, cfg, scoped_keys):
    """由启动/回退入口调用，能力证据另由入口绑定实际镜像与配置。"""
    require(evidence.get('client_ingress') == 'blocked', '客户端入口尚未阻断')
    require(evidence.get('runtime_evidence'), '回退镜像能力证据缺失')
    mode = evidence.get('mode')
    if mode == 'scope-enforced':
        require(evidence.get('scope_matrix_passed') is True, '隔离矩阵尚未通过')
    elif mode == 'revoke-scoped-keys':
        keys = cfg.get('access', {}).get('api-keys', cfg.get('api-keys', []))
        require(isinstance(keys, list) and keys and all(isinstance(k, str) and k for k in keys), '空Key不代表拒绝所有请求')
        require(scoped_keys and set(scoped_keys).isdisjoint(keys), '依赖scope的Key仍在有效集合内')
        require(evidence.get('invalid_key_rejected') is True and evidence.get('channel_matrix_passed') is True, '回退鉴权矩阵尚未通过')
    else:
        raise Failure('旧镜像隔离能力未知，保持入口阻断')
    return '权限材料通过；继续保持入口阻断，实际放流另行确认'


def scope_keys(cfg):
    scopes = cfg.get('access', {}).get('client-key-scopes', [])
    require(isinstance(scopes, list), 'scope配置格式待核')
    keys = []
    for item in scopes:
        require(isinstance(item, dict) and set(item) == {'api-key', 'channel'}, 'scope配置字段待核')
        require(isinstance(item['api-key'], str) and isinstance(item['channel'], str), 'scope配置值待核')
        key, channel = item['api-key'].strip(), item['channel'].strip()
        require(key and channel in ['kilo', 'codex', 'bps'] and key not in keys, 'scope配置绑定待核')
        keys.append(key)
    return keys


def revoke_scope_keys(cfg):
    before = cfg.get('access', {}).get('api-keys', [])
    require(isinstance(before, list) and all(isinstance(key, str) for key in before), '有效Key格式待核')
    scoped = scope_keys(cfg)
    require(scoped and set(scoped).issubset({key.strip() for key in before}), 'scope引用或撤销集合待核')
    kept = [key for key in before if key.strip() not in scoped]
    require(kept and all(key.strip() for key in kept), '撤销后无非scope有效Key，保持停态')
    result = copy.deepcopy(cfg)
    result['access']['api-keys'] = kept
    result['access'].pop('client-key-scopes', None)
    return result, scoped, len(before) - len(kept)


def storage_config(cfg):
    def text(current, legacy, default=''):
        value = config_value(cfg, current, legacy, default)
        require(isinstance(value, str), '存储路径须为字符串:' + current)
        return value.strip()
    def boolean(current, legacy, default):
        value = config_value(cfg, current, legacy, default)
        require(isinstance(value, bool), '存储开关须为布尔值:' + current)
        return value
    return {'auth-dir': text('oauth.auth-dir', 'auth-dir') or '~/.cli-proxy-api',
            'kilo-state-dir': text('upstream.kilo.state-dir', 'kilo.state-dir'),
            'kv-store': {'path': text('server.kv-store.path', 'kv-store.path'), 'disabled': boolean('server.kv-store.disabled', 'kv-store.disabled', False)},
            'bps-state-dir': text('upstream.bps.protocol.state-dir', 'bps-protocol.state-dir'),
            'bps-enabled': boolean('upstream.bps.protocol.enabled', 'bps-protocol.enabled', True)}


class Release:
    def __init__(self, spec, backend):
        validate_spec(spec)
        self.spec, self.b = spec, backend
        self.state = pathlib.Path(spec['state_dir'])

    def mark(self, stage):
        (self.state / 'stage').write_text(stage + '\n', encoding='utf-8')

    def check_baseline(self, live):
        actual = self.b.facts(live)
        require(set(actual) == set(self.spec['baseline']), '现场服务集合变化')
        for service, expected in self.spec['baseline'].items():
            require(live[service]['State']['Running'], '基线服务未运行')
            for key in ['id', 'image_id', 'image_ref', 'working_dir', 'compose_files', 'mounts']:
                require(actual[service][key] == expected[key], '现场基线变化:' + key)
        for service in self.spec['candidates']:
            require(actual[service]['working_dir'] == self.spec['root'], '实际Compose工作目录与计划不符')
        return actual

    def sources(self, live, cfg):
        cpa = live['cli-proxy-api']
        cwd = cpa['Config'].get('WorkingDir') or '/'
        env = dict(v.split('=', 1) for v in cpa['Config'].get('Env', []) if '=' in v)
        require(not any(env.get(k) for k in ['HOME_JWT', 'PGSTORE_DSN', 'GITSTORE_GIT_URL', 'OBJECTSTORE_ENDPOINT', 'WRITABLE_PATH']), '外部配置模式需专门适配')
        args = list(cpa.get('Args', [])); selected = 'config.yaml'
        while args:
            arg = args.pop(0)
            if arg in ['-config', '--config']:
                require(args, 'CPA配置参数缺失'); selected = args.pop(0)
            elif arg.startswith(('-config=', '--config=')):
                selected = arg.split('=', 1)[1]
            else:
                raise Failure('CPA启动参数待核')
        selected = self.b.resolve(selected, cwd)
        require(map_path(cpa, selected) == pathlib.Path(self.spec['cpa_config']), 'CPA配置来源与计划不符')
        require(self.b.command(['docker', 'exec', cpa['Id'], 'sh', '-c', 'test ! -e "$1" && printf absent', 'sh', posixpath.join(cwd, '.env')]) == 'absent', 'CPA容器内额外env来源待核')
        auth, old_state = state_directory(cfg, cwd, env.get('HOME', '/root'))
        target, host = self.spec['kilo_state']['container'], pathlib.Path(self.spec['kilo_state']['host'])
        require(not inside(target, auth), '候选Kilo目录位于凭证发现目录内')
        auth_host = map_path(cpa, auth)
        require(auth_host is not None and not host.is_relative_to(auth_host), 'Kilo宿主目录位于auth内或auth未持久化')
        selection = storage_config(cfg)
        kv = selection['kv-store']
        if not kv.get('disabled', False):
            require(map_path(cpa, self.b.resolve(kv.get('path') or posixpath.join(auth, 'kv.db'), cwd)) is not None, 'bbolt未位于持久挂载内')
        bps = resolve_storage_path(selection['bps-state-dir'] or posixpath.join(posixpath.dirname(auth), 'bps-state'), cwd, env.get('HOME', '/root'))
        require(not inside(bps, auth), 'BPS状态目录位于凭证发现目录内')
        if map_path(cpa, bps) is None:
            require(self.b.command(['docker', 'exec', cpa['Id'], 'sh', '-c', 'test ! -e "$1" && printf absent', 'sh', bps]) == 'absent', '既有BPS状态未持久化，先保全')
            raise Failure('BPS状态须持久挂载，包含默认或禁用目录')
        manager = live['cpa-manager-plus']
        variables = dict(v.split('=', 1) for v in manager['Config'].get('Env', []) if '=' in v)
        manager_cwd = manager['Config'].get('WorkingDir') or '/'
        selected_manager = variables.get('CPA_MANAGER_CONFIG')
        stored, config_dir = {}, '/usr/local/bin'
        if selected_manager:
            selected_manager = self.b.resolve(selected_manager, manager_cwd)
            selected_host = map_path(manager, selected_manager)
            require(selected_host is not None, 'Manager配置未持久化')
            stored, config_dir = read(selected_host), posixpath.dirname(selected_manager)
        else:
            check = 'test ! -e /usr/local/bin/config.json && printf absent'
            require(self.b.command(['docker', 'exec', manager['Id'], 'sh', '-c', check]) == 'absent', 'Manager默认配置待核')
        data_dir = variables.get('USAGE_DATA_DIR') or self.b.resolve(stored.get('dataDir') or ('./data' if selected_manager else '/data'), config_dir)
        db = variables.get('USAGE_DB_PATH') or (self.b.resolve(stored['dbPath'], config_dir) if stored.get('dbPath') and not variables.get('USAGE_DATA_DIR') else posixpath.join(data_dir, 'usage.sqlite'))
        archive_base = data_dir if variables.get('USAGE_DATA_DIR') or stored.get('dataDir') else posixpath.dirname(db)
        key = variables.get('CPA_MANAGER_DATA_KEY_FILE') or (self.b.resolve(stored['dataKeyFile'], config_dir) if stored.get('dataKeyFile') else '')
        if not key:
            require(self.b.command(['docker', 'exec', manager['Id'], 'sh', '-c', 'test ! -e /run/secrets/cpa_data_key && printf absent']) == 'absent', 'Manager默认外部密钥待核')
            key = variables.get('CPA_MANAGER_DATA_KEY_PATH') or (self.b.resolve(stored['dataKeyPath'], config_dir) if stored.get('dataKeyPath') else posixpath.join(data_dir, 'data.key'))
        require(not variables.get('CPA_MANAGER_DATA_KEY'), 'Manager内联密钥需专门备份方案')
        for path in [data_dir, db, key, posixpath.join(archive_base, 'usage-archives')]:
            require(map_path(manager, self.b.resolve(path, manager_cwd)) is not None, 'Manager持久路径未映射')
        db_path = self.b.resolve(db, manager_cwd)
        db_parent = map_path(manager, posixpath.dirname(db_path))
        require(db_parent is not None and db_parent.is_dir() and not any(m['Destination'].rstrip('/') == db_path for m in manager['Mounts']), 'Manager数据库须整目录持久挂载，拒绝单文件DB映射')
        mapped = map_path(cpa, old_state)
        if mapped is not None:
            require(mapped == host and old_state == target, '已有Kilo状态须原位保留')
            require(host.is_dir(), '已映射Kilo目录缺失，先核现场状态')
        else:
            require(old_state == target, '未挂载旧状态与候选路径不同，需定向保全')
            check = 'test ! -e "$1" && printf absent'
            require(self.b.command(['docker', 'exec', cpa['Id'], 'sh', '-c', check, 'sh', old_state]) == 'absent', '未挂载Kilo状态已存在，先保全')
            require(not host.exists(), '新增Kilo宿主路径已存在，需核归属')
        # 同一停态归档覆盖所有应用持久挂载，包含外置kv、命名卷和秘密文件。
        paths = {str(pathlib.Path(m['Source'])) for s in self.spec['candidates'] for m in live[s]['Mounts'] if m['Type'] in ['bind', 'volume']}
        paths.update(self.spec['compose_files']); paths.add(self.spec['cpa_config'])
        paths.update(self.spec.get('protected_files', []))
        env_path = pathlib.Path(self.spec['root']) / '.env'
        if env_path.exists(): paths.add(str(env_path))
        for file in self.spec['compose_files']:
            for service in self.b.yaml_document(file).get('services', {}).values():
                sources = service.get('env_file', [])
                for source in ([sources] if isinstance(sources, str) else sources):
                    value = source['path'] if isinstance(source, dict) else source
                    require('$' not in value, 'env_file动态路径待核')
                    path = pathlib.Path(value)
                    paths.add(str(path if path.is_absolute() else pathlib.Path(self.spec['root']) / path))
        require(all(pathlib.Path(p).exists() for p in paths), '备份来源缺失')
        paths.add(str(host))
        require(not any(self.state.is_relative_to(pathlib.Path(p)) for p in paths), '备份源包含发布状态目录')
        return {'paths': sorted(paths), 'new_state': mapped is None, 'state_host': str(host), 'state_container': target}

    def prepare(self, bundle):
        require(not (self.state / 'plan.json').exists() and not (self.state / 'maintenance').exists(), '发布状态已使用')
        live = self.b.snapshot(); actual = self.check_baseline(live)
        cfg = self.b.yaml_document(self.spec['cpa_config'])
        require(cfg.get('config-version') == 8, 'CPA配置版本待核')
        sources = self.sources(live, cfg)
        base = json.loads(self.b.command(compose(self.spec) + ['config', '--format', 'json']))
        self.b.compose_matches_live(base, live)
        images = self.b.load_bundle(['--bundle-dir', str(bundle)])
        candidate = copy.deepcopy(cfg)
        candidate.setdefault('upstream', {}).setdefault('kilo', {})['state-dir'] = sources['state_container']
        if isinstance(candidate.get('kilo'), dict):
            candidate['kilo'].pop('state-dir', None)
            if not candidate['kilo']: candidate.pop('kilo')
        write(self.state / 'cpa-config.yaml', candidate)
        override = {'services': {s: {'image': images[s], 'pull_policy': 'never'} for s in self.spec['candidates']}}
        mount = {'type': 'bind', 'source': sources['state_host'], 'target': sources['state_container'], 'bind': {'create_host_path': False}}
        existing = map_path(live['cli-proxy-api'], sources['state_container'])
        if existing is None: override['services']['cli-proxy-api']['volumes'] = [mount]
        write(self.state / 'compose.kilo.yaml', override)
        rendered = json.loads(self.b.command(compose(self.spec, self.state / 'compose.kilo.yaml') + ['config', '--format', 'json']))
        expected = copy.deepcopy(base)
        for service, image in images.items(): expected['services'][service].update(image=image, pull_policy='never')
        if existing is None: expected['services']['cli-proxy-api'].setdefault('volumes', []).append(mount)
        expected, rendered = normalize_compose_volumes(expected), normalize_compose_volumes(rendered)
        require(expected == rendered, '候选Compose改变了未授权字段')
        protected = [p for p in sources['paths'] if pathlib.Path(p).is_file()]
        protected += [str(pathlib.Path(__file__).resolve()), str(pathlib.Path(__file__).with_suffix('.sh').resolve()), str(pathlib.Path(__file__).with_name('phase2-release.py').resolve())]
        plan = {'spec': self.spec, 'facts': actual, 'base': base, 'rendered': rendered, 'storage_config': storage_config(candidate), 'images': images, 'sources': sources, 'digests': {p: digest(p) for p in protected}, 'candidate_digests': {n: digest(self.state / n) for n in ['cpa-config.yaml', 'compose.kilo.yaml']}}
        write(self.state / 'plan.json', plan); self.mark('prepared')
        return 'prepared：尚未停服；实际apply须人工DEPLOY'

    def stopped(self, plan):
        for service, old in plan['facts'].items():
            actual = json.loads(self.b.command(['docker', 'inspect', old['id']]))[0]
            require(actual['State']['Running'] == (service == 'caddy'), '停态确认失败')

    def backup(self, plan, rollback=False):
        if rollback:
            require(not any(c['State']['Running'] for c in self.b.snapshot().values()), '回退备份前服务未全停')
        else:
            self.stopped(plan)
        target = self.state / ('backup-rollback-current' if rollback else 'backup'); target.mkdir(mode=0o700)
        output = target / 'data.tar.gz'; paths = plan['sources']['paths']
        with tarfile.open(output, 'w:gz') as archive:
            for path in paths: archive.add(path, arcname=archive_name(path))
        output.chmod(0o600)
        with tarfile.open(output, 'r:gz') as archive:
            names = archive.getnames()
            for path in paths: require(archive_name(path) in names, '备份成员缺失')
            for member in archive:
                if member.isfile():
                    stream = archive.extractfile(member)
                    while stream.read(1024 * 1024): pass
        write(target / 'receipt.json', {'paths': paths, 'bytes': output.stat().st_size, 'sha256': digest(output), 'stopped': True})

    def apply(self, confirm):
        require((self.state / 'stage').read_text().strip() == 'prepared' and not (self.state / 'maintenance').exists(), '非prepared状态；维护失败需定向恢复')
        plan = read(self.state / 'plan.json'); require(plan['spec'] == self.spec, '发布计划漂移')
        confirm()
        live = self.b.snapshot(); require(self.check_baseline(live) == plan['facts'], '现场运行参数漂移')
        for path, value in plan['digests'].items(): require(digest(path) == value, '受保护文件漂移')
        for name, value in plan['candidate_digests'].items(): require(digest(self.state / name) == value, '候选材料漂移')
        require(self.b.image_records() == plan['images'], '候选镜像漂移')
        require(json.loads(self.b.command(compose(self.spec) + ['config', '--format', 'json'])) == plan['base'], 'Compose漂移')
        require(self.sources(live, self.b.yaml_document(self.spec['cpa_config'])) == plan['sources'], '持久来源漂移')
        (self.state / 'maintenance').touch(mode=0o600); self.mark('stopping')
        for service in ['oauth-login', 'cpa-manager-plus', 'cli-proxy-api']:
            self.b.command(compose(self.spec) + ['stop', '-t', '30', service])
        self.stopped(plan)
        if plan['sources']['new_state']: pathlib.Path(plan['sources']['state_host']).mkdir(mode=0o700)
        self.mark('backing_up'); self.backup(plan)
        self.stopped(plan)
        target = pathlib.Path(self.spec['cpa_config'])
        shutil.copyfile(self.state / 'cpa-config.yaml', str(target) + '.kilo-new')
        pathlib.Path(str(target) + '.kilo-new').chmod(0o600)
        os.replace(str(target) + '.kilo-new', target)
        self.mark('installed_stopped')
        return 'installed_stopped：备份与候选配置完成，应用保持停止；未启动、未放流、未完成发布'

    def lifecycle_plan(self):
        plan = read(self.state / 'plan.json')
        require(plan['spec'] == self.spec, '发布计划漂移')
        require(plan.get('rendered') and plan.get('storage_config') and (self.state / 'backup/receipt.json').is_file(), '缺少release03完整备份计划，须在维护前重新prepare')
        for path, value in plan['digests'].items():
            if path != self.spec['cpa_config']:
                require(digest(path) == value, '受保护文件漂移')
        for name, value in plan['candidate_digests'].items():
            require(digest(self.state / name) == value, '候选材料漂移')
        return plan

    def topology(self, plan, rendered, live, ingress_stopped=True):
        services = set(APPS + ['caddy'])
        require(set(rendered.get('services', {})) == services and set(live) == services, '仅支持既有四服务拓扑')
        network = self.spec.get('lifecycle', {}).get('network')
        require(isinstance(network, str) and network and '待填' not in network, '现场bridge网络未填写')
        for service, container in live.items():
            planned = rendered['services'][service]
            require(not planned.get('network_mode'), '不支持host或共享namespace网络')
            require(container['HostConfig']['NetworkMode'] == network and set(container['NetworkSettings']['Networks']) == {network}, '容器网络非唯一已核bridge')
            definitions = planned.get('networks') or {'default': None}
            names = {rendered.get('networks', {}).get(name, {}).get('name', self.spec['project'] + '_' + name) for name in definitions}
            require(names == {network}, 'Compose网络与实际网络不一致')
            if service != 'caddy':
                require(not planned.get('ports'), '应用存在Compose直达端口')
                require(not any((container['HostConfig'].get('PortBindings') or {}).values()) and not any((container['NetworkSettings'].get('Ports') or {}).values()), '应用存在宿主直达端口')
        observed = json.loads(self.b.command(['docker', 'network', 'inspect', network]))[0]
        require(observed.get('Driver') == 'bridge' and observed.get('Scope') == 'local', '不支持非本地bridge网络')
        require((observed.get('Labels') or {}).get('com.docker.compose.project') == self.spec['project'], '网络不属于当前Compose项目')
        options = observed.get('Options') or {}
        require(all(options.get('com.docker.network.bridge.gateway_mode_' + version, 'nat') == 'nat' for version in ['ipv4', 'ipv6']), '不支持routed或非NAT直达网络')
        require(set((observed.get('Containers') or {})).issubset({container['Id'] for container in live.values()}), '网络存在额外旁接容器')
        caddy = live['caddy']
        require(caddy['Id'] == plan['facts']['caddy']['id'], 'Caddy容器身份漂移')
        if ingress_stopped:
            require(not caddy['State']['Running'], 'Caddy入口仍运行')

    def isolate(self, plan, rendered):
        self.b.command(compose(self.spec) + ['stop', '-t', '30', 'caddy'])
        live = self.b.snapshot()
        self.topology(plan, rendered, live)
        return live

    def halt(self):
        failures = []
        for service in ['caddy'] + APPS:
            try:
                self.b.command(compose(self.spec) + ['stop', '-t', '30', service])
            except Exception:
                failures.append(service)
        try:
            live = self.b.snapshot()
            require(not any(container['State']['Running'] for container in live.values()), '仍有服务运行')
        except Exception:
            failures.append('停态核验')
        require(not failures, '停止状态未确认:' + ','.join(failures))

    def image_identities(self, images):
        result = {}
        for service, image_id in images.items():
            image = json.loads(self.b.command(['docker', 'image', 'inspect', image_id]))[0]
            require(image['Id'] == image_id and image['Os'] == 'linux' and image['Architecture'] == 'amd64', '目标镜像ID或平台不符')
            result[service] = image['Id']
        return result

    def capability(self, path, cfg_path, cfg, images, scoped, mode, source_config_sha256):
        receipt = read(path)
        require(receipt.get('schema') == 1 and receipt.get('review') == 'PASS' and receipt.get('reviewed_by'), '缺少已审scope能力回执')
        require(receipt.get('image_ids') == images and receipt.get('config_sha256') == digest(cfg_path), 'scope回执镜像或配置绑定不符')
        require(receipt.get('source_config_sha256') == source_config_sha256, 'scope回执当前配置绑定不符')
        require(receipt.get('mode') == mode, 'scope能力与显式操作模式不符')
        evidence = pathlib.Path(receipt.get('runtime_evidence', ''))
        require(evidence.is_absolute() and evidence.is_file() and evidence.stat().st_size > 0, 'scope实际能力证据缺失')
        require(digest(evidence) == receipt.get('runtime_evidence_sha256'), 'scope实际能力证据漂移')
        bound = dict(receipt, client_ingress='blocked')
        rollback_scope_gate(bound, cfg, scoped)
        return {'receipt': str(pathlib.Path(path).resolve()), 'receipt_sha256': digest(path), 'evidence': str(evidence), 'evidence_sha256': digest(evidence)}

    def runtime(self, plan, rendered, images, compose_files):
        live = self.b.snapshot()
        self.topology(plan, rendered, live)
        self.b.compose_matches_live(rendered, live)
        for service in APPS:
            require(live[service]['State']['Running'] and live[service]['Config']['Image'] == images[service] and live[service]['Image'] == images[service], '运行镜像或服务状态不符')
            labels = live[service]['Config'].get('Labels') or {}
            require(labels.get('com.docker.compose.project.config_files', '').split(',') == compose_files, '运行Compose链不完整')
        return live

    def authenticated_health(self, live):
        cfg = self.b.yaml_document(self.spec['cpa_config'])
        keys = cfg.get('access', {}).get('api-keys', [])
        require(isinstance(keys, list) and keys and all(isinstance(key, str) and key.strip() for key in keys), '健康检查有效Key缺失')
        manager = live['cpa-manager-plus']
        variables = dict(value.split('=', 1) for value in manager['Config'].get('Env', []) if '=' in value)
        require(not variables.get('CPA_MANAGER_ADMIN_KEY'), '内联Admin Key来源待核')
        selected = variables.get('CPA_MANAGER_ADMIN_KEY_FILE')
        require(selected and selected.startswith('/'), '健康检查要求明确Admin Key文件来源')
        host = map_path(manager, selected)
        require(host is not None and host.is_file(), '健康检查Admin Key文件未映射')
        admin = host.read_text(encoding='utf-8').strip()
        require(admin, '健康检查Admin Key为空')
        worker = live['oauth-login']
        env = dict(value.split('=', 1) for value in worker['Config'].get('Env', []) if '=' in value)
        require(not env.get('OAUTH_LOGIN_SECRET') and env.get('OAUTH_LOGIN_SECRET_FILE') == '/run/secrets/oauth_login_secret', 'Worker健康密钥来源待核')
        program = "const fs=require('fs');let s='';process.stdin.on('data',c=>s+=c);process.stdin.on('end',async()=>{try{const k=JSON.parse(s),w=fs.readFileSync('/run/secrets/oauth_login_secret','utf8').trim();for(const [u,t,status] of [['http://cli-proxy-api:8317/v1/models',k.api,200],['http://cpa-manager-plus:18317/api/oauth-login/tasks/00000000-0000-4000-8000-000000000001',k.admin,404],['http://oauth-login:18417/health',w,200]]){for(const [token,expected] of [[t,status],['invalid-kilo-release-'+Date.now(),401]]){const r=await fetch(u,{redirect:'error',headers:{authorization:'Bearer '+token},signal:AbortSignal.timeout(5000)});if(r.status!==expected)throw Error('status');await r.arrayBuffer()}}process.stdout.write('ok')}catch{process.exitCode=1}})"
        for attempt in range(15):
            try:
                if self.b.command(['docker', 'exec', '-i', worker['Id'], 'node', '-e', program], json.dumps({'api': keys[0].strip(), 'admin': admin})) == 'ok':
                    return
            except Exception:
                pass
            if attempt < 14:
                time.sleep(2)
        raise Failure('内网只读鉴权健康失败')

    def checked_health(self, plan, active):
        require(digest(self.spec['cpa_config']) == active['config_sha256'], '运行配置漂移')
        for name, value in active['material_digests'].items():
            require(digest(self.state / name) == value, '运行override漂移')
        self.image_identities(active['images'])
        cfg = self.b.yaml_document(self.spec['cpa_config'])
        proof = self.capability(active['capability']['receipt'], self.spec['cpa_config'], cfg, active['images'], active['scoped_keys'], active['mode'], active['source_config_sha256'])
        require(proof == active['capability'], '已审scope回执或证据漂移')
        live = self.runtime(plan, active['rendered'], active['images'], active['compose_files'])
        self.authenticated_health(live)
        return live

    def verify_current_storage(self, plan, live):
        cfg = self.b.yaml_document(self.spec['cpa_config'])
        require(storage_config(cfg) == plan['storage_config'], '当前存储路径漂移，须定向保全')
        current = copy.deepcopy(plan['rendered'])
        for service, container in live.items():
            original = plan['facts'][service]
            if container['Image'] == original['image_id'] and container['Config']['Image'] == original['image_ref']:
                current['services'][service] = copy.deepcopy(plan['base']['services'][service])
            else:
                require(service in APPS and container['Image'] == plan['images'][service], '回退前实际镜像漂移')
        self.b.compose_matches_live(current, live)

    def save_current_backup(self, plan, live):
        require(not (self.state / 'backup-rollback-current').exists(), '回退保全目录已存在，需定向恢复')
        require(not any(c['State']['Running'] for c in live.values()), '回退保全前服务未全停')
        self.backup(plan, rollback=True)

    def lifecycle(self, action, capability_path=None, revoke=False, confirm=None):
        try:
            plan = self.lifecycle_plan()
            stage = (self.state / 'stage').read_text().strip()
            if action == 'health':
                require(stage in ['candidate_healthy_blocked', 'rollback_healthy_blocked'], '当前阶段不支持健康复核')
                active = read(self.state / 'active.json')
                self.topology(plan, active['rendered'], self.b.snapshot())
                self.checked_health(plan, active)
                return 'health通过；Caddy仍停止，不代表入口已恢复'
            require(confirm is not None and capability_path, '启动或回退须明确确认和scope能力回执')
            if action == 'start':
                require(stage == 'installed_stopped', 'start仅接受已安装停态')
            else:
                require(action == 'rollback' and stage in ['installed_stopped', 'candidate_healthy_blocked', 'start_failed', 'health_failed'], '当前阶段不支持自动回退')
            confirm()
            live = self.isolate(plan, plan['rendered'])
            for service in APPS:
                self.b.command(compose(self.spec) + ['stop', '-t', '30', service])
            live = self.b.snapshot()
            require(not any(c['State']['Running'] for c in live.values()), '启动/回退前未确认全停')
            self.verify_current_storage(plan, live)
            cfg = self.b.yaml_document(self.spec['cpa_config'])
            source_config_sha256 = digest(self.spec['cpa_config'])
            scoped = scope_keys(cfg)
            if action == 'start':
                require(digest(self.spec['cpa_config']) == plan['candidate_digests']['cpa-config.yaml'], '候选配置漂移')
                images, rendered, mode = plan['images'], plan['rendered'], 'scope-enforced'
                override = self.state / 'compose.kilo.yaml'
                command = compose(self.spec, override)
                require(normalize_compose_volumes(json.loads(self.b.command(command + ['config', '--format', 'json']))) == normalize_compose_volumes(rendered), '启动Compose渲染漂移')
                cfg_path = pathlib.Path(self.spec['cpa_config'])
                materials = {'compose.kilo.yaml': digest(override)}
            else:
                self.save_current_backup(plan, live)
                images = {service: plan['facts'][service]['image_id'] for service in APPS}
                mode = 'revoke-scoped-keys' if revoke else 'scope-enforced'
                removed = 0
                if revoke:
                    cfg, scoped, removed = revoke_scope_keys(cfg)
                cfg_path = self.state / 'rollback-config.yaml'
                if revoke:
                    write(cfg_path, cfg)
                else:
                    shutil.copyfile(self.spec['cpa_config'], cfg_path)
                    cfg_path.chmod(0o600)
                override = self.state / 'compose.rollback.yaml'
                write(override, {'services': {s: {'image': image, 'pull_policy': 'never'} for s, image in images.items()}})
                command = compose(self.spec, self.state / 'compose.kilo.yaml') + ['-f', str(override)]
                rendered = normalize_compose_volumes(json.loads(self.b.command(command + ['config', '--format', 'json'])))
                expected = copy.deepcopy(plan['rendered'])
                for service, image in images.items(): expected['services'][service]['image'] = image
                require(rendered == normalize_compose_volumes(expected), '回退改变了镜像之外的Compose字段')
                materials = {name: digest(self.state / name) for name in ['compose.kilo.yaml', 'compose.rollback.yaml']}
                print('回退撤销scope专用Key数量=' + str(removed))
            self.image_identities(images)
            proof = self.capability(capability_path, cfg_path, cfg, images, scoped, mode, source_config_sha256)
            if action == 'rollback':
                target = pathlib.Path(self.spec['cpa_config'])
                shutil.copyfile(cfg_path, str(target) + '.kilo-new')
                pathlib.Path(str(target) + '.kilo-new').chmod(0o600)
                os.replace(str(target) + '.kilo-new', target)
            files = [command[i + 1] for i, value in enumerate(command) if value == '-f']
            active = {'images': images, 'rendered': rendered, 'compose_files': files, 'mode': mode, 'scoped_keys': scoped, 'capability': proof, 'source_config_sha256': source_config_sha256, 'config_sha256': digest(self.spec['cpa_config']), 'material_digests': materials}
            write(self.state / 'active.json', active)
            self.mark(action + '_starting')
            self.b.command(command + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate'] + APPS)
            self.checked_health(plan, active)
            self.mark('candidate_healthy_blocked' if action == 'start' else 'rollback_healthy_blocked')
            return action + '及内网健康通过；Caddy保持停止，流量恢复另行授权'
        except Exception as error:
            self.mark(action + '_failed')
            self.halt()
            raise error


def backend(spec):
    require(os.name == 'posix', '真实入口仅支持Linux/POSIX；Windows只运行合成测试')
    module_spec = importlib.util.spec_from_file_location('kilo_release_base', pathlib.Path(__file__).with_name('phase2-release.py'))
    module = importlib.util.module_from_spec(module_spec); module_spec.loader.exec_module(module)
    module.ROOT, module.STATE, module.PROJECT = pathlib.Path(spec['root']), pathlib.Path(spec['state_dir']), spec['project']
    module.CANDIDATES = spec['candidates']
    return module


def confirm_lifecycle(action):
    require(sys.stdin.isatty(), '启动/回退需要真实TTY')
    expected = 'START' if action == 'start' else 'ROLLBACK'
    print('将停止现有Caddy入口并核验；操作结束入口仍保持停止。输入' + expected + '：', end='', flush=True)
    require(select.select([sys.stdin], [], [], 120)[0], '确认超时')
    require(sys.stdin.readline().rstrip('\r\n') == expected, '确认取消')


@contextlib.contextmanager
def operation_state(state):
    require(os.name == 'posix' and os.geteuid() == 0, '真实入口需要Linux root')
    os.umask(0o077)
    state.mkdir(mode=0o700, exist_ok=True)
    require(state.stat().st_uid == 0 and state.stat().st_mode & 0o777 == 0o700, '发布状态目录权限不符')
    import fcntl
    with (state / 'operation.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare', 'apply', 'start', 'health', 'rollback'])
    parser.add_argument('--plan', required=True)
    parser.add_argument('--bundle-dir')
    parser.add_argument('--capability')
    parser.add_argument('--revoke-scoped-keys', action='store_true')
    args = parser.parse_args(argv)
    require(bool(args.bundle_dir) == (args.action == 'prepare'), 'prepare必须指定本地包；apply不接收包参数')
    require(not args.capability or args.action in ['start', 'rollback'], 'scope能力回执仅用于start/rollback')
    require(not args.revoke_scoped_keys or args.action == 'rollback', '撤销scope Key仅用于显式rollback')
    spec = read(args.plan); validate_spec(spec)
    state = pathlib.Path(spec['state_dir'])
    with operation_state(state):
        base = backend(spec); release = Release(spec, base)
        if args.action == 'prepare':
            result = release.prepare(args.bundle_dir)
        elif args.action == 'apply':
            result = release.apply(base.confirm_deploy)
        else:
            result = release.lifecycle(args.action, args.capability, args.revoke_scoped_keys, lambda: confirm_lifecycle(args.action))
        print(result)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('Kilo发布停止：' + (str(error) if isinstance(error, Failure) else '操作失败，保留状态并定向核查'), file=sys.stderr)
        sys.exit(1)
