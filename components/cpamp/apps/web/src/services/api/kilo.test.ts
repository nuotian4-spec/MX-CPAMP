import { beforeEach, describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import { kiloApi, kiloErrorMessage } from './kilo';
const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'ADMIN_FIXTURE' };
const connectionRef = 'a'.repeat(32);
describe('Kilo账号API', () => {
  beforeEach(() => vi.clearAllMocks());
  it('仅使用显式Manager作用域和当前读取引用', () => {
    kiloApi.list(scope);
    const config = {
      baseURL: 'https://panel.invalid/api',
      cpampScopedRequest: true,
      headers: { Authorization: 'Bearer ADMIN_FIXTURE' },
    };
    expect(client.get).toHaveBeenCalledWith('/kilo/accounts', config);
    kiloApi.create({ api_key: 'SYNTHETIC_KEY', concurrency_limit: null }, connectionRef, scope);
    expect(client.post).toHaveBeenCalledWith(
      '/kilo/accounts',
      { api_key: 'SYNTHETIC_KEY', concurrency_limit: null, connection_ref: connectionRef },
      config
    );
    kiloApi.update('account id.json', { enabled: false }, connectionRef, scope);
    expect(client.patch).toHaveBeenCalledWith(
      '/kilo/accounts/account%20id.json',
      { enabled: false, connection_ref: connectionRef },
      config
    );
    expect(client.patch.mock.calls[0][1]).not.toHaveProperty('api_key');
    kiloApi.remove('account id.json', connectionRef, scope);
    expect(client.delete).toHaveBeenCalledWith('/kilo/accounts/account%20id.json', {
      ...config,
      data: { connection_ref: connectionRef },
    });
  });
  it('错误只按状态表达，不显示底层正文或输入Key', () => {
    for (const status of [400, 401, 403, 404, 409, 499, 500, 503, undefined]) {
      expect(
        kiloErrorMessage({ status, message: 'SECRET_INPUT', data: { api_key: 'SECRET_INPUT' } })
      ).not.toContain('SECRET');
    }
    expect(kiloErrorMessage({ status: 409 })).toContain('结果待核');
  });
  it('组织查询只用账号ID/当前scope/signal，显式PATCH可提交个人空值', () => {
    const signal = new AbortController().signal;
    kiloApi.organizations('account id.json', scope, signal);
    expect(client.get).toHaveBeenCalledWith('/kilo/accounts/account%20id.json/organizations', {
      baseURL: 'https://panel.invalid/api',
      cpampScopedRequest: true,
      headers: { Authorization: 'Bearer ADMIN_FIXTURE' },
      signal,
    });
    kiloApi.update('account id.json', { organization_id: '' }, connectionRef, scope);
    expect(client.patch.mock.calls[0][1]).toEqual({
      organization_id: '',
      connection_ref: connectionRef,
    });
    expect(kiloErrorMessage({ status: 403 })).toContain('没有所选组织');
    expect(kiloErrorMessage({ status: 499 })).toContain('结果待核');
  });
});
