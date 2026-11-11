import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import crypto from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const directory=path.dirname(fileURLToPath(import.meta.url));
const cpaImage='sha256:a4428de0eeda96d76cfa5733eed190ea8b45cd49083bf6b881f4bc9dfc1f5933';
const cpaRevision='ef4d287ea3298a7680f8fc21e82db77417bbe634';
const nodeImage='sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758';
const label='mx-bps-durability-'+crypto.randomBytes(6).toString('hex');
const runtime=fs.mkdtempSync(path.join(os.tmpdir(),label+'-'));
const composeFile=path.join(runtime,'compose.json');
const wrongFile=path.join(runtime,'wrong.json');
const stateTarget='/var/lib/cpa/bps-state';
const authTarget='/state/auths';
const api='SYNTHETIC_API_KEY_MARKER';
let stage='input',exit=0,started=false;
const report=value=>process.stdout.write(JSON.stringify(value)+'\n');
const must=(ok,code)=>{if(!ok)throw new Error(code);};
function exec(command,args,input,allowFailure=false){
  const result=spawnSync(command,args,{input,encoding:'utf8',windowsHide:true,timeout:120000,maxBuffer:4*1024*1024});
  if(!allowFailure)must(result.status===0,'command_failed');
  return result;
}
const docker=(args,input,allowFailure=false)=>exec('docker',args,input,allowFailure);
const compose=(args,extra)=>docker(['compose','--project-name',label,'--file',composeFile,...(extra?['--file',extra]:[]),...args]);
const inspect=id=>JSON.parse(docker(['inspect',id]).stdout)[0];
const container=service=>compose(['ps','--all','--quiet',service]).stdout.trim();
function stateMount(value){return value.Mounts.find(item=>item.Destination===stateTarget);}
function guard(value,expected){
  const mount=stateMount(value);
  must(value.Image===cpaImage,'image_drift');
  must(mount?.Type==='volume'&&mount.Source===expected.Source&&mount.Destination===expected.Destination&&mount.RW===true,'state_mount_drift');
  must(stateTarget!==authTarget&&!stateTarget.startsWith(authTarget+'/'),'state_inside_auth');
}
function client(service,url,method='GET',body){
  const code=`let raw='';for await(const part of process.stdin)raw+=part;const p=JSON.parse(raw);const r=await fetch(p.url,{method:p.method,headers:{authorization:'Bearer ${api}','content-type':'application/json'},...(p.body===undefined?{}:{body:JSON.stringify(p.body)}),signal:AbortSignal.timeout(15000)});let body;try{body=await r.json();}catch{body=null;}process.stdout.write(JSON.stringify({status:r.status,body}));`;
  return JSON.parse(docker(['exec','--interactive',container(service),'node','--input-type=module','-e',code],JSON.stringify({url,method,body})).stdout);
}
async function ready(){
  for(let attempt=0;attempt<60;attempt++){
    try{const r=client('upstream','http://cpa:8317/v1/models');if(r.status===200&&r.body.data?.some(item=>item.id==='gpt-6-luna'))return;}catch{}
    await new Promise(resolve=>setTimeout(resolve,200));
  }
  throw new Error('service_not_ready');
}
function records(){
  const code=`const fs=require('node:fs');const p='/observe/bindings';const files=[];function walk(root){for(const item of fs.readdirSync(root,{withFileTypes:true})){const full=root+'/'+item.name;if(item.isDirectory())walk(full);else files.push(full);}}walk('/observe');if(files.length!==1||!/^\\/observe\\/bindings\\/call_bps_[a-f0-9-]+\\.json$/.test(files[0]))process.exit(9);process.stdout.write(JSON.stringify(fs.readdirSync(p).filter(f=>f.endsWith('.json')).map(f=>({record:JSON.parse(fs.readFileSync(p+'/'+f,'utf8')),mode:fs.statSync(p+'/'+f).mode&511}))));`;
  return JSON.parse(docker(['exec',container('upstream'),'node','-e',code]).stdout);
}
try{
  for(const [id,revision]of [[cpaImage,cpaRevision],[nodeImage,'95dd9ca2c16d5469c43ce7c910f38ff271af45e3']]){
    const value=JSON.parse(docker(['image','inspect',id]).stdout)[0];
    must(value.Id===id&&value.Config.Labels?.['org.opencontainers.image.revision']===revision,'image_input_mismatch');
  }
  fs.mkdirSync(path.join(runtime,'auths'));
  fs.writeFileSync(path.join(runtime,'auths','synthetic.json'),JSON.stringify({type:'bps',profile:'excel',client_id:'app_fnr0pYvVwwFDocDumLG3H2Bp',email:'fixture@example.invalid',account_id:'synthetic-account',sub:'synthetic-subject',access_token:'SYNTHETIC_TOKEN_MARKER',refresh_token:'SYNTHETIC_REFRESH_MARKER',expired:'2099-01-01T00:00:00Z',login_method:'2fa'}));
  const config={'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},management:{'allow-remote':true,'disable-control-panel':true,'disable-auto-update-panel':true},access:{'api-keys':[api]},requests:{'proxy-url':''},routing:{retry:{'request-retry':0,'max-retry-interval':0}},oauth:{'auth-dir':authTarget,providers:{bps:{models:['gpt-6-luna']}}},'bps-protocol':{'state-dir':stateTarget}};
  fs.writeFileSync(path.join(runtime,'config.yaml'),JSON.stringify(config));
  const openssl=process.platform==='win32'?'C:/Program Files/Git/usr/bin/openssl.exe':'openssl';
  exec(openssl,['req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=bps.openai.com','-addext','subjectAltName=DNS:bps.openai.com','-keyout',path.join(runtime,'server.key'),'-out',path.join(runtime,'server.cert')]);
  const labels={'mx.bpsdurability':label};
  const shared={read_only:true,logging:{driver:'none'},tmpfs:['/tmp:mode=1777,size=32m'],labels};
  const composeDoc={services:{upstream:{...shared,image:nodeImage,user:'0:0',entrypoint:['node','/checks/upstream.mjs'],volumes:[{type:'bind',source:directory,target:'/checks',read_only:true},{type:'bind',source:runtime,target:'/fixture',read_only:true},{type:'volume',source:'bps',target:'/observe',read_only:true}],networks:{isolated:{aliases:['bps.openai.com']}}},cpa:{...shared,image:cpaImage,entrypoint:['/CLIProxyAPI/CLIProxyAPI','--config','/fixture/config.yaml','--local-model'],environment:{SSL_CERT_FILE:'/fixture/server.cert',HTTP_PROXY:'',HTTPS_PROXY:'',http_proxy:'',https_proxy:'',NO_PROXY:'*',no_proxy:'*'},volumes:[{type:'bind',source:runtime,target:'/fixture',read_only:true},{type:'bind',source:path.join(runtime,'auths'),target:authTarget},{type:'volume',source:'bps',target:stateTarget}],networks:['isolated']}},volumes:{bps:{labels},empty:{labels}},networks:{isolated:{internal:true,labels}}};
  fs.writeFileSync(composeFile,JSON.stringify(composeDoc));
  stage='starting';started=true;compose(['up','--detach','--pull','never']);await ready();
  const old=inspect(container('cpa')),mount=stateMount(old);
  must(Boolean(mount),'state_mount_missing');guard(old,mount);
  const network=JSON.parse(docker(['network','inspect',Object.keys(old.NetworkSettings.Networks)[0]]).stdout)[0];must(network.Internal===true,'external_network');
  must(!Object.keys(old.HostConfig.PortBindings||{}).length&&old.HostConfig.ReadonlyRootfs&&old.HostConfig.LogConfig.Type==='none','container_boundary');
  stage='first_turn';
  const firstPayload={model:'gpt-6-luna',input:[{role:'user',content:'SYNTHETIC_CHAT_MARKER call ping'}],tools:[{type:'function',name:'ping',parameters:{type:'object',properties:{},required:[],additionalProperties:false},strict:true}],stream:false};
  const first=client('upstream','http://cpa:8317/v1/responses','POST',firstPayload);
  must(first.status===200,'first_turn_failed');
  const call=first.body?.output?.find(item=>item.type==='function_call');must(call?.name==='ping'&&call.call_id.startsWith('call_bps_'),'public_binding_missing');
  const before=records();must(before.length===1&&before[0].record.call_id===call.call_id,'binding_not_persisted');
  const keys=['account_id','auth_id','call_id','caller','expires','native_id','subject'].sort();
  must(Object.keys(before[0].record).sort().join(',')===keys.join(','),'binding_fields_changed');
  must(before[0].mode===384&&!JSON.stringify(before).includes('SYNTHETIC_CHAT_MARKER')&&!JSON.stringify(before).includes('SYNTHETIC_TOKEN_MARKER')&&!JSON.stringify(before).includes('SYNTHETIC_REFRESH_MARKER'),'binding_content_or_mode');
  report({stage:'persisted',synthetic:true,public_http:200,record_count:1,minimal_fields:true,file_mode:'600',auth_scan_separate:true,internal_network:true,public_ports:false});
  stage='recreate';compose(['up','--detach','--pull','never','--force-recreate','--no-deps','cpa']);await ready();
  const current=inspect(container('cpa'));must(current.Id!==old.Id,'container_not_recreated');guard(current,mount);
  const after=records();must(JSON.stringify(before)===JSON.stringify(after),'binding_content_drift');
  const follow={...firstPayload,tool_choice:'none',input:[...firstPayload.input,...first.body.output,{type:'function_call_output',call_id:call.call_id,output:'pong'}]};
  const second=client('upstream','http://cpa:8317/v1/responses','POST',follow);
  must(second.status===200&&JSON.stringify(second.body).includes('pong'),'recreated_continuation_failed');
  const status=client('upstream','http://127.0.0.1:8080').body;must(status.calls===2&&status.nativeRestored&&status.sameIdentity,'native_identity_not_restored');
  report({stage:'recreated',container_id_changed:true,volume_source_same:true,volume_target_same:true,volume_rw_same:true,record_preserved:true,continuation_http:200,native_call_restored:true,same_synthetic_identity:true,synthetic_upstream_calls:2});
  stage='wrong_state_dir';
  fs.writeFileSync(path.join(runtime,'bad-config.yaml'),JSON.stringify({...config,'bps-protocol':{'state-dir':authTarget+'/bps-state'}}));
  const bad=docker(['run','--rm','--network','none','--read-only','--log-driver','none','--label','mx.bpsdurability='+label,'--mount','type=bind,source='+runtime+',target=/fixture,readonly',cpaImage,'./CLIProxyAPI','--config','/fixture/bad-config.yaml','--local-model'],undefined,true);
  must(bad.status!==null&&(bad.stdout+bad.stderr).includes('BPS状态目录必须位于凭证扫描目录之外'),'bad_state_dir_not_rejected');
  report({stage:'wrong_state_dir',startup_rejected:true,configuration_error_observed:true,process_exit:bad.status,synthetic_upstream_calls:2});
  stage='wrong_mount';
  fs.writeFileSync(wrongFile,JSON.stringify({services:{cpa:{volumes:[{type:'volume',source:'empty',target:stateTarget}]}}}));
  compose(['up','--detach','--pull','never','--force-recreate','--no-deps','cpa'],wrongFile);
  const wrong=inspect(container('cpa'));let rejected=false;try{guard(wrong,mount);}catch(error){rejected=error.message==='state_mount_drift';}
  must(rejected,'wrong_mount_not_rejected');
  const finalStatus=client('upstream','http://127.0.0.1:8080').body;must(finalStatus.calls===2,'wrong_mount_called_upstream');
  must(JSON.stringify(records())===JSON.stringify(before),'old_volume_hidden_or_changed');
  report({stage:'wrong_mount',pre_request_guard_rejected:true,old_volume_preserved:true,no_additional_upstream_calls:true});
}catch(error){exit=1;report({stage,status:'failed',code:error.message});}
finally{
  stage='cleanup';
  let clean=true;
  if(started){const result=docker(['compose','--project-name',label,'--file',composeFile,...(fs.existsSync(wrongFile)?['--file',wrongFile]:[]),'down','--volumes','--remove-orphans'],undefined,true);clean=result.status===0;}
  const containers=docker(['ps','--all','--filter','label=mx.bpsdurability='+label,'--format','{{.ID}}'],undefined,true);
  const volumes=docker(['volume','ls','--filter','label=mx.bpsdurability='+label,'--format','{{.Name}}'],undefined,true);
  const networks=docker(['network','ls','--filter','label=mx.bpsdurability='+label,'--format','{{.ID}}'],undefined,true);
  clean&&=containers.status===0&&!containers.stdout.trim()&&volumes.status===0&&!volumes.stdout.trim()&&networks.status===0&&!networks.stdout.trim();
  const resolved=path.resolve(runtime),base=path.resolve(os.tmpdir());
  if(path.dirname(resolved)===base&&path.basename(resolved).startsWith(label+'-'))fs.rmSync(resolved,{recursive:true,force:true});else clean=false;
  report({stage,verified:clean,own_project_only:true,temporary_input_removed:!fs.existsSync(runtime)});if(!clean)exit=1;
}
process.exitCode=exit;
