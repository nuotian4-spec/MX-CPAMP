import { describe, expect, it, vi } from 'vitest';
import { authFilesApi } from '@/services/api/authFiles';
import { bpsSettingsApi } from '@/services/api/bpsSettings';
import { bpsAccessApi } from '@/services/api/bpsAccess';
import { requestCodexUsageRaw } from '@/services/api/codexQuota';
import {
  inspectCodexAccounts,
  serializeCodexInspectionLastRun,
  hydrateCodexInspectionLastRun,
} from '../codexInspection';
vi.mock('@/services/api/bpsSettings', () => ({ bpsSettingsApi: { get: vi.fn() } }));
vi.mock('@/services/api/bpsAccess', async (original) => ({
  ...(await original<object>()),
  bpsAccessApi: { query: vi.fn() },
}));
vi.mock('@/services/api/codexQuota', () => ({ requestCodexUsageRaw: vi.fn() }));
describe('BPS本地巡检纵切与历史', () => {
  it.each([[['bps']], [['codex', 'bps']]])(
    '目标%s走独立BPS探针且未知额度往返',
    async (targetTypes) => {
      vi.mocked(bpsSettingsApi.get).mockResolvedValue({
        enabled: true,
        images_enabled: true,
        image_transport: 'native',
        image_relay_base_url: '',
        limits: {},
        connection_ref: 'bound',
      });
      vi.spyOn(authFilesApi, 'list').mockResolvedValue({
        files: [{ name: 'bps.json', provider: 'bps', auth_index: 'one', disabled: true }],
      });
      vi.mocked(bpsAccessApi.query).mockResolvedValue({
        provider: 'bps',
        auth_index: 'one',
        auth_id: 'bps.json',
        queried_at: '2026-11-11T00:00:00Z',
        allowed: true,
        models: ['PAGE_ONLY'],
        upstream_status: 200,
        error: null,
        quota: null,
        inference_verified: false,
        auto_recover_eligible: false,
      });
      const result = await inspectCodexAccounts({
        config: null,
        apiBase: 'http://panel.invalid',
        managementKey: 'TEST',
        settings: { targetTypes },
      });
      expect(requestCodexUsageRaw).not.toHaveBeenCalled();
      expect(result.summary.probeSetCount).toBe(1);
      expect(bpsAccessApi.query).toHaveBeenLastCalledWith(
        'one',
        { apiBase: 'http://panel.invalid', managementKey: 'TEST' },
        'bound'
      );
      const stored = serializeCodexInspectionLastRun({ result });
      expect(JSON.stringify(stored)).not.toContain('PAGE_ONLY');
      const roundTrip = hydrateCodexInspectionLastRun(stored);
      expect(roundTrip?.result.results[0]).toMatchObject({
        provider: 'bps',
        usedPercent: null,
        action: 'keep',
        autoRecoverEligible: false,
        disabled: true,
      });
    }
  );
});
