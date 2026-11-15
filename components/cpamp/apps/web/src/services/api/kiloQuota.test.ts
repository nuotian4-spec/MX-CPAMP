import { beforeEach, describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), delete: vi.fn(), post: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import { kiloQuotaApi, kiloQuotaError, kiloQuotaFailure } from './kiloQuota';
const scope = { apiBase: 'https://manager.invalid/v0/management', managementKey: 'ADMIN_FIXTURE' },
  ref = 'a'.repeat(32);
describe('Kilo额度API', () => {
  beforeEach(() => vi.resetAllMocks());
  it('策略读写固定编码路径，周期与策略版本不混用来源绑定版本', () => {
    const signal = new AbortController().signal;
    const input = {
      enabled: true,
      interval_seconds: 1800,
      expected_policy_revision: 4,
      confirm_scope: true,
    };
    kiloQuotaApi.getPolicy('account 1', scope, signal);
    kiloQuotaApi.putPolicy('account 1', input, ref, scope, signal);
    expect(client.get).toHaveBeenCalledWith(
      '/kilo/accounts/account%201/source-policy',
      expect.objectContaining({ signal })
    );
    expect(client.put).toHaveBeenCalledWith(
      '/kilo/accounts/account%201/source-policy',
      { ...input, connection_ref: ref },
      expect.objectContaining({ signal })
    );
    expect(client.post).not.toHaveBeenCalled();
  });
  it('五接口固定路径、编码账号、revision与ref独立，材料仅传PUT正文', async () => {
    const signal = new AbortController().signal;
    const config = {
      baseURL: 'https://manager.invalid/api',
      cpampScopedRequest: true,
      headers: { Authorization: 'Bearer ADMIN_FIXTURE' },
      signal,
    };
    kiloQuotaApi.sources(scope, signal);
    expect(client.get).toHaveBeenNthCalledWith(1, '/kilo/quota-sources', config);
    kiloQuotaApi.get('account 1', scope, signal);
    expect(client.get).toHaveBeenNthCalledWith(2, '/kilo/accounts/account%201/quota', config);
    const source = { kind: 'imported' as const, access_token: 'SECRET_ACCESS' };
    kiloQuotaApi.put('account 1', source, 7, ref, scope, signal);
    expect(client.put).toHaveBeenCalledWith(
      '/kilo/accounts/account%201/quota-source',
      { source, expected_binding_revision: 7, connection_ref: ref },
      config
    );
    kiloQuotaApi.remove('account 1', 7, ref, scope, signal);
    expect(client.delete).toHaveBeenCalledWith('/kilo/accounts/account%201/quota-source', {
      ...config,
      data: { expected_binding_revision: 7, connection_ref: ref },
    });
    client.post.mockResolvedValue({
      query: { status: 'failed', error_code: 'usage_unauthorized' },
    });
    const result = await kiloQuotaApi.query('account 1', 7, ref, scope, signal);
    expect(result.view.query.status).toBe('failed');
    expect(result.cancelled).toBe(false);
    expect(client.post).toHaveBeenCalledWith(
      '/kilo/accounts/account%201/quota/query',
      { expected_binding_revision: 7, connection_ref: ref },
      config
    );
  });
  it('仅499安全观测可正常交给页面，管理401保持错误', async () => {
    const view = {
      account_id: 'a',
      binding_revision: 3,
      connection_ref: ref,
      query: { status: 'cancelled' },
      source: { kind: 'imported' },
      credits: { remaining: null },
      windows: [],
    };
    client.post.mockRejectedValueOnce({ status: 499, data: view });
    expect(await kiloQuotaApi.query('a', 3, ref, scope)).toEqual({ view, cancelled: true });
    client.post.mockRejectedValueOnce({ status: 401, data: { error: 'SECRET' } });
    await expect(kiloQuotaApi.query('a', 3, ref, scope)).rejects.toMatchObject({ status: 401 });
    client.post.mockRejectedValueOnce({ status: 499, data: { error: 'SECRET' } });
    await expect(kiloQuotaApi.query('a', 3, ref, scope)).rejects.toMatchObject({ status: 499 });
  });
  it('错误/业务失败只映射公开代码，不透传正文或把usage401当管理退出', () => {
    expect(kiloQuotaFailure('usage_unauthorized')).toContain('不影响管理登录');
    expect(kiloQuotaFailure('query_egress_unavailable')).toBe(
      '额度查询出口暂不可用，等待独立查询出口'
    );
    expect(kiloQuotaFailure('egress_unavailable')).toBe('Kilo正式出口暂不可用');
    expect(kiloQuotaError({ status: 401 })).toContain('管理认证');
    for (const status of [400, 401, 403, 404, 409, 499, 502, 503])
      expect(kiloQuotaError({ status, message: 'SECRET' })).not.toContain('SECRET');
  });
});
