import { describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), delete: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import {
  loginTasksApi,
  isLoginTaskActive,
  loginTaskLabel,
  loginDiagnosticLabel,
} from './loginTasks';
describe('登录API隔离', () => {
  it('只渲染已知诊断，不渲染原始输入', () => {
    expect(loginDiagnosticLabel({ stage: 'totp', reason: 'security_check' })).toContain('TOTP验证');
    expect(loginDiagnosticLabel({ stage: 'PASSWORD_SECRET', reason: 'COOKIE_SECRET' })).toBe('');
  });
  it('使用Manager根入口而非v0，固定BPS且保留管理员头', () => {
    const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'ADMIN' };
    loginTasksApi.startBPS(scope);
    expect(client.post).toHaveBeenCalledWith(
      '/oauth/auth-url',
      { provider: 'bps' },
      {
        baseURL: 'https://panel.invalid/api/oauth-login',
        cpampScopedRequest: true,
        headers: { Authorization: 'Bearer ADMIN' },
      }
    );
  });
  it('人工验证不是成功或进行中', () => {
    const task = { id: 'id', profile: 'bps' as const, status: 'needs_user_action' as const };
    expect(isLoginTaskActive(task)).toBe(false);
    expect(loginTaskLabel(task)).toContain('额外验证');
  });
});
