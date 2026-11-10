import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';

const base=['--import',new URL('./docker-fixture.mjs',import.meta.url).href,fileURLToPath(new URL('./run.mjs',import.meta.url)),'--cpa-image','fixture-cpa','--manager-image','fixture-manager','--worker-image','fixture-worker','--source-revision','2'.repeat(40),'--cpa-revision','1'.repeat(40),'--worker-revision','3'.repeat(40)];
for(const mode of ['run_failed','check_failed']){
  const args=[...base,...(mode==='check_failed'?['--execute']:[])];
  const result=spawnSync(process.execPath,args,{encoding:'utf8',windowsHide:true,timeout:10000,env:{...process.env,MX_FIXTURE_MODE:mode},input:mode==='check_failed'?JSON.stringify({email:'fixture@example.invalid',password:'SECRET_PASSWORD_MARKER',totp_secret:'ABCDEFGHIJKLMNOP'}):undefined});
  assert.equal(result.status,1);assert.equal(result.stderr,'');
  const reports=result.stdout.trim().split('\n').map(line=>JSON.parse(line));
  assert.ok(reports.some(item=>item.stage==='cleanup'&&item.verified===true&&item.token_state_retained===false));
  assert.equal(reports.find(item=>item.stage==='inputs').client_request_budget,mode==='check_failed'?2:0);
  if(mode==='run_failed')assert.ok(reports.some(item=>item.stage==='cpa_start'&&item.error_code==='docker_command_failed'));
  else assert.deepEqual(reports.filter(item=>item.stage==='business').map(item=>item.feature),['tools','image']);
  assert.equal(result.stdout.includes('SECRET_'),false);assert.equal(result.stdout.includes('fixture@example.invalid'),false);
}
for(const invalid of [false,true]){
  const args=[...base];args[args.indexOf('--cpa-revision')+1]=invalid?'short':'4'.repeat(40);
  const result=spawnSync(process.execPath,args,{encoding:'utf8',windowsHide:true,timeout:10000,env:{...process.env,MX_FIXTURE_MODE:'revision_failed'}});
  assert.equal(result.status,1);assert.equal(result.stderr,'');
  const reports=result.stdout.trim().split('\n').map(line=>JSON.parse(line));
  assert.ok(reports.some(item=>item.error_code===(invalid?'invalid_options':'image_revision_mismatch')));
  assert.ok(reports.some(item=>item.stage==='cleanup'&&item.verified===true));
}
process.stdout.write('tool_image_harness_cleanup_revision_redaction=passed\n');
