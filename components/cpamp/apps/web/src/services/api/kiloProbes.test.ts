import { beforeEach, describe, expect, it, vi } from 'vitest';
import { kiloProbesApi, kiloProbesError } from './kiloProbes';

const mocks = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: mocks,
  createScopedApiRequestConfig: (scope: unknown) => ({ requestScope: scope }),
}));
const scope = { apiBase: 'http://manager.invalid', managementKey: 'SYNTHETIC' };
beforeEach(() => vi.clearAllMocks());
describe('自动生成配置API', () => {
  it('固定账号路径、管理scope、signal，完整双配置与revision/ref白名单', async () => {
    const signal = new AbortController().signal;
    await kiloProbesApi.get('id space.json', scope, signal);
    expect(mocks.get).toHaveBeenCalledWith('/kilo/accounts/id%20space.json/probes', {
      requestScope: scope,
      baseURL: 'http://manager.invalid/api',
      signal,
    });
    const input = {
      expected_revision: 7,
      health: { enabled: true },
      model_probe: { enabled: false, model: 'gpt-6.1-sol', interval_seconds: 600 },
      endpoint: 'forbidden',
    };
    await kiloProbesApi.save('id space.json', input, 'ref', scope, signal);
    expect(mocks.put).toHaveBeenCalledWith(
      '/kilo/accounts/id%20space.json/probes',
      {
        expected_revision: 7,
        health: { enabled: true },
        model_probe: { enabled: false, model: 'gpt-6.1-sol', interval_seconds: 600 },
        connection_ref: 'ref',
      },
      { requestScope: scope, baseURL: 'http://manager.invalid/api', signal }
    );
  });
  it.each([400, 401, 403, 404, 409, 499, 503, 502])('HTTP%s固定提示不使用原始正文', (status) => {
    expect(kiloProbesError({ status, message: 'SECRET' })).not.toContain('SECRET');
  });
});
