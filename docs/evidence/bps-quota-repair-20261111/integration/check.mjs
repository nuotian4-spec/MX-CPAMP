const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);
const input=JSON.parse(Buffer.concat(chunks));chunks.length=0;
const cpa='http://127.0.0.1:8317',manager='http://127.0.0.1:18317';
const report=value=>process.stdout.write(JSON.stringify(value)+'\n');
const must=(value,code)=>{if(!value)throw new Error(code);};
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function request(base,path,key,method='GET',body){
  const response=await fetch(base+path,{method,headers:{...(key?{authorization:'Bearer '+key}:{}),'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)}),signal:AbortSignal.timeout(8000)});
  let value={};try{value=await response.json();}catch{}
  return{status:response.status,value};
}
async function poll(check,code,timeout=15000){
  const until=Date.now()+timeout;while(Date.now()<until){const value=await check();if(value)return value;await pause(100);}throw new Error(code);
}
const canonical=value=>JSON.stringify(value,(_,v)=>v&&typeof v==='object'&&!Array.isArray(v)?Object.fromEntries(Object.entries(v).sort(([a],[b])=>a.localeCompare(b))):v);
function withoutModels(value){const copy=structuredClone(value);delete copy.oauth.providers.bps.models;return copy;}
async function credentials(){const result=await request(cpa,'/v8/management/credentials',input.cpa);must(result.status===200&&Array.isArray(result.value.files),'credential_listing');return result.value.files;}
async function models(){const result=await request(manager,'/api/bps/models',input.admin);must(result.status===200&&Array.isArray(result.value.models)&&result.value.connection_ref&&result.value.revision,'models_contract');return result.value;}
async function cpaConfig(){const result=await request(cpa,'/v8/management/config',input.cpa);must(result.status===200&&result.value.oauth?.providers?.bps,'cpa_config');return result.value;}
try{
  await poll(async()=>{try{return(await request(manager,'/health')).status===200&&(await request(cpa,'/v1/models',input.api)).status===200;}catch{return false;}},'services_not_ready',30000);
  for(const endpoint of ['/api/bps/settings','/api/bps/models','/api/bps/access?auth_index=unknown','/api/bps/usage?auth_index=unknown','/v0/management/codex-inspection/runs','/api/oauth-login/capabilities'])must((await request(manager,endpoint)).status===401,'admin_gate');
  const loginCapability=await request(manager,'/api/oauth-login/capabilities',input.admin);
  must(loginCapability.status===200&&loginCapability.value.password_login_configured===false,'login_surface_without_worker');
  must((await request(cpa,'/v8/management/bps/credentials/access?auth_index=unknown')).status===401,'cpa_admin_gate');
  must((await request(cpa,'/v8/management/bps/credentials/usage?auth_index=unknown')).status===401,'cpa_usage_admin_gate');
  const listing=await poll(async()=>{const files=await credentials();return files.length===1&&files[0].provider==='bps'?files:null;},'synthetic_registration');
  const selected=listing[0];must(selected.auth_index&&selected.name,'synthetic_reference');
  const settings=await request(manager,'/api/bps/settings',input.admin);must(settings.status===200&&/^[a-f0-9]{32}$/.test(settings.value.connection_ref),'connection_ref');
  const reference=settings.value.connection_ref;
  const accessPath='/api/bps/access?auth_index='+encodeURIComponent(selected.auth_index)+'&connection_ref=';
  must((await request(manager,accessPath+'0'.repeat(32),input.admin)).status===409,'instance_reference_rejected');
  const access=await request(manager,accessPath+reference,input.admin);
  must(access.status===200&&access.value.provider==='bps'&&access.value.error==='credential_incomplete'&&access.value.allowed===null&&access.value.models===null&&access.value.quota===null&&access.value.inference_verified===false&&access.value.auto_recover_eligible===false,'access_incomplete_contract');
  report({stage:'access',admin_unauthorized_status:401,instance_reference_rejected:true,credential_incomplete:true,quota_unknown:true,real_access_called:false});

  const cpaUsagePath='/v8/management/bps/credentials/usage?auth_index='+encodeURIComponent(selected.auth_index)+'&auth_id='+encodeURIComponent(selected.name);
  const cpaUsage=await request(cpa,cpaUsagePath,input.cpa);
  must(cpaUsage.status===200&&cpaUsage.value.provider==='bps'&&cpaUsage.value.source==='chatgpt_account_usage'&&cpaUsage.value.error==='credential_incomplete'&&cpaUsage.value.rate_limit===null&&cpaUsage.value.plan_type===null,'cpa_usage_incomplete_contract');
  const usagePath='/api/bps/usage?auth_index='+encodeURIComponent(selected.auth_index)+'&connection_ref=';
  must((await request(manager,usagePath+'0'.repeat(32),input.admin)).status===409,'usage_instance_reference_rejected');
  const usage=await request(manager,usagePath+reference,input.admin);
  must(usage.status===200&&usage.value.provider==='bps'&&usage.value.source==='chatgpt_account_usage'&&usage.value.error==='credential_incomplete'&&usage.value.rate_limit===null&&usage.value.plan_type===null,'manager_usage_incomplete_contract');
  report({stage:'usage',cpa_admin_gate:true,manager_admin_gate:true,instance_reference_rejected:true,provider:'bps',source:'chatgpt_account_usage',credential_incomplete:true,quota_unknown:true,real_usage_called:false});

  const initial=await models(),before=await cpaConfig();must(initial.models.length===0,'initial_models_empty');
  const draft=['gpt-6-luna'];
  must((await models()).models.length===0&&(await cpaConfig()).oauth.providers.bps.models.length===0,'read_does_not_publish');
  const rejected=await request(manager,'/api/bps/models',input.admin,'PUT',{models:draft,connection_ref:'0'.repeat(32),revision:initial.revision});must(rejected.status===409,'model_instance_reference_rejected');
  const saved=await request(manager,'/api/bps/models',input.admin,'PUT',{models:draft,connection_ref:initial.connection_ref,revision:initial.revision});must(saved.status===200,'manual_model_save');
  await poll(async()=>{const response=await request(cpa,'/v8/management/credentials/models?name='+encodeURIComponent(selected.name),input.cpa);return response.status===200&&response.value.models?.some(item=>item.id===draft[0]);},'model_registered');
  const after=await cpaConfig();must(canonical(after.oauth.providers.bps.models)===canonical(draft)&&canonical(withoutModels(before))===canonical(withoutModels(after)),'unrelated_yaml_preserved');
  const current=await models();must(canonical(current.models)===canonical(draft),'published_models_readback');
  const stale=await request(manager,'/api/bps/models',input.admin,'PUT',{models:[],connection_ref:current.connection_ref,revision:initial.revision});must(stale.status===409&&stale.value.code==='config_changed','stale_revision_rejected');
  const cleared=await request(manager,'/api/bps/models',input.admin,'PUT',{models:[],connection_ref:current.connection_ref,revision:current.revision});must(cleared.status===200,'clear_models');
  await poll(async()=>{const response=await request(cpa,'/v8/management/credentials/models?name='+encodeURIComponent(selected.name),input.cpa);return response.status===200&&response.value.models?.length===0;},'model_clear_registered');
  must((await credentials()).some(item=>item.auth_index===selected.auth_index)&&canonical(withoutModels(before))===canonical(withoutModels(await cpaConfig())),'clear_preserves_credential_and_yaml');
  report({stage:'models',read_does_not_publish:true,manual_save_registered:true,clear_preserves_credential:true,unrelated_yaml_preserved:true,stale_revision_rejected:true});

  const originalConfig=await request(manager,'/usage-service/config',input.admin);must(originalConfig.status===200&&originalConfig.value.config,'manager_config');
  const config=structuredClone(originalConfig.value.config);delete config.cpaConnection.managementKeyConfigured;
  config.codexInspection={...config.codexInspection,enabled:false,targetTypes:['bps'],targetType:'bps',autoActionMode:'none',autoRecoverEnabled:false,workers:1,retries:0};
  const configured=await request(manager,'/usage-service/config',input.admin,'PUT',{config});
  const configErrorCodes=new Set(['connection_env_managed','account_processing_policy_env_locked','cpa_connection_already_bound','cpa_connection_required_for_monitoring','cpa_connection_required','invalid_admin_key','invalid_management_key','usage_service_not_configured']);
  report({stage:'inspection_config',http_status:configured.status,error_code:configured.status===200?null:configErrorCodes.has(configured.value.code)?configured.value.code:'unclassified'});
  must(configured.status===200&&configured.value.config.codexInspection.targetTypes?.join(',')==='bps','inspection_config');
  const started=await request(manager,'/v0/management/codex-inspection/run',input.admin,'POST');must(started.status===200&&started.value.run?.id,'inspection_start');
  const detail=await poll(async()=>{const response=await request(manager,'/v0/management/codex-inspection/runs/'+started.value.run.id,input.admin);must(response.status===200,'inspection_query');return response.value.run?.status==='completed'?response.value:null;},'inspection_complete');
  must(detail.run.totalFiles===1&&detail.run.probeSetCount===1&&detail.results?.length===1,'inspection_set_one');
  const result=detail.results[0];
  must(result.provider==='bps'&&result.action==='keep'&&result.autoRecoverEligible===false&&result.usedPercent==null&&!result.executedAction,'inspection_readonly_result');
  must(detail.run.deleteCount===0&&detail.run.disableCount===0&&detail.run.enableCount===0&&detail.run.reauthCount===0,'inspection_no_auto_actions');
  const summary=JSON.parse(result.errorDetail||'{}');must(summary.error==='credential_incomplete'&&summary.usage_source==='chatgpt_account_usage'&&summary.usage_error==='credential_incomplete','inspection_incomplete_record');
  const persisted=await request(manager,'/v0/management/codex-inspection/runs/'+detail.run.id,input.admin);must(persisted.status===200&&persisted.value.results[0].provider==='bps'&&persisted.value.results[0].usedPercent==null,'inspection_record_roundtrip');
  const finalFiles=await credentials();must(finalFiles.length===1&&finalFiles[0].auth_index===selected.auth_index&&finalFiles[0].disabled===selected.disabled,'inspection_credential_unchanged');
  report({stage:'inspection',probe_set_count:1,provider:'bps',action:'keep',quota_unknown:true,no_auto_actions:true,persisted:true,scheduler_enabled:false});

  report({stage:'snapshot_identity',file_name_matches:result.fileName===selected.name,auth_index_matches:result.authIndex===selected.auth_index,account_id_present:typeof result.accountId==='string'&&Boolean(result.accountId),account_id_matches_fixture:result.accountId==='synthetic-bps-account',account_snapshot_present:typeof result.accountSnapshot==='string'&&Boolean(result.accountSnapshot)});
  must(result.fileName===selected.name&&result.authIndex===selected.auth_index&&result.accountId==='synthetic-bps-account','snapshot_exact_inspection_identity');
  const snapshotAccount={auth_file_snapshot:result.fileName,auth_index:result.authIndex,auth_provider_snapshot:'bps',auth_account_id_snapshot:result.accountId,...(result.accountSnapshot?{account_snapshot:result.accountSnapshot}:{})};
  const observed=Date.now(),end=observed+2592000000;
  const snapshotWrite=await request(manager,'/v0/management/quota-snapshots',input.admin,'POST',{entries:[{row_key:'synthetic-bps',provider:'bps',account:snapshotAccount,windows:[{provider_window_id:'bps-primary',window_kind:'monthly',window_mode:'fixed',model_scope_kind:'feature',model_scope_key:'bps_account_usage',source:'api_query',observed_at_ms:observed,boundary_accuracy:'exact',cycle_start_ms:observed,cycle_end_ms:end,duration_seconds:2592000,used_percent:17}]}]});
  must(snapshotWrite.status===200&&snapshotWrite.value.items?.length===1&&snapshotWrite.value.items[0].provider==='bps','snapshot_bps_write');
  const snapshotQuery=await request(manager,'/v0/management/quota-snapshots/query',input.admin,'POST',{accounts:[{row_key:'synthetic-bps',provider:'bps',account:snapshotAccount}]});
  must(snapshotQuery.status===200&&snapshotQuery.value.items?.length===1&&snapshotQuery.value.items[0].provider==='bps'&&snapshotQuery.value.items[0].windows?.some(window=>window.used_percent===17&&window.model_scope_kind==='feature'&&window.model_scope_key==='bps_account_usage'),'snapshot_bps_roundtrip');
  report({stage:'snapshot',provider:'bps',synthetic_only:true,write_query_roundtrip:true});
  report({stage:'complete',result:'passed',synthetic_only:true,real_login_refresh_inference_called:false});
}catch(error){
  const code=String(error?.message||'integration_failed');report({stage:'failed',error_code:/^[a-z_]{1,80}$/.test(code)?code:'integration_failed'});process.exitCode=1;
}finally{for(const key of Object.keys(input))input[key]='';}
