import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';
const runner=fileURLToPath(new URL('./fixture-runner.mjs',import.meta.url));
const results=[];
for(const email of ['first-fixture@example.invalid','fail-fixture@example.invalid','last-fixture@example.invalid']){
  const child=spawnSync(process.execPath,[runner],{input:JSON.stringify({profile:'codex',email,password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'}),stdio:['pipe','pipe','pipe','pipe'],windowsHide:true,timeout:5000,encoding:'utf8'});
  assert.equal(child.status,0);assert.equal(child.stdout,'');assert.equal(child.stderr,'');
  results.push(JSON.parse(child.output[3]));
}
assert.deepEqual(results.map(item=>item.status),['succeeded','failed','succeeded']);
assert.equal(results[1].error_code,'password_rejected');
assert.notEqual(results[0].result.account_id,results[2].result.account_id);
assert.equal(results[0].result.client_id,'app_EMoamEEZ73f0CkXaXp7hrann');
assert.equal(results[0].result.profile,'codex');
process.stdout.write('synthetic_fixture_contract=passed\n');
