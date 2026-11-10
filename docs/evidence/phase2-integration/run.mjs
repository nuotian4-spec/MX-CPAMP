import {spawn} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import crypto from 'node:crypto';
import fs from 'node:fs';
import {parseArgs} from 'node:util';

const directory=path.dirname(fileURLToPath(import.meta.url));
let options;
try{options=parseArgs({options:{preflight:{type:'boolean'},'cpa-image':{type:'string',default:'mx-cpa:20261110-rc1'},'manager-image':{type:'string',default:'mx-cpamp:20261110-rc1'},'worker-image':{type:'string',default:'mx-oauth-login:20261110-rc1'},'worker-source':{type:'string'},'cpa-binary':{type:'string'},'manager-binary':{type:'string'},'source-revision':{type:'string'},'cpa-revision':{type:'string'},'manager-revision':{type:'string'},'worker-revision':{type:'string'}}}).values;}
catch{process.stdout.write('{"stage":"harness","status":"failed","error_code":"invalid_options"}\n');process.exit(1);}
const preflight=options.preflight;
const images={cpa:options['cpa-image'],manager:options['manager-image'],worker:options['worker-image']};
const overlays={worker:options['worker-source']?path.resolve(options['worker-source']):null,cpa:options['cpa-binary']?path.resolve(options['cpa-binary']):null,manager:options['manager-binary']?path.resolve(options['manager-binary']):null};
const revisions=Object.fromEntries(['cpa','manager','worker'].map(role=>[role,options[role+'-revision']??options['source-revision']]));
const prefix='mx-phase2-integration-'+crypto.randomBytes(6).toString('hex');
const names={cpa:prefix+'-cpa',manager:prefix+'-manager',worker:prefix+'-worker'};
const keys=Object.fromEntries(['cpa','admin','api','worker'].map(name=>[name,crypto.randomBytes(32).toString('hex')]));
const created=[];
let interrupted=false, running, stage='images';
function report(value){process.stdout.write(JSON.stringify(value)+'\n');}
function overlayFingerprint(role){
  const input=overlays[role];if(!input)return null;
  const digest=crypto.createHash('sha256');
  if(role!=='worker')digest.update(fs.readFileSync(input));
  else{
    const visit=directory=>{for(const entry of fs.readdirSync(directory,{withFileTypes:true}).sort((a,b)=>a.name.localeCompare(b.name))){const full=path.join(directory,entry.name);if(entry.isDirectory())visit(full);else if(entry.isFile()){digest.update(path.relative(input,full).replaceAll('\\','/')+'\0');digest.update(fs.readFileSync(full));}}};
    visit(input);
  }
  return digest.digest('hex');
}
let originalFingerprints;
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
const common=['--detach','--read-only','--log-driver','none','--label','mx.phase2integration='+prefix];
const bind=['--mount','type=bind,source='+directory+',target=/checks,readonly'];
async function create(name,args){
  if(interrupted)throw new Error('interrupted');
  created.push(name);
  await docker(['run',...common,'--name',name,...args]);
}
async function put(name,target,value,owner){
  await docker(['exec','--user','0','-i',name,'sh','-c','umask 077; cat > '+target+'.pending'+(owner?' && chown '+owner+' '+target+'.pending':'')+' && mv '+target+'.pending '+target],{input:value});
}
try{
  for(const image of Object.values(images))if(!/^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$/.test(image))throw new Error('invalid_options');
  for(const revision of [options['source-revision'],...Object.values(revisions)])if(revision!==undefined&&!/^[a-f0-9]{40}$/.test(revision))throw new Error('invalid_options');
  if(Object.values(overlays).some(Boolean)&&!/^[a-f0-9]{40}$/.test(options['source-revision']||''))throw new Error('overlay_revision_required');
  if(overlays.worker&&!fs.statSync(overlays.worker).isDirectory())throw new Error('invalid_overlay');
  if(overlays.manager&&!fs.statSync(overlays.manager).isFile())throw new Error('invalid_overlay');
  if(overlays.cpa&&!fs.statSync(overlays.cpa).isFile())throw new Error('invalid_overlay');
  originalFingerprints=Object.fromEntries(['worker','cpa','manager'].map(role=>[role,overlayFingerprint(role)]));
  report({stage:'inputs',source_revision:options['source-revision']||null,worker_source_overlay:Boolean(overlays.worker),cpa_binary_overlay:Boolean(overlays.cpa),manager_binary_overlay:Boolean(overlays.manager),overlay_fingerprints:originalFingerprints,external_network:'none',synthetic_only:true});
  for(const [role,image]of Object.entries(images)){
    const inspected=await docker(['image','inspect',image,'--format','{{json .}}']);const identity=JSON.parse(inspected.output);
    if(!/^sha256:[a-f0-9]{64}$/.test(identity.Id))throw new Error('image_identity_invalid');
    const revision=identity.Config?.Labels?.['org.opencontainers.image.revision'];
    const expected=(!overlays[role]||options[role+'-revision']!==undefined)?revisions[role]:undefined;
    report({stage:'image',role,image_id:identity.Id,image_revision:/^[a-f0-9]{40}$/.test(revision||'')?revision:null,expected_revision:expected||null});
    if(expected&&revision!==expected)throw new Error('image_revision_mismatch');
  }
  stage='cpa_start';
  await create(names.cpa,['--network','none','--tmpfs','/state:mode=0700,size=64m','--tmpfs','/tmp:mode=1777,size=32m',...(overlays.cpa?['--mount','type=bind,source='+overlays.cpa+',target=/candidate/CLIProxyAPI,readonly']:[]),'--entrypoint','/bin/sh',images.cpa,'-c','while [ ! -s /state/config.yaml ]; do sleep 0.1; done; exec '+(overlays.cpa?'/candidate/CLIProxyAPI':'/CLIProxyAPI/CLIProxyAPI')+' --config /state/config.yaml --local-model']);
  stage='cpa_config';
  await put(names.cpa,'/state/config.yaml',JSON.stringify({'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},management:{'allow-remote':true,'secret-key':keys.cpa,'disable-control-panel':true,'disable-auto-update-panel':true},access:{'api-keys':[keys.api]},oauth:{'auth-dir':'/state/auths',providers:{bps:{models:[]}}},'api-keys':{'openai-compatibility':[{name:'phase2-fixture','base-url':'http://127.0.0.1:18517/v1',models:[{name:'fixture-model-a',alias:'fixture-model-a'},{name:'fixture-model-b',alias:'fixture-model-b'}],keys:[{'api-key':'phase2-config-one'},{'api-key':'phase2-config-two'}]}]}}));
  stage='worker_start';
  await create(names.worker,['--init','--network','container:'+names.cpa,'--tmpfs','/tmp:mode=1777,size=64m','--tmpfs','/run/secrets:mode=0700,uid=1000,gid=1000,size=1m',...bind,...(overlays.worker?['--mount','type=bind,source='+overlays.worker+',target=/app/src,readonly']:[]),'--entrypoint','sh',images.worker,'-c','while [ ! -s /run/secrets/oauth_login_secret ] || [ ! -r /run/secrets/oauth_login_secret ]; do sleep 0.1; done; exec node /checks/worker.mjs']);
  stage='worker_config';
  await put(names.worker,'/run/secrets/oauth_login_secret',keys.worker,'1000:1000');
  stage='manager_start';
  await create(names.manager,['--network','container:'+names.cpa,'--tmpfs','/data:mode=0700,size=128m','--tmpfs','/run/config:mode=0700,size=1m','--tmpfs','/run/secrets:mode=0700,size=1m',...(overlays.manager?['--mount','type=bind,source='+overlays.manager+',target=/candidate/cpa-manager-plus,readonly']:[]),'--env','CPA_MANAGER_CONFIG=/run/config/config.json','--env','OAUTH_LOGIN_URL=http://127.0.0.1:18417','--env','OAUTH_LOGIN_SECRET_FILE=/run/secrets/oauth_login_secret','--entrypoint','sh',images.manager,'-c','while [ ! -s /run/config/ready ]; do sleep 0.1; done; exec '+(overlays.manager?'/candidate/cpa-manager-plus':'cpa-manager-plus')]);
  stage='manager_config';
  for(const [file,value]of [['cpa-key',keys.cpa],['admin-key',keys.admin]])await put(names.manager,'/run/config/'+file,value);
  await put(names.manager,'/run/secrets/oauth_login_secret',keys.worker);
  await put(names.manager,'/run/config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key'}));
  await put(names.manager,'/run/config/ready','ready');
  stage='boundaries';
  for(const name of created){
    const inspected=await docker(['inspect',name,'--format','{{json .}}']);const v=JSON.parse(inspected.output);
    if(!v.HostConfig.ReadonlyRootfs||v.HostConfig.LogConfig.Type!=='none'||Object.keys(v.HostConfig.PortBindings||{}).length||!Object.keys(v.HostConfig.Tmpfs||{}).length||!(v.HostConfig.NetworkMode==='none'||v.HostConfig.NetworkMode.startsWith('container:')))throw new Error('container_boundary_invalid');
  }
  report({stage:'boundaries',readonly:true,tmpfs_state:true,docker_logging:false,published_ports:false,external_network:'none',production_access:false});
  if(interrupted)throw new Error('interrupted');
  stage='check';
  const result=await docker(['exec','-i',names.worker,'node','/checks/check.mjs'],{input:JSON.stringify({...keys,preflight:Boolean(preflight)}),timeout:180000,allowFailure:true});
  for(const line of result.output.trim().split('\n').filter(Boolean)){
    const value=JSON.parse(line);
    const permitted=['stage','error_code','result','services_healthy','unauthorized_status','external_network','serial','failure_continued','latest_terminal_recovered','duplicate_not_replayed','synthetic_only','oauth_set_default_restored','oauth_persisted','config_set_default_restored','config_identity_stable','other_credential_unchanged','cross_model_shared','independent_credentials','pool_wait_observed','queued_cancel_removed','active_cancel_upstream_exit','release_woke_waiter','all_drained','real_upstream_called','synthetic_login','synthetic_upstream'];
    report(Object.fromEntries(Object.entries(value).filter(([key])=>permitted.includes(key))));
  }
  if(result.code!==0)process.exitCode=1;
}catch(error){
  const allowed=new Set(['invalid_options','overlay_revision_required','invalid_overlay','invalid_input','image_identity_invalid','image_revision_mismatch','container_boundary_invalid','interrupted','docker_start_failed','docker_command_failed']);
  report({stage,status:'failed',error_code:allowed.has(error.message)?error.message:'harness_error'});process.exitCode=1;
}finally{
  for(const name of [...created].reverse())await docker(['rm','--force',name],{allowFailure:true});
  const remaining=await docker(['ps','--all','--filter','label=mx.phase2integration='+prefix,'--format','{{.ID}}'],{allowFailure:true});
  const verified=remaining.code===0&&!remaining.output.trim();
  report({stage:'cleanup',verified,token_state_retained:verified?false:null});
  if(!verified)process.exitCode=1;
  if(originalFingerprints){
    let unchanged=false;try{unchanged=['worker','cpa','manager'].every(role=>overlayFingerprint(role)===originalFingerprints[role]);}catch{}
    report({stage:'input_recheck',unchanged});if(!unchanged)process.exitCode=1;
  }
  for(const name of Object.keys(keys))keys[name]='';
}
