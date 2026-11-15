import test from 'node:test';
import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {Tasks} from '../src/tasks.mjs';
import {createServer} from '../src/server.mjs';
import {spawn} from 'node:child_process';
import {safeDiagnostics} from '../src/diagnostics.mjs';
const runner=fileURLToPath(new URL('./fixture-runner.mjs',import.meta.url));
const credentials=(email='ok@example.test',profile='bps')=>({profile,email,password:'secret-canary-password',totp_secret:'JBSWY3DPEHPK3PXP'});
async function settle(tasks,id){for(let i=0;i<200;i++){const t=tasks.get(id);if(!['running','created'].includes(t.status))return t;await new Promise(r=>setTimeout(r,10));}throw new Error('test_timeout');}
test('鉴权、单任务与HTTP结果清理',async()=>{
  const tasks=new Tasks({runner});const secret='test-secret-'.repeat(4);const server=createServer({secret,tasks});await new Promise(r=>server.listen(0,'127.0.0.1',r));const url='http://127.0.0.1:'+server.address().port;
  try{
    assert.equal((await fetch(url+'/tasks',{method:'POST',body:JSON.stringify(credentials())})).status,401);
    const headers={authorization:'Bearer '+secret,'content-type':'application/json'};
    const created=await fetch(url+'/tasks',{method:'POST',headers,body:JSON.stringify(credentials())});assert.equal(created.status,202);const t=await created.json();assert.ok(!JSON.stringify(t).includes('secret-canary'));assert.ok(!JSON.stringify(t).includes('fake-access'));
    const done=await settle(tasks,t.id);assert.equal(done.status,'succeeded');assert.equal(done.result.profile,'excel');assert.equal(done.result.refresh_token,'fake-refresh');
    const cleared=await(await fetch(url+'/tasks/'+t.id,{method:'DELETE',headers})).json();assert.equal(cleared.status,'succeeded');assert.equal(cleared.result_cleared,true);assert.equal(cleared.result,undefined);assert.equal(tasks.get(t.id),null);
  }finally{await tasks.close();await new Promise(r=>server.close(r));}
});
test('需要人工验证与失败只回枚举',async()=>{
  const tasks=new Tasks({runner});try{let t=tasks.create(credentials('challenge@example.test'));const challenge=await settle(tasks,t.id);assert.equal(challenge.status,'needs_user_action');assert.equal(challenge.stage,'totp');assert.equal(challenge.reason,'security_check');t=tasks.create(credentials('bad@example.test'));const done=await settle(tasks,t.id);assert.equal(done.error_code,'protocol_error');assert.equal(done.stage,'unknown');assert.equal(done.reason,'unknown');assert.ok(!JSON.stringify(done).includes('secret-canary'));}finally{await tasks.close();}
});
test('取消与超时结束整个进程组',async()=>{
  const tasks=new Tasks({runner,timeoutMs:300});try{
    const t=tasks.create(credentials('hold@example.test'));assert.throws(()=>tasks.create(credentials()),/busy/);
    await new Promise(r=>setTimeout(r,150));const raw=JSON.parse(tasks.tasks.get(t.id).output);assert.ok(raw.grandchild);
    const done=await tasks.delete(t.id);assert.equal(done.status,'cancelled');
    if(process.platform!=='win32'){for(const pid of [raw.pid,raw.grandchild]){let alive=false;try{process.kill(pid,0);alive=true;}catch{}assert.equal(alive,false,'进程仍存在 '+pid);}}
    const expired=tasks.create(credentials('hold2@example.test'));assert.equal((await settle(tasks,expired.id)).status,'expired');
  }finally{await tasks.close();}
});
test('fd3诊断字段清洗、旧输出兼容以及终态清理后下一行继续',async()=>{
  const tasks=new Tasks({runner});
  try{
    const input=credentials('diagnostic@example.test');const task=tasks.create(input);const done=await settle(tasks,task.id);
    assert.deepEqual(safeDiagnostics(done),{http_status:403,endpoint_category:'chatgpt_home',security_check_subtype:'html_forbidden_unclassified',challenge_header_present:false});
    assert.equal(done.status,'needs_user_action');assert.equal(input.password,'');assert.equal(input.totp_secret,'');assert.ok(!JSON.stringify(done).includes('secret-canary'));assert.ok(!JSON.stringify(done).includes('JBSWY3DPEHPK3PXP'));assert.equal(done.body,undefined);assert.equal(done.url,undefined);
    assert.equal((await tasks.delete(task.id)).result_cleared,true);assert.equal(tasks.get(task.id),null);
    for(const email of ['challenge@example.test','bad@example.test']){const next=tasks.create(credentials(email));const result=await settle(tasks,next.id);assert.deepEqual(safeDiagnostics(result),safeDiagnostics());assert.ok(!JSON.stringify(result).includes('secret-canary'));assert.ok(!JSON.stringify(result).includes('JBSWY3DPEHPK3PXP'));await tasks.delete(next.id);}
    const success=tasks.create(credentials());assert.equal((await settle(tasks,success.id)).status,'succeeded');
  }finally{await tasks.close();}
});
test('真实login入口fd3仅输出固定失败字段，无HTTP时为unknown/null',async()=>{
  const child=spawn(process.execPath,[fileURLToPath(new URL('../src/login.mjs',import.meta.url))],{stdio:['pipe','pipe','pipe','pipe'],windowsHide:true});let output='',logs='';
  child.stdio[3].setEncoding('utf8');child.stdio[3].on('data',chunk=>output+=chunk);for(const stream of [child.stdout,child.stderr])stream.on('data',chunk=>logs+=chunk);
  child.stdin.end(JSON.stringify({profile:'invalid',password:'secret-canary',totp_secret:'secret-canary'}));
  assert.equal(await new Promise((resolve,reject)=>{child.on('error',reject);child.on('close',resolve)}),0);
  const result=JSON.parse(output);assert.equal(result.error_code,'invalid_profile');assert.deepEqual(safeDiagnostics(result),safeDiagnostics());assert.equal(logs,'');assert.ok(!output.includes('secret-canary'));
});
