import {spawn} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import crypto from 'node:crypto';
import fs from 'node:fs';
import {parseArgs} from 'node:util';
import {sanitizeReceipt} from './diagnostics.mjs';

const directory=path.dirname(fileURLToPath(import.meta.url));
let options;
try{options=parseArgs({options:{execute:{type:'boolean'},preflight:{type:'boolean'},...Object.fromEntries(['cpa','manager','worker'].flatMap(role=>['image','image-id','revision'].map(field=>[role+'-'+field,{type:'string'}])))}}).values;}
catch{process.stdout.write('{"stage":"harness","status":"failed","error_code":"invalid_options"}\n');process.exit(1);}
const preflight=!options.execute;
const images={cpa:options['cpa-image'],manager:options['manager-image'],worker:options['worker-image']};
const revisions=Object.fromEntries(['cpa','manager','worker'].map(role=>[role,options[role+'-revision']]));
const expectedIDs=Object.fromEntries(['cpa','manager','worker'].map(role=>[role,options[role+'-image-id']]));
const imageIDs={};
const prefix='mx-bps-tool-diagnostic-'+crypto.randomBytes(6).toString('hex');
const names={cpa:prefix+'-cpa',manager:prefix+'-manager',worker:prefix+'-worker'};
const keys=Object.fromEntries(['cpa','admin','api','worker'].map(name=>[name,crypto.randomBytes(32).toString('hex')]));
const created=[];
let credentials, interrupted=false, running, stage='images';
function report(value){process.stdout.write(JSON.stringify(value)+'\n');}
let harnessInputs;
const legacyDiagnostics=path.resolve(directory,'../phase2-tool-image-20261111/diagnostics.mjs');
const harnessFiles=['run.mjs','check.mjs','payloads.mjs','diagnostics.mjs','worker.mjs','proxy-runner.mjs'].map(file=>path.join(directory,file)).concat(legacyDiagnostics);
function docker(args,{input,allowFailure=false,timeout=60000}={}){
  return new Promise((resolve,reject)=>{
    const child=spawn('docker',args,{stdio:['pipe','pipe','pipe'],windowsHide:true});
    running=child;
    let output='',failed=false;
    const timer=setTimeout(()=>{failed=true;child.kill();},timeout);
    child.stdout.setEncoding('utf8');child.stdout.on('data',chunk=>{output+=chunk;if(output.length>1024*1024){failed=true;child.kill();}});
    child.stderr.resume();child.stdin.on('error',()=>{});
    child.on('error',()=>{clearTimeout(timer);reject(new Error('docker_start_failed'));});
    child.on('close',code=>{clearTimeout(timer);if(running===child)running=undefined;if((code!==0||failed)&&!allowFailure)reject(new Error('docker_command_failed'));else resolve({code,output});});
    child.stdin.end(input);
  });
}
for(const signal of ['SIGINT','SIGTERM'])process.on(signal,()=>{interrupted=true;running?.kill();});
const common=['--detach','--read-only','--log-driver','none','--label','mx.bpsfeature='+prefix];
const bind=['--mount','type=bind,source='+directory+',target=/checks,readonly','--mount','type=bind,source='+legacyDiagnostics+',target=/phase2-tool-image-20261111/diagnostics.mjs,readonly'];
async function create(name,args){
  if(interrupted)throw new Error('interrupted');
  created.push(name);
  await docker(['run',...common,'--name',name,...args]);
}
async function put(name,target,value,owner){
  await docker(['exec','--user','0','-i',name,'sh','-c','umask 077; cat > '+target+'.pending'+(owner?' && chown '+owner+' '+target+'.pending':'')+' && mv '+target+'.pending '+target],{input:value});
}
try{
  if(options.execute&&options.preflight)throw new Error('invalid_options');
  for(const image of Object.values(images))if(typeof image!=='string'||!/^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$/.test(image))throw new Error('invalid_options');
  for(const revision of Object.values(revisions))if(!/^[a-f0-9]{40}$/.test(revision||''))throw new Error('invalid_options');
  for(const id of Object.values(expectedIDs))if(!/^sha256:[a-f0-9]{64}$/.test(id||''))throw new Error('invalid_options');
  harnessInputs=harnessFiles.map(file=>fs.readFileSync(file));
  report({stage:'inputs',profile:'bps',mode:preflight?'preflight':'execute',login_batch_budget:preflight?0:1,client_request_budget:preflight?0:2,refresh_endpoint_budget:0,production_network_equivalence:false});
  if(!preflight){
    const chunks=[];let size=0;
    for await(const chunk of process.stdin){size+=chunk.length;if(size>16384)throw new Error('invalid_input');chunks.push(chunk);}
    credentials=JSON.parse(Buffer.concat(chunks).toString('utf8'));chunks.length=0;
    if(typeof credentials.email!=='string'||!credentials.email.includes('@')||typeof credentials.password!=='string'||!credentials.password||typeof credentials.totp_secret!=='string'||!/^[A-Z2-7\s=]{16,160}$/i.test(credentials.totp_secret))throw new Error('invalid_input');
  }
  for(const [role,image]of Object.entries(images)){
    const inspected=await docker(['image','inspect',image,'--format','{{json .}}']);const identity=JSON.parse(inspected.output);
    if(!/^sha256:[a-f0-9]{64}$/.test(identity.Id))throw new Error('image_identity_invalid');
    const revision=identity.Config?.Labels?.['org.opencontainers.image.revision'];
    report({stage:'image',role,image_id:identity.Id,image_revision:/^[a-f0-9]{40}$/.test(revision||'')?revision:null,expected_image_id:expectedIDs[role],expected_revision:revisions[role]});
    if(identity.Id!==expectedIDs[role])throw new Error('image_identity_mismatch');
    if(revision!==revisions[role])throw new Error('image_revision_mismatch');
    imageIDs[role]=identity.Id;
  }
  stage='cpa_start';
  await create(names.cpa,['--tmpfs','/state:mode=0700,size=64m','--tmpfs','/tmp:mode=1777,size=32m','--entrypoint','/bin/sh',imageIDs.cpa,'-c','while [ ! -s /state/config.yaml ]; do sleep 0.1; done; exec /CLIProxyAPI/CLIProxyAPI --config /state/config.yaml --local-model']);
  stage='cpa_config';
  await put(names.cpa,'/state/config.yaml',JSON.stringify({'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},management:{'allow-remote':true,'secret-key':keys.cpa,'disable-control-panel':true,'disable-auto-update-panel':true},access:{'api-keys':[keys.api]},requests:{'proxy-url':'http://host.docker.internal:7890'},routing:{retry:{'request-retry':0,'max-retry-interval':0}},oauth:{'auth-dir':'/state/auths',providers:{bps:{models:['gpt-6-luna']}}}}));
  stage='worker_start';
  await create(names.worker,['--init','--network','container:'+names.cpa,'--tmpfs','/tmp:mode=1777,size=64m','--tmpfs','/run/secrets:mode=0700,uid=1000,gid=1000,size=1m',...bind,'--entrypoint','sh',imageIDs.worker,'-c','while [ ! -s /run/secrets/oauth_login_secret ] || [ ! -r /run/secrets/oauth_login_secret ]; do sleep 0.1; done; exec node /checks/worker.mjs']);
  stage='worker_config';
  await put(names.worker,'/run/secrets/oauth_login_secret',keys.worker,'1000:1000');
  stage='manager_start';
  await create(names.manager,['--network','container:'+names.cpa,'--tmpfs','/data:mode=0700,size=128m','--tmpfs','/run/config:mode=0700,size=1m','--tmpfs','/run/secrets:mode=0700,size=1m','--env','CPA_MANAGER_CONFIG=/run/config/config.json','--env','OAUTH_LOGIN_URL=http://127.0.0.1:18417','--env','OAUTH_LOGIN_SECRET_FILE=/run/secrets/oauth_login_secret','--entrypoint','sh',imageIDs.manager,'-c','while [ ! -s /run/config/ready ]; do sleep 0.1; done; exec cpa-manager-plus']);
  stage='manager_config';
  for(const [file,value]of [['cpa-key',keys.cpa],['admin-key',keys.admin]])await put(names.manager,'/run/config/'+file,value);
  await put(names.manager,'/run/secrets/oauth_login_secret',keys.worker);
  await put(names.manager,'/run/config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key'}));
  await put(names.manager,'/run/config/ready','ready');
  stage='boundaries';
  for(const [role,name] of Object.entries(names)){
    const inspected=await docker(['inspect',name,'--format','{{json .}}']);const v=JSON.parse(inspected.output);
    if(v.Image!==imageIDs[role]||!v.HostConfig.ReadonlyRootfs||v.HostConfig.LogConfig.Type!=='none'||Object.keys(v.HostConfig.PortBindings||{}).length||!Object.keys(v.HostConfig.Tmpfs||{}).length)throw new Error('container_boundary_invalid');
  }
  report({stage:'boundaries',readonly:true,tmpfs_state:true,docker_logging:false,published_ports:false,secret_argv_env:false,production_access:false});
  if(interrupted)throw new Error('interrupted');
  stage='check';
  const result=await docker(['exec','-i',names.worker,'node','/checks/check.mjs'],{input:JSON.stringify({...keys,mode:preflight?'preflight':'execute',credentials}),timeout:38*60*1000,allowFailure:true});
  for(const line of result.output.trim().split('\n').filter(Boolean)){
    const value=JSON.parse(line);
    report(sanitizeReceipt(value));
  }
  if(result.code!==0)process.exitCode=1;
}catch(error){
  const allowed=new Set(['invalid_options','invalid_input','image_identity_invalid','image_identity_mismatch','image_revision_mismatch','container_boundary_invalid','interrupted','docker_start_failed','docker_command_failed']);
  report({stage,status:'failed',error_code:allowed.has(error.message)?error.message:'harness_error'});process.exitCode=1;
}finally{
  if(credentials){credentials.email=credentials.password=credentials.totp_secret='';credentials=undefined;}
  for(const name of [...created].reverse())await docker(['rm','--force',name],{allowFailure:true});
  const remaining=await docker(['ps','--all','--filter','label=mx.bpsfeature='+prefix,'--format','{{.ID}}'],{allowFailure:true});
  const verified=remaining.code===0&&!remaining.output.trim();
  report({stage:'cleanup',verified,token_state_retained:verified?false:null});
  if(!verified)process.exitCode=1;
  if(harnessInputs){
    let unchanged=false,legacyUnchanged=false;try{unchanged=harnessFiles.every((file,index)=>fs.readFileSync(file).equals(harnessInputs[index]));legacyUnchanged=fs.readFileSync(legacyDiagnostics).equals(harnessInputs.at(-1));}catch{}
    report({stage:'input_recheck',unchanged,harness_unchanged:unchanged,legacy_diagnostics_unchanged:legacyUnchanged});if(!unchanged)process.exitCode=1;
  }
  for(const name of Object.keys(keys))keys[name]='';
}
