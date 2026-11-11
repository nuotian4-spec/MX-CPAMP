import { bpsUsageApi } from '@/services/api/bpsUsage';
import { describe, expect, it, vi } from 'vitest';
import { bpsAccessApi } from '@/services/api/bpsAccess';
import { inspectSingleBpsAccount } from './bpsInspectionProbe';
import { toInspectionAccount } from './codexInspectionProbe';
import {
  normalizeCodexInspectionTargetTypes,
  DEFAULT_CODEX_INSPECTION_SETTINGS,
} from './codexInspectionSettings';
vi.mock('@/services/api/bpsAccess', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  bpsAccessApi: { query: vi.fn() },
}));
vi.mock('@/services/api/bpsUsage', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  bpsUsageApi: { query: vi.fn().mockRejectedValue(new Error('unknown')) },
}));
describe('BPS只读巡检', () => {
  it('单选混选保留BPS，旧默认保持Codex', () => {
    expect(normalizeCodexInspectionTargetTypes(['bps'])).toEqual(['bps']);
    expect(normalizeCodexInspectionTargetTypes(['bps', 'xai', 'codex', 'bps'])).toEqual([
      'codex',
      'xai',
      'bps',
    ]);
    expect(DEFAULT_CODEX_INSPECTION_SETTINGS.targetTypes).toEqual(['codex']);
  });
  it.each([null, 'unauthorized', 'forbidden', 'rate_limited', 'invalid_response'])(
    '权限结果%s始终keep且目录不进入历史',
    async (error) => {
      vi.mocked(bpsAccessApi.query).mockResolvedValue({
        provider: 'bps',
        auth_index: 'one',
        auth_id: 'bps.json',
        queried_at: '2026-11-11T00:00:00Z',
        allowed: error ? null : true,
        models: error ? null : ['PAGE_ONLY'],
        upstream_status: error ? 401 : 200,
        error,
        quota: null,
        inference_verified: false,
        auto_recover_eligible: false,
      });
      const result = await inspectSingleBpsAccount(
        toInspectionAccount({ name: 'bps.json', type: 'bps', auth_index: 'one', disabled: true }),
        { apiBase: 'http://panel.invalid', managementKey: 'TEST' }
      );
      expect(result.action).toBe('keep');
      expect(result.autoRecoverEligible).toBe(false);
      expect(result.usedPercent).toBeNull();
      expect(result.isQuota).toBe(false);
      expect(result.errorDetail).not.toContain('PAGE_ONLY');
    }
  );
});

it('权限通过后用量401仍保留权限；额度成功保留17和仅重置窗口', async () => {
  vi.mocked(bpsAccessApi.query).mockResolvedValue({
    provider: 'bps',
    auth_index: 'one',
    auth_id: 'one.json',
    queried_at: '2026-11-11T00:00:00Z',
    allowed: true,
    models: [],
    upstream_status: 200,
    error: null,
    quota: null,
    inference_verified: false,
    auto_recover_eligible: false,
  });
  const account = toInspectionAccount({
    name: 'one.json',
    type: 'bps',
    auth_index: 'one',
    bps_connection_ref: 'ref',
  });
  const scope = { apiBase: 'https://panel.invalid', managementKey: 'TEST' };
  vi.mocked(bpsUsageApi.query).mockResolvedValueOnce({
    provider: 'bps',
    auth_index: 'one',
    auth_id: 'one.json',
    source: 'chatgpt_account_usage',
    queried_at: '2026-11-11T00:00:00Z',
    upstream_status: 401,
    error: 'unauthorized',
    plan_type: null,
    rate_limit: null,
  });
  const failed = await inspectSingleBpsAccount(account, scope);
  expect(failed).toMatchObject({
    state: 'bps_access_allowed',
    statusCode: 200,
    action: 'keep',
    autoRecoverEligible: false,
    usedPercent: null,
    error: '',
  });
  expect(JSON.parse(failed.errorDetail!)).toMatchObject({
    usage_error: 'unauthorized',
    usage_source: 'chatgpt_account_usage',
  });
  vi.mocked(bpsUsageApi.query).mockResolvedValueOnce({
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
        used_percent: 17,
        limit_window_seconds: 2592000,
        reset_after_seconds: 2,
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
  const success = await inspectSingleBpsAccount(account, scope);
  expect(success).toMatchObject({
    action: 'keep',
    autoRecoverEligible: false,
    usedPercent: 17,
    quotaInventoryObserved: true,
    planType: 'plus',
    quotaWindows: [
      { usedPercent: 17, resetAtMs: 1796977220000 },
      { usedPercent: null, resetAtMs: 1796977220000 },
    ],
  });
});
