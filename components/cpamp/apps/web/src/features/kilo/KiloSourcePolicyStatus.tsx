import { kiloQuotaFailure, type KiloSourcePolicy } from '@/services/api/kiloQuota';
import styles from './KiloAccountsPage.module.scss';

const labels: Record<KiloSourcePolicy['status'], string> = {
  disabled: '来源额度策略已关闭',
  pending: '来源策略待核',
  ready: '来源策略当前未暂停',
  paused: '来源额度策略停调',
  source_pending: '来源待核',
  query_failed: '来源查询失败',
  waiting_query_egress: '等待查询出口',
  storage_failed: '来源策略保存待核',
};
const scopes: Record<KiloSourcePolicy['scope'], string> = {
  '': '窗口范围待确认',
  '18000+604800': '5小时＋7天主窗口',
  '604800': '7天主窗口',
  '2592000': '30天主窗口',
};
export function KiloSourcePolicyStatus({ policy }: { policy?: KiloSourcePolicy }) {
  if (!policy) return <span>来源策略状态未知</span>;
  return (
    <div className={styles.sourcePolicyStatus} aria-label="来源额度停调策略状态">
      <strong>{labels[policy.status]}</strong>
      {policy.paused && <small>来源策略暂停仍保留；等待有效观测或显式关闭本策略。</small>}
      {policy.enabled && (
        <>
          <small>
            {scopes[policy.scope]} · 服务端每{policy.interval_seconds}秒查询
          </small>
          <small>
            最近观测：{policy.observed_at ? new Date(policy.observed_at).toLocaleString() : '未知'}
          </small>
          <small>
            下次查询：
            {policy.next_query_at ? new Date(policy.next_query_at).toLocaleString() : '待安排'}
          </small>
        </>
      )}
      {!policy.persisted && (
        <small role="alert">运行状态保存待核，出口释放结果请重新读取确认。</small>
      )}
      {policy.error_code && (
        <small role="alert">
          {policy.error_code === 'query_egress_unavailable'
            ? '恢复待核 · 等待查询出口'
            : policy.error_code === 'storage_failed'
              ? '来源策略保存失败'
              : kiloQuotaFailure(policy.error_code)}
        </small>
      )}
    </div>
  );
}
