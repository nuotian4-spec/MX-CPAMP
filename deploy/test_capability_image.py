#!/usr/bin/env python3
"""CI 新镜像入口验收：只读合成配置、断网，不运行服务或挂载账号。"""

import argparse
import importlib.util
import json
import pathlib
import subprocess
import tempfile


SPEC = importlib.util.spec_from_file_location('capability_image_probe', pathlib.Path(__file__).with_name('capability_probe.py'))
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


def verify_image(image, commit, output):
    cfg = {'config-version': 8, 'access': {'api-keys': ['synthetic-kilo', 'synthetic-codex', 'synthetic-bps', 'synthetic-legacy'],
           'client-key-scopes': [{'api-key': 'synthetic-' + channel, 'channel': channel} for channel in probe.CHANNELS]}}
    with tempfile.TemporaryDirectory(prefix='mx-policy-') as temporary:
        path = pathlib.Path(temporary) / 'config.json'
        path.write_text(json.dumps(cfg), encoding='utf-8')
        path.chmod(0o600)
        result = subprocess.run(probe.probe_command(image, path), capture_output=True, text=True, timeout=60)
        probe.require(result.returncode == 0, '镜像策略入口成功分支退出异常')
        policy = json.loads(result.stdout)
        probe.validate_policy(policy, cfg, probe.digest(path), commit)
        path.write_text('config-version: [invalid', encoding='utf-8')
        failed = subprocess.run(probe.probe_command(image, path), capture_output=True, text=True, timeout=60)
        probe.require(failed.returncode != 0 and not failed.stdout.strip() and
                      'configuration_parse_failed' in failed.stderr, '镜像坏配置分支未正确拒绝')
    evidence = {'schema': 1, 'source_commit': commit, 'image': image, 'network': 'none',
                'account_or_database_mounts': False, 'http_server_started': False,
                'config_policy': policy, 'invalid_config_exit_code': failed.returncode,
                'production_wiring': 'inherited-separate-contract', 'real_provider_inference': False}
    probe.write(output, evidence)
    return evidence


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', required=True)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--output', required=True, type=pathlib.Path)
    args = parser.parse_args()
    verify_image(args.image, args.commit, args.output)
    print('候选镜像策略入口：22 项观察通过，坏配置退出通过。')
