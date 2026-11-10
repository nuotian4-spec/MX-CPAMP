import childProcess from 'node:child_process';
import {syncBuiltinESMExports} from 'node:module';
import {EventEmitter} from 'node:events';
import {PassThrough,Writable} from 'node:stream';

const containers=new Set();
let failedRun=false,removed=false;
childProcess.spawn=(command,args)=>{
  if(command!=='docker')throw new Error('unexpected_fixture_command');
  const child=new EventEmitter();
  child.stdout=new PassThrough();child.stderr=new PassThrough();
  child.stdin=new Writable({write(chunk,encoding,callback){callback();}});
  child.kill=()=>true;
  queueMicrotask(()=>{
    let code=0,output='';
    if(args[0]==='image'&&args[1]==='inspect'){
      output=JSON.stringify({Id:'sha256:'+'1'.repeat(64),Config:{Labels:{'org.opencontainers.image.revision':'9'.repeat(40)}}});
    }else if(args[0]==='run'){
      const name=args[args.indexOf('--name')+1];
      containers.add(name);failedRun=true;code=1;
    }else if(args[0]==='rm'){
      removed=containers.delete(args.at(-1));
    }else if(args[0]==='ps'){
      output=[...containers].join('\n');
    }else throw new Error('unexpected_fixture_docker_call');
    child.stdout.end(output);child.stderr.end();child.emit('close',code);
  });
  return child;
};
syncBuiltinESMExports();
process.on('exit',()=>{
  if(!failedRun||!removed||containers.size)process.exitCode=23;
});
