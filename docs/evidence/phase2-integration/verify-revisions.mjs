import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';

const preload=new URL('./cleanup-docker-fixture.mjs',import.meta.url).href;
const revisions={'fixture-cpa':'1'.repeat(40),'fixture-manager':'2'.repeat(40),'fixture-worker':'3'.repeat(40)};
for(const relative of ['./run.mjs','../phase2-business-20261111/run.mjs']){
  const args=['--import',preload,fileURLToPath(new URL(relative,import.meta.url)),'--cpa-image','fixture-cpa','--manager-image','fixture-manager','--worker-image','fixture-worker','--source-revision',revisions['fixture-manager'],'--cpa-revision',revisions['fixture-cpa'],'--worker-revision',revisions['fixture-worker']];
  for(const scenario of ['matched','mismatch','invalid']){
    const current=[...args];
    if(scenario==='mismatch')current[current.indexOf('--cpa-revision')+1]='4'.repeat(40);
    if(scenario==='invalid')current[current.indexOf('--worker-revision')+1]='short';
    const result=spawnSync(process.execPath,current,{encoding:'utf8',windowsHide:true,timeout:10000,env:{...process.env,MX_FIXTURE_IMAGE_REVISIONS:JSON.stringify(revisions),MX_FIXTURE_EXPECT_RUN:scenario==='matched'?'yes':'no'}});
    assert.equal(result.status,1);assert.equal(result.stderr,'');
    const reports=result.stdout.trim().split('\n').map(line=>JSON.parse(line));
    const code=scenario==='matched'?'docker_command_failed':scenario==='mismatch'?'image_revision_mismatch':'invalid_options';
    assert.ok(reports.some(item=>item.error_code===code));
    if(scenario==='matched')for(const role of ['cpa','manager','worker']){
      const identity=reports.find(item=>item.stage==='image'&&item.role===role);
      assert.equal(identity.image_revision,revisions['fixture-'+role]);assert.equal(identity.expected_revision,revisions['fixture-'+role]);
    }
    assert.ok(reports.some(item=>item.stage==='cleanup'&&item.verified===true));
  }
}
process.stdout.write('per_role_revision_contract=passed\n');
