import type { KiloAccount } from '@/services/api/kilo';
import { kiloQuotaFailure } from '@/services/api/kiloQuota';
import { KiloSourcePolicyStatus } from './KiloSourcePolicyStatus';
export function KiloRecoveryStatus({ account }: { account: KiloAccount }) {
  return (
    <>
      <KiloExecutionRestriction account={account} />
      <KiloSourcePolicyStatus policy={account.source_policy} />
    </>
  );
}
function KiloExecutionRestriction({ account }: { account: KiloAccount }) {
  const state = account.auto_restriction;
  if (!state) return <span>自动限制状态未知</span>;
  const reason = {
    none: '无自动限制',
    quota_exhausted: '额度耗尽限制',
    account_restricted: '账号受限',
  }[state.reason];
  const query = {
    never: '恢复时间未知',
    running: '正在查询恢复时间',
    success: '恢复时间查询成功',
    failed: '恢复时间查询失败',
    cancelled: '恢复时间查询已取消',
    no_source: '未绑定额度来源，恢复时间未知',
    no_future_reset: '未取得未来重置时间',
  }[state.query_status];
  return (
    <div aria-label="自动限制状态">
      <strong>{reason}</strong>
      {state.reason !== 'none' && (
        <>
          <small>{query}</small>
          <small>
            {state.recover_at
              ? `服务端计划解除时间：${state.recover_at}；是否已解除请刷新确认。`
              : '尚无自动解除计划'}
          </small>
        </>
      )}
      {!state.persisted && (
        <small role="alert">当前限制或恢复状态尚未持久化，请刷新确认保存结果。</small>
      )}
      {state.error_code && (
        <small role="alert">
          {state.error_code === 'storage_failed'
            ? '恢复状态保存失败；当前限制仍以服务端为准。'
            : kiloQuotaFailure(state.error_code)}
        </small>
      )}
      <details>
        <summary>限制诊断</summary>
        <small>
          状态代次 {state.generation} · 身份修订 {state.identity_revision} · 来源修订{' '}
          {state.source_revision}
        </small>
      </details>
    </div>
  );
}
