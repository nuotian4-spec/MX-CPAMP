import { credentialConcurrencyApi } from './credentialConcurrency';
import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';
export interface CpaBpsQuotaBucket {
  window: string;
  remainingFraction: number | null;
  resetTime: string;
  description: string;
}
export interface CpaBpsQuota {
  subscription: { plan: string; tierName: string } | null;
  summary: Array<{
    key: string;
    label: string;
    value: number | null;
    unit: string;
    format: string;
    currency: string;
  }>;
  groups: Array<{ displayName: string; buckets: CpaBpsQuotaBucket[] }>;
}
const record = (value: unknown): Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
const text = (value: unknown) => (typeof value === 'string' ? value : '');
const number = (value: unknown) =>
  typeof value === 'number' && Number.isFinite(value) ? value : null;
export function normalizeCpaBpsQuota(value: unknown): CpaBpsQuota {
  const raw = record(value),
    subscription = record(raw.subscription);
  return {
    subscription: Object.keys(subscription).length
      ? { plan: text(subscription.plan), tierName: text(subscription.tierName) }
      : null,
    summary: Array.isArray(raw.summary)
      ? raw.summary.map(record).map((item) => ({
          key: text(item.key),
          label: text(item.label),
          value: number(item.value),
          unit: text(item.unit),
          format: text(item.format),
          currency: text(item.currency),
        }))
      : [],
    groups: Array.isArray(raw.groups)
      ? raw.groups.map(record).map((group) => ({
          displayName: text(group.displayName),
          buckets: Array.isArray(group.buckets)
            ? group.buckets.map(record).map((bucket) => {
                const remaining = number(bucket.remainingFraction);
                return {
                  window: text(bucket.window),
                  remainingFraction:
                    remaining !== null && remaining >= 0 && remaining <= 1 ? remaining : null,
                  resetTime: text(bucket.resetTime),
                  description: text(bucket.description),
                };
              })
            : [],
        }))
      : [],
  };
}
export interface CpaBpsQuotaResult {
  provider: 'cpa-bps';
  source: 'cpa_bps_plugin_quota';
  auth_index: string;
  auth_id: string;
  queried_at: string;
  quota: CpaBpsQuota;
}
export const cpaBpsQuotaApi = {
  async query(
    authIndex: string,
    scope: ApiClientRequestScope,
    authId?: string
  ): Promise<CpaBpsQuotaResult> {
    if (!authIndex.trim()) throw new Error('缺少插件凭证索引，请刷新凭证列表');
    const binding = await credentialConcurrencyApi.list(scope);
    const matches =
      binding.mode === 'local'
        ? binding.credentials.filter(
            (item) =>
              item.provider === 'cpa-bps' &&
              item.auth_index === authIndex &&
              (!authId || item.auth_id === authId)
          )
        : [];
    if (binding.mode !== 'local' || !binding.connection_ref || matches.length !== 1)
      throw new Error('插件凭证或连接已变化，请刷新凭证列表');
    const expectedId = matches[0].auth_id;
    const raw = await apiClient.get<CpaBpsQuotaResult>('/cpa-bps/quota', {
      ...createScopedApiRequestConfig(scope),
      baseURL: normalizeApiBase(scope.apiBase) + '/api',
      params: {
        auth_index: authIndex,
        auth_id: expectedId,
        connection_ref: binding.connection_ref,
      },
    });
    if (
      raw.provider !== 'cpa-bps' ||
      raw.source !== 'cpa_bps_plugin_quota' ||
      raw.auth_index !== authIndex ||
      raw.auth_id !== expectedId ||
      !Number.isFinite(Date.parse(raw.queried_at))
    )
      throw new Error('插件额度回执与凭证不匹配');
    return { ...raw, quota: normalizeCpaBpsQuota(raw.quota) };
  },
};
