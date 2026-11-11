export const loginStages=new Set(['input','web_login','authorize','email','email_otp','password','totp','workspace_session','callback','token_exchange','credential_validation','cpa_import','unknown']);
export const loginReasons=new Set(['security_check','email_verification_required','phone_verification_required','account_setup_required','unexpected_login_page','session_selection_required','totp_factor_missing','interaction_required','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','unknown']);
export const errorCodes=new Set(['needs_user_action','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','process_failed','output_invalid','import_unconfirmed','worker_stop_unconfirmed','login_failed','worker_unavailable','create_rejected']);
export const taskStatuses=new Set(['created','running','importing','cancelling','succeeded','failed','needs_user_action','cancelled','expired']);
export function taskDiagnostics(task={}){
  return {stage:loginStages.has(task.stage)?task.stage:null,reason:loginReasons.has(task.reason)?task.reason:null};
}
export function sanitizeReceipt(value={}){
  const allowedStages=new Set(['ready','create','login','saved_registered','harness']);
  const out={stage:allowedStages.has(value.stage)?value.stage:'harness'};
  if(['codex','bps'].includes(value.profile))out.profile=value.profile;
  if(taskStatuses.has(value.status))out.status=value.status;
  const harnessCodes=new Set(['services_not_ready','proxy_unreachable','create_ack_unconfirmed','task_query_unconfirmed','task_timeout_unconfirmed','task_receipt_invalid','registration_unconfirmed','saved_file_unconfirmed','admin_gate_invalid','harness_error']);
  for(const field of ['error_code','worker_error_code'])if(field in value)out[field]=errorCodes.has(value[field])||harnessCodes.has(value[field])?value[field]:null;
  for(const field of ['login_stage','worker_stage'])if(field in value)out[field]=loginStages.has(value[field])?value[field]:null;
  for(const field of ['login_reason','worker_reason'])if(field in value)out[field]=loginReasons.has(value[field])?value[field]:null;
  for(const field of ['saved','registered','refresh_token_present','model_request_sent','services_healthy','admin_gate','local_proxy_port_reachable','production_network_equivalence'])if(typeof value[field]==='boolean')out[field]=value[field];
  if(value.login_network==='test_proxy_wrapper')out.login_network=value.login_network;
  return out;
}
