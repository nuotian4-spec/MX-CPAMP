import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button } from '@/components/ui/Button';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  kiloDeviceApi,
  kiloDeviceError,
  type KiloDeviceInput,
  type KiloDeviceSaved,
  type KiloDeviceStarted,
  type KiloDeviceState,
} from '@/services/api/kiloDevice';
import { KiloDeviceForm } from './KiloDeviceForm';
import styles from './KiloAccountsPage.module.scss';

const egressMessages: Record<KiloDeviceSaved['egress_reason'], string> = {
  '': '共享出口当前可选；这不代表模型已验证成功。',
  shared_proxy_not_configured: '账号已保存，尚无启用共享代理，请配置正式出口。',
  shared_proxy_unverified: '账号已保存，共享代理尚待健康检查。',
  shared_proxy_unhealthy: '账号已保存，共享出口故障，请检查代理。',
  shared_proxy_capacity: '账号已保存，共享代理容量或归属等待中。',
  auto_egress_waiting: '账号已保存，等待空闲固定公网IP分配。',
  auto_egress_draining: '账号已保存，独占IP等待在途排空。',
  auto_egress_unavailable: '账号已保存，独占出口当前不可用。',
  auto_egress_storage_failed: '账号已保存，自动分配归属保存待核；请刷新出口状态。',
  specified_conflict: '指定 IP 已被占用，等待配齐。',
  capacity_wait: '等待可用 IP 容量。',
  storage_failed: '出口配置保存待核，请刷新确认。',
  auto_egress_capacity: '账号已保存，独占出口请求容量暂满。',
};

export function KiloDeviceAuth({
  scope,
  connectionRef,
  onClose,
  onSaved,
  renderConfig,
}: {
  scope: ApiClientRequestScope;
  connectionRef: string;
  onClose: () => void;
  onSaved: (accountID: string, details?: KiloDeviceSaved) => Promise<void>;
  renderConfig?: (
    start: (input: KiloDeviceInput) => Promise<void>,
    busy: boolean,
    locked: boolean
  ) => ReactNode;
}) {
  const [session, setSession] = useState<KiloDeviceStarted>();
  const [phase, setPhase] = useState<'config' | KiloDeviceState['status']>('config');
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [email, setEmail] = useState('');
  const [accountID, setAccountID] = useState('');
  const [saved, setSaved] = useState<KiloDeviceSaved>();
  const [listPending, setListPending] = useState(false);
  const [autoPoll, setAutoPoll] = useState(false);
  const [pollSequence, setPollSequence] = useState(0);
  const [verifyFirst, setVerifyFirst] = useState(false);
  const [clock, setClock] = useState(Date.now());
  const [saveUntil, setSaveUntil] = useState<number>();
  const [authUntil, setAuthUntil] = useState<number>();
  const [copied, setCopied] = useState('');
  const generation = useRef(0);
  const inFlight = useRef(false);
  const requestController = useRef<AbortController | null>(null);
  const nextPollAt = useRef(0);
  const notifiedID = useRef('');
  const live = useRef({ scope, connectionRef });
  live.current = { scope, connectionRef };
  useEffect(() => {
    generation.current += 1;
    inFlight.current = false;
    setSession(undefined);
    setPhase('config');
    setBusy('');
    setError('');
    setEmail('');
    setSaved(undefined);
    setAccountID('');
    setListPending(false);
    setAutoPoll(false);
    setVerifyFirst(false);
    setSaveUntil(undefined);
    setAuthUntil(undefined);
    setCopied('');
    nextPollAt.current = 0;
    notifiedID.current = '';
    const timer = setInterval(() => setClock(Date.now()), 1000);
    return () => {
      generation.current += 1;
      requestController.current?.abort();
      clearInterval(timer);
    };
  }, [scope, connectionRef]);

  const run = useCallback(
    async <T extends { connection_ref: string }>(
      action: string,
      request: (signal: AbortSignal) => Promise<T>,
      accept: (value: T) => Promise<void> | void
    ) => {
      if (inFlight.current) return;
      const version = generation.current;
      const current = () =>
        generation.current === version &&
        live.current.scope === scope &&
        live.current.connectionRef === connectionRef;
      const abort = new AbortController();
      requestController.current = abort;
      inFlight.current = true;
      setBusy(action);
      setError('');
      try {
        const value = await request(abort.signal);
        if (!current()) return;
        if (abort.signal.aborted) throw { status: 499 };
        if (value.connection_ref !== connectionRef) throw { status: 409 };
        await accept(value);
      } catch (failure) {
        if (current()) {
          setError(kiloDeviceError(abort.signal.aborted ? { status: 499 } : failure, action));
          setAutoPoll(false);
          if (action !== 'start') setVerifyFirst(true);
        }
      } finally {
        if (current()) {
          inFlight.current = false;
          requestController.current = null;
          setBusy('');
          if (action === 'poll') setPollSequence((sequence) => sequence + 1);
        }
      }
    },
    [scope, connectionRef]
  );

  const confirmedSaved = useCallback(
    async (id: string, details?: KiloDeviceSaved) => {
      setPhase('saved');
      setAccountID(id);
      setSaved(details);
      setAutoPoll(false);
      setVerifyFirst(false);
      if (notifiedID.current === id) return;
      notifiedID.current = id;
      const version = generation.current;
      try {
        await onSaved(id, details);
        if (version === generation.current) setListPending(false);
      } catch {
        if (version === generation.current) setListPending(true);
      }
    },
    [onSaved]
  );

  const acceptState = useCallback(
    async (state: KiloDeviceState) => {
      if (state.status === 'saved') {
        if (!state.account_id) throw new Error('保存回执缺少账号ID');
        await confirmedSaved(state.account_id);
        return;
      }
      if (state.status === 'approved') {
        if (
          state.save_expires_in === undefined ||
          state.save_expires_in < 0 ||
          state.save_expires_in > 300
        )
          throw new Error('保存窗口未确认');
        const deadline = Date.now() + state.save_expires_in * 1000;
        setSaveUntil((old) => (old === undefined ? deadline : Math.min(old, deadline)));
        setEmail(state.email || '');
        setAutoPoll(false);
      }
      if (state.status !== 'pending') setAutoPoll(false);
      setPhase(state.status);
      setVerifyFirst(false);
    },
    [confirmedSaved]
  );

  const poll = useCallback(async () => {
    if (!session || inFlight.current || Date.now() < nextPollAt.current) return;
    nextPollAt.current = Date.now() + 3000;
    await run(
      'poll',
      (signal) => kiloDeviceApi.poll(session.id, connectionRef, scope, signal),
      acceptState
    );
  }, [session, run, scope, connectionRef, acceptState]);

  useEffect(() => {
    if (!autoPoll || phase !== 'pending' || busy || !session) return;
    const timer = setTimeout(
      () => {
        if (authUntil !== undefined && Date.now() >= authUntil) {
          setAutoPoll(false);
          setError('授权期限已到，自动查询已停止；请核实服务端状态。');
          return;
        }
        void poll();
      },
      Math.max(0, nextPollAt.current - Date.now())
    );
    return () => clearTimeout(timer);
  }, [autoPoll, phase, busy, session, authUntil, poll, pollSequence]);

  const start = async (input: KiloDeviceInput) =>
    run(
      'start',
      (signal) => kiloDeviceApi.start(input, connectionRef, scope, signal),
      (result) => {
        setSession(result);
        setAuthUntil(Date.now() + result.expires_in * 1000);
        setPhase('pending');
        setAutoPoll(true);
        setSaveUntil(undefined);
        setVerifyFirst(false);
        nextPollAt.current = 0;
      }
    );
  const save = async () => {
    if (
      !session ||
      phase !== 'approved' ||
      verifyFirst ||
      saveUntil === undefined ||
      Date.now() >= saveUntil
    )
      return;
    await run(
      'save',
      (signal) => kiloDeviceApi.save(session.id, connectionRef, scope, signal),
      (result) => confirmedSaved(result.id, result)
    );
  };
  const cancel = async () => {
    if (!session) return;
    await run(
      'cancel',
      (signal) => kiloDeviceApi.cancel(session.id, connectionRef, scope, signal),
      acceptState
    );
  };
  const copy = async () => {
    if (!session) return;
    const version = generation.current;
    try {
      await navigator.clipboard.writeText(session.user_code);
      if (version === generation.current) setCopied('验证码已复制。');
    } catch {
      if (version === generation.current) setCopied('复制未完成，请选中验证码手动复制。');
    }
  };
  const remaining =
    saveUntil === undefined ? undefined : Math.max(0, Math.ceil((saveUntil - clock) / 1000));
  const terminal = ['saved', 'denied', 'expired', 'cancelled'].includes(phase);
  return (
    <section className={styles.form} aria-label="Kilo设备授权">
      {renderConfig?.(start, !!busy, phase !== 'config')}
      {!renderConfig && phase === 'config' && (
        <KiloDeviceForm busy={!!busy} onStart={start} scope={scope} connectionRef={connectionRef} />
      )}
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {session?.authorization_egress && (
        <p>
          本次授权出口：
          {session.authorization_egress.mode === 'direct'
            ? '直连'
            : session.authorization_egress.proxy_id || '已固定代理'}
        </p>
      )}
      {session && !terminal && (
        <>
          <p>
            验证码：<strong>{session.user_code}</strong>
          </p>
          <div className={styles.actions}>
            <Button variant="secondary" onClick={() => void copy()}>
              复制验证码
            </Button>
            <a href={session.verification_url} target="_blank" rel="noopener noreferrer">
              打开官方授权页面
            </a>
          </div>
          {copied && <p role="status">{copied}</p>}
        </>
      )}
      {phase === 'pending' && (
        <p role="status">等待授权。自动查询间隔至少3秒；授权批准后仍须显式保存账号。</p>
      )}
      {phase === 'approved' && (
        <>
          <p role="status">
            {verifyFirst ? '授权已批准，保存结果待核。' : '已批准，尚未保存账号。'}
            {email && `授权邮箱：${email}`}
          </p>
          <p>保存窗口剩余 {remaining ?? 0} 秒；重复查询不延长窗口，服务端期限为准。</p>
          {remaining === 0 && (
            <p role="alert">保存窗口已到期，请核实会话；本地倒计时不代表服务端已取消。</p>
          )}
          <Button disabled={!!busy || verifyFirst || !remaining} onClick={() => void save()}>
            {busy === 'save' ? '正在保存账号' : '保存授权账号'}
          </Button>
        </>
      )}
      {session && !terminal && (
        <div className={styles.actions}>
          <Button
            variant="secondary"
            disabled={!!busy || clock < nextPollAt.current}
            onClick={() => void poll()}
          >
            核实授权状态
          </Button>
          <Button variant="secondary" disabled={!!busy} onClick={() => void cancel()}>
            取消服务端授权
          </Button>
        </div>
      )}
      {verifyFirst && (
        <p className={styles.note}>先核实同一会话；仍为已批准时，才可在原窗口内显式再次保存。</p>
      )}
      {phase === 'saved' && (
        <>
          <p role="status">账号已保存。AuthID：{accountID}</p>
          {saved ? (
            <p>
              {saved.egress_ready !== undefined
                ? saved.egress_ready
                  ? '出口已配齐。'
                  : saved.egress_error === 'specified_conflict'
                    ? '出口待配齐：指定 IP 已被占用，保留指定集合等待。'
                    : saved.egress_error === 'storage_failed'
                      ? '出口配置保存待核，请刷新确认。'
                      : '出口待配齐：等待可用 IP 容量。'
                : saved.egress_mode === 'auto' && saved.egress_status === 'available'
                  ? '自动独占出口当前已分配；这不代表模型已验证成功。'
                  : egressMessages[saved.egress_reason]}
            </p>
          ) : (
            <p>保存已由服务端状态确认；出口详情请在账号列表核实。</p>
          )}
          {listPending && <p role="alert">保存已成功，账号列表待刷新。</p>}
          <p className={styles.note}>
            凭证保存与出口配齐分别确认；订阅绑定和模型可用性以各自结果为准。
          </p>
        </>
      )}
      {phase === 'denied' && <p role="status">授权已被拒绝，未保存账号。</p>}
      {phase === 'expired' && <p role="status">授权会话已过期。</p>}
      {phase === 'cancelled' && <p role="status">服务端已确认取消本次授权会话；这不是凭据撤销。</p>}
      {busy && (
        <p role="status">
          正在处理
          {busy === 'poll'
            ? '状态查询'
            : busy === 'cancel'
              ? '服务端取消'
              : busy === 'save'
                ? '保存'
                : '授权发起'}
          …
        </p>
      )}
      {!renderConfig && (
        <>
          <p className={styles.note}>
            关闭窗口只停止本地请求和轮询，不代表服务端已取消；旧会话按服务端原期限结束。
          </p>
          <Button variant="secondary" onClick={onClose}>
            关闭窗口
          </Button>
        </>
      )}
    </section>
  );
}
