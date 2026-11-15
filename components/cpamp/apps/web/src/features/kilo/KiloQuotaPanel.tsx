import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { Button } from '@/components/ui/Button';
import { KiloQuotaSourceFields } from './KiloQuotaSourceFields';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  kiloQuotaApi,
  kiloQuotaError,
  kiloQuotaSourceLabel,
  type KiloQuotaBinding,
  type KiloQuotaSources,
  type KiloQuotaView,
} from '@/services/api/kiloQuota';
import { KiloQuotaObservation } from './KiloQuotaObservation';
import { KiloSourcePolicyFields } from './KiloSourcePolicyFields';
import type { KiloQuotaCoordinator } from './KiloQuotaCoordinator';
import styles from './KiloAccountsPage.module.scss';

export function KiloQuotaPanel({
  accountID,
  scope,
  connectionRef,
  onClose,
  onChanged,
  coordinator,
  renderEstimate,
}: {
  accountID: string;
  scope: ApiClientRequestScope;
  connectionRef: string;
  onClose: () => void;
  onChanged: () => Promise<void>;
  coordinator?: KiloQuotaCoordinator;
  renderEstimate?: (onQuery: () => Promise<void>, disabled: boolean) => ReactNode;
}) {
  const [view, setView] = useState<KiloQuotaView>();
  const [sources, setSources] = useState<KiloQuotaSources>();
  const [mode, setMode] = useState<'cpa_codex' | 'imported'>('cpa_codex');
  const [selected, setSelected] = useState('');
  const [sourceSearch, setSourceSearch] = useState('');
  const [access, setAccess] = useState('');
  const [outerID, setOuterID] = useState('');
  const [busy, setBusy] = useState('');
  const [policyBusy, setPolicyBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [needsRead, setNeedsRead] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const generation = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const currentView = useRef<KiloQuotaView | undefined>(undefined);
  const live = useRef({ accountID, scope, connectionRef });
  live.current = { accountID, scope, connectionRef };
  const run = useCallback(
    async (action: string, operation: (signal: AbortSignal) => Promise<void>) => {
      if (controller.current) return;
      const version = generation.current;
      const abort = new AbortController();
      controller.current = abort;
      const current = () =>
        version === generation.current &&
        live.current.accountID === accountID &&
        live.current.scope === scope &&
        live.current.connectionRef === connectionRef;
      setBusy(action);
      setError('');
      setMessage('');
      try {
        await operation(abort.signal);
      } catch (failure) {
        if (current()) {
          setError(kiloQuotaError(abort.signal.aborted ? { status: 499 } : failure));
          if (action !== 'sources') setNeedsRead(true);
        }
      } finally {
        if (current()) {
          controller.current = null;
          setBusy('');
        }
      }
    },
    [accountID, scope, connectionRef]
  );
  const accept = useCallback(
    (
      next: KiloQuotaView,
      version: number,
      signal: AbortSignal,
      expected?: number,
      mutation = false,
      cancelled = false
    ) => {
      if (
        version !== generation.current ||
        live.current.accountID !== accountID ||
        live.current.scope !== scope ||
        live.current.connectionRef !== connectionRef
      )
        return false;
      if (signal.aborted && !cancelled) throw { status: 499 };
      if (
        next.account_id !== accountID ||
        next.connection_ref !== connectionRef ||
        !Number.isSafeInteger(next.binding_revision)
      )
        throw { status: 409 };
      if (
        expected !== undefined &&
        (currentView.current?.binding_revision !== expected ||
          next.binding_revision !== (mutation ? expected + 1 : expected))
      )
        throw { status: 409 };
      if (
        expected === undefined &&
        currentView.current &&
        next.binding_revision < currentView.current.binding_revision
      )
        throw { status: 409 };
      coordinator?.observe(next);
      currentView.current = next;
      setView(next);
      setNeedsRead(false);
      return true;
    },
    [accountID, scope, connectionRef, coordinator]
  );
  const read = useCallback(async () => {
    const version = generation.current;
    await run('read', async (signal) => {
      const previous = coordinator?.snapshot().views[accountID];
      let next = await kiloQuotaApi.get(accountID, scope, signal);
      const latest = coordinator?.snapshot().views[accountID];
      if (latest && latest !== previous && latest.binding_revision === next.binding_revision)
        next = latest;
      if (accept(next, version, signal)) {
        try {
          const result = await kiloQuotaApi.sources(scope, signal);
          if (version !== generation.current || signal.aborted) return;
          if (result.connection_ref !== connectionRef) throw { status: 409 };
          setSources(result);
        } catch (failure) {
          if (version === generation.current && !signal.aborted) setError(kiloQuotaError(failure));
        }
      }
    });
  }, [run, accept, accountID, scope, coordinator, connectionRef]);
  useEffect(() => {
    generation.current += 1;
    controller.current = null;
    currentView.current = undefined;
    setView(undefined);
    setSources(undefined);
    setAccess('');
    setOuterID('');
    setSelected('');
    setSourceSearch('');
    setMode('cpa_codex');
    setNeedsRead(false);
    setConfirmRemove(false);
    setError('');
    setMessage('');
    setBusy('');
    void read();
    return () => {
      generation.current += 1;
      controller.current?.abort();
    };
  }, [read]);
  useEffect(() => {
    if (!coordinator) return;
    return coordinator.subscribe(() => {
      const next = coordinator.snapshot().views[accountID];
      if (
        next &&
        next !== currentView.current &&
        next.connection_ref === connectionRef &&
        (!currentView.current || next.binding_revision >= currentView.current.binding_revision)
      ) {
        currentView.current = next;
        setView(next);
      }
    });
  }, [coordinator, accountID, connectionRef]);
  const loadSources = async () => {
    const version = generation.current;
    await run('sources', async (signal) => {
      const result = await kiloQuotaApi.sources(scope, signal);
      if (version !== generation.current) return;
      if (signal.aborted) throw { status: 499 };
      if (result.connection_ref !== connectionRef) throw { status: 409 };
      setSources(result);
    });
  };
  const saved = async (version: number, description: string) => {
    setMessage(description);
    try {
      await onChanged();
    } catch {
      if (version === generation.current) setMessage(`${description}账号列表待刷新。`);
    }
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (controller.current || !view || needsRead) return;
    const secret = access;
    setAccess('');
    setError('');
    let source: KiloQuotaBinding;
    if (mode === 'cpa_codex') {
      const option = sources?.sources.find((item) => item.credential_id === selected);
      if (!option?.chatgpt_account_id) {
        setError('请选择精确AuthID并确认已知outer账号ID。');
        return;
      }
      source = {
        kind: 'cpa_codex',
        credential_id: option.credential_id,
        chatgpt_account_id: option.chatgpt_account_id,
      };
    } else {
      if (!secret.trim()) {
        setError('请填写独立access token。');
        return;
      }
      source = {
        kind: 'imported',
        access_token: secret,
        ...(outerID.trim() ? { chatgpt_account_id: outerID.trim() } : {}),
      };
    }
    const expected = view.binding_revision,
      version = generation.current;
    await run('save', async (signal) => {
      coordinator?.invalidate(accountID);
      const next = await kiloQuotaApi.put(
        accountID,
        source,
        expected,
        connectionRef,
        scope,
        signal
      );
      if (accept(next, version, signal, expected, true))
        await saved(version, '额度来源已保存，旧观测已清除。');
    });
  };
  const remove = async () => {
    if (!view || needsRead) return;
    const expected = view.binding_revision,
      version = generation.current;
    await run('remove', async (signal) => {
      coordinator?.invalidate(accountID);
      const next = await kiloQuotaApi.remove(accountID, expected, connectionRef, scope, signal);
      if (accept(next, version, signal, expected, true)) {
        setConfirmRemove(false);
        await saved(version, '已解除当前Kilo额度绑定，Codex来源未删除。');
      }
    });
  };
  const query = async () => {
    if (!view || needsRead) return;
    const expected = view.binding_revision,
      version = generation.current;
    await run('query', async (signal) => {
      const result = coordinator
        ? await coordinator.query(view, signal)
        : await kiloQuotaApi.query(accountID, expected, connectionRef, scope, signal);
      if (accept(result.view, version, signal, expected, false, result.cancelled)) {
        const description = result.cancelled
          ? '查询已取消，安全观测已保留。'
          : result.view.query.status === 'success'
            ? '本次额度查询成功。'
            : result.view.query.status === 'failed'
              ? '本次额度查询失败，未产生新成功观测。'
              : '本次查询状态已更新。';
        await saved(version, description);
      }
    });
  };
  const selectedSource = sources?.sources.find((source) => source.credential_id === selected);
  const blocked = !!busy || policyBusy || !view || needsRead;
  const currentSource = sources?.sources.find(
    (source) => source.credential_id === view?.source.credential_id
  );
  const queryBlocked = blocked || view?.source.kind === 'none' || view?.query.status === 'running';
  return (
    <section className={styles.quotaPanel} aria-label="Kilo额度管理">
      <div className={styles.quotaCurrentSource}>
        <span>当前额度来源</span>
        <strong>
          {view?.source.kind === 'cpa_codex'
            ? currentSource?.label || view.source.credential_id
            : view?.source.kind === 'imported'
              ? '独立导入'
              : view
                ? '尚未关联'
                : '读取中…'}
        </strong>
        <small>
          {view ? kiloQuotaSourceLabel[view.source.status] : '来源状态未知'}
          {view?.query.observed_at ? ' · 已有观测' : ' · 待观测'}
        </small>
      </div>
      <h3>关联 Codex 额度</h3>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      <form
        aria-label="额度来源绑定"
        className={styles.quotaSourceForm}
        autoComplete="off"
        onSubmit={submit}
      >
        <KiloQuotaSourceFields
          value={{ mode, selected, search: sourceSearch, access, outerID }}
          onChange={(next) => {
            setMode(next.mode === 'imported' ? 'imported' : 'cpa_codex');
            setSelected(next.selected);
            setSourceSearch(next.search);
            setAccess(next.access);
            setOuterID(next.outerID);
          }}
          sources={sources}
          busy={!!busy}
          loadSources={loadSources}
          onModeChange={() => setConfirmRemove(false)}
        />
        <div className={styles.actions}>
          <Button
            type="submit"
            size="sm"
            disabled={blocked || (mode === 'cpa_codex' && !selectedSource?.chatgpt_account_id)}
          >
            保存额度来源
          </Button>
          {view && view.source.kind !== 'none' && !confirmRemove && (
            <Button
              type="button"
              size="sm"
              variant="secondary"
              disabled={blocked}
              onClick={() => setConfirmRemove(true)}
            >
              解除额度绑定
            </Button>
          )}
        </div>
      </form>
      {confirmRemove && (
        <div className={styles.actions}>
          <p>仅解除当前Kilo绑定，不删除Codex来源。</p>
          <Button size="sm" variant="danger" disabled={blocked} onClick={() => void remove()}>
            确认解除绑定
          </Button>
          <Button size="sm" variant="secondary" onClick={() => setConfirmRemove(false)}>
            保留绑定
          </Button>
        </div>
      )}
      {view && <KiloQuotaObservation view={view} />}
      {renderEstimate?.(query, queryBlocked)}
      <div className={styles.actions}>
        {!renderEstimate && (
          <Button size="sm" disabled={queryBlocked} onClick={() => void query()}>
            查询一次额度
          </Button>
        )}
        <Button
          size="sm"
          variant="secondary"
          disabled={!!busy || policyBusy}
          onClick={() => void read()}
        >
          读取当前观测
        </Button>
        {busy && (
          <Button size="sm" variant="secondary" onClick={() => controller.current?.abort()}>
            中止本地请求
          </Button>
        )}
        {busy && (
          <small role="status">
            正在处理
            {busy === 'query'
              ? '额度查询'
              : busy === 'save'
                ? '来源保存'
                : busy === 'remove'
                  ? '解除绑定'
                  : '只读加载'}
            …
          </small>
        )}
      </div>
      <details className={styles.quotaPolicyDetails}>
        <summary>来源额度策略</summary>
        <KiloSourcePolicyFields
          accountID={accountID}
          scope={scope}
          connectionRef={connectionRef}
          observation={view}
          disabled={!!busy}
          onChanged={onChanged}
          onBusyChange={setPolicyBusy}
        />
      </details>
      <details className={styles.quotaPolicyDetails}>
        <summary>操作说明</summary>
        <p className={styles.note}>
          读取观测和保存来源均不发额度查询；查询不测试模型。关闭窗口只中止本地请求，服务端结果请重新读取确认。
        </p>
      </details>
      <Button
        size="sm"
        variant="secondary"
        onClick={() => {
          setAccess('');
          onClose();
        }}
      >
        关闭额度面板
      </Button>
    </section>
  );
}
