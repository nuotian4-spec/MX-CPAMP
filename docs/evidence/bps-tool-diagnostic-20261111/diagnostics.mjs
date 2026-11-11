import {sanitizeReceipt as baseSanitize} from '../phase2-tool-image-20261111/diagnostics.mjs';
export {errorCodes,taskDiagnostics,taskStatuses,businessCodes,publicErrorDiagnostics} from '../phase2-tool-image-20261111/diagnostics.mjs';

export function sanitizeReceipt(value={}) {
  const out=baseSanitize(value);
  for(const field of ['client_requests','tool_client_requests']) {
    if(!Number.isInteger(value[field])||value[field]<0||value[field]>2)delete out[field];
  }
  if(!['tools','tool_result'].includes(value.feature))delete out.feature;
  for(const field of ['image_client_requests','image_red_confirmed','text_gate_passed','refresh_endpoint_requests','refresh_token_present','access_token_changed','refresh_token_changed','concurrency_preserved'])delete out[field];
  if(value.stage==='refresh')out.stage='harness';
  if(Number.isInteger(value.login_batch_requests)&&value.login_batch_requests>=0&&value.login_batch_requests<=1)out.login_batch_requests=value.login_batch_requests;
  return out;
}
