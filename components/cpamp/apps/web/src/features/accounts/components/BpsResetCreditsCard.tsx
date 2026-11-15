import { useLayoutEffect, useRef } from 'react';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { useQuotaStore } from '@/stores/useQuotaStore';
import type { AuthFileItem } from '@/types';
import type { ApiClientRequestScope } from '@/services/api/client';
import { getBpsResetCreditsKey, refreshBpsResetCredits } from '../model/bpsResetCreditsState';
import { getCpaBpsQuotaScope } from '../model/cpaBpsQuotaState';
import { formatCpaBpsTime } from '../model/cpaBpsQuotaPresentation';
export function BpsResetCreditsCard({
  file,
  scope,
  connectionRef,
}: {
  file: AuthFileItem;
  scope: ApiClientRequestScope;
  connectionRef: string;
}) {
  const key = getBpsResetCreditsKey(file),
    connectionScope = getCpaBpsQuotaScope(scope);
  const identity = JSON.stringify([key, connectionScope, connectionRef]);
  const current = useRef(identity);
  useLayoutEffect(() => {
    current.current = identity;
    return () => {
      current.current = '';
    };
  }, [identity]);
  const stored = useQuotaStore((state) => state.bpsResetCredits[key]);
  const active =
    stored?.connectionScope === connectionScope && stored.connectionRef === connectionRef
      ? stored
      : undefined;
  const loading = active?.status === 'loading';
  const result = active?.result;
  return (
    <Card
      title="可用重置次数"
      extra={
        <Button
          variant="secondary"
          size="sm"
          disabled={loading || !connectionRef || !file.id}
          loading={loading}
          onClick={() =>
            refreshBpsResetCredits(file, scope, connectionRef, () => current.current === identity)
          }
        >
          查询重置次数
        </Button>
      }
    >
      <p role="status">
        {loading
          ? '查询中'
          : active?.status === 'known'
            ? String(result?.count) + ' 次'
            : active?.status === 'unknown'
              ? '本次未返回次数'
              : active?.status === 'error'
                ? '查询失败，当前次数未知'
                : '未查询'}
      </p>
      <p>来源：原生 BPS 独立查询 · 仅查询，不执行重置</p>
      {result && <p>查询时间：{formatCpaBpsTime(result.queried_at)}</p>}
      {active?.status !== 'known' && active?.lastKnown && (
        <p>
          上次结果：{active.lastKnown.count} 次 · {formatCpaBpsTime(active.lastKnown.queried_at)}
        </p>
      )}
    </Card>
  );
}
