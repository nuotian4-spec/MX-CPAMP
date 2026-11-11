import { describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: mocks,
  createScopedApiRequestConfig: () => ({ cpampScopedRequest: true }),
}));
import { bpsAccessApi } from './bpsAccess';
describe('BPS权限查询实例绑定', () => {
  it('用显示凭证时的引用请求BFF，无二次settings绑定', async () => {
    mocks.get.mockResolvedValueOnce({
      provider: 'bps',
      auth_index: 'one',
      auth_id: 'one.json',
      queried_at: '2026-11-11T00:00:00Z',
      allowed: true,
      models: ['gpt-one'],
      upstream_status: 200,
      error: null,
      quota: null,
      inference_verified: false,
      auto_recover_eligible: false,
    });
    await bpsAccessApi.query(
      'one',
      { apiBase: 'https://panel.invalid', managementKey: 'TEST' },
      'old-reference'
    );
    expect(mocks.get).toHaveBeenCalledOnce();
    expect(mocks.get).toHaveBeenCalledWith(
      '/bps/access',
      expect.objectContaining({ params: { auth_index: 'one', connection_ref: 'old-reference' } })
    );
  });
  it('缺少页面引用时不请求任何接口', async () => {
    mocks.get.mockClear();
    await expect(
      bpsAccessApi.query('one', { apiBase: 'https://panel.invalid', managementKey: 'TEST' }, '')
    ).rejects.toThrow();
    expect(mocks.get).not.toHaveBeenCalled();
  });
});
