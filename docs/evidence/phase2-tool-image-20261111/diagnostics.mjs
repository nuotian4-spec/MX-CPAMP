export const loginStages=new Set(['input','web_login','authorize','email','email_otp','password','totp','workspace_session','callback','token_exchange','credential_validation','cpa_import','unknown']);
export const loginReasons=new Set(['security_check','email_verification_required','phone_verification_required','account_setup_required','unexpected_login_page','session_selection_required','totp_factor_missing','interaction_required','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','unknown']);
export const errorCodes=new Set(['needs_user_action','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','process_failed','output_invalid','import_unconfirmed','credential_mapping_unconfirmed','worker_stop_unconfirmed','worker_cleanup_unconfirmed','worker_unavailable','login_failed','cpa_target_changed','import_busy']);
export const taskStatuses=new Set(['created','running','importing','cancelling','succeeded','failed','needs_user_action','cancelled','expired']);
export function taskDiagnostics(task={}){return{stage:loginStages.has(task.stage)?task.stage:null,reason:loginReasons.has(task.reason)?task.reason:null};}
export const businessCodes=new Set(['services_not_ready','proxy_unreachable','admin_gate_invalid','create_ack_unconfirmed','batch_create_rejected','batch_query_unconfirmed','batch_timeout_unconfirmed','batch_result_invalid','registration_unconfirmed','saved_file_unconfirmed','snapshot_invalid','concurrency_write_unconfirmed','concurrency_apply_unconfirmed','refresh_result_unconfirmed','refresh_rejected','refresh_identity_mismatch','refresh_limit_lost','catalog_mismatch','public_request_unconfirmed','public_request_rejected','public_terminal_invalid','public_output_empty','auth_target_unconfirmed','admission_not_drained','harness_error']);
const publicCodes=new Set(['invalid_api_key','upstream_authentication_required','insufficient_quota','rate_limit_exceeded','model_not_found','request_timeout','internal_server_error','auth_not_found','auth_unavailable','provider_not_found','executor_not_found','credential_queue_full','credential_concurrency_timeout','model_cooldown','request_scoped']);
const publicTypes=new Set(['authentication_error','permission_error','rate_limit_error','invalid_request_error','server_error']);
const publicCategories=new Set(['local_configuration','model_capability','admission','upstream_http','transport','authentication','unknown']);
const capabilityMessages=new Set(['BPS supports OpenAI text requests only','BPS requires text input','invalid BPS reasoning effort','previous_response_id requires the original provider','background responses are not supported','client tools require the original provider','opaque, tool or non-text history requires the original provider and account','BPS requires complete text history; its response IDs cannot be continued']);
export function publicErrorDiagnostics(value={}){
  const error=value?.error&&typeof value.error==='object'?value.error:{};
  const code=publicCodes.has(error.code)?error.code:'unknown',type=publicTypes.has(error.type)?error.type:'unknown';
  let message=typeof error.message==='string'?error.message.trim():'';
  for(const prefix of publicCodes)if(message.startsWith(prefix+': ')){message=message.slice(prefix.length+2);break;}
  let category='unknown',upstreamStatus=null;
  const upstream=/^BPS upstream returned HTTP ([1-5][0-9]{2})$/.exec(message);
  if(upstream){category='upstream_http';upstreamStatus=Number(upstream[1]);}
  else if(/^(?:no provider supplied|executor not registered(?: for provider(?:: [^\r\n]*)?)?|unknown provider for model(?: [^\r\n]*)?)$/.test(message))category='local_configuration';
  else if(capabilityMessages.has(message)||/^BPS does not support (?:WebSocket|this operation|temperature|top_p|max_output_tokens|max_tokens|max_completion_tokens|service_tier)$/.test(message))category='model_capability';
  else if(/^(?:no auth available|credential queue full|credential concurrency timeout|credential is no longer enabled)$/.test(message))category='admission';
  else if(/^(?:context deadline exceeded|context canceled|net\/http: TLS handshake timeout|i\/o timeout|EOF|unexpected EOF)$/.test(message)||/^(?:Post|Get) "[^"\r\n]+": (?:context deadline exceeded|context canceled|net\/http: TLS handshake timeout|EOF|unexpected EOF)$/.test(message))category='transport';
  else if(['provider_not_found','executor_not_found'].includes(code))category='local_configuration';
  else if(code==='model_not_found')category='model_capability';
  else if(['auth_not_found','auth_unavailable','credential_queue_full','credential_concurrency_timeout','model_cooldown','rate_limit_exceeded','insufficient_quota'].includes(code))category='admission';
  else if(['invalid_api_key','upstream_authentication_required'].includes(code))category='authentication';
  else if(code==='request_timeout')category='transport';
  return{public_error_code:code,public_error_type:type,public_error_category:category,public_upstream_http_status:upstreamStatus};
}
export function sanitizeReceipt(value={}){
  const out={stage:['ready','batch','saved_registered','concurrency','refresh','business','complete','harness'].includes(value.stage)?value.stage:'harness',profile:'bps'};
  if(taskStatuses.has(value.status))out.status=value.status;
  for(const field of ['error_code','worker_error_code'])if(field in value)out[field]=errorCodes.has(value[field])||businessCodes.has(value[field])?value[field]:null;
  for(const field of ['login_stage','worker_stage'])if(field in value)out[field]=loginStages.has(value[field])?value[field]:null;
  for(const field of ['login_reason','worker_reason'])if(field in value)out[field]=loginReasons.has(value[field])?value[field]:null;
  for(const field of ['services_healthy','admin_gate','saved','registered','single_bps_credential','concurrency_override_saved','same_auth','same_profile','same_client','same_identity','refresh_token_present','access_token_changed','refresh_token_changed','concurrency_preserved','catalog_exact','normal_terminal','text_output_nonempty','auth_target_confirmed','admission_drained','production_network_equivalence'])if(typeof value[field]==='boolean')out[field]=value[field];
  for(const field of ['client_requests','refresh_endpoint_requests'])if(Number.isInteger(value[field])&&value[field]>=0&&value[field]<=(field==='client_requests'?2:1))out[field]=value[field];
  if(Number.isInteger(value.public_http_status)&&value.public_http_status>=100&&value.public_http_status<=599)out.public_http_status=value.public_http_status;
  for(const [field,allowed]of [['public_error_code',publicCodes],['public_error_type',publicTypes],['public_error_category',publicCategories]])if(field in value)out[field]=allowed.has(value[field])?value[field]:'unknown';
  if('public_upstream_http_status'in value)out.public_upstream_http_status=value.public_error_category==='upstream_http'&&Number.isInteger(value.public_upstream_http_status)&&value.public_upstream_http_status>=100&&value.public_upstream_http_status<=599?value.public_upstream_http_status:null;
  if(['tools','image'].includes(value.feature))out.feature=value.feature;
  if(['succeeded','rejected','unconfirmed','invalid'].includes(value.probe_outcome))out.probe_outcome=value.probe_outcome;
  for(const field of ['function_call_present','probes_complete','all_requests_succeeded'])if(typeof value[field]==='boolean')out[field]=value[field];
  if(value.model==='gpt-5.6-sol')out.model=value.model;
  if(value.upstream_attempts==='unknown')out.upstream_attempts='unknown';
  if(value.login_network==='test_proxy_wrapper')out.login_network=value.login_network;
  if(value.result==='passed')out.result='passed';
  return out;
}
