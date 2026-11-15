import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export type LoginProfile = 'codex' | 'bps';
export type BPSLoginMode = 'builtin' | 'plugin' | 'unavailable';
export type LoginTaskStatus =
  | 'created'
  | 'running'
  | 'importing'
  | 'cancelling'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | 'expired'
  | 'needs_user_action';
export interface HTTPDiagnostics {
  http_status?: number | null;
  endpoint_category?: string;
  security_check_subtype?: string;
  challenge_header_present?: boolean | null;
}
export interface LoginTask extends HTTPDiagnostics {
  id: string;
  profile: LoginProfile;
  status: LoginTaskStatus;
  error_code?: string;
  stage?: string;
  reason?: string;
  auth_id?: string;
}
export interface LoginInput {
  task_id?: string;
  profile: LoginProfile;
  email: string;
  password: string;
  totp_secret: string;
}
const config = (scope: ApiClientRequestScope) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api/oauth-login`,
});
export const loginTasksApi = {
  capabilities: (scope: ApiClientRequestScope) =>
    apiClient.get<{ password_login_configured: boolean; bps_login_mode: BPSLoginMode }>(
      '/capabilities',
      config(scope)
    ),
  create: (input: LoginInput, scope: ApiClientRequestScope) =>
    apiClient.post<LoginTask>('/tasks', input, config(scope)),
  get: (id: string, scope: ApiClientRequestScope) =>
    apiClient.get<LoginTask>(`/tasks/${encodeURIComponent(id)}`, config(scope)),
  cancel: (id: string, scope: ApiClientRequestScope) =>
    apiClient.delete<LoginTask>(`/tasks/${encodeURIComponent(id)}`, config(scope)),
  startBPS: (scope: ApiClientRequestScope) =>
    apiClient.post<{ url: string; state: string }>(
      '/oauth/auth-url',
      { provider: 'bps' },
      config(scope)
    ),
  status: (state: string, scope: ApiClientRequestScope) =>
    apiClient.get<BPSOAuthStatus>('/oauth/status', {
      ...config(scope),
      params: { state },
    }),
  callback: (state: string, redirect_url: string, scope: ApiClientRequestScope) =>
    apiClient.post<BPSOAuthStatus>(
      '/oauth/callback',
      { provider: 'bps', state, redirect_url },
      config(scope)
    ),
  cancelOAuth: (state: string, scope: ApiClientRequestScope) =>
    apiClient.delete<BPSOAuthStatus>('/oauth/session', { ...config(scope), params: { state } }),
};
export const isLoginTaskActive = (task?: LoginTask) =>
  Boolean(task && ['created', 'running', 'importing', 'cancelling'].includes(task.status));
export const loginTaskLabel = (task: LoginTask) =>
  ({
    created: '已创建',
    running: '正在登录',
    importing: '正在保存并注册账号',
    cancelling: '正在终止登录进程',
    succeeded: '账号已保存并注册',
    failed: '登录未完成',
    cancelled: '已取消',
    expired: '登录超时',
    needs_user_action: '登录已停止，请查看诊断',
  })[task.status];

const stageLabels: Record<string, string> = {
  input: '输入校验',
  web_login: '网页登录',
  authorize: '授权入口',
  email: '邮箱提交',
  email_otp: '邮箱验证码',
  password: '密码验证',
  totp: 'TOTP验证',
  workspace_session: '工作区/会话选择',
  callback: '授权回调',
  token_exchange: '令牌兑换',
  credential_validation: '凭证校验',
  cpa_import: 'CPA保存注册',
};
const reasonLabels: Record<string, string> = {
  security_check: '官方安全检查',
  email_verification_required: '需要邮箱验证',
  phone_verification_required: '需要手机号验证',
  account_setup_required: '需要补全账号资料',
  unexpected_login_page: '未识别的登录页面',
  session_selection_required: '需要选择会话',
  totp_factor_missing: '缺少TOTP验证因子',
  interaction_required: '需要官方交互',
};
export const loginDiagnosticLabel = (
  task: Pick<LoginTask, 'stage' | 'reason'> & HTTPDiagnostics
) => {
  const stage = task.stage && stageLabels[task.stage];
  const subtype = task.security_check_subtype;
  const reason =
    task.reason === 'security_check'
      ? subtype === 'html_forbidden_unclassified'
        ? '网页登录被拒绝，具体原因待确认'
        : subtype === 'challenge_header' || subtype === 'challenge_html'
          ? '检测到官方挑战，当前任务已停止'
          : '登录请求被拦截，具体原因待确认'
      : task.reason && reasonLabels[task.reason];
  const endpoint =
    task.endpoint_category && endpointCategories.has(task.endpoint_category)
      ? task.endpoint_category
      : '';
  const http = safeHTTPStatus(task.http_status);
  const header =
    typeof task.challenge_header_present === 'boolean'
      ? task.challenge_header_present
        ? '挑战头：有'
        : '挑战头：无'
      : '';
  return [
    stage ? `阶段：${stage}` : '',
    reason ? `提示：${reason}` : '',
    endpoint ? `请求：${endpoint}` : '',
    http ? `HTTP ${http}` : '',
    header,
  ]
    .filter(Boolean)
    .join(' · ');
};

const endpointCategories = new Set([
  'chatgpt_home',
  'chatgpt_providers',
  'chatgpt_csrf',
  'chatgpt_signin',
  'auth_redirect',
  'password_verify',
  'totp_issue',
  'totp_verify',
  'workspace_select',
  'session_select',
  'web_finalize',
  'oauth_authorize',
  'oauth_token',
  'sentinel',
]);
const safeHTTPStatus = (value: unknown): number | null =>
  typeof value === 'number' && Number.isInteger(value) && value >= 100 && value <= 599
    ? value
    : null;
export interface BPSOAuthStatus extends HTTPDiagnostics {
  status: 'ok' | 'wait' | 'error';
  error?: string;
  error_code?: string;
  stage?: string;
  reason?: string;
  save_outcome?: string;
  saved?: boolean;
  adopted?: boolean;
  cancelled?: boolean;
  error_source?: string;
}
export interface BPSOutcome {
  provider: 'bps';
  error_code: string;
  stage: string;
  reason: string;
  http_status: number | null;
  save_outcome: 'confirmed' | 'not_submitted' | 'not_saved' | 'unknown';
  error_source?: string;
}
const bpsSourceLabels: Record<string, string> = {
  manager_session: 'Manager会话',
  cpa_session: 'CPA会话',
  upstream: '上游',
  transport: '传输',
};
const bpsCodes: Record<string, [string, string]> = {
  session_missing_or_expired: ['session', '授权会话记录缺失'],
  oauth_authorization_failed: ['authorize', '官方授权回调未成功'],
  oauth_exchange_failed: ['token_exchange', '令牌兑换失败'],
  oauth_identity_invalid: ['credential_validation', '授权身份校验失败'],
  oauth_target_identity_mismatch: ['credential_validation', '授权身份与目标不符'],
  credential_commit_failed: ['cpa_import', '保存或注册未全部确认'],
  oauth_start_failed: ['authorize', '创建授权失败，请检查管理员权限或 CPA'],
  oauth_status_unavailable: ['session', '授权状态暂不可读'],
  callback_result_unknown: ['callback', '回调结果未确认'],
  invalid_callback_input: ['callback', '回调地址未通过校验'],
  invalid_callback_url: ['callback', '回调地址未通过校验'],
  invalid_callback_query: ['callback', '回调参数未通过校验'],
  conflicting_callback_parameters: ['callback', '回调参数存在冲突'],
  callback_state_mismatch: ['callback', '回调与本次会话不符'],
  callback_code_required: ['callback', '回调缺少授权码'],
  oauth_provider_mismatch: ['callback', '回调授权通道不符'],
  invalid_body: ['callback', '回调输入未通过校验'],
  invalid_state: ['callback', '回调会话未通过校验'],
  unsupported_provider: ['callback', '回调授权通道不支持'],
  unknown: ['unknown', '授权未完成，具体原因待确认'],
};
export function bpsOutcome(raw: BPSOAuthStatus | undefined, submitted: boolean): BPSOutcome {
  const code =
    raw?.error_code && Object.prototype.hasOwnProperty.call(bpsCodes, raw.error_code)
      ? raw.error_code
      : 'unknown';
  const reason =
    raw?.reason &&
    [
      'invalid_grant',
      'invalid_client',
      'upstream_error',
      'transport_error',
      'protocol_error',
    ].includes(raw.reason)
      ? raw.reason
      : 'unknown';
  const confirmedNotSaved = [
    'oauth_authorization_failed',
    'oauth_exchange_failed',
    'oauth_identity_invalid',
    'oauth_target_identity_mismatch',
  ].includes(code);
  return {
    provider: 'bps',
    error_code: code,
    stage: bpsCodes[code][0],
    reason,
    http_status: safeHTTPStatus(raw?.http_status),
    save_outcome:
      raw?.save_outcome === 'not_saved' || raw?.save_outcome === 'unknown'
        ? raw.save_outcome
        : !submitted
          ? 'not_submitted'
          : confirmedNotSaved
            ? 'not_saved'
            : 'unknown',
    error_source:
      raw?.error_source && Object.prototype.hasOwnProperty.call(bpsSourceLabels, raw.error_source)
        ? raw.error_source
        : undefined,
  };
}
export function bpsOutcomeLabel(outcome: BPSOutcome): string {
  const result =
    outcome.save_outcome === 'confirmed'
      ? '账号已保存并注册'
      : outcome.save_outcome === 'unknown'
        ? '保存结果待核实，请先核对凭证结果；不要重复授权或导入'
        : outcome.save_outcome === 'not_submitted'
          ? '尚未提交回调'
          : '本次未保存凭证';
  const summary =
    outcome.save_outcome === 'confirmed'
      ? ''
      : bpsCodes[outcome.error_code]?.[1] || bpsCodes.unknown[1];
  const reason = (
    {
      invalid_grant: '授权码无效或已失效',
      invalid_client: '授权客户端被拒绝',
      upstream_error: '上游请求被拒绝',
      transport_error: '换码连接失败',
      protocol_error: '换码响应格式不符',
    } as Record<string, string>
  )[outcome.reason];
  return [
    summary,
    `阶段：${outcome.stage}`,
    outcome.error_code !== 'unknown' ? `诊断：${outcome.error_code}` : '',
    reason || '',
    outcome.http_status ? `HTTP ${outcome.http_status}` : '',
    outcome.error_source && bpsSourceLabels[outcome.error_source]
      ? `来源：${bpsSourceLabels[outcome.error_source]}`
      : '',
    result,
  ]
    .filter(Boolean)
    .join(' · ');
}
