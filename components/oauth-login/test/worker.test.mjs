import test from 'node:test';
import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {Tasks} from '../src/tasks.mjs';
import {createServer} from '../src/server.mjs';
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
  const tasks=new Tasks({runner});try{let t=tasks.create(credentials('challenge@example.test'));assert.equal((await settle(tasks,t.id)).status,'needs_user_action');t=tasks.create(credentials('bad@example.test'));const done=await settle(tasks,t.id);assert.equal(done.error_code,'protocol_error');assert.ok(!JSON.stringify(done).includes('secret-canary'));}finally{await tasks.close();}
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
