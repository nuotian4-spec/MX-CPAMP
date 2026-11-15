import { useLayoutEffect, useRef } from 'react';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { IconRefreshCw } from '@/components/ui/icons';
import { QuotaProgressBar } from './QuotaProgressBar';
import {
  cpaBpsRemainingPercent,
  cpaBpsResetCount,
  formatCpaBpsGroup,
  formatCpaBpsPercent,
  formatCpaBpsPlan,
  formatCpaBpsTime,
  formatCpaBpsWindow,
  presentCpaBpsMetric,
} from '../model/cpaBpsQuotaPresentation';
import styles from './CpaBpsQuotaCard.module.scss';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { AuthFileItem } from '@/types';
import { useQuotaStore } from '@/stores/useQuotaStore';
import {
  getCpaBpsQuotaKey,
  getCpaBpsQuotaState,
  getCpaBpsQuotaScope,
} from '../model/cpaBpsQuotaState';
import { refreshCpaBpsQuota } from '../model/refreshCpaBpsQuota';

export function CpaBpsQuotaCard({
  file,
  scope,
}: {
  file: AuthFileItem;
  scope: ApiClientRequestScope;
}) {
  const identity = JSON.stringify([scope.apiBase, scope.managementKey, getCpaBpsQuotaKey(file)]);
  const current = useRef(identity);
  useLayoutEffect(() => {
    current.current = identity;
    return () => {
      current.current = '';
    };
  }, [identity]);
  const stored = useQuotaStore((state) => getCpaBpsQuotaState(state.cpaBpsQuota, file));
  const active = stored?.connectionScope === getCpaBpsQuotaScope(scope) ? stored : undefined;
  const refresh = () => refreshCpaBpsQuota(file, scope, () => current.current === identity);
  const quota = active?.result?.quota;
  const resetCount = active?.status === 'success' && quota ? cpaBpsResetCount(quota) : null;
  const loading = active?.status === 'loading';
  const plan = quota ? formatCpaBpsPlan(quota) : '';
  const hasWindows = quota?.groups.some((group) => group.buckets.length > 0);
  return (
    <section aria-label="BPS插件额度" aria-busy={loading}>
      <Card
        className={styles.card}
        title={
          <span className={styles.heading}>
            BPS 插件额度
            {plan ? (
              <span className={styles.plan} aria-label={`套餐：${plan}`}>
                {plan}
              </span>
            ) : null}
          </span>
        }
        extra={
          <Button
            variant="secondary"
            size="sm"
            onClick={refresh}
            disabled={loading}
            loading={loading}
          >
            {!loading ? <IconRefreshCw size={15} /> : null}
            {loading ? '查询中' : '查询插件额度'}
          </Button>
        }
      >
        <div className={styles.meta}>
          <span>来源：cpa-bps 插件</span>
          <span title={active?.lastQueriedAt}>
            上次查询时间：
            <time dateTime={active?.lastQueriedAt}>{formatCpaBpsTime(active?.lastQueriedAt)}</time>
          </span>
          <span className={styles.timezone}>本机时间</span>
        </div>
        <div className={styles.resetCredits} data-plugin-quota-metric="reset_cards_available">
          <span>可用重置次数</span>
          <strong>
            {resetCount === null
              ? loading
                ? '查询中'
                : active?.status === 'success'
                  ? '本次未返回次数'
                  : active?.status === 'error'
                    ? '当前次数未知'
                    : '未查询'
              : resetCount}
          </strong>
          {resetCount !== null && <span>次</span>}
          <Button variant="secondary" size="sm" onClick={refresh} disabled={loading}>
            查询重置次数
          </Button>
          <p className={styles.metricHint}>
            通过插件综合额度查询获取；与推理剩余额度不同，仅查询，不执行重置
          </p>
          {active?.status !== 'success' && quota && cpaBpsResetCount(quota) !== null && (
            <p>
              上次结果：{cpaBpsResetCount(quota)} 次 · {formatCpaBpsTime(active.lastQueriedAt)}
            </p>
          )}
        </div>
        {quota && active?.status !== 'success' && (
          <p role="status">上次额度快照 · {formatCpaBpsTime(active?.lastQueriedAt)}</p>
        )}
        {active?.error ? (
          <div className={styles.error} role="alert">
            额度未知：{active.error}
          </div>
        ) : null}
        {!quota ? (
          <div className={styles.empty} role="status">
            {loading
              ? '正在查询插件额度…'
              : active?.error
                ? active.lastQueriedAt
                  ? '上次查询时间已保留，可手动重试。'
                  : '可手动重试额度查询。'
                : '额度未知，点击查询获取插件返回。'}
          </div>
        ) : (
          <>
            {!hasWindows ? (
              <div className={styles.empty}>
                {quota.summary.length
                  ? '插件未返回额度窗口，当前剩余额度未知。'
                  : '插件未返回额度数据，当前额度未知。'}
              </div>
            ) : null}
            {quota.groups
              .filter((group) => group.buckets.length > 0)
              .map((group, groupIndex) => (
                <div className={styles.group} key={`${group.displayName}:${groupIndex}`}>
                  <h3 className={styles.groupTitle} title={group.displayName}>
                    {formatCpaBpsGroup(group.displayName)}
                  </h3>
                  <div className={styles.windows}>
                    {group.buckets.map((bucket, index) => {
                      const percent = cpaBpsRemainingPercent(bucket.remainingFraction);
                      const label = formatCpaBpsWindow(bucket.window);
                      const resetTime = formatCpaBpsTime(bucket.resetTime);
                      return (
                        <div
                          className={styles.window}
                          key={`${bucket.window}:${index}`}
                          data-plugin-quota-window={bucket.window}
                        >
                          <div className={styles.windowHeader}>
                            <span className={styles.windowLabel} title={bucket.window}>
                              {label}
                            </span>
                            <div className={styles.remaining}>
                              <span>剩余</span>
                              <strong>
                                {percent === 0 ? '已用尽 / 0%' : formatCpaBpsPercent(percent)}
                              </strong>
                            </div>
                          </div>
                          {percent === null ? (
                            <div
                              className={styles.unknownTrack}
                              aria-label={`${label}剩余额度未知`}
                            />
                          ) : (
                            <div
                              role="progressbar"
                              aria-label={`${label}剩余额度`}
                              aria-valuemin={0}
                              aria-valuemax={100}
                              aria-valuenow={percent}
                            >
                              <QuotaProgressBar
                                percent={percent}
                                highThreshold={50}
                                mediumThreshold={20}
                              />
                            </div>
                          )}
                          <div className={styles.reset} title={bucket.resetTime || undefined}>
                            <span>重置时间</span>
                            <time dateTime={resetTime === '未知' ? undefined : bucket.resetTime}>
                              {resetTime}
                            </time>
                          </div>
                          {bucket.description ? (
                            <p className={styles.description}>{bucket.description}</p>
                          ) : null}
                        </div>
                      );
                    })}
                  </div>
                </div>
              ))}
            {quota.summary.length > 0 ? (
              <dl className={styles.metrics} aria-label="插件辅助额度信息">
                {quota.summary
                  .filter((metric) => metric.key !== 'reset_cards_available')
                  .map((metric, index) => {
                    const presentation = presentCpaBpsMetric(metric);
                    return (
                      <div
                        className={styles.metric}
                        key={metric.key || index}
                        title={presentation.hint || metric.label}
                        data-plugin-quota-metric={metric.key}
                      >
                        <dt>{presentation.label}</dt>
                        <dd>
                          <strong>{presentation.value}</strong>
                          {presentation.unit ? <span>{presentation.unit}</span> : null}
                        </dd>
                        {presentation.hint ? (
                          <dd className={styles.metricHint}>{presentation.hint}</dd>
                        ) : null}
                      </div>
                    );
                  })}
              </dl>
            ) : null}
          </>
        )}
      </Card>
    </section>
  );
}
