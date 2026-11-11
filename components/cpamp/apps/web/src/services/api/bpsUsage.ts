import { apiClient, type ApiClientRequestScope } from './client';
import { bpsRequestConfig } from './bpsAccess';
import type { AuthFileItem, CodexQuotaState, CodexQuotaWindow } from '@/types';
import { buildBpsQuotaCredentialIdentity } from '@/utils/quota/bpsIdentity';

export interface BPSUsageWindow {
  used_percent: number | null;
  limit_window_seconds: number | null;
  reset_after_seconds: number | null;
  reset_at: number | null;
}
export interface BPSUsage {
  provider: 'bps';
  auth_index: string;
  auth_id: string;
  source: 'chatgpt_account_usage';
  queried_at: string;
  upstream_status: number | null;
  error: string | null;
  plan_type: string | null;
  rate_limit: {
    allowed: boolean | null;
    limit_reached: boolean | null;
    primary_window: BPSUsageWindow | null;
    secondary_window: BPSUsageWindow | null;
  } | null;
}

export const bpsUsageWindows = (usage: BPSUsage): CodexQuotaWindow[] => {
  if (usage.error || !usage.rate_limit) return [];
  const observedAtMs = Date.parse(usage.queried_at);
  return [usage.rate_limit.primary_window, usage.rate_limit.secondary_window].flatMap(
    (window, index) => {
      if (!window) return [];
      const resetAtMs =
        window.reset_at !== null && window.reset_at > 0
          ? window.reset_at * 1000
          : window.reset_after_seconds !== null
            ? observedAtMs + window.reset_after_seconds * 1000
            : null;
      if (window.used_percent === null && resetAtMs === null) return [];
      return [
        {
          id: index === 0 ? 'bps-primary' : 'bps-secondary',
          label: '官方账号用量',
          labelKey: '官方账号用量',
          usedPercent: window.used_percent,
          resetLabel: '-',
          resetAtMs,
          resetAccuracy:
            window.reset_at !== null && window.reset_at > 0
              ? ('exact' as const)
              : resetAtMs !== null
                ? ('estimated' as const)
                : ('unknown' as const),
          limitWindowSeconds: window.limit_window_seconds,
          observedAtMs,
          quotaProgressObservedAtMs: window.used_percent !== null ? observedAtMs : undefined,
          observationSource: 'api_query' as const,
          modelScope: { kind: 'feature' as const, key: 'bps_account_usage', complete: false },
        },
      ];
    }
  );
};

export const bpsUsageApi = {
  async query(
    authIndex: string,
    scope: ApiClientRequestScope,
    connectionRef: string,
    authId?: string
  ): Promise<BPSUsage> {
    if (!authIndex.trim() || !connectionRef) throw new Error('请刷新凭证列表建立连接引用');
    const result = await apiClient.get<BPSUsage>('/bps/usage', {
      ...bpsRequestConfig(scope),
      params: {
        auth_index: authIndex,
        connection_ref: connectionRef,
        ...(authId ? { auth_id: authId } : {}),
      },
    });
    if (
      result.provider !== 'bps' ||
      result.source !== 'chatgpt_account_usage' ||
      result.auth_index !== authIndex ||
      (authId && result.auth_id !== authId) ||
      !Number.isFinite(Date.parse(result.queried_at)) ||
      (result.error !== null && (result.rate_limit !== null || result.plan_type !== null)) ||
      (result.error === null && (result.upstream_status !== 200 || !result.rate_limit))
    )
      throw new Error('官方账号用量回执无效');
    for (const window of [result.rate_limit?.primary_window, result.rate_limit?.secondary_window]) {
      if (!window) continue;
      for (const number of [
        window.used_percent,
        window.limit_window_seconds,
        window.reset_after_seconds,
        window.reset_at,
      ]) {
        if (
          number !== null &&
          (typeof number !== 'number' || !Number.isFinite(number) || number < 0)
        )
          throw new Error('官方账号用量回执无效');
      }
      if (window.used_percent !== null && window.used_percent > 100)
        throw new Error('官方账号用量回执无效');
    }
    if (result.error === null && !bpsUsageWindows(result).length)
      throw new Error('官方账号用量回执无效');
    return result;
  },
};

export const bpsUsageState = (usage: BPSUsage, file: AuthFileItem): CodexQuotaState => ({
  ...buildBpsQuotaCredentialIdentity(file),
  status: usage.error ? 'error' : 'success',
  windows: bpsUsageWindows(usage),
  planType: usage.plan_type,
  quotaInventoryObserved: usage.error === null,
  ...(usage.error
    ? {
        error: `官方账号用量：${usage.error}`,
        failedAtMs: Date.parse(usage.queried_at),
        errorStatus: usage.upstream_status ?? undefined,
      }
    : { fetchedAtMs: Date.parse(usage.queried_at) }),
});
