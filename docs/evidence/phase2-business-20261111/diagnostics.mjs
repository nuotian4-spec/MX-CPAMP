export const loginStages=new Set(['input','web_login','authorize','email','email_otp','password','totp','workspace_session','callback','token_exchange','credential_validation','cpa_import','unknown']);
export const loginReasons=new Set(['security_check','email_verification_required','phone_verification_required','account_setup_required','unexpected_login_page','session_selection_required','totp_factor_missing','interaction_required','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','unknown']);
export const errorCodes=new Set(['needs_user_action','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','process_failed','output_invalid','import_unconfirmed','worker_stop_unconfirmed','worker_cleanup_unconfirmed','worker_unavailable','login_failed','cpa_target_changed','import_busy']);
export const taskStatuses=new Set(['created','running','importing','cancelling','succeeded','failed','needs_user_action','cancelled','expired']);
export function taskDiagnostics(task={}){return{stage:loginStages.has(task.stage)?task.stage:null,reason:loginReasons.has(task.reason)?task.reason:null};}
export const businessCodes=new Set(['services_not_ready','proxy_unreachable','admin_gate_invalid','create_ack_unconfirmed','batch_create_rejected','batch_query_unconfirmed','batch_timeout_unconfirmed','batch_result_invalid','registration_unconfirmed','saved_file_unconfirmed','snapshot_invalid','concurrency_write_unconfirmed','concurrency_apply_unconfirmed','refresh_result_unconfirmed','refresh_rejected','refresh_identity_mismatch','refresh_limit_lost','catalog_mismatch','public_request_unconfirmed','public_request_rejected','public_terminal_invalid','public_output_empty','auth_target_unconfirmed','admission_not_drained','harness_error']);
export function sanitizeReceipt(value={}){
  const out={stage:['ready','batch','saved_registered','concurrency','refresh','business','complete','harness'].includes(value.stage)?value.stage:'harness',profile:'bps'};
  if(taskStatuses.has(value.status))out.status=value.status;
  for(const field of ['error_code','worker_error_code'])if(field in value)out[field]=errorCodes.has(value[field])||businessCodes.has(value[field])?value[field]:null;
  for(const field of ['login_stage','worker_stage'])if(field in value)out[field]=loginStages.has(value[field])?value[field]:null;
  for(const field of ['login_reason','worker_reason'])if(field in value)out[field]=loginReasons.has(value[field])?value[field]:null;
  for(const field of ['services_healthy','admin_gate','saved','registered','single_bps_credential','concurrency_override_saved','same_auth','same_profile','same_client','same_identity','refresh_token_present','access_token_changed','refresh_token_changed','concurrency_preserved','catalog_exact','normal_terminal','text_output_nonempty','auth_target_confirmed','admission_drained','production_network_equivalence'])if(typeof value[field]==='boolean')out[field]=value[field];
  for(const field of ['client_requests','refresh_endpoint_requests'])if(Number.isInteger(value[field])&&value[field]>=0&&value[field]<=1)out[field]=value[field];
  if(Number.isInteger(value.public_http_status)&&value.public_http_status>=100&&value.public_http_status<=599)out.public_http_status=value.public_http_status;
  if(value.model==='gpt-5.6-sol')out.model=value.model;
  if(value.upstream_attempts==='unknown')out.upstream_attempts='unknown';
  if(value.login_network==='test_proxy_wrapper')out.login_network=value.login_network;
  if(value.result==='passed')out.result='passed';
  return out;
}
