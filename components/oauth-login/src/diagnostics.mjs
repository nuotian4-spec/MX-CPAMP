// 诊断仅由固定枚举组成，不携带上游正文或登录材料。
const stages = new Set(['input', 'web_login', 'authorize', 'email', 'email_otp', 'password', 'totp', 'workspace_session', 'callback', 'token_exchange', 'credential_validation', 'cpa_import']);
const reasons = new Set(['security_check', 'email_verification_required', 'phone_verification_required', 'account_setup_required', 'unexpected_login_page', 'session_selection_required', 'totp_factor_missing', 'interaction_required', 'password_rejected', 'totp_rejected', 'callback_invalid', 'token_exchange_failed', 'credentials_incomplete', 'identity_mismatch', 'invalid_profile', 'transport_error', 'protocol_error']);
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
