import { Select } from '@/components/ui/Select';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { KiloModal as Modal } from './KiloModal';
import { useAuthStore } from '@/stores';
import type { ApiClientRequestScope } from '@/services/api/client';
import { kiloApi, type KiloAccount } from '@/services/api/kilo';
import {
  kiloProxiesApi,
  kiloProxyError,
  kiloProxyDuplicate,
  type KiloProxy,
  type KiloProxyState,
} from '@/services/api/kiloProxies';
import { KiloCapacityForm, KiloEgressForm, KiloProxyForm } from './KiloProxyForms';
import { KiloAutoAssignmentForm, KiloIPGroups } from './KiloAutoEgress';
import {
  KiloProxyActionDialog,
  KiloRecycleTable,
  type KiloProxyAction,
} from './KiloProxyLifecycle';
import styles from './KiloAccountsPage.module.scss';
import pageStyles from './KiloProxiesPage.module.scss';
import { Input } from '@/components/ui/Input';
import { KiloProxyBatchImport } from './KiloProxyBatchImport';

type Editor =
  | { kind: 'proxy'; proxy?: KiloProxy }
  | { kind: 'capacity' }
  | { kind: 'auto' }
  | { kind: 'egress'; account: KiloAccount };
export function KiloProxiesPanel({
  scope,
  accountID = '',
}: {
  scope: ApiClientRequestScope;
  accountID?: string;
}) {
  const [snapshot, setSnapshot] = useState<KiloProxyState>();
  const [focusProxyID, setFocusProxyID] = useState('');
  const runtimeSnapshotRef = useRef(snapshot);
  runtimeSnapshotRef.current = snapshot;
  const [accounts, setAccounts] = useState<KiloAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [needsRefresh, setNeedsRefresh] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [filter, setFilter] = useState('all');
  const [range, setRange] = useState('known');
  const [search, setSearch] = useState('');
  const [batchOpen, setBatchOpen] = useState(false);
  const [tab, setTab] = useState<'pool' | 'recycle'>('pool');
  const [runtimeReadAt, setRuntimeReadAt] = useState<number>();
  const [runtimeStale, setRuntimeStale] = useState(false);
  const [accountFilter, setAccountFilter] = useState(accountID);
  const [action, setAction] = useState<KiloProxyAction | null>(null);
  const writeController = useRef<AbortController | null>(null);
  const [editor, setEditor] = useState<Editor | null>(null);
  const activeScope = useRef(scope);
  activeScope.current = scope;
  const controller = useRef<AbortController | null>(null);
  const sequence = useRef(0);
  const writing = useRef(false);
  const load = useCallback(async () => {
    const attempt = ++sequence.current;
    const signal = controller.current?.signal;
    const current = () =>
      activeScope.current === scope && !signal?.aborted && sequence.current === attempt;
    setLoading(true);
    setEditor(null);
    setAction(null);
    setBatchOpen(false);
    setMessage('');
    try {
      const state = await kiloProxiesApi.list(scope, signal);
      if (!current()) return;
      const list = await kiloApi.list(scope);
      if (!current()) return;
      if (
        !state.connection_ref ||
        state.connection_ref !== list.connection_ref ||
        !Number.isSafeInteger(state.revision)
      )
        throw new Error('连接或版本未确认');
      setSnapshot(state);
      setRuntimeReadAt(Date.now());
      setRuntimeStale(false);
      setAccounts(list.accounts);
      setNeedsRefresh(false);
      setError('');
    } catch {
      if (current()) {
        setError('代理与账号状态读取失败，请刷新；旧列表不代表当前状态。');
        setNeedsRefresh(true);
      }
    } finally {
      if (current()) setLoading(false);
    }
  }, [scope]);
  useEffect(() => {
    const abort = new AbortController();
    controller.current = abort;
    setFocusProxyID('');
    setSnapshot(undefined);
    setAccounts([]);
    setBusy(false);
    writing.current = false;
    setEditor(null);
    setAction(null);
    setBatchOpen(false);
    setError('');
    setMessage('');
    void load();
    return () => {
      abort.abort();
      writeController.current?.abort();
      sequence.current += 1;
    };
  }, [load]);
  useEffect(() => {
    let active = true,
      pending = false;
    const abort = new AbortController();
    const poll = async () => {
      if (pending || writing.current || globalThis.document?.visibilityState === 'hidden') return;
      pending = true;
      const requestSequence = sequence.current;
      try {
        const state = await kiloProxiesApi.list(scope, abort.signal);
        if (
          !active ||
          activeScope.current !== scope ||
          requestSequence !== sequence.current ||
          writing.current
        )
          return;
        const previous = runtimeSnapshotRef.current;
        if (!previous) return;
        if (state.connection_ref !== previous.connection_ref) {
          sequence.current += 1;
          writeController.current?.abort();
          writing.current = false;
          setBusy(false);
          setSnapshot(undefined);
          setAccounts([]);
          setEditor(null);
          setAction(null);
          setBatchOpen(false);
          setMessage('');
          setRuntimeReadAt(undefined);
          setRuntimeStale(false);
          setNeedsRefresh(true);
          setError('管理连接已变化，请刷新读取当前账号与出口。');
          return;
        }
        if (state.revision !== previous.revision) {
          setNeedsRefresh(true);
          setRuntimeStale(true);
          setError('代理配置已变化，请显式刷新后核实。');
          return;
        }
        if (state.runtime_revision < previous.runtime_revision) return;
        setRuntimeReadAt(Date.now());
        setRuntimeStale(false);
        setSnapshot(state);
      } catch {
        if (active) setRuntimeStale(true);
      } finally {
        pending = false;
      }
    };
    const visible = () => {
      if (globalThis.document?.visibilityState !== 'hidden') void poll();
    };
    const timer = globalThis.setInterval(() => {
      void poll();
    }, 5000);
    globalThis.document?.addEventListener?.('visibilitychange', visible);
    return () => {
      active = false;
      abort.abort();
      globalThis.clearInterval(timer);
      globalThis.document?.removeEventListener?.('visibilitychange', visible);
    };
  }, [scope]);
  const write = async (
    operation: (state: KiloProxyState, signal?: AbortSignal) => Promise<KiloProxyState>,
    successMessage = '代理配置已保存。'
  ) => {
    if (writing.current || loading || needsRefresh || !snapshot) return;
    const attempt = ++sequence.current;
    const scopeSignal = controller.current?.signal;
    const abort = new AbortController();
    writeController.current = abort;
    const signal = abort.signal;
    const current = () =>
      activeScope.current === scope && !scopeSignal?.aborted && sequence.current === attempt;
    writing.current = true;
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const result = await operation(snapshot, signal);
      if (!current()) return;
      if (signal.aborted) throw { status: 499 };
      if (
        result.connection_ref !== snapshot.connection_ref ||
        !Number.isSafeInteger(result.revision)
      )
        throw new Error('连接或版本未确认');
      setSnapshot(result);
      setEditor(null);
      setAction(null);
      const added = result.proxies.filter(
        (proxy) => !snapshot.proxies.some((old) => old.id === proxy.id)
      );
      setMessage(
        editor?.kind === 'proxy' && !editor.proxy && added.length === 1
          ? added[0].admission_status === 'pending'
            ? '代理已保存但未入池，不增加容量；请在详情重验。'
            : added[0].admission_status === 'admitted'
              ? '代理已保存并登记公网IP；是否分配仍按健康与归属判断。'
              : '代理已保存；旧版兼容，准入状态未知。'
          : successMessage
      );
    } catch (failure) {
      if (current()) {
        setError(kiloProxyError(signal.aborted ? { status: 499 } : failure));
        setEditor(null);
        setAction(null);
        const duplicate = !signal.aborted ? kiloProxyDuplicate(failure) : null;
        setNeedsRefresh(!duplicate);
        if (duplicate?.proxy_id) {
          setFocusProxyID(duplicate.proxy_id);
          setRange('all');
          setFilter('all');
          setSearch('');
          setAccountFilter('');
        }
      }
    } finally {
      if (current()) {
        writing.current = false;
        writeController.current = null;
        setBusy(false);
      }
    }
  };
  const blocked = busy || loading || needsRefresh || !snapshot;
  const accountLabel = (id: string) => accounts.find((a) => a.id === id)?.name || id;
  return (
    <section className={pageStyles.page}>
      <header className={styles.header}>
        <div>
          <h1>出口池</h1>
          <p>一个公网 IP 一行，归属、正在使用与累计分配放在一起。</p>
        </div>
        <div className={styles.actions}>
          <Button variant="secondary" disabled={busy || loading} onClick={() => void load()}>
            {loading ? '正在刷新' : '刷新状态'}
          </Button>
          <Button disabled={blocked} onClick={() => setBatchOpen(true)}>
            批量导入代理
          </Button>
          <details>
            <summary>池设置</summary>
            <div className={styles.actions}>
              <Button
                variant="secondary"
                disabled={blocked}
                onClick={() => setEditor({ kind: 'capacity' })}
              >
                调整容量
              </Button>
              <Button disabled={blocked} onClick={() => setEditor({ kind: 'proxy' })}>
                增加代理
              </Button>
              <Button
                variant="secondary"
                disabled={blocked}
                onClick={() => setEditor({ kind: 'auto' })}
              >
                自动独占设置
              </Button>
            </div>
          </details>
        </div>
      </header>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      {snapshot && (
        <>
          <p className={styles.summary}>
            运行快照 {runtimeReadAt ? new Date(runtimeReadAt).toLocaleTimeString() : '尚未读取'}
            {runtimeStale ? ' · 陈旧' : ''} · 每 IP 容量 {snapshot.ip_concurrency_limit ?? '未知'}
          </p>
          <div className={pageStyles.tabs} role="tablist" aria-label="出口视图">
            <Button
              role="tab"
              aria-selected={tab === 'pool'}
              variant={tab === 'pool' ? 'primary' : 'secondary'}
              onClick={() => setTab('pool')}
            >
              出口池
            </Button>
            <Button
              role="tab"
              aria-selected={tab === 'recycle'}
              variant={tab === 'recycle' ? 'primary' : 'secondary'}
              onClick={() => setTab('recycle')}
            >
              回收站 · {snapshot.recycle.length}
            </Button>
          </div>
          {tab === 'pool' && (
            <>
              <div className={pageStyles.toolbar}>
                <Select
                  ariaLabel="出口范围"
                  value={range}
                  onChange={setRange}
                  options={[
                    { value: 'known', label: '已识别公网 IP' },
                    { value: 'unknown', label: '待识别入口' },
                    { value: 'archived', label: '已归档 IP' },
                    { value: 'all', label: '全部活跃出口' },
                  ]}
                />
                <Input
                  label="搜索IP或账号"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                />
                <Select
                  ariaLabel="代理范围"
                  value={filter}
                  onChange={setFilter}
                  options={[
                    { value: 'all', label: '全部出口' },
                    { value: 'shared', label: '共享' },
                    { value: 'exclusive', label: '专属' },
                    { value: 'active', label: '使用中' },
                    { value: 'unhealthy', label: '异常' },
                    { value: 'registered', label: '已登记 IP' },
                    { value: 'available', label: '当前可分配' },
                    { value: 'pending', label: '待核条目' },
                  ]}
                />
                <Select
                  ariaLabel="按账号筛选出口"
                  value={accountFilter}
                  onChange={setAccountFilter}
                  options={[
                    { value: '', label: '全部账号' },
                    ...accounts.map((account) => ({
                      value: account.id,
                      label: account.name || account.id,
                    })),
                  ]}
                />
              </div>
              <KiloIPGroups
                snapshot={snapshot}
                accountLabel={accountLabel}
                accountID={accountFilter}
                filter={filter}
                range={range}
                search={search}
                onEgress={(id) => {
                  const account = accounts.find((a) => a.id === id);
                  if (account) setEditor({ kind: 'egress', account });
                }}
                blocked={blocked}
                onEdit={(proxy) => setEditor({ kind: 'proxy', proxy })}
                onAction={setAction}
                focusProxyID={focusProxyID}
                onFocusHandled={() => setFocusProxyID('')}
              />
            </>
          )}
          {tab === 'recycle' && (
            <>
              <KiloRecycleTable
                records={snapshot.recycle}
                blocked={blocked}
                accountLabel={accountLabel}
                onAction={setAction}
              />
            </>
          )}
        </>
      )}
      {batchOpen && snapshot && (
        <KiloProxyBatchImport
          scope={scope}
          snapshot={snapshot}
          blocked={blocked}
          acquire={() => {
            if (writing.current || loading || needsRefresh) return false;
            sequence.current += 1;
            writing.current = true;
            setBusy(true);
            setError('');
            setMessage('');
            return true;
          }}
          release={(next, uncertain) => {
            if (activeScope.current !== scope || next.connection_ref !== snapshot.connection_ref)
              return;
            setSnapshot(next);
            setNeedsRefresh(uncertain);
            setRuntimeReadAt(Date.now());
            writing.current = false;
            setBusy(false);
          }}
          onClose={() => setBatchOpen(false)}
          onLocate={(id) => {
            setBatchOpen(false);
            setTab('pool');
            setRange('all');
            setFilter('all');
            setAccountFilter('');
            setSearch('');
            setFocusProxyID(id);
          }}
        />
      )}
      {action && snapshot && (
        <KiloProxyActionDialog
          action={action}
          busy={busy || needsRefresh}
          onClose={() => (busy ? writeController.current?.abort() : setAction(null))}
          onConfirm={() => {
            switch (action.kind) {
              case 'archive':
                return write(
                  (state, signal) => kiloProxiesApi.archive(action.proxy.id, state, scope, signal),
                  '代理已归档，相关主偏好和出口关联已清除。'
                );
              case 'restore':
                return write(
                  (state, signal) => kiloProxiesApi.restore(action.proxy.id, state, scope, signal),
                  action.proxy.fixed_exit_ip
                    ? '健康检测通过，固定IP已恢复到自动独占池；累计历史保留，分配状态请核对列表。'
                    : '健康检测通过，代理已启用并恢复到共享池；旧专属关系未恢复。'
                );
              case 'clear':
                return write(
                  (state, signal) => kiloProxiesApi.clear(state, scope, signal),
                  '回收站已清空，活跃代理池保持不变。'
                );
              case 'recheck':
                return write(
                  (state, signal) => kiloProxiesApi.recheck(action.proxy.id, state, scope, signal),
                  '强制健康重验通过；启停配置与配置版本保持不变，这不是模型可用性验证。'
                );
            }
          }}
        />
      )}
      {editor && snapshot && (
        <Modal
          open
          title={
            editor.kind === 'proxy'
              ? editor.proxy
                ? '编辑代理'
                : '增加代理'
              : editor.kind === 'capacity'
                ? '调整代理容量'
                : editor.kind === 'auto'
                  ? '自动独占固定IP设置'
                  : '账号出口策略'
          }
          onClose={() => setEditor(null)}
        >
          {editor.kind === 'proxy' ? (
            <KiloProxyForm
              proxy={editor.proxy}
              accounts={accounts}
              hasManualReferences={
                !!editor.proxy &&
                snapshot.accounts.some(
                  (account) =>
                    account.mode === 'pool' &&
                    (account.candidate_proxy_ids?.includes(editor.proxy!.id) ||
                      account.primary_proxy_id === editor.proxy!.id)
                )
              }
              busy={busy || needsRefresh}
              onClose={() => setEditor(null)}
              onSave={(input) =>
                write((state, signal) =>
                  editor.proxy
                    ? kiloProxiesApi.update(editor.proxy.id, input, state, scope, signal)
                    : kiloProxiesApi.create(input, state, scope, signal)
                )
              }
            />
          ) : editor.kind === 'capacity' ? (
            <KiloCapacityForm
              limit={snapshot.proxy_limit}
              busy={busy || needsRefresh}
              onClose={() => setEditor(null)}
              onSave={(limit) =>
                write((state, signal) => kiloProxiesApi.capacity(limit, state, scope, signal))
              }
            />
          ) : editor.kind === 'auto' ? (
            <KiloAutoAssignmentForm
              snapshot={snapshot}
              busy={busy || needsRefresh}
              onClose={() => setEditor(null)}
              onSave={(input) =>
                write(
                  (state, signal) => kiloProxiesApi.autoAssignment(input, state, scope, signal),
                  '自动独占配置已保存；当前归属、累计历史与退休状态请核对列表。'
                )
              }
            />
          ) : (
            <KiloEgressForm
              account={editor.account}
              snapshot={snapshot}
              busy={busy || needsRefresh}
              onClose={() => setEditor(null)}
              onSave={(input) =>
                write((state, signal) =>
                  kiloProxiesApi.egress(editor.account.id, input, state, scope, signal)
                )
              }
            />
          )}
        </Modal>
      )}
    </section>
  );
}
export function KiloProxiesPage() {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const scope = useMemo(() => ({ apiBase, managementKey }), [apiBase, managementKey]);
  const [params] = useSearchParams();
  return (
    <KiloProxiesPanel
      key={JSON.stringify([apiBase, managementKey])}
      scope={scope}
      accountID={params.get('account') || ''}
    />
  );
}
