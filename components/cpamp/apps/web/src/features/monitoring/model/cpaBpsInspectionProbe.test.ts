import { describe, it, expect, vi } from 'vitest';
import { inspectSingleCpaBpsAccount } from './cpaBpsInspectionProbe';
import { toInspectionAccount } from './codexInspectionProbe';
import { cpaBpsQuotaApi } from '@/services/api/cpaBpsQuota';
vi.mock('@/services/api/cpaBpsQuota', () => ({ cpaBpsQuotaApi: { query: vi.fn() } }));
const account = toInspectionAccount({
  name: 'a.json',
  id: 'a',
  provider: 'cpa-bps',
  auth_index: 'a',
  disabled: true,
});
const scope = { apiBase: 'https://a.invalid', managementKey: 'key' };
describe('插件巡检只读', () => {
  it('额度耗尽仍keep且不自动恢复', async () => {
    vi.mocked(cpaBpsQuotaApi.query).mockResolvedValue({
      provider: 'cpa-bps',
      source: 'cpa_bps_plugin_quota',
      auth_index: 'a',
      auth_id: 'a',
      queried_at: '2026-11-12T00:00:00Z',
      quota: {
        subscription: null,
        summary: [],
        groups: [
          {
            displayName: 'BPS',
            buckets: [{ window: 'daily', remainingFraction: 0, resetTime: '', description: '' }],
          },
        ],
      },
    });
    const result = await inspectSingleCpaBpsAccount(account, scope);
    expect(result.action).toBe('keep');
    expect(result.autoRecoverEligible).toBe(false);
    expect(result.quotaWindows?.[0].usedPercent).toBe(100);
    expect(result.actionReason).toContain('推理未验证');
  });
  it('认证错误不转换为禁用删除或重登', async () => {
    vi.mocked(cpaBpsQuotaApi.query).mockRejectedValue({ status: 401 });
    expect(await inspectSingleCpaBpsAccount(account, scope)).toMatchObject({
      action: 'keep',
      autoRecoverEligible: false,
      statusCode: 401,
      usedPercent: null,
    });
  });
});
