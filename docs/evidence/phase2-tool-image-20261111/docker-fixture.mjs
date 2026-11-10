import childProcess from 'node:child_process';
import {syncBuiltinESMExports} from 'node:module';
import {EventEmitter} from 'node:events';
import {PassThrough,Writable} from 'node:stream';

// 完全替换 Docker CLI；离线覆盖创建回执失败与业务失败后的 finally 清理。
const containers=new Set(),mode=process.env.MX_FIXTURE_MODE||'run_failed';
const revisions={'fixture-cpa':'1'.repeat(40),'fixture-manager':'2'.repeat(40),'fixture-worker':'3'.repeat(40)};
let runs=0,checks=0,removed=0;
childProcess.spawn=(command,args)=>{
  if(command!=='docker')throw new Error('unexpected_fixture_command');
  const child=new EventEmitter();child.stdout=new PassThrough();child.stderr=new PassThrough();
  child.stdin=new Writable({write(chunk,encoding,callback){callback();}});child.kill=()=>true;
  queueMicrotask(()=>{
    let code=0,output='';
    if(args[0]==='image')output=JSON.stringify({Id:'sha256:'+'1'.repeat(64),Config:{Labels:{'org.opencontainers.image.revision':revisions[args[2]]}}});
    else if(args[0]==='run'){containers.add(args[args.indexOf('--name')+1]);runs++;if(mode==='run_failed')code=1;}
    else if(args[0]==='inspect')output=JSON.stringify({HostConfig:{ReadonlyRootfs:true,LogConfig:{Type:'none'},PortBindings:{},Tmpfs:{'/tmp':''}}});
    else if(args[0]==='exec'){
      if(args.includes('/checks/check.mjs')){
        checks++;code=1;
        output=JSON.stringify({stage:'business',feature:'tools',probe_outcome:'rejected',public_http_status:400,message:'SECRET_MESSAGE_MARKER',url:'SECRET_URL_MARKER',access_token:'SECRET_ACCESS_MARKER'})+'\n'+JSON.stringify({stage:'business',feature:'image',probe_outcome:'rejected',public_http_status:400,text:'SECRET_OUTPUT_MARKER'});
      }
    }else if(args[0]==='rm'){if(containers.delete(args.at(-1)))removed++;}
    else if(args[0]==='ps')output=[...containers].join('\n');
    else throw new Error('unexpected_fixture_docker_call');
    child.stdout.end(output);child.stderr.end();child.emit('close',code);
  });
  return child;
};
syncBuiltinESMExports();
process.on('exit',()=>{
  const wanted=mode==='revision_failed'?0:mode==='run_failed'?1:3;
  if(runs!==wanted||removed!==wanted||containers.size||checks!==(mode==='check_failed'?1:0))process.exitCode=23;
});
