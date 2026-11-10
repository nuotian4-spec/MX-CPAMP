import fs from 'node:fs';
import crypto from 'node:crypto';
import {pathToFileURL} from 'node:url';
import {errorCodes,taskDiagnostics,sanitizeReceipt,businessCodes,publicErrorDiagnostics} from './diagnostics.mjs';

export const models=Object.freeze(['gpt-6-astra','gpt-6.1-sol','gpt-5.6-sol']);
const bases={cpa:'http://127.0.0.1:8317',manager:'http://127.0.0.1:18317',worker:'http://127.0.0.1:18417'};
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
function must(value,code){if(!value)throw new Error(code);}
export async function transport(service,path,key,method='GET',body,timeout=10000){
  const response=await fetch(bases[service]+path,{method,headers:{...(key?{authorization:'Bearer '+key}:{}),'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)}),signal:AbortSignal.timeout(timeout)});
  let value={};try{value=await response.json();}catch{}
  return{status:response.status,value};
}
export async function runBusiness(input,{request=transport,report=value=>process.stdout.write(JSON.stringify(sanitizeReceipt(value))+'\n'),wait=pause,readWorker=id=>{try{return JSON.parse(fs.readFileSync('/tmp/result-'+id+'.json','utf8'));}catch{return{};}}}={}){
  const emit=value=>report(sanitizeReceipt(value));
  const poll=async(check,code,timeout=10000)=>{const until=Date.now()+timeout;while(Date.now()<until){const value=await check();if(value)return value;await wait(200);}throw new Error(code);};
  const batch_id=crypto.randomUUID();
  let expectedEmail=input.credentials.email;
  const row={line_no:1,email:expectedEmail,password:input.credentials.password,totp_secret:input.credentials.totp_secret};
  let created;
  try{created=await request('manager','/api/oauth-login/batches',input.admin,'POST',{batch_id,profile:'bps',items:[row]});}
  catch{created=await request('manager','/api/oauth-login/batches/'+batch_id,input.admin);if(created.status!==200)throw new Error('create_ack_unconfirmed');}
  finally{row.email=row.password=row.totp_secret='';input.credentials.email=input.credentials.password=input.credentials.totp_secret='';}
  must(created.status===200&&created.value.id===batch_id,'batch_create_rejected');
  const done=await poll(async()=>{const result=await request('manager','/api/oauth-login/batches/'+batch_id,input.admin);must(result.status===200,'batch_query_unconfirmed');return ['completed','cancelled','interrupted'].includes(result.value.status)?result.value:null;},'batch_timeout_unconfirmed',26*60*1000);
  must(done.profile==='bps'&&done.items?.length===1&&!Object.hasOwn(done.items[0],'auth_id')&&!JSON.stringify(done).toLowerCase().includes(expectedEmail.toLowerCase()),'batch_result_invalid');
  const task=done.items[0],observed=readWorker(task.id),a=taskDiagnostics(task),b=taskDiagnostics(observed);
  emit({stage:'batch',status:task.status,error_code:errorCodes.has(task.error_code)?task.error_code:null,worker_error_code:errorCodes.has(observed.error_code)?observed.error_code:null,login_stage:a.stage,login_reason:a.reason,worker_stage:b.stage,worker_reason:b.reason});
  if(done.status!=='completed'||task.status!=='succeeded')return false;
  must(/^[a-f0-9]{16}$/.test(task.auth_index||''),'batch_result_invalid');
  const registered=await request('cpa','/v8/management/credentials',input.cpa);
  must(registered.status===200&&registered.value.files?.length===1,'registration_unconfirmed');
  const matches=registered.value.files.filter(entry=>entry.auth_index===task.auth_index&&entry.provider==='bps');
  must(matches.length===1&&typeof matches[0].id==='string'&&matches[0].id&&matches[0].login_method==='2fa','registration_unconfirmed');
  const authID=matches[0].id;
  const listing=async()=>{const result=await request('cpa','/v8/management/credentials',input.cpa);must(result.status===200&&result.value.files?.length===1,'registration_unconfirmed');const only=result.value.files[0];must(only.id===authID&&only.auth_index===task.auth_index&&only.provider==='bps'&&only.login_method==='2fa','registration_unconfirmed');return only;};
  const saved=async()=>{const result=await request('cpa','/v8/management/credentials/download?name='+encodeURIComponent(authID),input.cpa);must(result.status===200&&result.value.type==='bps'&&result.value.profile==='excel'&&result.value.client_id==='app_fnr0pYvVwwFDocDumLG3H2Bp'&&result.value.email?.toLowerCase()===expectedEmail.toLowerCase()&&typeof result.value.access_token==='string'&&Boolean(result.value.access_token)&&typeof result.value.refresh_token==='string'&&Boolean(result.value.refresh_token)&&Boolean(result.value.account_id),'saved_file_unconfirmed');return result.value;};
  let before=await saved();emit({stage:'saved_registered',saved:true,registered:true,single_bps_credential:true});
  const snapshot=async()=>{const result=await request('manager','/api/credential-concurrency',input.admin);must(result.status===200&&result.value.mode==='local'&&/^[a-f0-9]{32}$/.test(result.value.connection_ref)&&result.value.credentials?.length===1,'snapshot_invalid');const only=result.value.credentials[0];must(only.auth_id===authID&&only.provider==='bps','snapshot_invalid');return result.value;};
  const shown=await snapshot();
  let update;try{update=await request('manager','/api/credential-concurrency',input.admin,'PATCH',{auth_id:authID,connection_ref:shown.connection_ref,concurrency_limit:3});}catch{throw new Error('concurrency_write_unconfirmed');}
  must(update.status===200,'concurrency_write_unconfirmed');
  await poll(async()=>{const s=await snapshot();must(s.connection_ref===shown.connection_ref,'snapshot_invalid');return s.credentials[0].concurrency_limit===3&&s.credentials[0].effective_limit===3;},'concurrency_apply_unconfirmed');
  before=await saved();must(before.concurrency_limit===3,'concurrency_apply_unconfirmed');emit({stage:'concurrency',concurrency_override_saved:true});
  let refreshed;try{refreshed=await request('cpa','/v8/management/credentials/refresh',input.cpa,'POST',{name:authID},90000);}catch{throw new Error('refresh_result_unconfirmed');}
  must(refreshed.status===200&&refreshed.value.ok===true,'refresh_rejected');
  must((refreshed.value.auth?.id===authID||refreshed.value.auth?.name===authID)&&refreshed.value.auth?.provider==='bps','refresh_identity_mismatch');
  const after=await saved();
  must(['account_id','email','profile','client_id','type'].every(field=>after[field]===before[field]),'refresh_identity_mismatch');
  const postRefresh=await snapshot();must(after.concurrency_limit===3&&postRefresh.credentials[0].effective_limit===3,'refresh_limit_lost');
  emit({stage:'refresh',same_auth:true,same_profile:true,same_client:true,same_identity:true,refresh_token_present:true,access_token_changed:after.access_token!==before.access_token,refresh_token_changed:after.refresh_token!==before.refresh_token,concurrency_preserved:true,refresh_endpoint_requests:1});
  before=null;for(const field of ['access_token','refresh_token','id_token'])after[field]='';
  const catalog=await request('cpa','/v1/models',input.api);const ids=catalog.value.data?.map(item=>item.id).sort();
  must(catalog.status===200&&ids?.length===3&&ids.join(',')===[...models].sort().join(','),'catalog_mismatch');
  const original=await listing();const successes=Number(original.success);must(Number.isFinite(successes),'auth_target_unconfirmed');
  let response;try{response=await request('cpa','/v1/responses',input.api,'POST',{model:'gpt-5.6-sol',input:'Reply with OK.',reasoning:{effort:'low'},stream:false},120000);}catch{throw new Error('public_request_unconfirmed');}
  emit({stage:'business',model:'gpt-5.6-sol',public_http_status:response.status,client_requests:1,upstream_attempts:'unknown',...(response.status===200?{}:publicErrorDiagnostics(response.value))});
  must(response.status===200,'public_request_rejected');must(response.value.status==='completed'&&typeof response.value.id==='string'&&response.value.id.startsWith('resp_bps_'),'public_terminal_invalid');
  const nonempty=response.value.output?.some(item=>item.type==='message'&&item.content?.some(content=>content.type==='output_text'&&typeof content.text==='string'&&content.text.trim()));
  must(nonempty,'public_output_empty');response.value=null;
  await poll(async()=>Number((await listing()).success)>successes,'auth_target_unconfirmed');
  await poll(async()=>{const s=await snapshot();return s.pool_waiting===0&&s.credentials[0].active===0&&s.credentials[0].pinned_waiting===0;},'admission_not_drained');
  emit({stage:'complete',result:'passed',catalog_exact:true,normal_terminal:true,text_output_nonempty:true,auth_target_confirmed:true,admission_drained:true,client_requests:1,upstream_attempts:'unknown'});
  expectedEmail='';return true;
}
async function main(){
  let input;
  try{
    const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);input=JSON.parse(Buffer.concat(chunks));chunks.length=0;
    let healthy=false;
    for(let attempt=0;attempt<150;attempt++){try{const a=await transport('manager','/health'),b=await transport('cpa','/v1/models',input.api),c=await transport('worker','/health',input.worker);if(a.status===200&&a.value.ok&&b.status===200&&c.status===200){healthy=true;break;}}catch{}await pause(200);}
    must(healthy,'services_not_ready');must((await transport('manager','/api/oauth-login/batches/active')).status===401,'admin_gate_invalid');
    process.stdout.write(JSON.stringify(sanitizeReceipt({stage:'ready',services_healthy:true,admin_gate:true,login_network:'test_proxy_wrapper',production_network_equivalence:false}))+'\n');
    if(input.mode==='execute'&&!await runBusiness(input))process.exitCode=1;
  }catch(error){process.stdout.write(JSON.stringify(sanitizeReceipt({stage:'harness',status:'failed',error_code:businessCodes.has(error.message)?error.message:'harness_error'}))+'\n');process.exitCode=1;}
  finally{if(input?.credentials)input.credentials.email=input.credentials.password=input.credentials.totp_secret='';if(input)input.cpa=input.admin=input.api=input.worker='';}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)await main();
