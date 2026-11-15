import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  kiloQuotaApi,
  type KiloSourcePolicySnapshot,
  type KiloQuotaView,
} from '@/services/api/kiloQuota';
import { KiloSourcePolicyStatus } from './KiloSourcePolicyStatus';
import styles from './KiloAccountsPage.module.scss';

export function KiloSourcePolicyFields({
  accountID,
  scope,
  connectionRef,
  observation,
  disabled,
  onChanged,
  onBusyChange,
}: {
  accountID: string;
  scope: ApiClientRequestScope;
  connectionRef: string;
  observation?: KiloQuotaView;
  disabled: boolean;
  onChanged: () => Promise<void>;
  onBusyChange: (busy: boolean) => void;
}) {
  const [policy, setPolicy] = useState<KiloSourcePolicySnapshot>();
  const [enabled, setEnabled] = useState(false);
  const [interval, setInterval] = useState('1800');
  const [confirmScope, setConfirmScope] = useState(false);
  const [busy, setBusy] = useState(false);
  const [needsRead, setNeedsRead] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const generation = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const callbacks = useRef({ onChanged, onBusyChange });
  callbacks.current = { onChanged, onBusyChange };
  const live = useRef({ accountID, scope, connectionRef });
  live.current = { accountID, scope, connectionRef };
  const run = useCallback(
    async (write?: {
      enabled: boolean;
      interval_seconds: number;
      expected_policy_revision: number;
      confirm_scope?: boolean;
    }) => {
      if (controller.current) return;
      const version = generation.current;
      const current = () =>
        version === generation.current &&
        live.current.accountID === accountID &&
        live.current.scope === scope &&
        live.current.connectionRef === connectionRef;
      const abort = new AbortController();
      controller.current = abort;
      setBusy(true);
      callbacks.current.onBusyChange(true);
      setError('');
      setMessage('');
      try {
        const next = write
          ? await kiloQuotaApi.putPolicy(accountID, write, connectionRef, scope, abort.signal)
          : await kiloQuotaApi.getPolicy(accountID, scope, abort.signal);
        if (!current()) return;
        if (abort.signal.aborted) throw { status: 499 };
        if (
          next.connection_ref !== connectionRef ||
          !Number.isSafeInteger(next.revision) ||
          (write && next.revision !== write.expected_policy_revision + 1)
        )
          throw { status: 409 };
        setPolicy(next);
        setEnabled(next.enabled);
        setInterval(String(next.interval_seconds));
        setConfirmScope(false);
        setNeedsRead(!next.persisted);
        if (write) {
          const note = next.persisted
            ? next.enabled
              ? '来源策略已保存，判定以新观测为准。'
              : '来源策略已关闭；人工停用、Kilo实际限制和出口等待保持各自状态。'
            : '来源策略保存待核，请重新读取确认。';
          setMessage(note);
          try {
            await callbacks.current.onChanged();
          } catch {
            if (current()) setMessage(`${note}账号列表待刷新。`);
          }
        }
      } catch (failure) {
        if (current()) {
          setNeedsRead(true);
          const status = (failure as { status?: number })?.status;
          setError(
            status === 401
              ? '管理认证已失效，请重新连接。'
              : status === 409
                ? '策略版本或管理连接已变化，请重新读取策略后提交。'
                : write
                  ? '来源策略保存结果待核，请重新读取，勿直接重复提交。'
                  : '来源策略读取失败，当前状态未知；请重新读取策略。'
          );
        }
      } finally {
        if (current()) {
          controller.current = null;
          setBusy(false);
          callbacks.current.onBusyChange(false);
        }
      }
    },
    [accountID, scope, connectionRef]
  );
  useEffect(() => {
    generation.current += 1;
    controller.current = null;
    setPolicy(undefined);
    setEnabled(false);
    setInterval('1800');
    setConfirmScope(false);
    setNeedsRead(false);
    void run();
    return () => {
      generation.current += 1;
      controller.current?.abort();
      callbacks.current.onBusyChange(false);
    };
  }, [run, observation]);
  const pendingScope = policy?.enabled && ['pending', 'source_pending'].includes(policy.status);
  const blocked = disabled || busy || !policy || needsRead;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (blocked || !policy) return;
    const seconds = Number(interval);
    if (
      !/^\d+$/.test(interval) ||
      !Number.isSafeInteger(seconds) ||
      seconds < 60 ||
      seconds > 86400
    ) {
      setError('查询周期应为60至86400之间的整数秒。');
      return;
    }
    void run({
      enabled,
      interval_seconds: seconds,
      expected_policy_revision: policy.revision,
      ...(enabled && pendingScope && confirmScope ? { confirm_scope: true } : {}),
    });
  };
  return (
    <section className={styles.form} aria-label="来源额度100%停调策略">
      <h3>来源额度100%停调</h3>
      <p className={styles.note}>
        默认关闭。开启后由服务端按当前来源完整主窗口判定，关闭页面后继续查询。该策略不代表Kilo权益相同，当前未暂停也不表示推理可用。
      </p>
      <KiloSourcePolicyStatus policy={needsRead && error ? undefined : policy} />
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      <Button variant="secondary" disabled={disabled || busy} onClick={() => void run()}>
        重新读取策略
      </Button>
      <form className={styles.form} onSubmit={submit}>
        <ToggleSwitch
          label="启用来源额度100%停调"
          checked={enabled}
          onChange={setEnabled}
          disabled={blocked}
        />
        <Input
          label="服务端查询周期（秒）"
          type="number"
          min={60}
          max={86400}
          step={1}
          value={interval}
          disabled={blocked}
          onChange={(event) => setInterval(event.target.value)}
        />
        {enabled && pendingScope && (
          <ToggleSwitch
            label="确认新窗口范围，下次观测重新核实"
            checked={confirmScope}
            onChange={setConfirmScope}
            disabled={blocked}
          />
        )}
        {enabled && pendingScope && (
          <p className={styles.note}>确认范围仍保留已有来源暂停，直到完整有效的新观测确认恢复。</p>
        )}
        {!enabled && policy?.enabled && (
          <p className={styles.note}>
            保存关闭只解除本项来源策略，不清除人工停用或Kilo实际额度限制，不保证立即取得出口。
          </p>
        )}
        <Button type="submit" disabled={blocked}>
          {enabled ? '保存来源策略' : '保存关闭策略'}
        </Button>
      </form>
    </section>
  );
}
