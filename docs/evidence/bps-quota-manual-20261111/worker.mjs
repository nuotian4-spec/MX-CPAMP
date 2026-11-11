import fs from 'node:fs';
import {spawn} from 'node:child_process';
import {createServer} from '/app/src/server.mjs';
import {Tasks} from '/app/src/tasks.mjs';

// 空EOF穿过同一代理入口和原login模块，不创建任务或提交账号。
const probe = await new Promise((resolve, reject) => {
  const child = spawn(process.execPath, ['/checks/proxy-runner.mjs','--empty-input-probe'], {
    stdio: ['pipe','ignore','ignore','pipe','pipe'],
    env: {PATH:process.env.PATH, HOME:process.env.HOME, TMPDIR:process.env.TMPDIR, LANG:'C.UTF-8'},
  });
  let result = '', proxy = '';
  const timer = setTimeout(() => child.kill(), 10000);
  child.stdio[3].setEncoding('utf8'); child.stdio[3].on('data', chunk => { result += chunk; });
  child.stdio[4].setEncoding('utf8'); child.stdio[4].on('data', chunk => { proxy += chunk; });
  child.stdin.on('error', () => {}); child.stdin.end();
  child.on('error', reject);
  child.on('close', code => {
    clearTimeout(timer);
    try {
      const response = JSON.parse(result), configured = JSON.parse(proxy);
      if (code !== 0 || response.status !== 'failed' || !configured.proxy_configured) throw new Error();
      result = proxy = '';
      resolve({proxy_configured_after_environment_filter:true, empty_input_rejected:true, probe_process_exited:true, probe_buffers_cleared:true, submitted_login_material:false});
    } catch { reject(new Error('proxy_runner_probe_failed')); }
  });
});
fs.writeFileSync('/tmp/worker-startup.json', JSON.stringify(probe), {mode:0o600});
const secret = fs.readFileSync('/run/secrets/oauth_login_secret','utf8').trim();
const server = createServer({secret, tasks:new Tasks({runner:'/checks/proxy-runner.mjs'})});
server.listen(18417,'0.0.0.0');
const stop = () => {server.close();server.tasks.close().finally(() => process.exit(0));};
process.on('SIGTERM',stop); process.on('SIGINT',stop);
