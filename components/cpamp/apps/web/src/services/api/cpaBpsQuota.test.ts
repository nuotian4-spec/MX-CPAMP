import {
  buildPluginConfigPatch,
  createPluginConfigDraft,
} from '@/features/plugins/pluginConfigDraft';
import { apiClient } from './client';
import { credentialConcurrencyApi } from './credentialConcurrency';
import { cpaBpsQuotaApi } from './cpaBpsQuota';
vi.mock('./client', async (original) => ({
  ...(await original<typeof import('./client')>()),
  apiClient: { get: vi.fn() },
}));
vi.mock('./credentialConcurrency', () => ({ credentialConcurrencyApi: { list: vi.fn() } }));
import { describe, it, expect, vi } from 'vitest';
import { normalizeCpaBpsQuota } from './cpaBpsQuota';
import {
  normalizeCodexInspectionTargetTypes,
  DEFAULT_CODEX_INSPECTION_SETTINGS,
} from '@/features/monitoring/model/codexInspectionSettings';
import { getInspectionProbePresentation } from '@/features/monitoring/model/codexInspectionPresentation';
describe('cpa-bps插件额度契约', () => {
  it('保持SDK比例单位与真实零值', () => {
    const q = normalizeCpaBpsQuota({
      subscription: { plan: 'Team' },
      summary: [{ key: 'balance', label: '余额', value: 0, format: 'currency', currency: 'USD' }],
      groups: [
        {
          displayName: 'BPS',
          buckets: [
            { window: 'daily', remainingFraction: 0.25, resetTime: '2026-11-13T00:00:00Z' },
            { window: 'empty', remainingFraction: 0 },
          ],
        },
      ],
    });
    expect(q.subscription?.plan).toBe('Team');
    expect(q.summary[0].value).toBe(0);
    expect(q.groups[0].buckets.map((b) => b.remainingFraction)).toEqual([0.25, 0]);
  });
  it('缺值、字符串和越界比例保持未知', () => {
    const q = normalizeCpaBpsQuota({
      groups: [
        {
          buckets: [
            {},
            { remainingFraction: null },
            { remainingFraction: '0.5' },
            { remainingFraction: 2 },
          ],
        },
      ],
    });
    expect(q.groups[0].buckets.map((b) => b.remainingFraction)).toEqual([null, null, null, null]);
  });
  it('空响应不伪造额度', () => {
    expect(normalizeCpaBpsQuota({})).toMatchObject({ summary: [], groups: [] });
  });
  it('巡检可明确选择插件但默认不开启', () => {
    expect(normalizeCodexInspectionTargetTypes(['cpa-bps'])).toEqual(['cpa-bps']);
    expect(DEFAULT_CODEX_INSPECTION_SETTINGS.targetTypes).toEqual(['codex']);
  });
  it('巡检来源不是Codex用量', () => {
    expect(
      getInspectionProbePresentation({ provider: 'cpa-bps', statusCode: 200, errorKind: '' }).source
    ).toBe('cpa_bps_quota');
  });
});

describe('插件配置与认证路由', () => {
  it('models JSON只提交models，不改auto_discover和none后缀', () => {
    const fields = [{ name: 'models', type: 'array', enumValues: [], description: '' }];
    const draft = createPluginConfigDraft(
      fields,
      { models: [], auto_discover: false, model_suffix: 'none' },
      true
    );
    draft.values.models = '["fixture-model"]';
    draft.touched.add('models');
    expect(buildPluginConfigPatch(draft, fields, (key) => key)).toEqual({
      patch: { models: ['fixture-model'] },
      errors: {},
    });
  });
  it('先绑定当前凭证再查询并传递连接引用', async () => {
    vi.mocked(credentialConcurrencyApi.list).mockResolvedValue({
      mode: 'local',
      connection_ref: 'ref',
      credentials: [{ provider: 'cpa-bps', auth_id: 'a', auth_index: 'i' }],
    } as Awaited<ReturnType<typeof credentialConcurrencyApi.list>>);
    vi.mocked(apiClient.get).mockResolvedValue({
      provider: 'cpa-bps',
      source: 'cpa_bps_plugin_quota',
      auth_id: 'a',
      auth_index: 'i',
      queried_at: '2026-11-12T00:00:00Z',
      quota: {},
    });
    const scope = { apiBase: 'https://panel.invalid', managementKey: 'key' };
    await cpaBpsQuotaApi.query('i', scope, 'a');
    expect(apiClient.get).toHaveBeenCalledWith(
      '/cpa-bps/quota',
      expect.objectContaining({
        baseURL: 'https://panel.invalid/api',
        params: { auth_id: 'a', auth_index: 'i', connection_ref: 'ref' },
        headers: { Authorization: 'Bearer key' },
      })
    );
  });
  it('旧bps身份不进入插件额度查询', async () => {
    vi.mocked(apiClient.get).mockClear();
    vi.mocked(credentialConcurrencyApi.list).mockResolvedValue({
      mode: 'local',
      connection_ref: 'ref',
      credentials: [{ provider: 'bps', auth_id: 'a', auth_index: 'i' }],
    } as Awaited<ReturnType<typeof credentialConcurrencyApi.list>>);
    await expect(
      cpaBpsQuotaApi.query('i', { apiBase: 'https://panel.invalid', managementKey: 'key' }, 'a')
    ).rejects.toThrow('插件凭证');
    expect(apiClient.get).not.toHaveBeenCalled();
  });
});
