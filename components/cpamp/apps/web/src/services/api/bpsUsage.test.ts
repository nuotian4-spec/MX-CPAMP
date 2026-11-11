import { describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: mocks,
  createScopedApiRequestConfig: () => ({ cpampScopedRequest: true }),
}));
import { bpsUsageApi, bpsUsageState, bpsUsageWindows, type BPSUsage } from './bpsUsage';

export const usage = (): BPSUsage => ({
  provider: 'bps',
  auth_index: 'one',
  auth_id: 'one.json',
  source: 'chatgpt_account_usage',
  queried_at: '2026-11-11T00:00:00Z',
  upstream_status: 200,
  error: null,
  plan_type: 'plus',
  rate_limit: {
    allowed: true,
    limit_reached: false,
    primary_window: {
      used_percent: 0,
      limit_window_seconds: 2592000,
      reset_after_seconds: 3,
      reset_at: 1796977220,
    },
    secondary_window: {
      used_percent: null,
      limit_window_seconds: 2592000,
      reset_after_seconds: null,
      reset_at: 1796977220,
    },
  },
});

describe('官方账号用量契约', () => {
  it('保留合法零、仅重置窗口、绝对时间和未确认模型范围', () => {
    const windows = bpsUsageWindows(usage());
    expect(windows).toHaveLength(2);
    expect(windows[0]).toMatchObject({
      usedPercent: 0,
      resetAtMs: 1796977220000,
      modelScope: { complete: false },
    });
    expect(windows[1]).toMatchObject({ usedPercent: null, resetAtMs: 1796977220000 });
    expect(
      bpsUsageState(usage(), { name: 'one.json', type: 'bps', auth_index: 'one' })
    ).toMatchObject({ status: 'success', planType: 'plus' });
  });
  it('携带原实例引用并拒绝跨凭据回执', async () => {
    mocks.get.mockResolvedValueOnce(usage());
    await bpsUsageApi.query(
      'one',
      { apiBase: 'https://panel.invalid', managementKey: 'TEST' },
      'ref'
    );
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/bps/usage',
      expect.objectContaining({ params: { auth_index: 'one', connection_ref: 'ref' } })
    );
    mocks.get.mockResolvedValueOnce({ ...usage(), auth_index: 'other' });
    await expect(
      bpsUsageApi.query('one', { apiBase: 'https://panel.invalid', managementKey: 'TEST' }, 'ref')
    ).rejects.toThrow();
  });
  it('额度失败保留未知，无引用不发请求', async () => {
    const failed = { ...usage(), error: 'unauthorized', rate_limit: null, plan_type: null };
    expect(bpsUsageState(failed, { name: 'one', type: 'bps' })).toMatchObject({
      status: 'error',
      windows: [],
    });
    mocks.get.mockClear();
    await expect(
      bpsUsageApi.query('one', { apiBase: 'https://panel.invalid', managementKey: 'TEST' }, '')
    ).rejects.toThrow();
    expect(mocks.get).not.toHaveBeenCalled();
  });
});
