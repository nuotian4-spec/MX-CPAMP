import childProcess from 'node:child_process';
import {syncBuiltinESMExports} from 'node:module';
import {EventEmitter} from 'node:events';
import {PassThrough,Writable} from 'node:stream';
import fs from 'node:fs';
import assert from 'node:assert/strict';

// 完全替换 Docker，验证ID绑定、零凭证预检和失败后的清理。
const containers=new Map(),mode=process.env.MX_FIXTURE_MODE||'run_failed';
const roles=['cpa','manager','worker'];
const ids=Object.fromEntries(roles.map((role,index)=>[role,'sha256:'+String(index+1).repeat(64)]));
const revisions=Object.fromEntries(roles.map((role,index)=>[role,String(index+1).repeat(40)]));
let runs=0,checks=0,removed=0;
const readFile=fs.readFileSync;
fs.readFileSync=(file,...args)=>{
  const value=readFile(file,...args),name=String(file).replaceAll('\\','/');
  const changed=mode==='input_changed'&&name.endsWith('/bps-tool-diagnostic-20261111/payloads.mjs')||mode==='legacy_changed'&&name.endsWith('/phase2-tool-image-20261111/diagnostics.mjs');
  return checks&&changed?Buffer.concat([value,Buffer.from('\n')]):value;
};
childProcess.spawn=(command,args)=>{
  assert.equal(command,'docker');
  const child=new EventEmitter();child.stdout=new PassThrough();child.stderr=new PassThrough();
  child.stdin=new Writable({write(chunk,encoding,callback){callback();}});child.kill=()=>true;
  queueMicrotask(()=>{
    let code=0,output='';
    if(args[0]==='image'){
      const role=args[2].replace('fixture-','');
      output=JSON.stringify({Id:ids[role],Config:{Labels:{'org.opencontainers.image.revision':revisions[role]}}});
    }else if(args[0]==='run'){
      const name=args[args.indexOf('--name')+1],role=roles.find(value=>name.endsWith('-'+value));
      assert.equal(args.includes(ids[role]),true);assert.equal(args.includes('fixture-'+role),false);
      assert.equal(args.includes('--log-driver'),true);assert.equal(args[args.indexOf('--log-driver')+1],'none');
      containers.set(name,ids[role]);runs++;if(mode==='run_failed')code=1;
    }else if(args[0]==='inspect')output=JSON.stringify({Image:mode==='boundary_changed'?'sha256:'+'9'.repeat(64):containers.get(args[1]),HostConfig:{ReadonlyRootfs:true,LogConfig:{Type:'none'},PortBindings:{},Tmpfs:{'/tmp':''}}});
    else if(args[0]==='exec'){
      if(args.includes('/checks/check.mjs')){
        checks++;const success=mode==='preflight'||mode==='check_success';code=success?0:1;
        output=mode==='preflight'?JSON.stringify({stage:'ready',services_healthy:true,admin_gate:true}):JSON.stringify({stage:'business',feature:'tools',probe_outcome:'rejected',public_http_status:502,message:'SECRET_MESSAGE_MARKER',access_token:'SECRET_ACCESS_MARKER'})+'\n'+JSON.stringify({stage:'complete',client_requests:1,tool_client_requests:1,all_requests_succeeded:false,text:'SECRET_OUTPUT_MARKER'});
      }
    }else if(args[0]==='rm'){
      if(mode==='cleanup_failed')code=1;else if(containers.delete(args.at(-1)))removed++;
    }else if(args[0]==='ps')output=[...containers.keys()].join('\n');
    else throw new Error('unexpected_fixture_docker_call');
    child.stdout.end(output);child.stderr.end();child.emit('close',code);
  });
  return child;
};
syncBuiltinESMExports();
process.on('exit',()=>{
  const wanted=['revision_failed','identity_failed'].includes(mode)?0:mode==='run_failed'?1:3;
  const wantedChecks=['check_failed','check_success','preflight','input_changed','legacy_changed','cleanup_failed'].includes(mode)?1:0;
  if(runs!==wanted||removed!==(mode==='cleanup_failed'?0:wanted)||containers.size!==(mode==='cleanup_failed'?3:0)||checks!==wantedChecks)process.exitCode=23;
});
