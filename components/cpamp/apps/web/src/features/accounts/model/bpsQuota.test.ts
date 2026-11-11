import { describe, expect, it } from 'vitest';
import type { TFunction } from 'i18next';
import type { AuthFileItem } from '@/types';
import { bpsUsageState, type BPSUsage } from '@/services/api/bpsUsage';
import { getBpsQuotaStoreKey } from '@/utils/quota/bpsIdentity';
import { buildAccountRows, type AccountQuotaStores } from './accountRows';
import { buildAccountQuotaDisplayWindows } from './accountQuotaDisplayWindows';
import { buildAccountQuotaWindowDefinitions } from './accountQuotaWindowDefinitions';
import {
  buildAccountQuotaSnapshotQueryAccounts,
  buildAccountQuotaSnapshotWriteEntries,
  mergeAccountQuotaSnapshotWindows,
} from './accountQuotaSnapshots';
import { selectAccountQuotaMainListWindows } from './accountsPagePresentation';
import { buildAccountDetailViewModel } from './accountDetailViewModel';
import { resolveMonitoringAccountQuotaProvider } from '@/features/monitoring/accountOverviewQuotaTargets';
import { buildAccountQuotaEntryFromProviderState } from '@/features/monitoring/model/monitoringCenterPageModel';
import { buildBpsInspectionQuotaState } from './bpsQuota';

const t = ((key: string) => key) as TFunction;
const file: AuthFileItem = {
  name: 'one.json',
  type: 'bps',
  auth_index: 'one',
  account_id: 'account-one',
  email: 'one@example.test',
};
const observed = Date.parse('2026-11-11T00:00:00Z');
const usage: BPSUsage = {
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
      reset_after_seconds: null,
      reset_at: 1796977220,
    },
    secondary_window: {
      used_percent: null,
      limit_window_seconds: 2592000,
      reset_after_seconds: null,
      reset_at: 1796977220,
    },
  },
};
const stores = (): AccountQuotaStores => ({
  antigravityQuota: {},
  claudeQuota: {},
  codexQuota: {},
  devinQuota: {},
  kimiQuota: {},
  metaQuota: {},
  xaiQuota: {},
  bpsQuota: { [getBpsQuotaStoreKey(file)]: bpsUsageState(usage, file) },
});

describe('BPS账号额度完整归并', () => {
  it('列表、卡片、详情、监控保留官方账号来源及reset-only', () => {
    const state = stores();
    const row = buildAccountRows([file], state)[0];
    expect(row.quota).toMatchObject({ remainingPercent: 83, planType: 'plus' });
    const windows = buildAccountQuotaDisplayWindows(row, {
      stores: state,
      translateQuotaWindowLabel: (label) => label || '',
      t,
      nowMs: observed,
    });
    expect(windows).toHaveLength(2);
    expect(windows[1]).toMatchObject({
      source: 'bps',
      usedPercent: null,
      modelScope: { complete: false },
    });
    expect(selectAccountQuotaMainListWindows(row, windows)).toHaveLength(2);
    const detail = buildAccountDetailViewModel(row, { t, quotaWindows: windows });
    expect(detail.quota.windows).toHaveLength(2);
    expect(resolveMonitoringAccountQuotaProvider(file)).toBe('bps');
    const entry = buildAccountQuotaEntryFromProviderState(
      {
        key: 'bps',
        provider: 'bps',
        authIndex: 'one',
        authLabel: 'one',
        fileName: file.name,
        file,
        accountId: null,
        planType: null,
      },
      state.bpsQuota![getBpsQuotaStoreKey(file)],
      t
    );
    expect(entry).toMatchObject({ provider: 'bps', metaLabels: ['官方账号用量'] });
    expect(entry?.windows[1].remainingPercent).toBeNull();
  });
  it('快照按bps写查，还原未确认模型范围；跨provider与换身份无旧额度', () => {
    const state = stores();
    const row = buildAccountRows([file], state)[0];
    const windows = buildAccountQuotaDisplayWindows(row, {
      stores: state,
      translateQuotaWindowLabel: (label) => label || '',
      t,
    });
    const definitions = buildAccountQuotaWindowDefinitions(windows);
    const writes = buildAccountQuotaSnapshotWriteEntries(
      [row],
      new Map([[row.selectionKey, definitions]])
    );
    expect(writes[0]).toMatchObject({
      provider: 'bps',
      windows: [
        { provider_window_id: 'bps-primary', used_percent: 17, source: 'api_query' },
        { provider_window_id: 'bps-secondary' },
      ],
    });
    const query = buildAccountQuotaSnapshotQueryAccounts([row])[0];
    expect(query).toMatchObject({
      provider: 'bps',
      account: {
        auth_account_id_snapshot: 'account-one',
        account_snapshot: 'one@example.test',
      },
    });
    const replacedFile = { ...file, account_id: 'account-two', email: 'two@example.test' };
    const replacedRow = buildAccountRows([replacedFile], state)[0];
    const replacedQuery = buildAccountQuotaSnapshotQueryAccounts([replacedRow])[0];
    expect(replacedQuery.account).toMatchObject({
      auth_file_snapshot: query.account.auth_file_snapshot,
      auth_index: query.account.auth_index,
      auth_account_id_snapshot: 'account-two',
      account_snapshot: 'two@example.test',
    });
    const snapshotCache = new Map([[getBpsQuotaStoreKey(file), writes[0].windows]]);
    expect(snapshotCache.get(getBpsQuotaStoreKey(replacedFile))).toBeUndefined();
    expect(
      buildAccountRows([{ ...file, type: 'codex' }], state)[0].quota.remainingPercent
    ).toBeNull();
    expect(
      buildAccountRows([{ ...file, auth_index: 'changed' }], state)[0].quota.remainingPercent
    ).toBeNull();
    expect(
      buildAccountRows([file], state, [], { bpsQuotaBySelectionKey: new Map() })[0].quota
        .remainingPercent
    ).toBeNull();
    const restored = mergeAccountQuotaSnapshotWindows(
      [],
      [{ ...writes[0].windows[0], stale: false }],
      { provider: 'bps' }
    );
    expect(restored[0]).toMatchObject({
      provider: 'bps',
      usedPercent: 17,
      modelScope: { complete: false },
    });
  });
  it('权限失败和额度成功独立；用量失败清空窗口而非0', () => {
    const inspection = {
      source: 'server' as const,
      action: 'keep',
      actionReason: '',
      actionStatus: 'none',
      statusCode: 401,
      usedPercent: 17,
      planType: 'plus',
      quotaInventoryObserved: true,
      quotaWindows: [
        {
          id: 'bps-primary',
          labelKey: '官方账号用量',
          usedPercent: 17,
          resetLabel: '-',
          limitWindowSeconds: 2592000,
        },
      ],
      errorKind: 'unauthorized',
      runId: 1,
      resultId: 1,
      createdAtMs: observed,
      credentialIdentity: {
        provider: 'bps',
        authIndex: 'one',
        accountId: 'account-one',
        accountSnapshot: 'one@example.test',
      },
    };
    expect(
      buildBpsInspectionQuotaState(
        {
          ...file,
          account_id: 'account-two',
          email: 'two@example.test',
          last_refresh: '2026-11-01T00:00:00Z',
          modtime: '2026-11-11T00:01:00Z',
        },
        inspection
      )
    ).toBeUndefined();
    expect(
      buildBpsInspectionQuotaState(file, { ...inspection, credentialIdentity: undefined })
    ).toBeUndefined();
    expect(buildBpsInspectionQuotaState(file, inspection)).toMatchObject({
      status: 'success',
      windows: [{ usedPercent: 17 }],
    });
    expect(
      buildBpsInspectionQuotaState(file, {
        ...inspection,
        errorDetail: JSON.stringify({ usage_error: 'unauthorized' }),
      })
    ).toMatchObject({ status: 'error', windows: [] });
  });
});
