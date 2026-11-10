import { describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ get: vi.fn(), patch: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import { credentialConcurrencyApi } from './credentialConcurrency';
describe('并发快照连接绑定', () => {
  it('PATCH显式回传快照引用且不接收客户端上游路径', () => {
    const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'ADMIN' };
    const reference = 'a'.repeat(32);
    credentialConcurrencyApi.update('same.json', null, reference, scope);
    expect(client.patch).toHaveBeenCalledWith(
      '/credential-concurrency',
      { auth_id: 'same.json', concurrency_limit: null, connection_ref: reference },
      {
        baseURL: 'https://panel.invalid/api',
        cpampScopedRequest: true,
        headers: { Authorization: 'Bearer ADMIN' },
      }
    );
  });
});
