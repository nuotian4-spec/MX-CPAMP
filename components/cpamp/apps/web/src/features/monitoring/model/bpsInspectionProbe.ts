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
    const result = await bpsAccessApi.query(account.authIndex ?? '', scope);
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
  return base;
};
