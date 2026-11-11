import {spawn} from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';

const fixed={
  cpa:{ref:'mx-cpa:bps-callback-scope-20261111',id:'sha256:d96e26782b4b080a703ee9668bf1c653e18cf164035b2bd9e11bcac2d11023fd',revision:'346b9479e12bc45ccb0e72556acc4cad4e9a0a04'},
  manager:{ref:'mx-cpamp:oauth-input-diagnostics-20261111',id:'sha256:cdf235bee2e22f2c33af145b8fb4d27e93b4478162b8738dcadd3595f3b74c4a',revision:'7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44'},
};
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
  const response=await fetch(state.origin+pathname,{redirect:'error',headers:key?{authorization:'Bearer '+key}:{},signal:AbortSignal.timeout(5000)});
  const text=await response.text();let value;try{value=JSON.parse(text);}catch{}
  return {status:response.status,value,html:response.headers.get('content-type')?.includes('text/html')&&text.includes('<html')};
}
async function cleanup(state){
  for(const name of [state.names.manager,state.names.cpa]){
    let item;try{item=await inspect(name);}catch{continue;}
    must(item.Config.Labels?.['mx.bps-scope-manual']===state.label,'cleanup_owner_mismatch');
    await docker(['rm','--force',name]);
  }
  const remaining=await docker(['ps','-aq','--filter','label=mx.bps-scope-manual='+state.label]);
  must(!remaining.trim(),'cleanup_incomplete');
  await fs.rm(state.access_file,{force:true});
  state.status='cleaned';state.cleaned_at=new Date().toISOString();
  await save(path.join(state.directory,'state.json'),state);
  report({stage:'cleanup',verified:true,token_state_retained:false,state_dir:state.directory});
}
async function start(){
  const label='mx-bps-scope-manual-'+crypto.randomBytes(6).toString('hex');
  const parent=path.join(process.env.CODEX_HOME||path.join(os.homedir(),'.codex'),'artifacts');
  const directory=path.join(parent,label);await privateDirectory(directory);
  const state={schema:1,label,directory,names:{cpa:label+'-cpa',manager:label+'-manager'},access_file:path.join(directory,'本机访问.txt'),created_at:new Date().toISOString(),expires_at:new Date(Date.now()+ttl*1000).toISOString(),status:'starting',budgets:{manual_oauth_sessions:1,manual_refresh:0,model_requests:0},background_refresh_hard_blocked:false};
  const keys={cpa:crypto.randomBytes(32).toString('hex'),admin:crypto.randomBytes(32).toString('hex'),api:crypto.randomBytes(32).toString('hex')};
  await save(path.join(directory,'state.json'),state);
  try{
    for(const [role,want] of Object.entries(fixed)){
      const image=JSON.parse(await docker(['image','inspect',want.ref,'--format','{{json .}}']));
      must(image.Id===want.id&&image.Os==='linux'&&image.Architecture==='amd64'&&image.Config.Labels?.['org.opencontainers.image.revision']===want.revision,'image_identity_mismatch');
      report({stage:'image',role,id:image.Id,revision:want.revision});
    }
    const common=['--detach','--read-only','--log-driver','none','--label','mx.bps-scope-manual='+label];
    await docker(['run',...common,'--name',state.names.cpa,'--publish','127.0.0.1::18317/tcp','--tmpfs','/state:mode=0700,size=64m','--tmpfs','/tmp:mode=1777,size=32m','--entrypoint','/bin/sh',fixed.cpa.id,'-c','while [ ! -s /state/config.yaml ]; do sleep 0.1; done; exec timeout -s TERM '+ttl+' /CLIProxyAPI/CLIProxyAPI --config /state/config.yaml --local-model']);
    await put(state.names.cpa,'/state/config.yaml',JSON.stringify({'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},management:{'allow-remote':true,'secret-key':keys.cpa,'disable-control-panel':true,'disable-auto-update-panel':true},access:{'api-keys':[keys.api]},requests:{'proxy-url':'http://host.docker.internal:7890'},routing:{retry:{'request-retry':0,'max-retry-interval':0}},oauth:{'auth-dir':'/state/auths'},'bps-protocol':{'state-dir':'/state/bps-state'}}));
    await docker(['run',...common,'--name',state.names.manager,'--network','container:'+state.names.cpa,'--tmpfs','/data:mode=0700,size=128m','--tmpfs','/run/config:mode=0700,size=1m','--env','CPA_MANAGER_CONFIG=/run/config/config.json','--entrypoint','/bin/sh',fixed.manager.id,'-c','while [ ! -s /run/config/ready ]; do sleep 0.1; done; exec timeout -s TERM '+ttl+' cpa-manager-plus']);
    await put(state.names.manager,'/run/config/cpa-key',keys.cpa);
    await put(state.names.manager,'/run/config/admin-key',keys.admin);
    await put(state.names.manager,'/run/config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key',quotaCooldownEnabled:false,accountActionsEnabled:false,accountActionsAutoDisable:false}));
    await put(state.names.manager,'/run/config/ready','ready');
    const cpa=await inspect(state.names.cpa),manager=await inspect(state.names.manager);
    const ports=cpa.NetworkSettings.Ports?.['18317/tcp'];
    must(ports?.length===1&&ports[0].HostIp==='127.0.0.1'&&Object.entries(cpa.HostConfig.PortBindings||{}).every(([port])=>port==='18317/tcp'),'port_boundary_invalid');
    must(!Object.keys(manager.HostConfig.PortBindings||{}).length&&manager.HostConfig.NetworkMode==='container:'+cpa.Id,'namespace_boundary_invalid');
    for(const [role,item] of [['cpa',cpa],['manager',manager]])must(item.Image===fixed[role].id&&item.HostConfig.ReadonlyRootfs&&item.HostConfig.LogConfig.Type==='none'&&Object.keys(item.HostConfig.Tmpfs||{}).length>=2,'container_boundary_invalid');
    state.origin='http://127.0.0.1:'+ports[0].HostPort;
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
    await fs.writeFile(state.access_file,'仅限本机的一次手工 BPS OAuth 验收\n面板地址：'+state.url+'\n临时管理员密钥：'+keys.admin+'\n登录时关闭“记住密码”，不要填写 CPA 内部地址或密钥。\n不要在聊天粘贴本密钥、官方回调、密码或 TOTP。\n有效期至：'+state.expires_at+'\n',{mode:0o600});
    state.status='ready';state.images=fixed;await save(path.join(directory,'state.json'),state);
    report({stage:'ready',url:state.url,state_dir:directory,access_file:state.access_file,expires_at:state.expires_at,readonly:true,tmpfs_state:true,logs:false,cpa_published:false,manual_oauth_started:false,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,background_refresh_hard_blocked:false,ttl_behavior:'stops_processes_cleanup_requires_stop'});
  }catch(error){await cleanup(state);throw error;}
  finally{keys.cpa=keys.admin=keys.api='';}
}
async function status(directory){
  const state=JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8'));
  const items={};for(const [role,name] of Object.entries(state.names)){try{const item=await inspect(name);items[role]={running:item.State.Running,image:item.Image};}catch{items[role]={running:false};}}
  report({stage:'status',state_dir:directory,status:state.status,url:state.url,expires_at:state.expires_at,services:items,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,background_refresh_hard_blocked:false,ttl_behavior:'stops_processes_cleanup_requires_stop'});
}
async function verify(directory){
  const state=JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8'));
  const manager=await inspect(state.names.manager),cpa=await inspect(state.names.cpa);
  must(manager.Config.Labels?.['mx.bps-scope-manual']===state.label&&cpa.Config.Labels?.['mx.bps-scope-manual']===state.label,'verify_owner_mismatch');
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
  const receipt={stage:'verification',checked_at:new Date().toISOString(),result:passed?'passed':'pending',saved_total_count:names.length,runtime_total_count:files.length,saved_bps_count:savedCount,registered_bps_count:registered.length,unique_saved_and_registered:passed,saved_file_matches_registration:sameRegistration,profile_client_match:profileClient,saved_oauth_method_match:savedMethod,runtime_active:runtimeActive,access_refresh_present:tokensPresent,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,background_refresh_hard_blocked:false,upstream_refresh_attempts:'not_monitored'};
  await save(path.join(directory,'verification.json'),receipt);
  report(receipt);
}
const [action,directory]=process.argv.slice(2);
try{
  must((action==='start'&&!directory)||(['status','verify','stop'].includes(action)&&directory&&path.isAbsolute(directory)),'usage_start_or_status_verify_stop_directory');
  if(action==='start')await start();
  else if(action==='status')await status(directory);
  else if(action==='verify')await verify(directory);
  else await cleanup(JSON.parse(await fs.readFile(path.join(directory,'state.json'),'utf8')));
}catch(error){
  const codes=new Set(['image_identity_mismatch','port_boundary_invalid','namespace_boundary_invalid','container_boundary_invalid','services_not_ready','command_failed','command_start_failed','cleanup_owner_mismatch','cleanup_incomplete','usage_start_or_status_verify_stop_directory','verify_owner_mismatch','registration_query_unconfirmed','saved_credential_unconfirmed']);
  report({stage:'harness',status:'failed',error_code:codes.has(error.message)?error.message:'harness_error'});process.exitCode=1;
}
