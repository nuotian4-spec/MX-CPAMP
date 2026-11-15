#!/usr/bin/env python3
"""从 main 的固定提交构建三镜像，生成现有发布入口消费的版本包。"""

import argparse
import datetime
import gzip
import hashlib
import importlib.util
import json
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile


ROOT = pathlib.Path(__file__).resolve().parents[1]
ARCHIVE = 'mx-cpamp-images-linux-amd64.tar.gz'
MANIFEST = 'mx-cpamp-images.manifest.json'
SCRIPTS = ('kilo-release.py', 'kilo-release.sh', 'phase2-release.py',
           'mx-update.py', 'mx-update.sh', 'capability_probe.py', 'capability-matrix.json',
           'production-wiring.contract.json', 'production-wiring.evidence.json',
           'mx-update.site.example.json', 'README.update.zh-CN.md')
GENERATED = ('production-wiring.json', 'capability-image-evidence.json')
COMPONENTS = {
    'cpa': ('mx-cpa', 'components/cpa', 'Dockerfile', 'cli-proxy-api'),
    'manager': ('mx-cpamp', 'components/cpamp', 'Dockerfile.manager-server', 'cpa-manager-plus'),
    'worker': ('mx-oauth-login', 'components/oauth-login', 'Dockerfile', 'oauth-login'),
}
VERSION_PATTERN = r'v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?'


class Failure(Exception):
    pass


def require(value, message):
    if not value:
        raise Failure(message)


def run(args, root, capture=False):
    result = subprocess.run([str(arg) for arg in args], cwd=root, check=True,
                            text=True, stdout=subprocess.PIPE if capture else None)
    return result.stdout.strip() if capture else ''


def digest(path):
    result = hashlib.sha256()
    with pathlib.Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            result.update(block)
    return result.hexdigest()


def write_json(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def load_module(name, path):
    specification = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def prepare_wiring(root, commit, staging):
    probe = load_module('release_capability', root / 'deploy' / 'capability_probe.py')
    probe.create_wiring_binding(root, commit, staging / 'production-wiring.json',
                               lambda args: run(args, root, True))


def verify_probe_image(root, image, commit, staging):
    smoke = load_module('release_image_smoke', root / 'deploy' / 'test_capability_image.py')
    smoke.verify_image(image, commit, staging / 'capability-image-evidence.json')


def validate_source(root, version, commit, exact=True):
    require(len(version) <= 128 and re.fullmatch(VERSION_PATTERN, version), '版本必须为 vX.Y.Z 或带预发布后缀的固定版本')
    require(re.fullmatch(r'[0-9a-f]{40}', commit), '提交必须是完整的小写 40 位 SHA')
    run(['git', 'cat-file', '-e', commit + '^{commit}'], root)
    run(['git', 'merge-base', '--is-ancestor', commit, 'origin/main'], root)
    if exact:
        require(run(['git', 'rev-parse', 'HEAD'], root, True) == commit, '工作区 HEAD 与指定提交不一致')
    require(not run(['git', 'status', '--porcelain', '--untracked-files=all'], root, True), '发布工作区必须干净')
    for name in SCRIPTS:
        run(['git', 'cat-file', '-e', commit + ':deploy/' + name], root)
    timestamp = int(run(['git', 'show', '-s', '--format=%ct', commit], root, True))
    return datetime.datetime.fromtimestamp(timestamp, datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')


def build_commands(root, version, commit, build_date):
    commands = {}
    for role, (image, context, dockerfile, _) in COMPONENTS.items():
        command = ['docker', 'build', '--pull', '--platform', 'linux/amd64',
                   '--file', str(root / context / dockerfile), '--tag', image + ':' + version,
                   '--label', 'org.opencontainers.image.revision=' + commit,
                   '--label', 'org.opencontainers.image.version=' + version,
                   '--label', 'io.mx-cpamp.role=' + role]
        arguments = {'VERSION': version, 'SOURCE_COMMIT': commit}
        if role in ('cpa', 'manager'):
            arguments.update(TARGETOS='linux', TARGETARCH='amd64')
        if role == 'cpa':
            arguments = {'VERSION': version, 'COMMIT': commit, 'BUILD_DATE': build_date,
                         'TARGETOS': 'linux', 'TARGETARCH': 'amd64'}
        for key, value in arguments.items():
            command += ['--build-arg', key + '=' + value]
        commands[role] = command + [str(root / context)]
    return commands


def check_image(config, role, version, commit):
    require(config.get('os') == 'linux' and config.get('architecture') == 'amd64', '镜像平台不匹配：' + role)
    labels = config.get('config', {}).get('Labels') or {}
    expected = {'org.opencontainers.image.revision': commit,
                'org.opencontainers.image.version': version, 'io.mx-cpamp.role': role}
    require(all(labels.get(key) == value for key, value in expected.items()), '镜像标签不匹配：' + role)


def inspect_images(root, version, commit):
    result = {}
    for role, (image, _, _, _) in COMPONENTS.items():
        ref = image + ':' + version
        records = json.loads(run(['docker', 'image', 'inspect', ref], root, True))
        require(len(records) == 1, '镜像检查记录数量错误：' + role)
        record = records[0]
        check_image({'os': record.get('Os'), 'architecture': record.get('Architecture'),
                     'config': record.get('Config') or {}}, role, version, commit)
        require(re.fullmatch(r'sha256:[0-9a-f]{64}', record.get('Id', '')), '镜像 ID 格式错误：' + role)
        result[role] = {'ref': ref, 'revision': commit, 'os': 'linux',
                        'architecture': 'amd64', 'accepted_ids': [record['Id']]}
    return result


def archive_images(root, images, destination):
    args = ['docker', 'save'] + [item['ref'] for item in images.values()]
    with destination.open('wb') as output:
        with gzip.GzipFile(filename='', mode='wb', fileobj=output, mtime=0) as compressed:
            with subprocess.Popen(args, cwd=root, stdout=subprocess.PIPE) as process:
                try:
                    shutil.copyfileobj(process.stdout, compressed, length=1024 * 1024)
                finally:
                    process.stdout.close()
                require(process.wait() == 0, 'docker save 失败')


def verify_archive(archive, images, version, commit):
    # 只读取 docker save 的清单和小型配置，不展开镜像层。
    files = {}
    with tarfile.open(archive, 'r|gz') as bundle:
        for member in bundle:
            if member.isfile() and member.size <= 4 * 1024 * 1024 and (
                    member.name == 'manifest.json' or
                    re.fullmatch(r'(?:[0-9a-f]{64}\.json|blobs/sha256/[0-9a-f]{64})', member.name)):
                files[member.name] = bundle.extractfile(member).read()
    require('manifest.json' in files, '归档缺少 docker save 清单')
    records = json.loads(files['manifest.json'])
    require(isinstance(records, list) and len(records) == len(images), '归档镜像数量错误')
    by_ref = {item['ref']: role for role, item in images.items()}
    seen = set()
    for record in records:
        tags = record.get('RepoTags') or []
        require(len(tags) == 1 and tags[0] in by_ref and tags[0] not in seen, '归档镜像标签集合错误')
        ref = tags[0]
        seen.add(ref)
        role = by_ref[ref]
        config_name = record.get('Config', '')
        require(config_name in files, '归档缺少镜像配置：' + role)
        raw = files[config_name]
        config_digest = hashlib.sha256(raw).hexdigest()
        require(config_name in (config_digest + '.json', 'blobs/sha256/' + config_digest), '归档配置摘要与路径不匹配：' + role)
        check_image(json.loads(raw), role, version, commit)
        config_id = 'sha256:' + config_digest
        if config_id not in images[role]['accepted_ids']:
            images[role]['accepted_ids'].append(config_id)
    require(seen == set(by_ref), '归档镜像不完整')


def package_release(root, version, commit, output_root):
    root = pathlib.Path(root).resolve()
    output_root = pathlib.Path(output_root).resolve()
    require(not output_root.is_relative_to(root), '输出目录必须位于源码工作区之外')
    build_date = validate_source(root, version, commit)
    for name in SCRIPTS:
        require((root / 'deploy' / name).is_file(), '缺少发布脚本：' + name)
    destination = output_root / version
    staging = output_root / ('.' + version + '.building')
    require(not destination.exists() and not staging.exists(), '该版本目录或未完成构建已存在，不覆盖')
    staging.mkdir(parents=True)
    prepare_wiring(root, commit, staging)
    for role, command in build_commands(root, version, commit, build_date).items():
        print('构建组件：' + role, flush=True)
        run(command, root)
    images = inspect_images(root, version, commit)
    verify_probe_image(root, images['cpa']['accepted_ids'][0], commit, staging)
    archive_images(root, images, staging / ARCHIVE)
    verify_archive(staging / ARCHIVE, images, version, commit)
    write_json(staging / MANIFEST, {'schema': 1,
               'archive': {'file': ARCHIVE, 'sha256': digest(staging / ARCHIVE)}, 'images': images})
    for name in SCRIPTS:
        shutil.copyfile(root / 'deploy' / name, staging / name)
    assets = {}
    for name in (ARCHIVE, MANIFEST) + SCRIPTS + GENERATED:
        path = staging / name
        assets[name] = {'size': path.stat().st_size, 'sha256': digest(path)}
    candidates = {COMPONENTS[role][3]: {'role': role, 'ref': item['ref'],
                  'revision': item['revision'], 'accepted_ids': item['accepted_ids']}
                  for role, item in images.items()}
    write_json(staging / 'release.json', {'schema': 1, 'version': version, 'commit': commit,
               'platform': 'linux/amd64', 'build_date': build_date,
               'assets': assets, 'candidates': candidates})
    staging.rename(destination)
    print('版本包完成：' + str(destination), flush=True)
    return destination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--output-root', type=pathlib.Path)
    parser.add_argument('--validate-only', action='store_true')
    args = parser.parse_args()
    try:
        if args.validate_only:
            validate_source(ROOT, args.version, args.commit, exact=False)
        else:
            require(args.output_root is not None, '构建需要 --output-root')
            package_release(ROOT, args.version, args.commit, args.output_root)
    except (Failure, subprocess.CalledProcessError, OSError, ValueError, tarfile.TarError) as exc:
        print('发布包失败：' + str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
