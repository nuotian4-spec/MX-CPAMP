import type { KiloProxyState } from '@/services/api/kiloProxies';
import type { KiloClientIdentity } from '@/services/api/kilo';
import { KiloAllocationStatus } from './KiloAutoEgress';
import { KiloIdentityExitLabel } from './KiloClientIdentity';
import styles from './KiloAccountsPage.module.scss';
export function KiloEgressSummary({
  accountID,
  identity,
  snapshot,
  readAt,
  stale = false,
}: {
  accountID: string;
  identity?: KiloClientIdentity | null;
  snapshot?: KiloProxyState;
  readAt?: number;
  stale?: boolean;
}) {
  if (!snapshot) return <span>出口状态未知</span>;
  const policy = snapshot.accounts.find((account) => account.account_id === accountID);
  if (policy?.mode === 'direct')
    return (
      <span>
        正式出口：直连 · <KiloIdentityExitLabel identity={identity} kind="direct" />
        {stale ? ' · 快照陈旧' : ''}
        {readAt ? ` · 出口快照 ${new Date(readAt).toLocaleTimeString()}` : ''}
      </span>
    );
  const assigned = policy?.assigned_ips ?? (policy?.assigned_ip ? [policy.assigned_ip] : []);
  const candidates = snapshot.proxies.filter((proxy) =>
    policy?.mode === 'auto'
      ? assigned.includes(proxy.fixed_exit_ip ?? '')
      : policy?.candidate_proxy_ids
        ? policy.candidate_proxy_ids.includes(proxy.id)
        : !proxy.account_id || proxy.account_id === accountID
  );
  return (
    <details
      className={styles.egressSummary}
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => event.stopPropagation()}
    >
      <summary>
        正式出口 ·{' '}
        {policy?.mode === 'auto'
          ? `已分配 ${assigned.length}/${policy.max_ips ?? 1} IP`
          : `${policy?.candidate_proxy_ids ? '指定' : '共享池'} ${candidates.length} 个入口`}{' '}
        {stale ? ' · 快照陈旧' : ''}
      </summary>
      {policy?.mode === 'auto' && <KiloAllocationStatus egress={policy} />}
      {policy?.capacity_mode === 'ip' && (
        <div className={styles.capacityMetrics}>
          <div>
            <small>配置上界</small>
            <strong>{policy.configured_capacity ?? '未知'}</strong>
          </div>
          <div>
            <small>当前容量</small>
            <strong>{policy.current_capacity ?? '未知'}</strong>
          </div>
          <div>
            <small>推理占用</small>
            <strong>{policy.inference_active ?? '未知'}</strong>
          </div>
          <div>
            <small>管理占用</small>
            <strong>{policy.management_active ?? '未知'}</strong>
          </div>
        </div>
      )}
      {policy?.manual_limit != null && (
        <small>
          人工账号限额 {policy.manual_limit} · 最终有效上限 {policy.effective_limit ?? '未知'}
        </small>
      )}
      {policy?.entry_limit != null && <small>每入口限额 {policy.entry_limit}</small>}
      {policy?.capacity_mode === 'ip' && (
        <small>当前容量为健康已分配 IP 可承载上界；可用空闲量还受在途占用影响。</small>
      )}
      <ul>
        {policy?.mode === 'auto'
          ? assigned.map((ip) => {
              const group = snapshot.ip_groups?.find(
                (item) => item.ip === ip && item.owner_account_id === accountID
              );
              return (
                <li key={ip}>
                  <strong>{ip}</strong>
                  <KiloIdentityExitLabel identity={identity} kind="ip" ip={ip} />
                  <small>
                    推理 {group?.inference_active ?? '未知'} · 管理{' '}
                    {group?.management_active ?? '未知'} · 容量 {group?.capacity ?? '未知'} ·{' '}
                    {group?.draining ? '排空中' : group ? '已绑定' : '状态待核'}
                  </small>
                </li>
              );
            })
          : candidates.map((proxy) => (
              <li key={proxy.id}>
                <strong>{proxy.observed_exit_ip || '公网 IP 未知'}</strong>
                <KiloIdentityExitLabel identity={identity} kind="ip" ip={proxy.observed_exit_ip} />
                <small>
                  {proxy.name || proxy.id} ·{' '}
                  {proxy.active === 0
                    ? '当前空闲'
                    : proxy.owner === accountID
                      ? `推理 ${proxy.inference_active ?? '未知'} · 管理 ${proxy.management_active ?? '未知'}`
                      : '当前未占用'}{' '}
                  · {proxy.healthy ? '健康' : '未健康'}
                </small>
              </li>
            ))}
      </ul>
      {policy?.mode === 'auto' && assigned.length === 0 && <small>等待分配固定 IP</small>}
      {readAt && <small>出口快照 {new Date(readAt).toLocaleTimeString()}</small>}
    </details>
  );
}
