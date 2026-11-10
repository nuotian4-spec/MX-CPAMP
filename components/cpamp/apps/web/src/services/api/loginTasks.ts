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
  auth_id?: string;
}
export interface LoginInput {
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
