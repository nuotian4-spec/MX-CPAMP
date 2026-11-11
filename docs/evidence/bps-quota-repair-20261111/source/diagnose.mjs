import {spawn} from 'node:child_process';
import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const directory = path.dirname(fileURLToPath(import.meta.url));
const stateDirectory = 'C:/Users/子羽/.codex/artifacts/mx-full-login-manual-19e8a490cf66';
const label = 'mx-full-login-manual-19e8a490cf66';
const proxy = 'http://host.docker.internal:7890';
const report = value => process.stdout.write(JSON.stringify(value) + '\n');
const must = (value, code) => { if (!value) throw new Error(code); };

async function command(args, input, limit = 2 * 1024 * 1024, timeout = 60000) {
  return await new Promise((resolve, reject) => {
    const child = spawn('docker', args, {stdio:['pipe','pipe','ignore'], windowsHide:true});
    const chunks = [];
    let length = 0, failed = false;
    const timer = setTimeout(() => { failed = true; child.kill(); }, timeout);
    child.stdout.on('data', chunk => {
      chunks.push(chunk); length += chunk.length;
      if (length > limit) { failed = true; child.kill(); }
    });
    child.stdin.on('error', () => {});
    child.on('error', () => { failed = true; });
    child.on('close', code => {
      clearTimeout(timer);
      const output = Buffer.concat(chunks);
      for (const chunk of chunks) chunk.fill(0);
      if (failed || code !== 0) {
        // Worker 的非零退出也只输出其固定脱敏协议。
        if (args.includes('python3') && output.length) {
          try { for (const line of output.toString('utf8').trim().split('\n')) report(JSON.parse(line)); }
          catch {}
        }
        output.fill(0); reject(new Error('local_command_failed'));
      } else resolve(output);
    });
    child.stdin.end(input);
  });
}

async function inspect(name) {
  const raw = await command(['inspect', name, '--format', '{{json .}}']);
  try { return JSON.parse(raw.toString('utf8')); } finally { raw.fill(0); }
}

let raw, input, output;
try {
  const args = process.argv.slice(2);
  must(args.length === 1 && ['--prepare', '--run'].includes(args[0]), 'usage_prepare_or_run');
  const mode = args[0].slice(2);
  const state = JSON.parse(await fs.readFile(path.join(stateDirectory, 'state.json'), 'utf8'));
  must(state.label === label && state.status === 'ready' && state.offline === false, 'state_mismatch');
  must(Date.parse(state.expires_at) - Date.now() > 60000, 'window_expired');
  must(state.names?.cpa === label + '-cpa' && state.names?.worker === label + '-worker', 'state_mismatch');
  const cpa = await inspect(state.names.cpa), worker = await inspect(state.names.worker);
  for (const [role, item] of [['cpa', cpa], ['worker', worker]]) {
    must(item.State.Running && item.Config.Labels?.['mx.full-login-manual'] === label &&
      item.Image === state.images?.[role]?.id && item.HostConfig.LogConfig.Type === 'none', 'runtime_mismatch');
  }
  must(worker.HostConfig.NetworkMode === 'container:' + cpa.Id, 'namespace_mismatch');
  report({stage:'runtime', mode, owned_running_services:true, original_namespace:true,
    proxy_port:7890, expires_at:state.expires_at});
  const code = await fs.readFile(path.join(directory, 'worker.py'), 'utf8');
  const syntax = await command(['exec', '-i', state.names.worker, 'python3', '-c',
    'import sys; compile(sys.stdin.read(), "source-worker", "exec"); import curl_cffi; print("ok")'], code, 1024, 10000);
  must(syntax.toString('utf8').trim() === 'ok', 'dependency_unavailable'); syntax.fill(0);
  // 文件名、凭证和正文只进入内存，Docker stdout 不直接回显。
  raw = await command(['exec', state.names.cpa, 'sh', '-c',
    'for p in /state/auths/*.json; do [ -f "$p" ] || continue; cat "$p" || exit 1; printf "\\000"; done']);
  input = Buffer.concat([Buffer.from(JSON.stringify({mode}) + '\n'), raw]);
  raw.fill(0); raw = undefined;
  output = await command(['exec', '-i', '-e', 'HTTP_PROXY=' + proxy, '-e', 'HTTPS_PROXY=' + proxy,
    '-e', 'http_proxy=' + proxy, '-e', 'https_proxy=' + proxy, state.names.worker,
    'python3', '-c', code], input, 128 * 1024);
  for (const line of output.toString('utf8').trim().split('\n')) report(JSON.parse(line));
} catch (error) {
  const codes = new Set(['usage_prepare_or_run','state_mismatch','window_expired','runtime_mismatch',
    'namespace_mismatch','dependency_unavailable','local_command_failed']);
  report({stage:'diagnostic', status:'stopped', error_code:codes.has(error.message) ? error.message : 'local_error'});
  process.exitCode = 1;
} finally {
  raw?.fill(0); input?.fill(0); output?.fill(0);
  raw = input = output = undefined;
}
