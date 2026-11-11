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
