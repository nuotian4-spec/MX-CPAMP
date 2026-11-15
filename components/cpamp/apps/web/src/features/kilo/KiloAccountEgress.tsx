import { useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/Button';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { KiloAccount } from '@/services/api/kilo';
import { kiloProxiesApi, kiloProxyError, type KiloProxyState } from '@/services/api/kiloProxies';
import { KiloEgressForm } from './KiloProxyForms';
import { KiloEgressSummary } from './KiloEgressSummary';
import styles from './KiloAccountsPage.module.scss';

export function KiloAccountEgress({
  account,
  scope,
  connectionRef,
}: {
  account: KiloAccount;
  scope: ApiClientRequestScope;
  connectionRef: string;
}) {
  const [snapshot, setSnapshot] = useState<KiloProxyState>();
  const [refresh, setRefresh] = useState(0);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [stale, setStale] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const abortRef = useRef<AbortController | undefined>(undefined);
  const writing = useRef(false);
  useEffect(() => {
    const abort = new AbortController();
    abortRef.current = abort;
    setSnapshot(undefined);
    setEditing(false);
    setError('');
    setMessage('');
    setBusy(false);
    writing.current = false;
    void kiloProxiesApi
      .list(scope, abort.signal)
      .then((state) => {
        if (abort.signal.aborted) return;
        if (state.connection_ref !== connectionRef) throw new Error();
        setSnapshot(state);
        setStale(false);
      })
      .catch(() => {
        if (!abort.signal.aborted) {
          setError('出口读取失败，请刷新核实。');
          setStale(true);
        }
      });
    return () => abort.abort();
  }, [scope, connectionRef, refresh, account.id]);
  const egress = snapshot?.accounts.find((item) => item.account_id === account.id);
  return (
    <section className={styles.form} aria-label="账号出口配置">
      <h3>正式推理出口</h3>
      <KiloEgressSummary
        accountID={account.id}
        identity={account.client_identity}
        snapshot={snapshot}
        stale={stale}
      />
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      <div className={styles.actions}>
        <Button
          variant="secondary"
          disabled={busy}
          onClick={() => setRefresh((value) => value + 1)}
        >
          刷新出口
        </Button>
        <Button disabled={busy || !snapshot || stale} onClick={() => setEditing(true)}>
          调整出口配置
        </Button>
      </div>
      {egress?.capacity_mode === 'ip' && (
        <p className={styles.note}>
          配置上界 {egress.configured_capacity ?? '未知'} · 当前容量{' '}
          {egress.current_capacity ?? '未知'} · 人工账号限额 {egress.manual_limit ?? '未设置'} ·
          最终有效上限 {egress.effective_limit ?? '未知'}
        </p>
      )}
      {editing && snapshot && (
        <KiloEgressForm
          account={account}
          snapshot={snapshot}
          busy={busy || stale}
          onClose={() => setEditing(false)}
          onSave={async (input) => {
            const abort = abortRef.current;
            if (!abort || abort.signal.aborted || writing.current) return;
            writing.current = true;
            setBusy(true);
            setError('');
            setMessage('');
            try {
              const result = await kiloProxiesApi.egress(
                account.id,
                input,
                snapshot,
                scope,
                abort.signal
              );
              if (abort.signal.aborted) return;
              if (result.connection_ref !== connectionRef) throw { status: 409 };
              setSnapshot(result);
              setEditing(false);
              const current = result.accounts.find((item) => item.account_id === account.id);
              setMessage(
                current?.egress_ready === true
                  ? '出口配置已保存，出口已配齐。'
                  : '出口配置已保存，配齐状态请核对当前出口摘要。'
              );
            } catch (failure) {
              if (!abort.signal.aborted) {
                setError(kiloProxyError(failure));
                setStale(true);
              }
            } finally {
              if (!abort.signal.aborted) {
                writing.current = false;
                setBusy(false);
              }
            }
          }}
        />
      )}
    </section>
  );
}
