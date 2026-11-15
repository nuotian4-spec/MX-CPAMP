import { useRef, useState } from 'react';
import { Button } from '@/components/ui/Button';
import styles from './QuotaEstimateSummary.module.scss';

export interface QuotaEstimateWindow {
  key: string;
  label: string;
  cost: number | null;
  previous: boolean;
}
export function QuotaEstimateSummary({
  windows,
  sourceLabel,
  waitingReason,
  resetCount,
  quotaObservedAtMs,
  resetObservedAtMs,
  quotaError,
  resetError,
  onQueryQuota,
  onQueryReset,
  onUseReset,
  disabled = false,
  compact = false,
  onOpen,
}: {
  windows: QuotaEstimateWindow[];
  sourceLabel?: string;
  waitingReason?: string;
  resetCount?: number | null;
  quotaObservedAtMs?: number | null;
  resetObservedAtMs?: number | null;
  quotaError?: string | null;
  resetError?: string | null;
  onQueryQuota?: () => Promise<void>;
  onQueryReset?: () => Promise<void>;
  onUseReset?: () => Promise<void>;
  disabled?: boolean;
  compact?: boolean;
  onOpen?: () => void;
}) {
  const [pending, setPending] = useState('');
  const [failedAction, setFailedAction] = useState('');
  const running = useRef(false);
  const run = async (key: string, action?: () => Promise<void>) => {
    if (!action || disabled || running.current) return;
    running.current = true;
    setPending(key);
    setFailedAction('');
    try {
      await action();
    } catch {
      setFailedAction(key);
    } finally {
      running.current = false;
      setPending('');
    }
  };
  if (compact)
    return (
      <button
        type="button"
        className={styles.compact}
        aria-label="打开额度详情"
        disabled={disabled}
        onClick={(event) => {
          event.stopPropagation();
          onOpen?.();
        }}
      >
        <span>
          预计总额度{' '}
          {windows.length
            ? windows.map((window, index) => (
                <span key={window.key}>
                  {index > 0 ? ' · ' : ''}
                  {window.label}{' '}
                  <strong>
                    {window.cost === null
                      ? '待估算'
                      : `${window.previous ? '上周期 ' : '≈'}$${window.cost.toFixed(2)}`}
                  </strong>
                </span>
              ))
            : '待估算'}
        </span>
        <span>
          {resetCount !== undefined && (
            <>
              可用重置 <strong>{resetCount === null ? '未知' : `${resetCount} 次`}</strong> ·{' '}
            </>
          )}
          <span className={styles.detailLink}>额度详情 ›</span>
        </span>
      </button>
    );
  return (
    <section
      className={styles.summary}
      aria-label="预计额度摘要"
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => event.stopPropagation()}
    >
      <div className={styles.heading}>
        <strong>预计额度</strong>
        <small>根据本地用量估算</small>
      </div>
      {sourceLabel && <p className={styles.source}>同源共享 · {sourceLabel}</p>}
      <div className={styles.windows}>
        {windows.length ? (
          windows.map((window) => (
            <div key={window.key}>
              <small>{window.label}</small>
              <strong>
                {window.cost === null
                  ? '待估算'
                  : `${window.previous ? '上周期参考 ' : '≈ '}$${window.cost.toFixed(2)}`}
              </strong>
            </div>
          ))
        ) : (
          <span>待估算</span>
        )}
      </div>
      <p className={styles.source}>
        额度观测：{quotaObservedAtMs ? new Date(quotaObservedAtMs).toLocaleString() : '尚无观测'}
      </p>
      {(quotaError || failedAction === 'quota') && (
        <p role="alert" className={styles.source}>
          额度查询失败，保留上次观测。
        </p>
      )}
      {waitingReason && <p className={styles.source}>{waitingReason}</p>}
      {resetCount !== undefined && (
        <p className={styles.reset}>
          可用重置次数 <strong>{resetCount === null ? '未知' : resetCount}</strong>
        </p>
      )}
      {resetCount !== undefined && (
        <p className={styles.source}>
          次数观测：{resetObservedAtMs ? new Date(resetObservedAtMs).toLocaleString() : '尚无观测'}
        </p>
      )}
      {(resetError || failedAction === 'reset') && (
        <p role="alert" className={styles.source}>
          重置次数查询失败，{resetCount == null ? '次数仍为未知。' : '保留上次观测。'}
        </p>
      )}
      <div className={styles.actions}>
        {onQueryQuota && (
          <Button
            size="sm"
            variant="secondary"
            disabled={disabled || !!pending}
            onClick={() => void run('quota', onQueryQuota)}
          >
            {pending === 'quota' ? '查询中…' : '查询额度'}
          </Button>
        )}
        {onQueryReset && (
          <Button
            size="sm"
            variant="secondary"
            disabled={disabled || !!pending}
            onClick={() => void run('reset', onQueryReset)}
          >
            {pending === 'reset' ? '查询中…' : '查询重置次数'}
          </Button>
        )}
        {onUseReset && (
          <Button
            size="sm"
            variant="secondary"
            disabled={
              disabled ||
              !!pending ||
              resetCount === null ||
              resetCount === undefined ||
              resetCount <= 0
            }
            onClick={() => void run('use', onUseReset)}
          >
            确认使用重置
          </Button>
        )}
      </div>
      {failedAction === 'use' && (
        <p role="alert">重置结果待核，请在来源账号详情核实，勿重复使用。</p>
      )}
    </section>
  );
}
