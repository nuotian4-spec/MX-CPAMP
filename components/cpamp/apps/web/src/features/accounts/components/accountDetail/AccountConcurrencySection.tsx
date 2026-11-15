import { useMemo } from 'react';
import { Button } from '@/components/ui/Button';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { AccountRow } from '@/features/accounts/model/accountRows';
import { matchAccountConcurrency } from '@/features/accounts/model/credentialConcurrencyIdentity';
import type { CredentialConcurrencyState } from '@/features/accounts/hooks/useCredentialConcurrencySnapshot';
import {
  CredentialConcurrencyEditor,
  type ConcurrencyEditorState,
} from '../CredentialConcurrencyEditor';
import styles from '@/features/accounts/AccountsPage.module.scss';

export function AccountConcurrencySection({
  row,
  scope,
  disabled,
  onStateChange,
  resetVersion,
  concurrency,
}: {
  row: AccountRow;
  scope: ApiClientRequestScope;
  disabled: boolean;
  onStateChange?: (state: ConcurrencyEditorState) => void;
  resetVersion?: number;
  concurrency: CredentialConcurrencyState;
}) {
  const { snapshot, error, busy, load, refresh } = concurrency;
  const match = useMemo(
    () => (snapshot?.mode === 'local' ? matchAccountConcurrency(row, snapshot.credentials) : null),
    [row, snapshot]
  );
  return (
    <section className={styles.configurationSection} aria-label="凭证并发">
      <h3 className={styles.configurationSectionTitle}>凭证并发</h3>
      <p>满额最多等待30秒，超时返回429。保存并发仅提交此设置。</p>
      <Button variant="secondary" size="sm" onClick={() => void refresh()} disabled={busy}>
        {busy ? '正在刷新' : '刷新状态'}
      </Button>
      {error && <p role="alert">{error}</p>}
      {!snapshot && !error && <p role="status">正在读取并发状态</p>}
      {snapshot?.mode === 'home' && <p>并发由Home管理，此处仅查看模式说明。</p>}
      {match?.status === 'identity-missing' && (
        <p role="status">凭证身份待确认，暂未关联并发设置。</p>
      )}
      {match?.status === 'missing' && <p role="status">当前连接中未找到该凭证的并发状态。</p>}
      {match?.status === 'ambiguous' && (
        <p role="alert">凭证身份匹配不唯一，请刷新账号列表后核对。</p>
      )}
      {snapshot?.mode === 'local' && match?.status === 'matched' && (
        <CredentialConcurrencyEditor
          key={JSON.stringify([
            snapshot.connection_ref,
            match.credential.auth_id,
            match.credential.auth_index,
            match.credential.provider,
          ])}
          credential={match.credential}
          defaultLimit={snapshot.default_limit}
          connectionRef={snapshot.connection_ref}
          scope={scope}
          onSaved={load}
          onStateChange={onStateChange}
          resetVersion={resetVersion}
          disabled={disabled}
        />
      )}
    </section>
  );
}
