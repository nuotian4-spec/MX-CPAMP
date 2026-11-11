import fs from 'node:fs';
import {createServer} from '/app/src/server.mjs';
import {Tasks} from '/app/src/tasks.mjs';

const codes = new Set(['needs_user_action','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','process_failed','output_invalid']);
// 只保留原 Worker 的终态稳定码，避免 Manager 清理任务后失去诊断依据。
class ObservedTasks extends Tasks {
  finish(task, status, code) {
    super.finish(task, status, code);
    if (!/^[a-f0-9-]{36}$/.test(task.id)) return;
    fs.writeFileSync('/tmp/result-'+task.id+'.json', JSON.stringify({status:task.status,error_code:codes.has(task.error_code)?task.error_code:null}), {mode:0o600});
  }
}
const secret = fs.readFileSync('/run/secrets/oauth_login_secret','utf8').trim();
const server = createServer({secret,tasks:new ObservedTasks({runner:'/checks/proxy-runner.mjs'})});
server.listen(18417,'0.0.0.0');
process.on('SIGTERM',()=>{server.close();server.tasks.close().finally(()=>process.exit(0));});
