import assert from 'node:assert/strict';
import {publicErrorDiagnostics,sanitizeReceipt} from './diagnostics.mjs';

for(const diagnostic of [
  {stage:'upstream_http',code:'http_rejected',upstream_http_status:502,inband_status:null},
  {stage:'upstream_inband',code:'upstream_failed',upstream_http_status:200,inband_status:403},
  {stage:'tool_validation',code:'tool_validation_failed',upstream_http_status:200,inband_status:null},
  {stage:'binding_identity',code:'identity_incomplete_or_changed',upstream_http_status:null,inband_status:null},
  {stage:'binding_storage',code:'storage_unavailable',upstream_http_status:null,inband_status:null}
]) {
  const result=sanitizeReceipt({stage:'business',feature:'tools',client_requests:1,...publicErrorDiagnostics({error:{code:'internal_server_error',type:'server_error',message:'SECRET_MESSAGE_MARKER',diagnostics:{...diagnostic,subject:'SECRET_SUBJECT_MARKER',access_token:'SECRET_ACCESS_MARKER'}},body:'SECRET_BODY_MARKER'})});
  assert.equal(result.bps_diagnostic_stage,diagnostic.stage);assert.equal(result.bps_diagnostic_code,diagnostic.code);
  assert.equal(result.bps_upstream_http_status,diagnostic.upstream_http_status);assert.equal(result.bps_inband_status,diagnostic.inband_status);
  assert.deepEqual(sanitizeReceipt(result),result);
  assert.equal(JSON.stringify(result).includes('SECRET_'),false);
}
const unknown=sanitizeReceipt(publicErrorDiagnostics({error:{diagnostics:{stage:'SECRET_STAGE_MARKER',code:'SECRET_CODE_MARKER',upstream_http_status:'403',inband_status:399,body:'SECRET_BODY_MARKER'}}}));
assert.equal(unknown.bps_diagnostic_stage,'unknown');assert.equal(unknown.bps_diagnostic_code,'unknown');
assert.equal(unknown.bps_upstream_http_status,null);assert.equal(unknown.bps_inband_status,null);
assert.equal(JSON.stringify(unknown).includes('SECRET_'),false);
process.stdout.write('tool_diagnostic_double_whitelist_contract=passed\n');
