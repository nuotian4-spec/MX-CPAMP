import {spawn} from 'node:child_process';
import crypto from 'node:crypto';

const images={
  cpa:{ref:'mx-cpa:bps-tool-diagnostic-20261111',id:'sha256:a4428de0eeda96d76cfa5733eed190ea8b45cd49083bf6b881f4bc9dfc1f5933',revision:'ef4d287ea3298a7680f8fc21e82db77417bbe634'},
  manager:{ref:'mx-cpamp:oauth-input-diagnostics-20261111',id:'sha256:cdf235bee2e22f2c33af145b8fb4d27e93b4478162b8738dcadd3595f3b74c4a',revision:'7b6d6ca7408856f2e345a0f5c1e5cdea29f39a44'},
};
const prefix='mx-manual-oauth-candidate-'+crypto.randomBytes(6).toString('hex');
const names={cpa:prefix+'-cpa',manager:prefix+'-manager'};
const keys={cpa:crypto.randomBytes(32).toString('hex'),admin:crypto.randomBytes(32).toString('hex'),api:crypto.randomBytes(32).toString('hex')};
const report=value=>process.stdout.write(JSON.stringify(value)+'\n');
const must=(value,code)=>{if(!value)throw new Error(code);};
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function docker(args,input){
  return await new Promise((resolve,reject)=>{
    const child=spawn('docker',args,{stdio:['pipe','pipe','pipe'],windowsHide:true});
    let text='',done=false;const timeout=setTimeout(()=>child.kill(),60000);
    child.stdout.setEncoding('utf8');child.stdout.on('data',chunk=>{text+=chunk;if(text.length>2*1024*1024)child.kill();});
    child.stderr.resume();child.stdin.on('error',()=>{});
    child.on('error',()=>{clearTimeout(timeout);done=true;reject(new Error('docker_start_failed'));});
    child.on('close',code=>{clearTimeout(timeout);if(!done){done=true;code===0?resolve(text):reject(new Error('docker_command_failed'));}});
    child.stdin.end(input);
  });
}
const inspect=async name=>JSON.parse(await docker(['inspect',name,'--format','{{json .}}']));
const put=(name,file,value)=>docker(['exec','-i',name,'sh','-c','umask 077; cat > '+file+'.pending && mv '+file+'.pending '+file],value);
let origin;
async function request(route,key){
  const response=await fetch(origin+route,{redirect:'error',headers:key?{authorization:'Bearer '+key}:{},signal:AbortSignal.timeout(4000)});
  const text=await response.text();let value;try{value=JSON.parse(text);}catch{}
  return {status:response.status,text,value};
}
try{
  must(/^sha256:[a-f0-9]{64}$/.test(images.manager.id),'candidate_identity_pending');
  for(const [role,want] of Object.entries(images)){
    const image=JSON.parse(await docker(['image','inspect',want.ref,'--format','{{json .}}']));
    must(image.Id===want.id&&image.Os==='linux'&&image.Architecture==='amd64'&&image.Config.Labels?.['org.opencontainers.image.revision']===want.revision,'candidate_identity_mismatch');
    report({stage:'image',role,id:image.Id,revision:want.revision});
  }
  const common=['--detach','--read-only','--log-driver','none','--label','mx.manual-oauth-candidate='+prefix];
  await docker(['run',...common,'--name',names.cpa,'--publish','127.0.0.1::18317/tcp','--tmpfs','/state:mode=0700,size=64m','--tmpfs','/tmp:mode=1777,size=32m','--entrypoint','/bin/sh',images.cpa.id,'-c','while [ ! -s /state/config.yaml ]; do sleep 0.1; done; exec timeout -s TERM 120 /CLIProxyAPI/CLIProxyAPI --config /state/config.yaml --local-model']);
  await put(names.cpa,'/state/config.yaml',JSON.stringify({'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},management:{'allow-remote':true,'secret-key':keys.cpa,'disable-control-panel':true,'disable-auto-update-panel':true},access:{'api-keys':[keys.api]},requests:{'proxy-url':'http://host.docker.internal:7890'},routing:{retry:{'request-retry':0,'max-retry-interval':0}},oauth:{'auth-dir':'/state/auths'},'bps-protocol':{'state-dir':'/state/bps-state'}}));
  await docker(['run',...common,'--name',names.manager,'--network','container:'+names.cpa,'--tmpfs','/data:mode=0700,size=128m','--tmpfs','/run/config:mode=0700,size=1m','--env','CPA_MANAGER_CONFIG=/run/config/config.json','--entrypoint','/bin/sh',images.manager.id,'-c','while [ ! -s /run/config/ready ]; do sleep 0.1; done; exec timeout -s TERM 120 cpa-manager-plus']);
  await put(names.manager,'/run/config/cpa-key',keys.cpa);
  await put(names.manager,'/run/config/admin-key',keys.admin);
  await put(names.manager,'/run/config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key',quotaCooldownEnabled:false,accountActionsEnabled:false,accountActionsAutoDisable:false}));
  await put(names.manager,'/run/config/ready','ready');
  const cpa=await inspect(names.cpa),manager=await inspect(names.manager);
  const bindings=cpa.NetworkSettings.Ports?.['18317/tcp'];
  must(bindings?.length===1&&bindings[0].HostIp==='127.0.0.1'&&Object.keys(cpa.HostConfig.PortBindings||{}).length===1,'loopback_boundary_invalid');
  must(manager.HostConfig.NetworkMode==='container:'+cpa.Id&&!Object.keys(manager.HostConfig.PortBindings||{}).length,'namespace_boundary_invalid');
  for(const [role,item] of [['cpa',cpa],['manager',manager]])must(item.Image===images[role].id&&item.HostConfig.ReadonlyRootfs&&item.HostConfig.LogConfig.Type==='none'&&Object.keys(item.HostConfig.Tmpfs||{}).length>=2,'container_boundary_invalid');
  origin='http://127.0.0.1:'+bindings[0].HostPort;
  let healthy=false;
  for(let count=0;count<80;count++){
    try{const health=await request('/health');if(health.status===200&&health.value?.ok){healthy=true;break;}}catch{}
    await sleep(200);
  }
  must(healthy,'services_not_ready');
  const unauthenticated=await request('/api/oauth-login/capabilities');
  const capabilities=await request('/api/oauth-login/capabilities',keys.admin);
  must(unauthenticated.status===401&&capabilities.status===200&&capabilities.value?.password_login_configured===false,'capabilities_contract_failed');
  report({stage:'capabilities',unauthenticated_http:unauthenticated.status,authenticated_http:capabilities.status,password_login_configured:false});
  const page=await request('/management.html');
  const disabledHint=page.text.includes('当前服务未配置2FA登录器');
  const callbackHint=page.text.includes('回调地址未通过校验');
  must(page.status===200&&disabledHint&&callbackHint,'panel_hint_missing');
  report({stage:'panel',http_status:page.status,unconfigured_password_login_hint_present:disabledHint,callback_validation_hint_present:callbackHint,rendered_browser_acceptance:false});
  const listing=await request('/v0/management/auth-files',keys.admin);
  must(listing.status===200&&Array.isArray(listing.value?.files)&&listing.value.files.length===0,'empty_registry_failed');
  const files=await docker(['exec',names.cpa,'sh','-c','for p in /state/auths/*.json; do [ -f "$p" ] || continue; printf present; done']);
  must(!files,'empty_saved_state_failed');
  report({stage:'verify_empty',saved_count:0,registered_count:0,manual_oauth_sessions_created:0,harness_initiated_refresh_requests:0,harness_initiated_model_requests:0,background_refresh_hard_blocked:false});
}catch(error){
  const allowed=new Set(['candidate_identity_pending','candidate_identity_mismatch','docker_start_failed','docker_command_failed','loopback_boundary_invalid','namespace_boundary_invalid','container_boundary_invalid','services_not_ready','capabilities_contract_failed','panel_hint_missing','empty_registry_failed','empty_saved_state_failed']);
  report({stage:'preflight',status:'failed',error_code:allowed.has(error.message)?error.message:'preflight_error'});process.exitCode=1;
}finally{
  let cleanup=true;
  for(const name of [names.manager,names.cpa]){
    try{const item=await inspect(name);must(item.Config.Labels?.['mx.manual-oauth-candidate']===prefix,'cleanup_owner_mismatch');await docker(['rm','--force',name]);}
    catch{try{const present=await docker(['ps','-aq','--filter','name=^/'+name+'$']);if(present.trim())cleanup=false;}catch{cleanup=false;}}
  }
  try{cleanup=cleanup&&!(await docker(['ps','-aq','--filter','label=mx.manual-oauth-candidate='+prefix])).trim();}catch{cleanup=false;}
  report({stage:'cleanup',verified:cleanup,token_state_retained:cleanup?false:null});
  keys.cpa=keys.admin=keys.api='';if(!cleanup)process.exitCode=1;
}
