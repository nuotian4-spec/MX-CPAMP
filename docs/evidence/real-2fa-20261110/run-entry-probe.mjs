import {spawnSync} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';

const directory=path.dirname(fileURLToPath(import.meta.url));
const name='mx-entry-probe-'+crypto.randomBytes(6).toString('hex');
const records=[];
function docker(args,timeout=90000){return spawnSync('docker',args,{encoding:'utf8',timeout,windowsHide:true,maxBuffer:1024*1024});}
try{
  const result=docker(['run','--rm','--init','--name',name,'--read-only','--log-driver','none','--tmpfs','/tmp:mode=1777,size=32m','--mount','type=bind,source='+directory+',target=/checks,readonly','--entrypoint','node','mx-oauth-login:20261110-rc1','/checks/entry-probe.mjs']);
  if(result.status!==0)throw new Error('probe_execution_failed');
  for(const line of result.stdout.trim().split('\n').filter(Boolean)){
    const item=JSON.parse(line);
    if(!['chatgpt_entry','auth_entry'].includes(item.target)||!(item.http_status===null||Number.isInteger(item.http_status))||typeof item.security_check_page!=='boolean'||![null,'transport_error'].includes(item.error_code))throw new Error('probe_output_invalid');
    records.push({target:item.target,http_status:item.http_status,security_check_page:item.security_check_page,error_code:item.error_code});
  }
  if(records.length!==2||new Set(records.map(item=>item.target)).size!==2)throw new Error('probe_output_invalid');
}catch{
  records.push({stage:'probe',error_code:'probe_failed'});process.exitCode=1;
}finally{
  docker(['rm','--force',name],10000);
  const remaining=docker(['ps','--all','--filter','name=^/'+name+'$','--format','{{.ID}}'],10000);
  const verified=remaining.status===0&&!remaining.stdout.trim();
  records.push({stage:'cleanup',verified});
  if(!verified)process.exitCode=1;
  fs.writeFileSync(path.join(directory,'entry-probe-receipt.jsonl'),records.map(value=>JSON.stringify(value)).join('\n')+'\n');
  for(const record of records)process.stdout.write(JSON.stringify(record)+'\n');
}
