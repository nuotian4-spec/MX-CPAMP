import { KiloQuotaWindow } from './KiloQuotaWindow';
import {
  kiloQuotaFailure,
  kiloQuotaSourceLabel,
  type KiloQuotaView,
} from '@/services/api/kiloQuota';
import styles from './KiloAccountsPage.module.scss';

export function KiloQuotaObservation({ view }: { view: KiloQuotaView }) {
  const groups = [...new Set(view.windows.map((window) => window.group))];
  const duration = (seconds: number | null) =>
    seconds === null
      ? '时长未知'
      : seconds === 18000
        ? '5小时'
        : seconds === 604800
          ? '7天'
          : seconds === 2592000
            ? '30天'
            : `${seconds}秒`;
  return (
    <section className={styles.quotaObservation} aria-label="额度观测">
      <h3>额度窗口</h3>
      {view.query.status === 'failed' && (
        <p role="alert">
          本次查询失败：{kiloQuotaFailure(view.query.error_code)}；旧成功观测不代表本次成功。
        </p>
      )}
      {view.query.status === 'cancelled' && (
        <p role="status">本次查询已取消；仅展示服务端返回的安全观测。</p>
      )}
      {view.query.status === 'never' && <p>尚无成功查询。</p>}
      {view.query.status === 'running' && (
        <p role="status">服务端查询进行中，请稍后读取当前观测。</p>
      )}
      {groups.length === 0 && <p>额度窗口：未知</p>}
      {groups.map((group) => (
        <div key={group}>
          <h4>
            {group === 'rate_limit'
              ? '主额度窗口'
              : `附加额度窗口 ${Number(group.split(':')[1]) + 1}`}
          </h4>
          <div className={styles.quotaWindows}>
            {view.windows
              .filter((window) => window.group === group)
              .map((window, index) => (
                <KiloQuotaWindow
                  key={`${window.slot}:${index}`}
                  label={duration(window.duration_seconds)}
                  percent={window.remaining_percent}
                  resetAt={window.reset_at}
                />
              ))}
          </div>
        </div>
      ))}
      <p className={styles.note}>
        额度观测：
        {view.query.observed_at ? new Date(view.query.observed_at).toLocaleString() : '尚无观测'}
        {view.stale ? ' · 陈旧观测' : ''}
      </p>
      <details className={styles.quotaPolicyDetails}>
        <summary>观测明细</summary>
        <p>
          来源：
          {view.source.kind === 'cpa_codex'
            ? `引用 ${view.source.credential_id}`
            : view.source.kind === 'imported'
              ? '独立导入'
              : '未绑定'}{' '}
          · {kiloQuotaSourceLabel[view.source.status]} · 绑定版本 {view.binding_revision}
        </p>
        <p>
          计划：{view.plan ?? '未知'} · {view.stale ? '陈旧观测' : '已记录观测'}
        </p>
        <p>
          最近尝试：{view.query.attempted_at ?? '未知'}
          <br />
          最近成功：{view.query.observed_at ?? '未知'}
        </p>
        <p>
          Credits：{view.credits.remaining ?? '未知'} · {view.credits.unit ?? '单位未知'} ·
          来源字段：
          {view.credits.source_field ?? '未知'}
        </p>
        <p className={styles.note}>
          这是关联Codex/ChatGPT来源的观测，不是Kilo余额、订阅资格或模型可用性证明。缺值保持未知。
        </p>
      </details>
    </section>
  );
}
