import type { AuthFileItem, CodexQuotaState } from '@/types';
import type { AccountInspectionSummary } from './accountCredentialEvidence';
import {
  buildBpsQuotaCredentialIdentity,
  matchesBpsInspectionIdentity,
} from '@/utils/quota/bpsIdentity';

export const buildBpsInspectionQuotaState = (
  file: AuthFileItem,
  inspection: AccountInspectionSummary | null | undefined
): CodexQuotaState | undefined => {
  if (!inspection) return undefined;
  if (!matchesBpsInspectionIdentity(file, inspection.credentialIdentity, inspection.createdAtMs))
    return undefined;
  let detail: Record<string, unknown> = {};
  try {
    detail = JSON.parse(inspection.errorDetail || '{}');
  } catch {
    /* 兼容仅有窗口的历史记录。 */
  }
  const observedAtMs =
    typeof detail.usage_queried_at === 'string'
      ? Date.parse(detail.usage_queried_at)
      : inspection.createdAtMs;
  if (!Number.isFinite(observedAtMs) || observedAtMs <= 0) return undefined;
  if (typeof detail.usage_error === 'string')
    return {
      ...buildBpsQuotaCredentialIdentity(file),
      status: 'error',
      windows: [],
      error: `官方账号用量：${detail.usage_error}`,
      failedAtMs: observedAtMs,
    };
  if (!inspection.quotaInventoryObserved || !inspection.quotaWindows?.length) return undefined;
  return {
    ...buildBpsQuotaCredentialIdentity(file),
    status: 'success',
    planType: inspection.planType,
    quotaInventoryObserved: true,
    observedAtMs,
    windows: inspection.quotaWindows.map((window) => ({
      ...window,
      label: '官方账号用量',
      resetLabel: window.resetLabel || '-',
      usedPercent: window.usedPercent ?? null,
      resetAccuracy: window.resetAccuracy === 'derived' ? 'estimated' : window.resetAccuracy,
      observationSource: 'inspection',
      observedAtMs,
      quotaProgressObservedAtMs:
        window.usedPercent !== null && window.usedPercent !== undefined ? observedAtMs : undefined,
      modelScope: { kind: 'feature', key: 'bps_account_usage', complete: false },
    })),
  };
};
