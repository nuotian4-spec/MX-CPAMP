// 诊断仅由固定枚举组成，不携带上游正文或登录材料。
const stages = new Set(['input', 'web_login', 'authorize', 'email', 'email_otp', 'password', 'totp', 'workspace_session', 'callback', 'token_exchange', 'credential_validation', 'cpa_import']);
const reasons = new Set(['security_check', 'email_verification_required', 'phone_verification_required', 'account_setup_required', 'unexpected_login_page', 'session_selection_required', 'totp_factor_missing', 'interaction_required', 'password_rejected', 'totp_rejected', 'callback_invalid', 'token_exchange_failed', 'credentials_incomplete', 'identity_mismatch', 'invalid_profile', 'transport_error', 'protocol_error']);
const endpoints = new Set(['chatgpt_home', 'chatgpt_providers', 'chatgpt_csrf', 'chatgpt_signin', 'auth_redirect', 'password_verify', 'totp_issue', 'totp_verify', 'workspace_select', 'session_select', 'web_finalize', 'oauth_authorize', 'oauth_token', 'sentinel']);
const subtypes = new Set(['challenge_header', 'challenge_html', 'html_forbidden_unclassified']);
export function safeDiagnostics(value) {
  return {
    http_status: Number.isInteger(value?.http_status) && value.http_status >= 100 && value.http_status <= 599 ? value.http_status : null,
    endpoint_category: endpoints.has(value?.endpoint_category) ? value.endpoint_category : 'unknown',
    security_check_subtype: subtypes.has(value?.security_check_subtype) ? value.security_check_subtype : 'unknown',
    challenge_header_present: typeof value?.challenge_header_present === 'boolean' ? value.challenge_header_present : null,
  };
}
export function responseDiagnostics(response, text, endpointCategory) {
  const headers = response?.headers;
  const mitigated = headers?.get?.('cf-mitigated') || headers?.get?.('x-cf-mitigated');
  const contentType = String(headers?.get?.('content-type') || '');
  const body = String(text || '');
  const challengePage = /Just a moment|cdn-cgi|challenge-platform|cf-challenge/i.test(body);
  const htmlPage = /text\/html/i.test(contentType) || /<(?:!doctype\s+html|html|head|body)\b/i.test(body);
  let subtype = 'unknown';
  if (mitigated && /challenge/i.test(mitigated)) subtype = 'challenge_header';
  else if ((response?.status === 403 || [400, 409].includes(response?.status) && htmlPage) && challengePage) subtype = 'challenge_html';
  else if (response?.status === 403 && /text\/html/i.test(contentType)) subtype = 'html_forbidden_unclassified';
  const headerPresent = typeof headers?.get === 'function' ? headers.get('cf-mitigated') != null || headers.get('x-cf-mitigated') != null : null;
  return safeDiagnostics({http_status: response?.status, endpoint_category: endpointCategory, security_check_subtype: subtype, challenge_header_present: headerPresent});
}
export const safeStage = value => stages.has(value) ? value : 'unknown';
export const safeReason = value => reasons.has(value) ? value : 'unknown';
export function failureReason(error, stage) {
  if (reasons.has(error?.diagnostic_reason)) return error.diagnostic_reason;
  const message = String(error?.message || '');
  if (/security.check|PROXY_RISK_CONTROL/i.test(message)) return 'security_check';
  if (/TOTP factor ID/.test(message)) return 'totp_factor_missing';
  if (/unexpected.login/i.test(message)) return 'unexpected_login_page';
  if (message === 'interaction_required') return stage === 'email_otp' ? 'email_verification_required' : stage === 'workspace_session' ? 'session_selection_required' : 'interaction_required';
  return reasons.has(message) ? message : 'protocol_error';
}
