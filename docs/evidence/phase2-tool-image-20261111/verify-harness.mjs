import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';

const base=['--import',new URL('./docker-fixture.mjs',import.meta.url).href,fileURLToPath(new URL('./run.mjs',import.meta.url)),'--cpa-image','fixture-cpa','--manager-image','fixture-manager','--worker-image','fixture-worker','--source-revision','2'.repeat(40),'--cpa-revision','1'.repeat(40),'--worker-revision','3'.repeat(40)];
for(const mode of ['run_failed','check_failed','preflight','input_changed']){
  const execute=['check_failed','input_changed'].includes(mode);
  const args=[...base,...(execute?['--execute']:[])];
  const result=spawnSync(process.execPath,args,{encoding:'utf8',windowsHide:true,timeout:10000,env:{...process.env,MX_FIXTURE_MODE:mode},input:execute?JSON.stringify({email:'fixture@example.invalid',password:'SECRET_PASSWORD_MARKER',totp_secret:'ABCDEFGHIJKLMNOP'}):undefined});
  assert.equal(result.status,mode==='preflight'?0:1);assert.equal(result.stderr,'');
  const reports=result.stdout.trim().split('\n').map(line=>JSON.parse(line));
  assert.ok(reports.some(item=>item.stage==='cleanup'&&item.verified===true&&item.token_state_retained===false));
  assert.equal(reports.find(item=>item.stage==='inputs').client_request_budget,execute?4:0);
  if(mode==='run_failed')assert.ok(reports.some(item=>item.stage==='cpa_start'&&item.error_code==='docker_command_failed'));
  else if(execute)assert.deepEqual(reports.filter(item=>item.stage==='business').map(item=>item.feature),['text']);
  else assert.equal(reports.some(item=>item.stage==='business'),false);
  assert.ok(reports.some(item=>item.stage==='input_recheck'&&item.unchanged===(mode!=='input_changed')&&item.harness_unchanged===(mode!=='input_changed')));
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
const preflight=spawnSync(process.execPath,['--import',new URL('./preflight-fixture.mjs',import.meta.url).href,fileURLToPath(new URL('./check.mjs',import.meta.url))],{encoding:'utf8',windowsHide:true,timeout:10000,input:JSON.stringify({mode:'preflight',cpa:'synthetic-cpa',admin:'synthetic-admin',api:'synthetic-api',worker:'synthetic-worker'})});
assert.equal(preflight.status,0);assert.equal(preflight.stderr,'');
assert.deepEqual(preflight.stdout.trim().split('\n').map(line=>JSON.parse(line).stage),['ready']);
process.stdout.write('tool_image_harness_cleanup_revision_redaction=passed\n');
