import type { KiloAccount } from '@/services/api/kilo';
import { kiloQuotaSourceLabel } from '@/services/api/kiloQuota';
import { KiloQuotaWindow } from './KiloQuotaWindow';
import styles from './KiloAccountsPage.module.scss';

export function KiloQuotaSummary({
  account,
  sharedSource = false,
}: {
  account: KiloAccount;
  sharedSource?: boolean;
}) {
  const source = account.quota_source_summary;
  const view = account.quota_snapshot;
  const valid =
    source &&
    view &&
    source.binding_revision === view.binding_revision &&
    source.kind === view.source.kind &&
    (source.kind !== 'cpa_codex' || source.credential_id === view.source.credential_id);
  const status = valid ? view.source.status : source?.status;
  const windows =
    valid && !['source_missing', 'source_changed', 'needs_reimport'].includes(view.source.status)
      ? view.windows.filter((window) => window.group === 'rate_limit')
      : [];
  return (
    <section className={styles.quotaSummary} aria-label="Codex订阅额度">
      {!source && <small>额度来源未读取</small>}
      <div className={styles.quotaWindows}>
        {windows.map((window) => (
          <KiloQuotaWindow
            key={`${window.group}:${window.slot}`}
            label={
              window.duration_seconds === null
                ? '时长未知'
                : window.duration_seconds % 86400 === 0
                  ? `${window.duration_seconds / 86400} 天`
                  : window.duration_seconds % 3600 === 0
                    ? `${window.duration_seconds / 3600} 小时`
                    : `${window.duration_seconds} 秒`
            }
            percent={window.remaining_percent}
            resetAt={window.reset_at}
          />
        ))}
        {!windows.length && <KiloQuotaWindow label="窗口未知" percent={null} resetAt={null} />}
      </div>
      {!windows.length && <small>暂无窗口观测</small>}
      {source && source.kind !== 'none' && status !== 'ready' && (
        <small>
          {status ? kiloQuotaSourceLabel[status] : '来源状态未知'}
          {windows.length ? ' · 历史观测' : ' · 待观测'}
        </small>
      )}
      {sharedSource && <span className={styles.srOnly}>共享额度来源</span>}
      {valid && (view.stale || ['failed', 'running'].includes(view.query.status)) && (
        <small>
          {view.query.status === 'running'
            ? '查询中 · '
            : view.query.status === 'failed'
              ? '查询失败 · '
              : ''}
          {view.stale ? '陈旧观测 · ' : ''}更新：
          {view.query.observed_at ? new Date(view.query.observed_at).toLocaleString() : '尚未查询'}
        </small>
      )}
    </section>
  );
}
