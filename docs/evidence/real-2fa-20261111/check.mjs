import fs from 'node:fs';
import crypto from 'node:crypto';
import net from 'node:net';
import {errorCodes,taskDiagnostics,sanitizeReceipt,taskStatuses} from './diagnostics.mjs';

const chunks=[];
for await (const chunk of process.stdin) chunks.push(chunk);
const input=JSON.parse(Buffer.concat(chunks).toString('utf8'));
chunks.length=0;
const cpa='http://127.0.0.1:8317', manager='http://127.0.0.1:18317', worker='http://127.0.0.1:18417';
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const terminal=new Set(['succeeded','failed','needs_user_action','cancelled','expired']);
const safeCode=value=>errorCodes.has(value)?value:null;
function report(value){process.stdout.write(JSON.stringify(sanitizeReceipt(value))+'\n');}
async function request(base,path,key,method='GET',body){
  const response=await fetch(base+path,{method,headers:{...(key?{authorization:'Bearer '+key}:{}),'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)}),signal:AbortSignal.timeout(10000)});
  let value;try{value=await response.json();}catch{value={};}
  return {status:response.status,value};
}
async function ready(){
  for(let i=0;i<150;i++){
    try{
      const a=await request(manager,'/health');
      const b=await request(cpa,'/v1/models',input.api);
      const c=await request(worker,'/health',input.worker);
      if(a.status===200&&a.value.ok&&b.status===200&&c.status===200)return;
    }catch{}
    await pause(200);
  }
  throw new Error('services_not_ready');
}
async function proxyReachable(){
  await new Promise((resolve,reject)=>{
    const socket=net.connect({host:'host.docker.internal',port:7890});
    socket.setTimeout(5000);
    socket.once('connect',()=>{socket.destroy();resolve();});
    socket.once('error',()=>{socket.destroy();reject(new Error('proxy_unreachable'));});
    socket.once('timeout',()=>{socket.destroy();reject(new Error('proxy_unreachable'));});
  });
}
async function login(profile){
  const task_id=crypto.randomUUID();
  const body={task_id,profile,email:input.credentials.email,password:input.credentials.password,totp_secret:input.credentials.totp_secret};
  report({stage:'create',profile});
  let created;
  try{created=await request(manager,'/api/oauth-login/tasks',input.admin,'POST',body);}
  catch{
    const known=await request(manager,'/api/oauth-login/tasks/'+task_id,input.admin);
    if(known.status!==200)throw new Error('create_ack_unconfirmed');
    created={status:200,value:known.value};
  }
  finally{body.password='';body.totp_secret='';}
  // POST 只发送一次；回执丢失也按预先生成的 ID 查询。
  if(created.status!==200) return {stage:'create',profile,status:'failed',error_code:'create_rejected'};
  const deadline=Date.now()+26*60*1000;
  let done;
  while(Date.now()<deadline){
    const current=await request(manager,'/api/oauth-login/tasks/'+task_id,input.admin);
    if(current.status!==200)throw new Error('task_query_unconfirmed');
    if(terminal.has(current.value.status)){done=current.value;break;}
    await pause(1000);
  }
  if(!done)throw new Error('task_timeout_unconfirmed');
  let observed={};
  try{observed=JSON.parse(fs.readFileSync('/tmp/result-'+task_id+'.json','utf8'));}catch{}
  const loginDiagnostic=taskDiagnostics(done),workerDiagnostic=taskDiagnostics(observed);
  const diagnostic={login_stage:loginDiagnostic.stage,login_reason:loginDiagnostic.reason,worker_stage:workerDiagnostic.stage,worker_reason:workerDiagnostic.reason};
  if(!taskStatuses.has(done.status))throw new Error('task_receipt_invalid');
  if(done.status!=='succeeded')return {stage:'login',profile,status:done.status,error_code:safeCode(done.error_code),worker_error_code:safeCode(observed.error_code),...diagnostic};
  if(done.profile!==profile||typeof done.auth_id!=='string'||!done.auth_id)throw new Error('task_receipt_invalid');
  const listing=await request(cpa,'/v8/management/credentials',input.cpa);
  const runtime=(listing.value.files||[]).find(item=>(item.name===done.auth_id||item.id===done.auth_id)&&item.provider===profile);
  if(listing.status!==200||!runtime||runtime.login_method!=='2fa')throw new Error('registration_unconfirmed');
  const stored=await request(cpa,'/v8/management/credentials/download?name='+encodeURIComponent(done.auth_id),input.cpa);
  const saved=stored.status===200&&stored.value.type===profile&&stored.value.email?.toLowerCase()===input.credentials.email.toLowerCase()&&typeof stored.value.access_token==='string'&&Boolean(stored.value.access_token)&&typeof stored.value.refresh_token==='string'&&Boolean(stored.value.refresh_token)&&stored.value.login_method==='2fa';
  if(!saved)throw new Error('saved_file_unconfirmed');
  stored.value=null;
  return {stage:'saved_registered',profile,status:'succeeded',saved:true,registered:true,refresh_token_present:true,model_request_sent:false,...diagnostic};
}
try{
  await ready();
  await proxyReachable();
  const unauth=await request(manager,'/api/oauth-login/tasks');
  if(unauth.status!==401)throw new Error('admin_gate_invalid');
  report({stage:'ready',services_healthy:true,admin_gate:true,local_proxy_port_reachable:true,login_network:'test_proxy_wrapper',production_network_equivalence:false});
  if(input.mode!=='preflight'){
    for(const profile of input.profiles){
      const result=await login(profile);
      report(result);
      if(result.status!=='succeeded')process.exitCode=1;
      if(result.error_code==='import_unconfirmed'||result.error_code==='worker_stop_unconfirmed')break;
    }
  }
}catch(error){
  const allowed=new Set(['services_not_ready','proxy_unreachable','create_ack_unconfirmed','task_query_unconfirmed','task_timeout_unconfirmed','task_receipt_invalid','registration_unconfirmed','saved_file_unconfirmed','admin_gate_invalid']);
  report({stage:'harness',status:'failed',error_code:allowed.has(error.message)?error.message:'harness_error'});
  process.exitCode=1;
}finally{
  if(input.credentials){input.credentials.email='';input.credentials.password='';input.credentials.totp_secret='';}
  input.admin=input.cpa=input.api=input.worker='';
}
