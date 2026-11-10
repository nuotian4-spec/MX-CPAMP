import fs from 'node:fs';
import crypto from 'node:crypto';
import http from 'node:http';
const key=name=>fs.readFileSync('/run/config/'+name,'utf8').trim();
const cpa='http://127.0.0.1:8317',manager='http://127.0.0.1:18317',admin=key('admin-key'),management=key('cpa-key'),api=key('api-key');
function must(condition,message){if(!condition)throw new Error(message);}
async function request(base,path,key,method='GET',body){const response=await fetch(base+path,{method,headers:{...(key?{authorization:'Bearer '+key}:{}),'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)}),signal:AbortSignal.timeout(5000)});let value;try{value=await response.json()}catch{value={}};return {status:response.status,value,headers:response.headers};}
async function poll(check,label){for(let attempt=0;attempt<150;attempt++){try{const value=await check();if(value)return value}catch{}await new Promise(r=>setTimeout(r,200));}throw new Error(label);}
async function login(profile){
 const task_id=crypto.randomUUID(),password=crypto.randomBytes(24).toString('hex'),totp_secret='JBSWY3DPEHPK3PXP';
 const created=await request(manager,'/api/oauth-login/tasks',admin,'POST',{task_id,profile,email:'isolated@example.invalid',password,totp_secret});
 must(created.status===200&&created.value.id===task_id,'create_'+profile);
 const done=await poll(async()=>{const result=await request(manager,'/api/oauth-login/tasks/'+task_id,admin);if(['failed','cancelled','expired','needs_user_action'].includes(result.value.status))throw new Error('task_'+profile+'_'+result.value.error_code);return result.value.status==='succeeded'?result.value:null;},'login_'+profile);
 const raw=JSON.stringify(done);must(!raw.includes(password)&&!raw.includes(totp_secret)&&!raw.includes('access_token')&&!raw.includes('refresh_token')&&!raw.includes('e30.'),'redaction_'+profile);
 must(done.profile===profile&&done.auth_id,'task_receipt_'+profile);
 const stored=JSON.parse(fs.readFileSync('/cpa-state/auths/'+done.auth_id,'utf8'));
 must(stored.type===profile&&stored.refresh_token==='isolated-refresh-'+profile,'filestore_'+profile);
 must(stored.login_method==='2fa','source_'+profile);
 const registered=await poll(async()=>{const response=await request(cpa,'/v8/management/credentials/models?name='+encodeURIComponent(done.auth_id),management);return response.status===200&&response.value.models?.length?response.value.models:null;},'registered_'+profile);
 console.log(JSON.stringify({profile,status:done.status,provider:stored.type,refresh_token_preserved:true,registered_model_count:registered.length,browser_secrets:false}));
 return {done,models:registered};
}
try{
 await poll(async()=>{const result=await request(manager,'/health');return result.status===200&&result.value.ok;},'manager_health');
 await poll(async()=>{const result=await request(cpa,'/v1/models',api);return result.status===200;},'cpa_health');
 for(const [base,path]of [[manager,'/api/oauth-login/tasks'],[cpa,'/v8/management/credentials']]){const result=await request(base,path);must(result.status===401,'admin_gate');}
 const codex=await login('codex');const shared=codex.models[0].id;must(typeof shared==='string'&&shared,'codex_model');
 const configured=await request(cpa,'/v8/management/config/oauth/providers/bps/models',management,'PUT',[shared,shared]);must(configured.status===200,'bps_config');
 await new Promise(r=>setTimeout(r,1000));
 const bps=await login('bps');must(bps.models.filter(model=>model.id===shared).length===1,'bps_catalog_dedup');
 const models=await request(cpa,'/v1/models',api);must(models.status===200&&models.value.data.filter(model=>model.id===shared).length===1,'public_model_dedup');
 const lostID=crypto.randomUUID();
 let acceptedButLost=false;
 try{await new Promise((resolve,reject)=>{
   const req=http.request(manager+'/api/oauth-login/tasks',{method:'POST',headers:{authorization:'Bearer '+admin,'content-type':'application/json'}},res=>{acceptedButLost=res.statusCode===200;res.destroy();reject(new Error('response_lost'));});
   req.on('error',reject);req.end(JSON.stringify({task_id:lostID,profile:'bps',email:'hold-isolated@example.invalid',password:crypto.randomBytes(24).toString('hex'),totp_secret:'JBSWY3DPEHPK3PXP'}));
 });}catch{}
 must(acceptedButLost,'post_accepted_response_lost');
 const cancelled=await request(manager,'/api/oauth-login/tasks/'+lostID,admin,'DELETE');must(cancelled.status===200,'cancel_known_id');
 await poll(async()=>{const result=await request(manager,'/api/oauth-login/tasks/'+lostID,admin);return result.value.status==='cancelled';},'lost_response_cancelled');
 const listing=await request(cpa,'/v8/management/credentials',management);const listed=listing.value.files||listing.value.items||listing.value.data||[];
 must(listing.status===200&&['codex','bps'].every(provider=>listed.some(item=>item.provider===provider&&item.login_method==='2fa')),'v8_listing');
 must(listed.length===2&&fs.readdirSync('/cpa-state/auths').filter(name=>name.endsWith('.json')).length===2,'cancelled_task_not_imported');
 must(!JSON.stringify(listing.value).includes('refresh_token')&&!JSON.stringify(listing.value).includes('e30.'),'listing_redaction');
 console.log(JSON.stringify({manager_health:true,cpa_models_health:true,unauthorized_status:401,channel_files:2,shared_model_deduplicated:true,source_visible:true,accepted_response_lost_then_cancelled:true,cancelled_task_not_imported:true,external_network:'none',result:'passed'}));
}catch(error){console.error('crossline_check_failed='+String(error.message||'unknown').replace(/[^a-zA-Z0-9_]/g,'_'));process.exitCode=1;}
