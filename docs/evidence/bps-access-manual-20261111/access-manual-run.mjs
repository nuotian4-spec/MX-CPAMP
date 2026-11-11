import {spawn} from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';

const fixed={
  cpa:{ref:'mx-cpa:bps-inspection-repair-20261111',id:'sha256:54e47035d502e2875d4fb5bd63f62cb7f69e5bdf8532a0d2ab5a85eb87666abc',revision:'06a2f9f121b06e8609c5f9d148213dc6ceeaea6b'},
  manager:{ref:'mx-cpamp:bps-inspection-repair-20261111',id:'sha256:54648d3ce33cfa9b1361eb7f0d99568f2f62497e5dea78124feaaa17c8f73429',revision:'de1c4356cd80363c6b28384fdf3e3a704380ec26'},
};
const checksImage='sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758';
const ownerLabel='mx.bps-access-manual';
const ttl=900;
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
async function request(state,pathname,key){
  if(state.offline){
    const script=`const chunks=[];for await(const c of process.stdin)chunks.push(c);const input=JSON.parse(Buffer.concat(chunks));const r=await fetch('http://127.0.0.1:18317'+input.pathname,{redirect:'error',headers:input.key?{authorization:'Bearer '+input.key}:{},signal:AbortSignal.timeout(5000)});const text=await r.text();let value;try{value=JSON.parse(text);}catch{}process.stdout.write(JSON.stringify({status:r.status,value,html:r.headers.get('content-type')?.includes('text/html')&&text.includes('<html')}));`;
    return JSON.parse(await docker(['exec','-i',state.names.checks,'node','--input-type=module','-e',script],JSON.stringify({pathname,key})));
  }
  const response=await fetch(state.origin+pathname,{redirect:'error',headers:key?{authorization:'Bearer '+key}:{},signal:AbortSignal.timeout(5000)});
  const text=await response.text();let value;try{value=JSON.parse(text);}catch{}
  return {status:response.status,value,html:response.headers.get('content-type')?.includes('text/html')&&text.includes('<html')};
}
async function cleanup(state){
  for(const name of [state.names.checks,state.names.manager,state.names.cpa].filter(Boolean)){
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
  const label='mx-bps-access-manual-'+crypto.randomBytes(6).toString('hex');
  const parent=path.join(process.env.CODEX_HOME||path.join(os.homedir(),'.codex'),'artifacts');
  const directory=path.join(parent,label);await privateDirectory(directory);
  const state={schema:1,label,directory,offline,names:{cpa:label+'-cpa',manager:label+'-manager',...(offline?{checks:label+'-checks'}:{})},access_file:path.join(directory,'本机访问.txt'),created_at:new Date().toISOString(),expires_at:new Date(Date.now()+ttl*1000).toISOString(),status:'starting',budgets:{manual_oauth_sessions:1,manual_access_clicks:1,manual_refresh:0,model_requests:0},ui_budget_hard_blocked:false,background_refresh_hard_blocked:false};
  const keys={cpa:crypto.randomBytes(32).toString('hex'),admin:crypto.randomBytes(32).toString('hex'),api:crypto.randomBytes(32).toString('hex')};
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
    await docker(['run',...common,'--name',state.names.manager,'--network','container:'+state.names.cpa,'--tmpfs','/data:mode=0700,size=128m','--tmpfs','/run/config:mode=0700,size=1m','--env','CPA_MANAGER_CONFIG=/run/config/config.json','--entrypoint','/bin/sh',fixed.manager.id,'-c','while [ ! -s /run/config/ready ]; do sleep 0.1; done; exec timeout -s TERM '+ttl+' cpa-manager-plus']);
    await put(state.names.manager,'/run/config/cpa-key',keys.cpa);
    await put(state.names.manager,'/run/config/admin-key',keys.admin);
    await put(state.names.manager,'/run/config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key',quotaCooldownEnabled:false,accountActionsEnabled:false,accountActionsAutoDisable:false}));
    await put(state.names.manager,'/run/config/ready','ready');
    if(offline){
      await docker(['image','inspect',checksImage,'--format','{{.Id}}']);
      await docker(['run',...common,'--name',state.names.checks,'--network','container:'+state.names.cpa,'--tmpfs','/tmp:mode=1777,size=16m','--entrypoint','node',checksImage,'-e','setTimeout(()=>process.exit(0),'+ttl+'000)']);
    }
    const cpa=await inspect(state.names.cpa),manager=await inspect(state.names.manager);
    const ports=cpa.NetworkSettings.Ports?.['18317/tcp'];
    if(offline)must(cpa.HostConfig.NetworkMode==='none'&&!Object.keys(cpa.HostConfig.PortBindings||{}).length,'port_boundary_invalid');
    else must(ports?.length===1&&ports[0].HostIp==='127.0.0.1'&&Object.entries(cpa.HostConfig.PortBindings||{}).every(([port])=>port==='18317/tcp'),'port_boundary_invalid');
    must(!Object.keys(manager.HostConfig.PortBindings||{}).length&&manager.HostConfig.NetworkMode==='container:'+cpa.Id,'namespace_boundary_invalid');
    for(const [role,item] of [['cpa',cpa],['manager',manager]])must(item.Image===fixed[role].id&&item.Config.Labels?.[ownerLabel]===label&&item.HostConfig.ReadonlyRootfs&&item.HostConfig.LogConfig.Type==='none'&&Object.keys(item.HostConfig.Tmpfs||{}).length>=2&&item.Mounts.every(mount=>mount.Type==='tmpfs'),'container_boundary_invalid');
    if(offline){
      const checks=await inspect(state.names.checks);
      must(checks.Image===checksImage&&checks.HostConfig.NetworkMode==='container:'+cpa.Id&&checks.HostConfig.ReadonlyRootfs&&checks.HostConfig.LogConfig.Type==='none'&&!Object.keys(checks.HostConfig.PortBindings||{}).length&&checks.Mounts.every(mount=>mount.Type==='tmpfs'),'container_boundary_invalid');
    }
    state.origin=offline?'http://127.0.0.1:18317':'http://127.0.0.1:'+ports[0].HostPort;
    state.url=state.origin+'/management.html#/oauth';
    let ready=false;
    for(let count=0;count<120;count++){
      try{
        const health=await request(state,'/health');
        const page=await request(state,'/management.html');
        const gate=await request(state,'/api/oauth-login/oauth/status?state=00000000-0000-4000-8000-000000000001');
        const authenticated=await request(state,'/api/oauth-login/oauth/status?state=00000000-0000-4000-8000-000000000001',keys.admin);
        if(count===0)report({stage:'ready_probes',health:health.status,panel:page.status,unauthenticated:gate.status,authenticated_unknown_state:authenticated.status});
        if(health.status===200&&health.value?.ok&&page.status===200&&page.html&&gate.status===401&&authenticated.status===404){ready=true;break;}
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
    if(offline){
      const gate=await request(state,'/api/bps/access');
      const missing=await request(state,'/api/bps/access',keys.admin);
      must(gate.status===401&&missing.status===400,'bps_access_gate_unconfirmed');
      report({stage:'preflight',ready:true,unauthenticated_status:401,authenticated_empty_reference_status:400,credential_count:0,published_model_count:0,official_access_called:false,oauth_started:false,refresh_called:false,inference_called:false});
    }else await fs.writeFile(state.access_file,'仅限本机的一次手工 BPS OAuth 与模型页面验收\n面板地址：'+state.url+'\n临时管理员密钥：'+keys.admin+'\n登录时关闭“记住密码”，不要填写 CPA 内部地址或密钥。\n仅新建一次 BPS OAuth；系统设置只点一次“获取官方允许模型”。\n账号模型页只核全局发布和本站注册，不点“查询本凭证官方权限”，不启动巡检。\n不要在聊天粘贴本密钥、官方回调、密码或 TOTP。\n有效期至：'+state.expires_at+'\n',{mode:0o600});
    state.status='ready';state.images=fixed;await save(path.join(directory,'state.json'),state);
    report({stage:'ready',url:offline?null:state.url,state_dir:directory,access_file:offline?null:state.access_file,expires_at:state.expires_at,readonly:true,tmpfs_state:true,logs:false,cpa_published:false,manual_oauth_started:false,harness_initiated_official_access_requests:0,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,ui_budget_hard_blocked:false,background_refresh_hard_blocked:false,ttl_behavior:'stops_processes_cleanup_requires_stop'});
    return state;
  }catch(error){await cleanup(state);throw error;}
  finally{keys.cpa=keys.admin=keys.api='';}
}
async function status(directory){
  const state=JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8'));
  const items={};for(const [role,name] of Object.entries(state.names)){try{const item=await inspect(name);must(item.Config.Labels?.[ownerLabel]===state.label,'verify_owner_mismatch');items[role]={running:item.State.Running,image:item.Image};}catch(error){if(error.message==='verify_owner_mismatch')throw error;items[role]={running:false};}}
  report({stage:'status',state_dir:directory,status:state.status,url:state.url,expires_at:state.expires_at,services:items,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,background_refresh_hard_blocked:false,ttl_behavior:'stops_processes_cleanup_requires_stop'});
}
async function verify(directory){
  const state=JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8'));
  const manager=await inspect(state.names.manager),cpa=await inspect(state.names.cpa);
  must(manager.Config.Labels?.[ownerLabel]===state.label&&cpa.Config.Labels?.[ownerLabel]===state.label&&manager.Image===fixed.manager.id&&cpa.Image===fixed.cpa.id,'verify_owner_mismatch');
  const admin=(await docker(['exec',state.names.manager,'cat','/run/config/admin-key'])).trim();
  const response=await request(state,'/v0/management/auth-files',admin);
  must(response.status===200&&Array.isArray(response.value?.files),'registration_query_unconfirmed');
  const files=response.value.files;
  const bps=files.filter(file=>file.provider==='bps');
  const registered=bps.filter(file=>typeof file.id==='string'&&Boolean(file.id)&&typeof file.auth_index==='string'&&Boolean(file.auth_index));
  const raw=await docker(['exec',state.names.cpa,'sh','-c','for p in /state/auths/*.json; do [ -f "$p" ] || continue; printf "%s\\n" "${p##*/}"; done']);
  const names=raw.trim()?raw.trim().split('\n'):[];
  let savedCount=0,profileClient=false,savedMethod=false,tokensPresent=false,sameRegistration=false;
  for(const name of names){
    const document=JSON.parse(await docker(['exec',state.names.cpa,'cat','/state/auths/'+name]));
    if(document.type==='bps'){
      savedCount++;
      const match=registered.filter(item=>item.id===name);
      sameRegistration=match.length===1&&registered.length===1;
      profileClient=document.profile==='excel'&&document.client_id==='app_fnr0pYvVwwFDocDumLG3H2Bp';
      savedMethod=document.login_method==='oauth';
      tokensPresent=typeof document.access_token==='string'&&Boolean(document.access_token)&&typeof document.refresh_token==='string'&&Boolean(document.refresh_token);
    }
    for(const field of ['access_token','refresh_token','id_token'])document[field]='';
  }
  const runtimeActive=registered.length===1&&registered[0].status==='active'&&!registered[0].disabled;
  const passed=names.length===1&&files.length===1&&savedCount===1&&registered.length===1&&bps.length===1&&sameRegistration&&profileClient&&savedMethod&&tokensPresent&&runtimeActive;
  const published=await request(state,'/api/bps/models',admin);
  must(published.status===200&&Array.isArray(published.value?.models)&&published.value.models.every(id=>typeof id==='string'),'model_config_query_unconfirmed');
  let registeredModels=[];
  if(registered.length===1){
    const response=await request(state,'/v0/management/auth-files/models?name='+encodeURIComponent(registered[0].name||registered[0].id),admin);
    must(response.status===200&&Array.isArray(response.value?.models),'registered_models_query_unconfirmed');
    registeredModels=response.value.models.map(item=>item.id).filter(id=>typeof id==='string');
  }
  const modelIds=published.value.models;
  const configRegistered=modelIds.length>0&&registeredModels.length===modelIds.length&&modelIds.every(id=>registeredModels.includes(id));
  let observation=null;
  try{
    const source=JSON.parse(await fs.readFile(path.join(directory,'page-observation.json'),'utf8'));
    const allowed=new Set(['allowed','denied','error','unconfirmed']);
    must(allowed.has(source.official_access_result)&&Number.isInteger(source.official_access_click_count)&&source.official_access_click_count>=0&&Number.isInteger(source.official_allowed_model_count)&&source.official_allowed_model_count>=0,'page_observation_invalid');
    observation={official_access_result:source.official_access_result,official_access_click_count:source.official_access_click_count,official_allowed_model_count:source.official_allowed_model_count};
    for(const field of ['official_model_list_seen','query_time_seen','model_save_message_seen','account_published_and_registered_seen','account_official_query_clicked','inspections_started']){must(typeof source[field]==='boolean','page_observation_invalid');observation[field]=source[field];}
  }catch(error){if(error.code!=='ENOENT')throw error;}
  const manualConfirmed=Boolean(observation&&observation.official_access_click_count===1&&observation.official_access_result==='allowed'&&observation.official_allowed_model_count===modelIds.length&&observation.official_model_list_seen&&observation.query_time_seen&&observation.model_save_message_seen&&observation.account_published_and_registered_seen&&!observation.account_official_query_clicked&&!observation.inspections_started);
  const receipt={stage:'verification',checked_at:new Date().toISOString(),result:passed&&configRegistered&&manualConfirmed?'passed':'pending',saved_total_count:names.length,runtime_total_count:files.length,saved_bps_count:savedCount,registered_bps_count:registered.length,unique_saved_and_registered:passed,saved_file_matches_registration:sameRegistration,profile_client_match:profileClient,saved_oauth_method_match:savedMethod,runtime_active:runtimeActive,access_refresh_present:tokensPresent,published_model_count:modelIds.length,registered_model_count:registeredModels.length,config_matches_registration:configRegistered,manual_observation:observation,manual_observation_is_user_report:true,harness_initiated_official_access_requests:0,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,ui_budget_hard_blocked:false,background_refresh_hard_blocked:false,upstream_refresh_attempts:'not_monitored'};
  await save(path.join(directory,'verification.json'),receipt);
  report(receipt);
}
const [action,directory]=process.argv.slice(2);
try{
  must((['start','preflight'].includes(action)&&!directory)||(['status','verify','stop'].includes(action)&&directory&&path.isAbsolute(directory)),'usage_start_or_status_verify_stop_directory');
  if(action==='preflight'){const state=await start(true);try{await status(state.directory);await verify(state.directory);}finally{await cleanup(state);}}
  else if(action==='start')await start();
  else if(action==='status')await status(directory);
  else if(action==='verify')await verify(directory);
  else await cleanup(JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8')));
}catch(error){
  const codes=new Set(['image_identity_mismatch','port_boundary_invalid','namespace_boundary_invalid','container_boundary_invalid','services_not_ready','command_failed','command_start_failed','cleanup_owner_mismatch','cleanup_incomplete','usage_start_or_status_verify_stop_directory','verify_owner_mismatch','registration_query_unconfirmed','saved_credential_unconfirmed','empty_credentials_unconfirmed','empty_models_unconfirmed','bps_settings_unconfirmed','bps_access_gate_unconfirmed','model_config_query_unconfirmed','registered_models_query_unconfirmed','page_observation_invalid']);
  report({stage:'harness',status:'failed',error_code:codes.has(error.message)?error.message:'harness_error'});process.exitCode=1;
}
