import { describe, expect, it } from 'vitest';
import type { AccountRow } from '@/features/accounts/model/accountRows';
import type { KiloAccount } from '@/services/api/kilo';
import {
  includeQuotaSourceRows,
  resolveKiloQuotaSourceRow,
  buildKiloQuotaEstimateWindows,
} from './kiloQuotaEstimate';
import { accountWindowUsageRequestKey } from '@/features/accounts/model/accountWindowUsageRows';
import { resolveAccountQuotaWindowUsageAndForecast } from '@/features/accounts/model/accountQuotaWindowUsagePresentation';
import type { MonitoringAccountWindowUsageItem } from '@/services/api/usageService';
const row = (id: string, provider: string): AccountRow =>
  ({ selectionKey: id, provider, raw: { id }, accountLabel: '同名账号' }) as AccountRow;
const account = (id: string, source = 'source'): KiloAccount =>
  ({
    id,
    quota_source_summary: {
      kind: 'cpa_codex',
      credential_id: source,
      display_label: '同名账号',
      status: 'ready',
      binding_revision: 1,
    },
  }) as KiloAccount;
describe('Kilo 精确来源估算', () => {
  it('只匹配唯一 Codex AuthID，拒绝同名、错类型与身份变化', () => {
    const source = row('source', 'codex');
    expect(resolveKiloQuotaSourceRow(account('k1'), [row('other', 'codex')])).toBeUndefined();
    expect(resolveKiloQuotaSourceRow(account('k1'), [row('source', 'kilo')])).toBeUndefined();
    expect(resolveKiloQuotaSourceRow(account('k1'), [source, { ...source }])).toBeUndefined();
    expect(resolveKiloQuotaSourceRow(account('k1'), [source])).toBe(source);
    const changed = account('k1');
    changed.quota_source_summary!.status = 'source_changed';
    expect(resolveKiloQuotaSourceRow(changed, [source])).toBeUndefined();
  });
  it('只扩充查询依赖，多个 Kilo 共用来源去重且不改变可见行', () => {
    const page = [row('k1', 'kilo'), row('k2', 'kilo')];
    const source = row('source', 'codex');
    const targets = includeQuotaSourceRows(page, [account('k1'), account('k2')], [...page, source]);
    expect(targets.map((item) => item.selectionKey)).toEqual(['k1', 'k2', 'source']);
    expect(page.map((item) => item.selectionKey)).toEqual(['k1', 'k2']);
    expect(includeQuotaSourceRows(page, [account('k1', 'new')], [...page, source])).toEqual(page);
  });
});

describe('Kilo当前绑定窗口估算接线', () => {
  const source = row('source', 'codex');
  const observed = Date.parse('2026-11-14T05:00:00Z');
  const value = {
    ...account('k1'),
    quota_snapshot: {
      account_id: 'k1',
      binding_revision: 1,
      connection_ref: 'ref',
      source: { kind: 'cpa_codex', credential_id: 'source', status: 'ready' },
      query: {
        status: 'success',
        observed_at: new Date(observed).toISOString(),
        attempted_at: null,
        error_code: null,
      },
      windows: [
        {
          group: 'rate_limit',
          slot: 'primary_window',
          duration_seconds: 18000,
          remaining_percent: 75,
          used_percent: 25,
          reset_at: '2026-11-14T08:00:00Z',
          source: 'api_query',
        },
      ],
      plan: null,
      credits: { remaining: null, unit: null, source_field: null },
      stale: false,
    },
  } as KiloAccount;
  it('当前revision真实窗口复用原公式，源自身缓存无额度仍可估算', () => {
    const window = buildKiloQuotaEstimateWindows(value, source)[0];
    expect(window.quotaProgressObservedAtMs).toBe(observed);
    expect(window.limitWindowSeconds).toBe(18000);
    const key = accountWindowUsageRequestKey(
      source.selectionKey,
      window.key,
      'current',
      window.modelScope
    );
    const usage = new Map([
      [
        key,
        {
          matched: true,
          scope_match_status: 'complete',
          total_requests: 2,
          success_calls: 2,
          failure_calls: 0,
          total_tokens: 250000,
          total_cost: 0.8,
          success_rate: 1,
          last_seen_ms: observed - 1000,
          sync_status: 'ready',
          from_ms: window.fromMs!,
          to_ms: observed,
        } as MonitoringAccountWindowUsageItem,
      ],
    ]);
    expect(resolveAccountQuotaWindowUsageAndForecast(source, window, usage).forecastCost).toBe(3.2);
  });
  it('改绑、无观测、源不匹配、缺窗口保持未知', () => {
    expect(
      buildKiloQuotaEstimateWindows(
        { ...value, quota_source_summary: { ...value.quota_source_summary!, binding_revision: 2 } },
        source
      )
    ).toEqual([]);
    expect(
      buildKiloQuotaEstimateWindows(
        {
          ...value,
          quota_snapshot: {
            ...value.quota_snapshot!,
            query: { ...value.quota_snapshot!.query, observed_at: null },
          },
        },
        source
      )
    ).toEqual([]);
    expect(buildKiloQuotaEstimateWindows(value, row('other', 'codex'))).toEqual([]);
    expect(
      buildKiloQuotaEstimateWindows(
        { ...value, quota_snapshot: { ...value.quota_snapshot!, windows: [] } },
        source
      )
    ).toEqual([]);
  });
  it('不同绑定版本窗口键隔离，不改变实际来源身份', () => {
    const first = buildKiloQuotaEstimateWindows(value, source)[0];
    const next = buildKiloQuotaEstimateWindows(
      {
        ...value,
        quota_source_summary: { ...value.quota_source_summary!, binding_revision: 2 },
        quota_snapshot: { ...value.quota_snapshot!, binding_revision: 2 },
      },
      source
    )[0];
    expect(first.key).not.toBe(next.key);
    expect(first.source).toBe('codex');
    expect(next.source).toBe('codex');
  });
});
