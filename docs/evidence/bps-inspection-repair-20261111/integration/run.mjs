import {spawn} from 'node:child_process';
import {parseArgs} from 'node:util';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import crypto from 'node:crypto';

const directory=path.dirname(fileURLToPath(import.meta.url));
const options=parseArgs({options:Object.fromEntries(['cpa-image','cpa-id','cpa-revision','manager-image','manager-id','manager-revision','checks-image'].map(key=>[key,{type:'string'}]))}).values;
const checksImage=options['checks-image']||'sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758';
const prefix='mx-bps-inspection-'+crypto.randomBytes(6).toString('hex');
const label='mx.bpsinspection='+prefix;
const names={cpa:prefix+'-cpa',manager:prefix+'-manager',checks:prefix+'-checks'};
const keys=Object.fromEntries(['cpa','admin','api'].map(key=>[key,crypto.randomBytes(32).toString('hex')]));
const created=[];
let stage='inputs',running,interrupted=false;
const report=value=>process.stdout.write(JSON.stringify(value)+'\n');
function docker(args,{input,allowFailure=false,timeout=60000}={}){
  return new Promise((resolve,reject)=>{
    const child=spawn('docker',args,{stdio:['pipe','pipe','pipe'],windowsHide:true});running=child;
    let output='',expired=false;
    const timer=setTimeout(()=>{expired=true;child.kill();},timeout);
    child.stdout.setEncoding('utf8');child.stdout.on('data',chunk=>{output+=chunk;if(output.length>1024*1024){expired=true;child.kill();}});
    child.stderr.resume();child.stdin.on('error',()=>{});
    child.on('error',()=>{clearTimeout(timer);reject(new Error('docker_start_failed'));});
    child.on('close',code=>{clearTimeout(timer);if(running===child)running=undefined;if((code!==0||expired)&&!allowFailure)reject(new Error('docker_command_failed'));else resolve({code,output});});
    child.stdin.end(input);
  });
}
for(const signal of ['SIGINT','SIGTERM'])process.on(signal,()=>{interrupted=true;running?.kill();});
async function create(name,args){
  if(interrupted)throw new Error('interrupted');
  created.push(name);
  await docker(['run','--detach','--read-only','--log-driver','none','--label',label,'--name',name,...args]);
}
async function put(name,target,value){
  await docker(['exec','--user','0','-i',name,'sh','-c','umask 077; mkdir -p '+path.posix.dirname(target)+' && cat > '+target+'.pending && mv '+target+'.pending '+target],{input:value});
}
try{
  const identities={};
  for(const role of ['cpa','manager']){
    const image=options[role+'-image'],id=options[role+'-id'],revision=options[role+'-revision'];
    if(!image||!/^sha256:[a-f0-9]{64}$/.test(id||'')||!/^[a-f0-9]{40}$/.test(revision||''))throw new Error('invalid_inputs');
    const inspected=await docker(['image','inspect',image,'--format','{{json .}}']);const actual=JSON.parse(inspected.output);
    if(actual.Id!==id||actual.Config?.Labels?.['org.opencontainers.image.revision']!==revision)throw new Error('image_identity_mismatch');
    identities[role]=id;report({stage:'image',role,image_id:id,image_revision:revision});
  }
  const checks=JSON.parse((await docker(['image','inspect',checksImage,'--format','{{json .}}'])).output);
  if(!/^sha256:[a-f0-9]{64}$/.test(checks.Id))throw new Error('image_identity_mismatch');
  identities.checks=checks.Id;report({stage:'image',role:'checks_runner',image_id:checks.Id,oauth_service_started:false});
  stage='cpa_start';
  await create(names.cpa,['--network','none','--tmpfs','/state:mode=0700,size=64m','--tmpfs','/tmp:mode=1777,size=32m','--entrypoint','/bin/sh',identities.cpa,'-c','while [ ! -s /state/ready ]; do sleep 0.1; done; exec /CLIProxyAPI/CLIProxyAPI --config /state/config.yaml --local-model']);
  await put(names.cpa,'/state/config.yaml',JSON.stringify({'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},management:{'allow-remote':true,'secret-key':keys.cpa,'disable-control-panel':true,'disable-auto-update-panel':true},access:{'api-keys':[keys.api]},oauth:{'auth-dir':'/state/auths',providers:{bps:{models:[]}}}}));
  await put(names.cpa,'/state/auths/bps-incomplete.json',JSON.stringify({type:'bps',profile:'excel',client_id:'app_fnr0pYvVwwFDocDumLG3H2Bp',email:'bps-incomplete@example.invalid',account_id:'synthetic-bps-account',sub:'synthetic-bps-sub',access_token:'',refresh_token:'',expired:'2099-01-01T00:00:00Z',last_refresh:new Date().toISOString()}));
  await put(names.cpa,'/state/ready','ready');
  stage='manager_start';
  await create(names.manager,['--network','container:'+names.cpa,'--tmpfs','/data:mode=0700,size=128m','--tmpfs','/run/config:mode=0700,size=1m','--tmpfs','/tmp:mode=1777,size=32m','--env','CPA_MANAGER_CONFIG=/run/config/config.json','--entrypoint','sh',identities.manager,'-c','while [ ! -s /run/config/ready ]; do sleep 0.1; done; exec cpa-manager-plus']);
  for(const [file,value]of [['cpa-key',keys.cpa],['admin-key',keys.admin]])await put(names.manager,'/run/config/'+file,value);
  await put(names.manager,'/run/config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key'}));
  await put(names.manager,'/run/config/ready','ready');
  await create(names.checks,['--network','container:'+names.cpa,'--tmpfs','/tmp:mode=1777,size=16m','--mount','type=bind,source='+directory+',target=/checks,readonly','--entrypoint','node',identities.checks,'-e','setInterval(()=>{},1000)']);
  stage='boundaries';
  for(const name of created){
    const actual=JSON.parse((await docker(['inspect',name,'--format','{{json .}}'])).output);
    if(!actual.HostConfig.ReadonlyRootfs||actual.HostConfig.LogConfig.Type!=='none'||Object.keys(actual.HostConfig.PortBindings||{}).length||!Object.keys(actual.HostConfig.Tmpfs||{}).length||!(actual.HostConfig.NetworkMode==='none'||actual.HostConfig.NetworkMode.startsWith('container:'))||actual.Config.Labels?.['mx.bpsinspection']!==prefix)throw new Error('container_boundary_invalid');
    if(actual.Mounts.some(m=>m.Type!=='tmpfs'&&!(name===names.checks&&m.Type==='bind'&&m.Destination==='/checks'&&!m.RW)))throw new Error('container_boundary_invalid');
  }
  report({stage:'boundaries',readonly:true,tmpfs_state:true,docker_logging:false,published_ports:false,external_network:'none',worker_started:false,synthetic_only:true});
  stage='checks';
  const result=await docker(['exec','-i',names.checks,'node','/checks/check.mjs'],{input:JSON.stringify(keys),timeout:120000,allowFailure:true});
  for(const line of result.output.trim().split('\n').filter(Boolean))report(JSON.parse(line));
  if(result.code!==0)process.exitCode=1;
}catch(error){
  const allowed=new Set(['invalid_inputs','image_identity_mismatch','container_boundary_invalid','interrupted','docker_start_failed','docker_command_failed']);
  report({stage,status:'failed',error_code:allowed.has(error.message)?error.message:'harness_error'});process.exitCode=1;
}finally{
  for(const name of [...created].reverse()){
    const inspected=await docker(['inspect',name,'--format','{{json .Config.Labels}}'],{allowFailure:true});
    let owned=false;try{owned=JSON.parse(inspected.output)?.['mx.bpsinspection']===prefix;}catch{}
    if(owned)await docker(['rm','--force',name],{allowFailure:true});
  }
  const remaining=await docker(['ps','--all','--filter','label='+label,'--format','{{.ID}}'],{allowFailure:true});
  const verified=remaining.code===0&&!remaining.output.trim();
  report({stage:'cleanup',verified,synthetic_state_retained:verified?false:null});
  if(!verified)process.exitCode=1;
  for(const key of Object.keys(keys))keys[key]='';
}
