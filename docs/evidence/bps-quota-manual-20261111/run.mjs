import {spawn} from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const codeDirectory=path.dirname(fileURLToPath(import.meta.url));

const fixed={
  cpa:{ref:'mx-cpa:bps-quota-identity-20261111',id:'sha256:eeb5bf17435a4c3d1a508188fd149eb8b2dbc5d9d20390cf4fd22d6e40f40d4b',revision:'b9f98a54a1ce9cb1f44cfddbc242c5476f74d41c'},
  manager:{ref:'mx-cpamp:bps-quota-repair-20261111',id:'sha256:db9f59c696d74d55cc151564754ec98615683df5f7370b2d32f0e977b74313cd',revision:'d26cbf7595ecf41ba8cbfd90c956291ac351b471'},
  worker:{ref:'mx-oauth-login:phase2-local-20261111',id:'sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758',revision:'95dd9ca2c16d5469c43ce7c910f38ff271af45e3'},
};
const ownerLabel='mx.bps-quota-manual';
let ttl=3600;
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const report=value=>process.stdout.write(JSON.stringify(value)+'\n');
const must=(value,code)=>{if(!value)throw new Error(code);};
async function command(args,input){
  return await new Promise((resolve,reject)=>{
    const child=spawn(args[0],args.slice(1),{stdio:['pipe','pipe','pipe'],windowsHide:true});
    let output='',finished=false;
    const timer=setTimeout(()=>child.kill(),60000);
    child.stdout.setEncoding('utf8');child.stdout.on('data',chunk=>{output+=chunk;if(output.length>2*1024*1024)child.kill();});
    child.stderr.resume();child.stdin.on('error',()=>{});
    child.on('error',()=>{clearTimeout(timer);finished=true;reject(new Error('command_start_failed'));});
    child.on('close',code=>{clearTimeout(timer);if(!finished){finished=true;code===0?resolve(output):reject(new Error('command_failed'));}});
    child.stdin.end(input);
  });
}
const docker=(args,input)=>command(['docker',...args],input);
async function privateDirectory(directory){
  await fs.mkdir(directory,{recursive:false,mode:0o700});
  if(process.platform==='win32'){
    const owner=process.env.USERDOMAIN+'\\'+process.env.USERNAME;
    await command(['icacls',directory,'/inheritance:r','/grant:r',owner+':(OI)(CI)F','SYSTEM:(OI)(CI)F']);
  }
}
const save=(file,value)=>fs.writeFile(file,JSON.stringify(value,null,2)+'\n',{mode:0o600});
async function put(name,target,value){
  await docker(['exec','-i',name,'sh','-c','umask 077; cat > '+target+'.pending && mv '+target+'.pending '+target],value);
}
async function inspect(name){return JSON.parse(await docker(['inspect',name,'--format','{{json .}}']));}
async function request(state,pathname,key,service='manager'){
  if(state.offline||service==='worker'){
    const script=`const chunks=[];for await(const c of process.stdin)chunks.push(c);const input=JSON.parse(Buffer.concat(chunks));const r=await fetch('http://127.0.0.1:'+input.port+input.pathname,{redirect:'error',headers:input.key?{authorization:'Bearer '+input.key}:{},signal:AbortSignal.timeout(5000)});const text=await r.text();let value;try{value=JSON.parse(text);}catch{}process.stdout.write(JSON.stringify({status:r.status,value,html:r.headers.get('content-type')?.includes('text/html')&&text.includes('<html')}));`;
    return JSON.parse(await docker(['exec','-i',state.names.worker,'node','--input-type=module','-e',script],JSON.stringify({pathname,key,port:service==='worker'?18417:18317})));
  }
  const response=await fetch(state.origin+pathname,{redirect:'error',headers:key?{authorization:'Bearer '+key}:{},signal:AbortSignal.timeout(5000)});
  const text=await response.text();let value;try{value=JSON.parse(text);}catch{}
  return {status:response.status,value,html:response.headers.get('content-type')?.includes('text/html')&&text.includes('<html')};
}
async function cleanup(state){
  for(const name of [state.names.manager,state.names.worker,state.names.cpa].filter(Boolean)){
    let item;try{item=await inspect(name);}catch{continue;}
    must(item.Config.Labels?.[ownerLabel]===state.label,'cleanup_owner_mismatch');
    await docker(['rm','--force',name]);
  }
  const remaining=await docker(['ps','-aq','--filter','label='+ownerLabel+'='+state.label]);
  must(!remaining.trim(),'cleanup_incomplete');
  await fs.rm(state.access_file,{force:true});
  state.status='cleaned';state.cleaned_at=new Date().toISOString();
  await save(path.join(state.directory,'state.json'),state);
  report({stage:'cleanup',verified:true,token_state_retained:false,state_dir:state.directory});
}
async function start(offline=false){
  const label='mx-bps-quota-manual-'+crypto.randomBytes(6).toString('hex');
  const parent=path.join(process.env.CODEX_HOME||path.join(os.homedir(),'.codex'),'artifacts');
  const directory=path.join(parent,label);await privateDirectory(directory);
  const state={schema:1,label,directory,offline,names:{cpa:label+'-cpa',manager:label+'-manager',worker:label+'-worker'},access_file:path.join(directory,'本机访问.txt'),created_at:new Date().toISOString(),expires_at:new Date(Date.now()+ttl*1000).toISOString(),status:'starting',ttl_seconds:ttl,budgets:{login_submission_by_harness:0,official_access_by_harness:0,official_usage_by_harness:0,manual_refresh:0,model_requests:0},ui_budget_hard_blocked:false,background_refresh_hard_blocked:false};
  const keys={cpa:crypto.randomBytes(32).toString('hex'),admin:crypto.randomBytes(32).toString('hex'),api:crypto.randomBytes(32).toString('hex'),worker:crypto.randomBytes(32).toString('hex')};
  await save(path.join(directory,'state.json'),state);
  try{
    for(const [role,want] of Object.entries(fixed)){
      const image=JSON.parse(await docker(['image','inspect',want.ref,'--format','{{json .}}']));
      must(image.Id===want.id&&image.Os==='linux'&&image.Architecture==='amd64'&&image.Config.Labels?.['org.opencontainers.image.revision']===want.revision,'image_identity_mismatch');
      report({stage:'image',role,id:image.Id,revision:want.revision});
    }
    const common=['--detach','--read-only','--log-driver','none','--label',ownerLabel+'='+label];
    await docker(['run',...common,'--name',state.names.cpa,...(offline?['--network','none']:['--publish','127.0.0.1::18317/tcp']),'--tmpfs','/state:mode=0700,size=64m','--tmpfs','/tmp:mode=1777,size=32m','--entrypoint','/bin/sh',fixed.cpa.id,'-c','while [ ! -s /state/config.yaml ]; do sleep 0.1; done; exec timeout -s TERM '+ttl+' /CLIProxyAPI/CLIProxyAPI --config /state/config.yaml --local-model']);
    await put(state.names.cpa,'/state/config.yaml',JSON.stringify({'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},management:{'allow-remote':true,'secret-key':keys.cpa,'disable-control-panel':true,'disable-auto-update-panel':true},access:{'api-keys':[keys.api]},requests:{'proxy-url':'http://host.docker.internal:7890'},routing:{retry:{'request-retry':0,'max-retry-interval':0}},oauth:{'auth-dir':'/state/auths',providers:{bps:{models:[]}}},'bps-protocol':{'state-dir':'/state/bps-state'}}));
    await docker(['run',...common,'--init','--name',state.names.worker,'--network','container:'+state.names.cpa,'--tmpfs','/tmp:mode=1777,size=64m','--tmpfs','/run/secrets:mode=0700,uid=1000,gid=1000,size=1m','--mount','type=bind,source='+codeDirectory+',target=/checks,readonly','--entrypoint','/bin/sh',fixed.worker.id,'-c','while [ ! -s /run/secrets/oauth_login_secret ]; do sleep 0.1; done; exec timeout -s TERM '+ttl+' node /checks/worker.mjs']);
    await put(state.names.worker,'/run/secrets/oauth_login_secret',keys.worker);
    await docker(['run',...common,'--name',state.names.manager,'--network','container:'+state.names.cpa,'--tmpfs','/data:mode=0700,size=128m','--tmpfs','/run/config:mode=0700,size=1m','--tmpfs','/run/secrets:mode=0700,size=1m','--env','OAUTH_LOGIN_URL=http://127.0.0.1:18417','--env','OAUTH_LOGIN_SECRET_FILE=/run/secrets/oauth_login_secret','--env','CPA_MANAGER_CONFIG=/run/config/config.json','--entrypoint','/bin/sh',fixed.manager.id,'-c','while [ ! -s /run/config/ready ]; do sleep 0.1; done; exec timeout -s TERM '+ttl+' cpa-manager-plus']);
    await put(state.names.manager,'/run/config/cpa-key',keys.cpa);
    await put(state.names.manager,'/run/config/admin-key',keys.admin);
    await put(state.names.manager,'/run/secrets/oauth_login_secret',keys.worker);
    await put(state.names.manager,'/run/config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key',quotaCooldownEnabled:false,accountActionsEnabled:false,accountActionsAutoDisable:false}));
    await put(state.names.manager,'/run/config/ready','ready');
    const cpa=await inspect(state.names.cpa),manager=await inspect(state.names.manager),worker=await inspect(state.names.worker);
    const ports=cpa.NetworkSettings.Ports?.['18317/tcp'];
    if(offline)must(cpa.HostConfig.NetworkMode==='none'&&!Object.keys(cpa.HostConfig.PortBindings||{}).length,'port_boundary_invalid');
    else must(ports?.length===1&&ports[0].HostIp==='127.0.0.1'&&Object.entries(cpa.HostConfig.PortBindings||{}).every(([port])=>port==='18317/tcp'),'port_boundary_invalid');
    must(!Object.keys(manager.HostConfig.PortBindings||{}).length&&manager.HostConfig.NetworkMode==='container:'+cpa.Id,'namespace_boundary_invalid');
    for(const [role,item] of [['cpa',cpa],['manager',manager]])must(item.Image===fixed[role].id&&item.Config.Labels?.[ownerLabel]===label&&item.HostConfig.ReadonlyRootfs&&item.HostConfig.LogConfig.Type==='none'&&Object.keys(item.HostConfig.Tmpfs||{}).length>=2&&item.Mounts.every(mount=>mount.Type==='tmpfs'),'container_boundary_invalid');
    must(worker.Image===fixed.worker.id&&worker.Config.User==='node'&&worker.HostConfig.Init&&worker.HostConfig.NetworkMode==='container:'+cpa.Id&&worker.HostConfig.ReadonlyRootfs&&worker.HostConfig.LogConfig.Type==='none'&&!Object.keys(worker.HostConfig.PortBindings||{}).length&&worker.Config.Labels?.[ownerLabel]===label&&worker.Mounts.every(mount=>mount.Type==='tmpfs'||mount.Type==='bind'&&mount.Destination==='/checks'&&!mount.RW),'container_boundary_invalid');
    state.origin=offline?'http://127.0.0.1:18317':'http://127.0.0.1:'+ports[0].HostPort;
    state.url=state.origin+'/management.html#/oauth';
    let ready=false;
    for(let count=0;count<120;count++){
      try{
        const health=await request(state,'/health');
        const workerHealth=await request(state,'/health',keys.worker,'worker');
        const page=await request(state,'/management.html');
        const gate=await request(state,'/api/oauth-login/oauth/status?state=00000000-0000-4000-8000-000000000001');
        const authenticated=await request(state,'/api/oauth-login/oauth/status?state=00000000-0000-4000-8000-000000000001',keys.admin);
        if(count===0)report({stage:'ready_probes',health:health.status,panel:page.status,unauthenticated:gate.status,authenticated_unknown_state:authenticated.status});
        if(workerHealth.status===200&&workerHealth.value?.status==='ok'&&health.status===200&&health.value?.ok&&page.status===200&&page.html&&gate.status===401&&authenticated.status===404){ready=true;break;}
      }catch{}
      await sleep(250);
    }
    must(ready,'services_not_ready');
    const files=await request(state,'/v0/management/auth-files',keys.admin);
    const models=await request(state,'/api/bps/models',keys.admin);
    const settings=await request(state,'/api/bps/settings',keys.admin);
    must(files.status===200&&Array.isArray(files.value?.files)&&files.value.files.length===0,'empty_credentials_unconfirmed');
    must(models.status===200&&Array.isArray(models.value?.models)&&models.value.models.length===0&&Boolean(models.value.connection_ref)&&Boolean(models.value.revision),'empty_models_unconfirmed');
    must(settings.status===200&&typeof settings.value?.enabled==='boolean','bps_settings_unconfirmed');
    report({stage:'empty_state',credential_count:0,published_model_count:0,bps_settings_present:true,manual_oauth_started:false,official_access_called:false,external_network:offline?'none':'proxy_7890'});
    await verifyLocal(state,keys.admin,keys.worker,true);
    if(!offline)await fs.writeFile(state.access_file,'仅限本机的BPS账号用量页面验收窗口\n面板地址：'+state.url+'\n临时管理员密钥：'+keys.admin+'\n关闭“记住密码”。不要在聊天粘贴账号、密码、TOTP、令牌或完整回调。\n建议本轮：手工BPS OAuth一次，官方页面自主完成；账号页手动刷新用量一次，再对该BPS手动巡检一次。脚本不会替你提交这些动作。\n有效期至：'+state.expires_at+'\n完成后执行stop清理，过期停进程不替代清理。\n',{mode:0o600});
    state.status='ready';state.images=fixed;await save(path.join(directory,'state.json'),state);
    report({stage:'ready',url:offline?null:state.url,state_dir:directory,access_file:offline?null:state.access_file,expires_at:state.expires_at,ttl_seconds:ttl,worker_configured:true,readonly:true,tmpfs_state:true,logs:false,cpa_published:false,manual_oauth_started:false,harness_initiated_official_access_requests:0,harness_initiated_official_usage_requests:0,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,ui_budget_hard_blocked:false,background_refresh_hard_blocked:false,ttl_behavior:'stops_processes_cleanup_requires_stop'});
    return state;
  }catch(error){await cleanup(state);throw error;}
  finally{keys.cpa=keys.admin=keys.api=keys.worker='';}
}
async function status(directory){
  const state=JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8'));
  const items={};for(const [role,name] of Object.entries(state.names)){try{const item=await inspect(name);must(item.Config.Labels?.[ownerLabel]===state.label,'verify_owner_mismatch');items[role]={running:item.State.Running,image:item.Image};}catch(error){if(error.message==='verify_owner_mismatch')throw error;items[role]={running:false};}}
  report({stage:'status',state_dir:directory,status:state.status,url:state.url,expires_at:state.expires_at,services:items,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,background_refresh_hard_blocked:false,ttl_behavior:'stops_processes_cleanup_requires_stop'});
}
async function verifyLocal(state,admin,workerKey,requireEmpty=false){
  const capability=await request(state,'/api/oauth-login/capabilities',admin);
  const gate=await request(state,'/api/oauth-login/capabilities');
  const health=await request(state,'/health',workerKey,'worker');
  const wrong=await request(state,'/health','invalid-bps-quota-probe','worker');
  const missing=await request(state,'/health',undefined,'worker');
  const unknown=await request(state,'/tasks/00000000-0000-4000-8000-000000000001',workerKey,'worker');
  must(capability.status===200&&capability.value?.password_login_configured===true&&gate.status===401,'manager_capability_unconfirmed');
  must(health.status===200&&health.value?.status==='ok'&&wrong.status===401&&missing.status===401&&unknown.status===404,'worker_auth_unconfirmed');
  const batches={};
  for(const profile of ['codex','bps']){
    const latest=await request(state,'/api/oauth-login/batches/latest?profile='+profile,admin);
    must(latest.status===404||latest.status===200,'empty_batches_unconfirmed');
    const empty=latest.status===404||latest.value===null||!latest.value?.id;
    if(requireEmpty)must(empty,'empty_batches_unconfirmed');
    batches[profile]={empty,item_count:Array.isArray(latest.value?.items)?latest.value.items.length:0};
  }
  const active=await request(state,'/api/oauth-login/batches/active',admin);
  must(active.status===404||active.status===200,'empty_batches_unconfirmed');
  const activeEmpty=active.status===404||active.value===null||!active.value?.id;
  if(requireEmpty)must(activeEmpty,'empty_batches_unconfirmed');
  const startup=JSON.parse(await docker(['exec',state.names.worker,'cat','/tmp/worker-startup.json']));
  must(startup.proxy_configured_after_environment_filter&&startup.empty_input_rejected&&startup.probe_process_exited&&startup.probe_buffers_cleared&&!startup.submitted_login_material,'proxy_startup_unconfirmed');
  // 只读本地列表与已保存巡检；不调用 access、usage 或巡检执行入口。
  const files=await request(state,'/v0/management/auth-files',admin);
  must(files.status===200&&Array.isArray(files.value?.files),'registration_query_unconfirmed');
  const counts={total:files.value.files.length,bps:files.value.files.filter(file=>file.provider==='bps').length};
  if(requireEmpty)must(counts.total===0,'empty_credentials_unconfirmed');
  const runs=await request(state,'/v0/management/codex-inspection/runs?limit=20',admin);
  must(runs.status===200&&Array.isArray(runs.value?.items),'inspection_query_unconfirmed');
  const inspections={local_run_count:runs.value.items.length,run_limit:20,bps_result_count:0,official_usage_observation_count:0};
  if(requireEmpty)must(inspections.local_run_count===0,'empty_inspections_unconfirmed');
  for(const run of runs.value.items){
    must(Number.isSafeInteger(run.id)&&run.id>0,'inspection_query_unconfirmed');
    const detail=await request(state,'/v0/management/codex-inspection/runs/'+run.id,admin);
    must(detail.status===200&&Array.isArray(detail.value?.results),'inspection_query_unconfirmed');
    for(const result of detail.value.results){
      if(result.provider!=='bps')continue;
      inspections.bps_result_count++;
      let summary;try{summary=JSON.parse(result.errorDetail||'{}');}catch{}
      if(summary?.usage_source==='chatgpt_account_usage'&&summary.usage_upstream_status===200&&summary.usage_error==null&&Array.isArray(result.quotaWindows)&&result.quotaWindows.length>0)inspections.official_usage_observation_count++;
    }
  }
  const receipt={stage:'local_verification',checked_at:new Date().toISOString(),manager_capability:true,manager_unauthenticated_status:gate.status,worker_authenticated_status:health.status,worker_wrong_key_status:wrong.status,worker_missing_key_status:missing.status,worker_unknown_task_status:unknown.status,batches,active_batch_empty:activeEmpty,credentials:counts,inspections,official_usage_observed:inspections.official_usage_observation_count>0,proxy_startup:startup,harness_login_submitted:false,harness_official_access_called:false,harness_official_usage_called:false,harness_refresh_called:false,harness_inference_called:false,ui_evidence:'manual_acceptance_pending',background_refresh_hard_blocked:false};
  await save(path.join(state.directory,'verification.json'),receipt);report(receipt);
}
async function verify(directory){
  const state=JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8'));
  for(const [role,name]of Object.entries(state.names)){
    const item=await inspect(name);must(item.Config.Labels?.[ownerLabel]===state.label&&item.Image===fixed[role].id,'verify_owner_mismatch');
  }
  const admin=(await docker(['exec',state.names.manager,'cat','/run/config/admin-key'])).trim();
  const workerKey=(await docker(['exec',state.names.manager,'cat','/run/secrets/oauth_login_secret'])).trim();
  await verifyLocal(state,admin,workerKey);
}
const [action,...arguments_]=process.argv.slice(2);
let directory;
try{
  if(['start','preflight'].includes(action)){
    must(arguments_.length===0||arguments_.length===2&&arguments_[0]==='--ttl'&&/^[0-9]+$/.test(arguments_[1]),'usage_start_or_status_verify_stop_directory');
    if(arguments_.length)ttl=Number(arguments_[1]);
    must(Number.isSafeInteger(ttl)&&ttl>=60&&ttl<=86400,'ttl_invalid');
  }else{must(arguments_.length===1,'usage_start_or_status_verify_stop_directory');directory=arguments_[0];}
  must((['start','preflight'].includes(action)&&!directory)||(['status','verify','stop'].includes(action)&&directory&&path.isAbsolute(directory)),'usage_start_or_status_verify_stop_directory');
  if(action==='preflight'){const state=await start(true);try{await status(state.directory);}finally{await cleanup(state);}}
  else if(action==='start')await start();
  else if(action==='status')await status(directory);
  else if(action==='verify')await verify(directory);
  else await cleanup(JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8')));
}catch(error){
  const codes=new Set(['image_identity_mismatch','port_boundary_invalid','namespace_boundary_invalid','container_boundary_invalid','services_not_ready','command_failed','command_start_failed','cleanup_owner_mismatch','cleanup_incomplete','usage_start_or_status_verify_stop_directory','verify_owner_mismatch','registration_query_unconfirmed','saved_credential_unconfirmed','empty_credentials_unconfirmed','empty_models_unconfirmed','bps_settings_unconfirmed','bps_access_gate_unconfirmed','model_config_query_unconfirmed','registered_models_query_unconfirmed','page_observation_invalid','manager_capability_unconfirmed','worker_auth_unconfirmed','empty_batches_unconfirmed','proxy_startup_unconfirmed','ttl_invalid','inspection_query_unconfirmed','empty_inspections_unconfirmed']);
  report({stage:'harness',status:'failed',error_code:codes.has(error.message)?error.message:'harness_error'});process.exitCode=1;
}
