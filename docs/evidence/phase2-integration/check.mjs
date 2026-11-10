import crypto from 'node:crypto';
const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);const input=JSON.parse(Buffer.concat(chunks));chunks.length=0;
const cpa='http://127.0.0.1:8317',manager='http://127.0.0.1:18317',fixture='http://127.0.0.1:18517';
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms)),controllers=[];
function must(value,code){if(!value)throw new Error(code);}
function report(value){process.stdout.write(JSON.stringify(value)+'\n');}
async function request(base,path,key,method='GET',body){const response=await fetch(base+path,{method,headers:{...(key?{authorization:'Bearer '+key}:{}),'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)}),signal:AbortSignal.timeout(5000)});let value={};try{value=await response.json();}catch{}return{status:response.status,value};}
async function poll(check,code,timeout=12000){const until=Date.now()+timeout;while(Date.now()<until){const value=await check();if(value)return value;await pause(100);}throw new Error(code);}
async function ready(){await poll(async()=>{try{const a=await request(manager,'/health'),b=await request(cpa,'/v1/models',input.api),c=await request(fixture,'/control');return a.status===200&&a.value.ok&&b.status===200&&c.status===200;}catch{return false;}},'services_not_ready',30000);}
async function snapshot(){const result=await request(manager,'/api/credential-concurrency',input.admin);must(result.status===200&&result.value.mode==='local'&&result.value.default_limit===20&&/^[a-f0-9]{32}$/.test(result.value.connection_ref),'bff_snapshot');const serialized=JSON.stringify(result.value);must(!['phase2-config-one','phase2-config-two','phase2-refresh-','access_token','refresh_token','e30.'].some(marker=>serialized.includes(marker)),'bff_redaction');return result.value;}
async function state(){return(await request(fixture,'/control')).value;}
async function limit(auth_id,value){const before=await snapshot();const result=await request(manager,'/api/credential-concurrency',input.admin,'PATCH',{auth_id,connection_ref:before.connection_ref,concurrency_limit:value});must(result.status===200,'bff_limit_write');await poll(async()=>{const s=await snapshot(),entry=s.credentials.find(item=>item.auth_id===auth_id);must(s.connection_ref===before.connection_ref,'bff_connection_changed');return entry&&entry.effective_limit===(value??20)&&entry.concurrency_limit===value;},'bff_limit_apply');}
async function batch(){
 const batch_id=crypto.randomUUID(),password=crypto.randomBytes(24).toString('hex'),totp_secret='JBSWY3DPEHPK3PXP';
 const emails=['first-phase2@example.invalid','fail-phase2@example.invalid','last-phase2@example.invalid'];
 const redacted=value=>{const raw=JSON.stringify(value);must(![...emails,password,totp_secret,'access_token','refresh_token','e30.'].some(marker=>raw.includes(marker)),'batch_redaction');must(value.items?.every(item=>!Object.hasOwn(item,'auth_id')),'batch_redaction');};
 const created=await request(manager,'/api/oauth-login/batches',input.admin,'POST',{batch_id,profile:'codex',items:emails.map((email,index)=>({line_no:index+1,email,password,totp_secret}))});
 must(created.status===200&&created.value.id===batch_id,'batch_create');redacted(created.value);
 const done=await poll(async()=>{const result=await request(manager,'/api/oauth-login/batches/'+batch_id,input.admin);must(result.status===200,'batch_query');redacted(result.value);if(result.value.error_code==='credential_mapping_unconfirmed'||result.value.items?.some(item=>item.error_code==='credential_mapping_unconfirmed'))throw new Error('credential_mapping_unconfirmed');return result.value.status==='completed'?result.value:null;},'batch_terminal');
 must(done.items?.map(item=>item.status).join(',')==='succeeded,failed,succeeded','batch_continue');
 const tracked=(await state()).tasks;must(tracked.max_active===1&&tracked.events.length===6,'batch_serial');
 for(let i=0;i<6;i++)must(tracked.events[i].event===(i%2?'finished':'created'),'batch_serial_order');
 const latest=await request(manager,'/api/oauth-login/batches/latest?profile=codex',input.admin);must(latest.status===200&&latest.value?.id===batch_id&&latest.value.status==='completed'&&latest.value.items?.[2]?.status==='succeeded','batch_latest_terminal');redacted(latest.value);
 const duplicate=await request(manager,'/api/oauth-login/batches',input.admin,'POST',{batch_id,profile:'codex',items:[{line_no:1,email:emails[0],password,totp_secret}]});must(duplicate.status===200,'batch_dedup_receipt');redacted(duplicate.value);
 must((await state()).tasks.events.length===6,'batch_no_replay');
 const listing=await request(cpa,'/v8/management/credentials',input.cpa);must(listing.status===200&&Array.isArray(listing.value.files),'batch_registration');
 const ids=[];
 for(const item of done.items.filter(item=>item.status==='succeeded')){
  must(/^[a-f0-9]{16}$/.test(item.auth_index||''),'batch_index');
  const matches=listing.value.files.filter(entry=>entry.auth_index===item.auth_index&&entry.provider==='codex');
  must(matches.length===1&&typeof matches[0].id==='string'&&matches[0].id&&matches[0].login_method==='2fa','batch_registration');
  const id=matches[0].id,stored=await request(cpa,'/v8/management/credentials/download?name='+encodeURIComponent(id),input.cpa);
  must(stored.status===200&&stored.value.type==='codex'&&stored.value.profile==='codex'&&stored.value.client_id==='app_EMoamEEZ73f0CkXaXp7hrann'&&stored.value.email===emails[item.line_no-1]&&stored.value.account_id&&stored.value.access_token&&stored.value.refresh_token,'batch_saved_identity');
  stored.value=null;ids.push(id);
 }
 must(ids.length===2&&new Set(ids).size===2,'batch_registration');
 report({stage:'batch',serial:true,failure_continued:true,latest_terminal_recovered:true,duplicate_not_replayed:true,mapped_registered_saved:true,synthetic_only:true});
 return ids;
}
function launch(marker,model){const controller=new AbortController();controllers.push(controller);const result=fetch(cpa+'/v1/chat/completions',{method:'POST',headers:{authorization:'Bearer '+input.api,'content-type':'application/json'},body:JSON.stringify({model,messages:[{role:'user',content:marker}],stream:false}),signal:controller.signal}).then(async res=>({status:res.status,value:await res.json()})).catch(()=>({aborted:true}));return{controller,result};}
async function concurrency(){
 const first=launch('hold-1','fixture-model-a');await poll(async()=>(await state()).pending.some(item=>item.marker==='hold-1'),'first_upstream');
 const second=launch('hold-2','fixture-model-b');await poll(async()=>(await state()).pending.length===2,'second_upstream');
 let upstreamState=await state();must(new Set(upstreamState.pending.map(item=>item.key)).size===2,'independent_credentials');
 await poll(async()=>{const s=await snapshot();return s.credentials.filter(item=>item.source==='config').every(item=>item.active===1)&&s.pool_waiting===0;},'active_per_credential');
 const third=launch('hold-3','fixture-model-a');await poll(async()=>(await snapshot()).pool_waiting===1,'pool_wait');must(!(await state()).pending.some(item=>item.marker==='hold-3'),'wait_not_sent');
 const fourth=launch('hold-4','fixture-model-b');await poll(async()=>(await snapshot()).pool_waiting===2,'second_pool_wait');fourth.controller.abort();must((await fourth.result).aborted,'queued_abort');await poll(async()=>(await snapshot()).pool_waiting===1,'queued_cancel_released');
 must(!(await state()).pending.some(item=>item.marker==='hold-4'),'cancelled_wait_not_sent');
 first.controller.abort();must((await first.result).aborted,'active_abort');
 await poll(async()=>{const s=await state();return s.closed.some(item=>item.marker==='hold-1')&&!s.pending.some(item=>item.marker==='hold-1');},'cancelled_upstream_exited');
 await poll(async()=>{const s=await state();return s.pending.some(item=>item.marker==='hold-3')&&s.pending.length===2;},'pool_woken');
 await poll(async()=>{const s=await snapshot();return s.pool_waiting===0&&s.credentials.filter(item=>item.source==='config').every(item=>item.active===1);},'post_wake_counts');
 for(const [marker,handle]of [['hold-2',second],['hold-3',third]]){const released=await request(fixture,'/control/release',undefined,'POST',{marker});must(released.status===200,'fixture_release');must((await handle.result).status===200,'synthetic_response');await poll(async()=>(await state()).closed.some(item=>item.marker===marker),'released_upstream_exited');}
 await poll(async()=>{const s=await snapshot(),u=await state();return s.pool_waiting===0&&s.credentials.every(item=>item.active===0&&item.pinned_waiting===0)&&u.pending.length===0;},'admission_drained');
 report({stage:'concurrency',cross_model_shared:true,independent_credentials:true,pool_wait_observed:true,queued_cancel_removed:true,active_cancel_upstream_exit:true,release_woke_waiter:true,all_drained:true,real_upstream_called:false});
}
try{
 await ready();
 for(const path of ['/api/oauth-login/batches/active','/api/credential-concurrency'])must((await request(manager,path)).status===401,'admin_gate');
 report({stage:'ready',services_healthy:true,unauthorized_status:401,external_network:'none'});
 if(!input.preflight){
  const ids=await batch(),initial=await snapshot();must(initial.credentials.filter(item=>item.source==='config').length===2,'config_credentials');
  must(initial.credentials.every(item=>item.effective_limit===20&&item.concurrency_limit===null),'initial_defaults');
  await limit(ids[0],2);const changed=await snapshot();must(changed.credentials.find(item=>item.auth_id===ids[1]).effective_limit===20,'oauth_independent');
  const stored=await request(cpa,'/v8/management/credentials/download?name='+encodeURIComponent(ids[0]),input.cpa);must(stored.status===200&&stored.value.concurrency_limit===2,'oauth_limit_persist');stored.value=null;
  await limit(ids[0],null);
  const configIds=(await snapshot()).credentials.filter(item=>item.source==='config').map(item=>item.auth_id).sort();
  await limit(configIds[0],2);await limit(configIds[0],null);
  for(const id of configIds)await limit(id,1);
  must((await snapshot()).credentials.filter(item=>item.source==='config').map(item=>item.auth_id).sort().join(',')===configIds.join(','),'config_identity_stable');
  report({stage:'bff',oauth_set_default_restored:true,oauth_persisted:true,config_set_default_restored:true,config_identity_stable:true,other_credential_unchanged:true});
  await concurrency();
  report({stage:'complete',result:'passed',synthetic_login:true,synthetic_upstream:true});
 }
}catch(error){const code=String(error?.message||'integration_failed');report({stage:'failed',error_code:/^[a-z_]{1,80}$/.test(code)?code:'integration_failed'});process.exitCode=1;}
finally{for(const controller of controllers)controller.abort();input.cpa=input.admin=input.api=input.worker='';}
