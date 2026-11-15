import { beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: mocks,
  createScopedApiRequestConfig: (scope: { apiBase: string; managementKey: string }) => ({
    headers: { Authorization: 'Bearer ' + scope.managementKey },
    cpampScopedRequest: true,
  }),
}));
import { kiloManualApi } from './kiloManual';
describe('手动模型API', () => {
  beforeEach(() => vi.clearAllMocks());
  it('请求固定账号/连接，仅model与原prompt，禁用推理总超时并透传取消', async () => {
    const scope = { apiBase: 'http://manager.invalid', managementKey: 'SYNTHETIC_ADMIN' };
    const signal = new AbortController().signal;
    await kiloManualApi.test(
      'a/b.json',
      'kilo-auto/free',
      '  原提示  ',
      'reference',
      scope,
      signal
    );
    expect(mocks.post).toHaveBeenCalledExactlyOnceWith(
      '/kilo/accounts/a%2Fb.json/test',
      { model: 'kilo-auto/free', prompt: '  原提示  ', connection_ref: 'reference' },
      {
        headers: { Authorization: 'Bearer SYNTHETIC_ADMIN' },
        cpampScopedRequest: true,
        baseURL: 'http://manager.invalid/api',
        signal,
        timeout: 0,
      }
    );
    await kiloManualApi.models(scope, signal);
    expect(mocks.get).toHaveBeenCalledWith(
      '/kilo/test-models',
      expect.objectContaining({ signal, baseURL: 'http://manager.invalid/api' })
    );
  });
});
