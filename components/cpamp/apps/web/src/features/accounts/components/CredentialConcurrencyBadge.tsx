import type { CredentialConcurrencyState } from '../hooks/useCredentialConcurrencySnapshot';
import type { AccountRow } from '../model/accountRows';
import { matchAccountConcurrency } from '../model/credentialConcurrencyIdentity';

export function CredentialConcurrencyBadge({
  row,
  state,
}: {
  row: AccountRow;
  state: CredentialConcurrencyState;
}) {
  const { snapshot, error } = state;
  const match =
    snapshot?.mode === 'local' ? matchAccountConcurrency(row, snapshot.credentials) : null;
  const credential = match?.status === 'matched' ? match.credential : null;
  return (
    <span data-credential-concurrency={row.selectionKey} role="status">
      {credential ? (
        <>
          当前并发 {credential.active} / {credential.effective_limit}
          {credential.pinned_waiting > 0 && <> · 等待 {credential.pinned_waiting}</>}
          {error && ' · 状态陈旧'}
        </>
      ) : (
        <>
          当前并发未知
          {snapshot?.mode === 'home'
            ? ' · Home管理'
            : match?.status === 'ambiguous'
              ? ' · 身份待核实'
              : error
                ? ' · 读取失败'
                : ''}
        </>
      )}
    </span>
  );
}
