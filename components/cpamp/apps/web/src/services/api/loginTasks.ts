import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export type LoginProfile = 'codex' | 'bps';
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
export interface LoginTask {
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
    apiClient.get<{ status: 'ok' | 'wait' | 'error'; error?: string }>('/oauth/status', {
      ...config(scope),
      params: { state },
    }),
  callback: (state: string, redirect_url: string, scope: ApiClientRequestScope) =>
    apiClient.post('/oauth/callback', { provider: 'bps', state, redirect_url }, config(scope)),
  cancelOAuth: (state: string, scope: ApiClientRequestScope) =>
    apiClient.delete('/oauth/session', { ...config(scope), params: { state } }),
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
    needs_user_action: '需要在官方页面完成额外验证',
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
export const loginDiagnosticLabel = (task: Pick<LoginTask, 'stage' | 'reason'>) => {
  const stage = task.stage && stageLabels[task.stage];
  const reason = task.reason && reasonLabels[task.reason];
  return [stage ? `阶段：${stage}` : '', reason ? `提示：${reason}` : '']
    .filter(Boolean)
    .join(' · ');
};
