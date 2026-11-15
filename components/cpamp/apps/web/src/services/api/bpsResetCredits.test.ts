import { expect, it, vi } from 'vitest';
import { bpsResetCreditsApi } from './bpsResetCredits';
import { apiClient } from './client';
vi.mock('./client', async (original) => ({
  ...(await original<typeof import('./client')>()),
  apiClient: { get: vi.fn(), post: vi.fn() },
}));
const scope = { apiBase: 'https://fixture.invalid', managementKey: 'test' };
const result = {
  provider: 'bps',
  auth_id: 'id',
  auth_index: 'idx',
  source: 'chatgpt_account_reset_credits',
  query_mode: 'direct',
  queried_at: '2026-11-15T00:00:00Z',
  status: 'known',
  error: null,
};
it.each([undefined, null, -1, 0.1, '0', Number.MAX_SAFE_INTEGER + 1])(
  '原生次数%s保持未知',
  async (count) => {
    vi.mocked(apiClient.get).mockResolvedValue({ ...result, count });
    expect(await bpsResetCreditsApi.query('idx', 'id', scope, 'ref')).toMatchObject({
      count: null,
      status: 'unknown',
    });
  }
);
it('真实0保留且仅GET', async () => {
  vi.mocked(apiClient.get).mockResolvedValue({ ...result, count: 0 });
  expect(await bpsResetCreditsApi.query('idx', 'id', scope, 'ref')).toMatchObject({
    count: 0,
    status: 'known',
  });
  expect(apiClient.get).toHaveBeenLastCalledWith(
    '/bps/reset-credits',
    expect.objectContaining({ params: { auth_id: 'id', auth_index: 'idx', connection_ref: 'ref' } })
  );
  expect(apiClient.post).not.toHaveBeenCalled();
});
it.each(['provider', 'auth_id', 'auth_index', 'source', 'query_mode', 'queried_at'])(
  '拒绝%s错配',
  async (field) => {
    vi.mocked(apiClient.get).mockResolvedValue({ ...result, count: 0, [field]: 'invalid' });
    await expect(bpsResetCreditsApi.query('idx', 'id', scope, 'ref')).rejects.toThrow('不匹配');
  }
);
