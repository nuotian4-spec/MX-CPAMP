import type { AccountQuotaDisplayWindow } from '@/features/accounts/model/accountQuotaDisplayWindows';
import type { KiloAccount } from '@/services/api/kilo';
import type { AccountRow } from '@/features/accounts/model/accountRows';

export function resolveKiloQuotaSourceRow(
  account: KiloAccount | undefined,
  rows: AccountRow[]
): AccountRow | undefined {
  const source = account?.quota_source_summary;
  if (
    source?.kind !== 'cpa_codex' ||
    !source.credential_id ||
    !['ready', 'refresh_required', 'needs_sign_in'].includes(source.status)
  )
    return undefined;
  const matches = rows.filter(
    (row) => row.provider === 'codex' && row.raw.id === source.credential_id
  );
  return matches.length === 1 ? matches[0] : undefined;
}

export function includeQuotaSourceRows(
  pageRows: AccountRow[],
  accounts: KiloAccount[],
  rows: AccountRow[]
): AccountRow[] {
  const result = new Map(pageRows.map((row) => [row.selectionKey, row]));
  for (const row of pageRows) {
    if (row.provider !== 'kilo') continue;
    const source = resolveKiloQuotaSourceRow(
      accounts.find((account) => account.id === row.raw.id),
      rows
    );
    if (source) result.set(source.selectionKey, source);
  }
  return [...result.values()];
}

export function buildKiloQuotaEstimateWindows(
  account: KiloAccount | undefined,
  source: AccountRow | undefined
): AccountQuotaDisplayWindow[] {
  const view = account?.quota_snapshot;
  const binding = account?.quota_source_summary;
  if (
    !account ||
    !source ||
    !view ||
    !binding ||
    source.provider !== 'codex' ||
    binding.kind !== 'cpa_codex' ||
    view.source.kind !== 'cpa_codex' ||
    source.raw.id !== binding.credential_id ||
    view.source.credential_id !== binding.credential_id ||
    view.binding_revision !== binding.binding_revision ||
    !['ready', 'refresh_required', 'needs_sign_in'].includes(view.source.status) ||
    !view.query.observed_at
  )
    return [];
  const observed = Date.parse(view.query.observed_at);
  if (!Number.isFinite(observed)) return [];
  return view.windows
    .filter((window) => window.group === 'rate_limit')
    .map((window) => {
      const duration = window.duration_seconds;
      const reset = window.reset_at ? Date.parse(window.reset_at) : NaN;
      const knownBoundary =
        duration !== null && duration > 0 && Number.isFinite(duration) && Number.isFinite(reset);
      const remaining = window.remaining_percent;
      const used = window.used_percent ?? (remaining === null ? null : 100 - remaining);
      return {
        key: ['kilo', account.id, view.binding_revision, window.group, window.slot].join(':'),
        label:
          duration === null
            ? '时长未知'
            : duration % 86400 === 0
              ? duration / 86400 + ' 天'
              : duration % 3600 === 0
                ? duration / 3600 + ' 小时'
                : duration + ' 秒',
        kind: duration === 18000 ? 'five_hour' : duration === 604800 ? 'weekly' : 'unknown',
        remainingPercent: remaining,
        usedPercent: used,
        resetLabel: window.reset_at ?? '-',
        resetAccuracy: knownBoundary ? 'exact' : 'unknown',
        limitWindowSeconds: duration,
        resetAtMs: knownBoundary ? reset : null,
        fromMs: knownBoundary ? reset - duration * 1000 : null,
        toMs: knownBoundary ? Math.min(observed, reset) : null,
        source: 'codex',
        observationSource: 'api_query',
        observedAtMs: observed,
        quotaProgressObservedAtMs: observed,
        windowMode: knownBoundary ? 'fixed' : 'unknown',
        cycleStartMs: knownBoundary ? reset - duration * 1000 : null,
        cycleEndMs: knownBoundary ? reset : null,
        modelScope: { kind: 'all', complete: true },
      };
    });
}
