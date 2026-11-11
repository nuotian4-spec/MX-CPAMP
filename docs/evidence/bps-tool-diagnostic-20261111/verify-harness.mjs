import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';

const base=['--import',new URL('./docker-fixture.mjs',import.meta.url).href,fileURLToPath(new URL('./run.mjs',import.meta.url)),...['cpa','manager','worker'].flatMap((role,index)=>['--'+role+'-image','fixture-'+role,'--'+role+'-image-id','sha256:'+String(index+1).repeat(64),'--'+role+'-revision',String(index+1).repeat(40)])];
for(const mode of ['run_failed','check_failed','check_success','preflight','input_changed','legacy_changed','boundary_changed','cleanup_failed']){
  const execute=['check_failed','check_success','input_changed','legacy_changed','cleanup_failed'].includes(mode);
  const result=spawnSync(process.execPath,[...base,...(execute?['--execute']:[])],{encoding:'utf8',windowsHide:true,timeout:10000,env:{...process.env,MX_FIXTURE_MODE:mode},input:execute?JSON.stringify({email:'fixture@example.invalid',password:'SECRET_PASSWORD_MARKER',totp_secret:'ABCDEFGHIJKLMNOP'}):undefined});
  assert.equal(result.status,['preflight','check_success'].includes(mode)?0:1,mode+': '+result.stdout+result.stderr);assert.equal(result.stderr,'');
  const reports=result.stdout.trim().split('\n').map(line=>JSON.parse(line));
  assert.ok(reports.some(item=>item.stage==='cleanup'&&item.verified===(mode!=='cleanup_failed')&&item.token_state_retained===(mode==='cleanup_failed'?null:false)));
  const inputs=reports.find(item=>item.stage==='inputs');assert.equal(inputs.client_request_budget,execute?2:0);assert.equal(inputs.login_batch_budget,execute?1:0);assert.equal(inputs.refresh_endpoint_budget,0);
  if(mode==='run_failed')assert.ok(reports.some(item=>item.stage==='cpa_start'&&item.error_code==='docker_command_failed'));
  else if(mode==='boundary_changed')assert.ok(reports.some(item=>item.stage==='boundaries'&&item.error_code==='container_boundary_invalid'));
  else if(execute)assert.deepEqual(reports.filter(item=>item.stage==='business').map(item=>item.feature),['tools']);
  else assert.equal(reports.some(item=>item.stage==='business'),false);
  const recheck=reports.find(item=>item.stage==='input_recheck');
  assert.equal(recheck.unchanged,!['input_changed','legacy_changed'].includes(mode));assert.equal(recheck.legacy_diagnostics_unchanged,mode!=='legacy_changed');
  assert.equal(result.stdout.includes('SECRET_'),false);assert.equal(result.stdout.includes('fixture@example.invalid'),false);
}
for(const [field,mode,expected] of [['--cpa-revision','revision_failed','image_revision_mismatch'],['--cpa-image-id','identity_failed','image_identity_mismatch']]){
  for(const invalid of [false,true]){
    const args=[...base];args[args.indexOf(field)+1]=invalid?'short':field.endsWith('-id')?'sha256:'+'4'.repeat(64):'4'.repeat(40);
    const result=spawnSync(process.execPath,args,{encoding:'utf8',windowsHide:true,timeout:10000,env:{...process.env,MX_FIXTURE_MODE:mode}});
    assert.equal(result.status,1);assert.equal(result.stderr,'');
    const reports=result.stdout.trim().split('\n').map(line=>JSON.parse(line));
    assert.ok(reports.some(item=>item.error_code===(invalid?'invalid_options':expected)));
    assert.ok(reports.some(item=>item.stage==='cleanup'&&item.verified===true));
  }
}
const preflight=spawnSync(process.execPath,['--import',new URL('./preflight-fixture.mjs',import.meta.url).href,fileURLToPath(new URL('./check.mjs',import.meta.url))],{encoding:'utf8',windowsHide:true,timeout:10000,input:JSON.stringify({mode:'preflight',cpa:'synthetic-cpa',admin:'synthetic-admin',api:'synthetic-api',worker:'synthetic-worker'})});
assert.equal(preflight.status,0);assert.equal(preflight.stderr,'');
assert.deepEqual(preflight.stdout.trim().split('\n').map(line=>JSON.parse(line).stage),['ready']);
process.stdout.write('tool_diagnostic_harness_identity_cleanup_redaction=passed\n');
