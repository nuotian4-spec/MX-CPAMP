import assert from 'node:assert/strict';
import {publicErrorDiagnostics,sanitizeReceipt} from './diagnostics.mjs';

for(const [error,category,status]of [
  [{code:'executor_not_found',message:'executor not registered'},'local_configuration',null],
  [{code:'request_scoped',message:'BPS does not support max_output_tokens'},'model_capability',null],
  [{code:'credential_concurrency_timeout',message:'credential concurrency timeout'},'admission',null],
  [{message:'BPS upstream returned HTTP 503'},'upstream_http',503],
  [{message:'auth_unavailable: BPS upstream returned HTTP 403'},'upstream_http',403],
  [{message:'Post "https://fixture.invalid/SECRET_URL_MARKER": net/http: TLS handshake timeout'},'transport',null],
  [{code:'upstream_authentication_required'},'authentication',null],
  [{code:'internal_server_error',type:'server_error',message:'SECRET_MESSAGE_MARKER status=401'},'unknown',null],
  [{code:'SECRET_CODE_MARKER',type:'SECRET_TYPE_MARKER',message:'SECRET_MESSAGE_MARKER',upstream_status:403},'unknown',null],
  [{message:'BPS upstream returned HTTP 503 SECRET_MESSAGE_MARKER'},'unknown',null]
]){
  const result=sanitizeReceipt({stage:'business',...publicErrorDiagnostics({error,body:'SECRET_BODY_MARKER'}),message:'SECRET_MESSAGE_MARKER',url:'SECRET_URL_MARKER'});
  assert.equal(result.public_error_category,category);assert.equal(result.public_upstream_http_status,status);
  assert.equal(JSON.stringify(result).includes('SECRET_'),false);
  if(error.code?.startsWith('SECRET_'))assert.equal(result.public_error_code,'unknown');
  if(error.type?.startsWith('SECRET_'))assert.equal(result.public_error_type,'unknown');
}
const forged=sanitizeReceipt({stage:'business',public_error_code:'SECRET_MARKER',public_error_type:'SECRET_MARKER',public_error_category:'SECRET_MARKER',public_upstream_http_status:503});
assert.equal(forged.public_error_code,'unknown');assert.equal(forged.public_error_type,'unknown');assert.equal(forged.public_error_category,'unknown');assert.equal(forged.public_upstream_http_status,null);
process.stdout.write('public_failure_diagnostics_redaction=passed\n');
