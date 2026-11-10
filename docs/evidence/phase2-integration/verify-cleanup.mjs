import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';

const preload=new URL('./cleanup-docker-fixture.mjs',import.meta.url).href;
for(const relative of ['./run.mjs','../phase2-business-20261111/run.mjs']){
  const result=spawnSync(process.execPath,['--import',preload,fileURLToPath(new URL(relative,import.meta.url)),'--source-revision','9'.repeat(40)],{encoding:'utf8',windowsHide:true,timeout:10000});
  assert.equal(result.status,1);assert.equal(result.stderr,'');
  const reports=result.stdout.trim().split('\n').map(line=>JSON.parse(line));
  assert.ok(reports.some(item=>item.stage==='cpa_start'&&item.error_code==='docker_command_failed'));
  assert.ok(reports.some(item=>item.stage==='cleanup'&&item.verified===true&&item.token_state_retained===false));
}
process.stdout.write('daemon_created_run_failed_cleanup=passed\n');
