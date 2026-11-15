import { cpaBpsQuotaApi } from '@/services/api/cpaBpsQuota';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { CodexInspectionAccount, CodexInspectionResultItem } from '../codexInspection';
export async function inspectSingleCpaBpsAccount(
  account: CodexInspectionAccount,
  scope: ApiClientRequestScope
): Promise<CodexInspectionResultItem> {
  const result: CodexInspectionResultItem = {
    ...account,
    state: 'inspection_unknown',
    action: 'keep',
    actionReason: '插件额度未知；推理未验证；保留账号',
    statusCode: null,
    usedPercent: null,
    isQuota: false,
    autoRecoverEligible: false,
    error: '',
  };
  try {
    const response = await cpaBpsQuotaApi.query(
      account.authIndex ?? '',
      scope,
      account.runtimeId ?? undefined
    );
    const buckets = response.quota.groups.flatMap((group) => group.buckets);
    const known = buckets.filter((bucket) => bucket.remainingFraction !== null);
    result.quotaWindows = buckets.map((bucket, index) => ({
      id: 'cpa-bps-' + index,
      labelKey: bucket.window || '插件额度',
      usedPercent: bucket.remainingFraction === null ? null : (1 - bucket.remainingFraction) * 100,
      resetLabel: bucket.resetTime || '未知',
      resetAtMs: Number.isFinite(Date.parse(bucket.resetTime))
        ? Date.parse(bucket.resetTime)
        : null,
      resetAccuracy: bucket.resetTime ? ('exact' as const) : ('unknown' as const),
      limitWindowSeconds: null,
      modelScope: { kind: 'feature' as const, key: 'cpa_bps_plugin_quota', complete: false },
    }));
    result.quotaInventoryObserved = known.length > 0;
    result.planType = response.quota.subscription?.plan || null;
    result.statusCode = 200;
    result.state = known.length ? 'cpa_bps_quota_observed' : 'inspection_unknown';
    result.actionReason = known.length
      ? '插件额度查询成功；推理未验证；保留账号'
      : '插件未返回可用额度；推理未验证；保留账号';
    result.errorDetail = JSON.stringify({
      source: response.source,
      queried_at: response.queried_at,
      quota: response.quota,
    });
  } catch (error) {
    result.errorKind = 'cpa_bps_quota_failed';
    result.error = '插件额度查询失败；保留账号';
    result.statusCode =
      typeof (error as { status?: number }).status === 'number'
        ? (error as { status: number }).status
        : null;
  }
  return result;
}
