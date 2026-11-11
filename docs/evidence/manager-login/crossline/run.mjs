import {spawnSync} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import crypto from 'node:crypto';
const directory=path.dirname(fileURLToPath(import.meta.url));
const cpaImage=process.argv[2]||'mx-cpa:integration',managerImage=process.argv[3]||'mx-cpamp:integration',workerImage=process.argv[4]||'mx-oauth-login:candidate';
const prefix='mx-crossline-'+crypto.randomBytes(5).toString('hex'),volumes=[prefix+'-cpa',prefix+'-manager',prefix+'-worker'],containers=[prefix+'-cpa',prefix+'-worker',prefix+'-manager'];
function docker(args,{quiet=false,allowFailure=false}={}){const result=spawnSync('docker',args,{encoding:'utf8',timeout:60000,windowsHide:true,maxBuffer:8*1024*1024});if(!quiet){if(result.stdout)process.stdout.write(result.stdout);if(result.status!==0&&result.stderr)process.stderr.write(result.stderr);}if(result.status!==0&&!allowFailure)throw new Error('docker_'+args[0]+'_exit_'+result.status);return result.stdout?.trim()||'';}
const bind=['--mount','type=bind,source='+directory+',target=/checks,readonly'];
try{
 for(const image of [cpaImage,managerImage,workerImage]){process.stdout.write(image+' ');docker(['image','inspect',image,'--format','{{.Id}}']);}
 for(const volume of volumes)docker(['volume','create',volume],{quiet:true});
 docker(['run','--rm','--network','none','--user','0',...bind,...volumes.flatMap((volume,i)=>['--mount',`type=volume,source=${volume},target=${['/cpa-state','/manager-config','/worker-secret'][i]}`]),'--entrypoint','node',workerImage,'/checks/prepare.mjs']);
 docker(['run','-d','--name',containers[0],'--network','none','--mount',`type=volume,source=${volumes[0]},target=/state`,'--entrypoint','/CLIProxyAPI/CLIProxyAPI',cpaImage,'--config','/state/config.yaml','--local-model'],{quiet:true});
 docker(['run','-d','--init','--read-only','--tmpfs','/tmp:mode=1777,size=64m','--name',containers[1],'--network','container:'+containers[0],...bind,'--mount',`type=volume,source=${volumes[2]},target=/run/secrets,readonly`,'--entrypoint','node',workerImage,'/checks/worker.mjs'],{quiet:true});
 docker(['run','-d','--name',containers[2],'--network','container:'+containers[0],'--read-only','--tmpfs','/data:mode=1777,size=128m','--mount',`type=volume,source=${volumes[1]},target=/run/config,readonly`,'--mount',`type=volume,source=${volumes[2]},target=/run/secrets,readonly`,'-e','CPA_MANAGER_CONFIG=/run/config/config.json','-e','OAUTH_LOGIN_URL=http://127.0.0.1:18417',managerImage],{quiet:true});
 docker(['run','--rm','--user','0','--network','container:'+containers[0],...bind,'--mount',`type=volume,source=${volumes[0]},target=/cpa-state,readonly`,'--mount',`type=volume,source=${volumes[1]},target=/run/config,readonly`,'--entrypoint','node',workerImage,'/checks/check.mjs']);
 console.log('crossline_exit=0');
}catch(error){console.error(String(error.message||'crossline_failed'));process.exitCode=1;}
finally{
 for(const container of [...containers].reverse())docker(['rm','-f',container],{quiet:true,allowFailure:true});
 for(const volume of [...volumes].reverse())docker(['volume','rm',volume],{quiet:true,allowFailure:true});
 const remainingContainers=docker(['container','ls','-a','--filter','name='+prefix,'--format','{{.Names}}'],{quiet:true});
 const remainingVolumes=docker(['volume','ls','--filter','name='+prefix,'--format','{{.Name}}'],{quiet:true});
 if(remainingContainers||remainingVolumes){console.error('isolated_resources_cleanup=incomplete');process.exitCode=1;}else console.log('isolated_resources_cleanup=verified');
}
