import { bpsUsageApi, bpsUsageWindows } from '@/services/api/bpsUsage';
import { bpsAccessApi, bpsAccessMessage } from '@/services/api/bpsAccess';
import type { ApiClientRequestScope } from '@/services/api/client';
import type {
  CodexInspectionAccount,
  CodexInspectionLogHandler,
  CodexInspectionResultItem,
} from '../codexInspection';

export const inspectSingleBpsAccount = async (
  account: CodexInspectionAccount,
  scope: ApiClientRequestScope,
  onLog?: CodexInspectionLogHandler
): Promise<CodexInspectionResultItem> => {
  const base: CodexInspectionResultItem = {
    ...account,
    state: 'bps_access_unknown',
    action: 'keep',
    actionReason: 'BPS权限未确认；额度未知；推理未验证',
    statusCode: null,
    usedPercent: null,
    isQuota: false,
    autoRecoverEligible: false,
    error: '',
    quotaWindows: [],
    quotaInventoryObserved: false,
  };
  try {
    const result = await bpsAccessApi.query(
      account.authIndex ?? '',
      scope,
      String(account.raw.bps_connection_ref ?? '')
    );
    base.actionReason = bpsAccessMessage(result);
    base.statusCode = result.upstream_status;
    base.state = result.error
      ? 'bps_access_unknown'
      : result.allowed
        ? 'bps_access_allowed'
        : 'bps_access_restricted';
    base.errorKind = result.error ?? '';
    base.error = result.error ? base.actionReason : '';
    const summary = {
      allowed: result.allowed,
      queried_at: result.queried_at,
      upstream_status: result.upstream_status,
      error: result.error,
    };
    base.errorDetail = JSON.stringify(summary);
    onLog?.('info', base.actionReason, {
      provider: 'bps',
      fileName: account.fileName,
      action: 'keep',
      ...summary,
    });
  } catch {
    base.errorKind = 'bps_access_failed';
    base.error = 'BPS权限查询未确认';
  }
  try {
    const usage = await bpsUsageApi.query(
      account.authIndex ?? '',
      scope,
      String(account.raw.bps_connection_ref ?? '')
    );
    const windows = bpsUsageWindows(usage);
    base.quotaWindows = windows.map((window) => ({
      ...window,
      labelKey: '官方账号用量',
      limitWindowSeconds: window.limitWindowSeconds ?? null,
    }));
    base.quotaInventoryObserved = usage.error === null && windows.length > 0;
    base.planType = usage.plan_type ?? undefined;
    const percents = windows.flatMap((window) =>
      typeof window.usedPercent === 'number' ? [window.usedPercent] : []
    );
    base.usedPercent = percents.length ? Math.max(...percents) : null;
    base.actionReason =
      base.actionReason.replace('；额度未知', '') +
      (usage.error ? '；官方账号用量未知' : '；官方账号用量已查询');
    const detail = JSON.parse(base.errorDetail || '{}');
    base.errorDetail = JSON.stringify({
      ...detail,
      usage_source: usage.source,
      usage_queried_at: usage.queried_at,
      usage_upstream_status: usage.upstream_status,
      usage_error: usage.error,
    });
  } catch {
    base.errorDetail = JSON.stringify({
      ...JSON.parse(base.errorDetail || '{}'),
      usage_error: 'bps_usage_failed',
    });
  }
  return base;
};
