import { kiloProxiesApi, type KiloProxyState } from '@/services/api/kiloProxies';
import { KiloEgressSummary } from './KiloEgressSummary';
import { KiloClientIdentity } from './KiloClientIdentity';
import { DropdownMenu } from '@/components/ui/DropdownMenu';
import { KiloQuotaSummary } from './KiloQuotaSummary';
import { useCredentialConcurrencySnapshot } from '@/features/accounts/hooks/useCredentialConcurrencySnapshot';
import type { CredentialConcurrencyState } from '@/features/accounts/hooks/useCredentialConcurrencySnapshot';
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { KiloModal as Modal } from './KiloModal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useAuthStore } from '@/stores';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  kiloApi,
  kiloErrorMessage,
  kiloOrganizationLabel,
  type KiloAccount,
  type KiloAccountInput,
  type KiloAccountsSnapshot,
  type KiloAccountSaved,
} from '@/services/api/kilo';
import styles from './KiloAccountsPage.module.scss';
import { KiloDeviceAuth } from './KiloDeviceAuth';
import { KiloOrganizationPicker } from './KiloOrganizationPicker';
import { KiloQuotaPanel } from './KiloQuotaPanel';
import { KiloQuotaCoordinator } from './KiloQuotaCoordinator';
import { kiloQuotaApi } from '@/services/api/kiloQuota';
import { KiloQuotaBatch } from './KiloQuotaBatch';
import { KiloRecoveryStatus } from './KiloRecoveryStatus';
import { KiloManualTest } from './KiloManualTest';
import { KiloProbesPanel } from './KiloProbesPanel';

type Editor = { kind: 'create' } | { kind: 'edit' | 'delete'; account: KiloAccount };

export function KiloAccountForm({
  account,
  busy,
  scope,
  connectionRef,
  onSave,
  onClose,
}: {
  account?: KiloAccount;
  busy: boolean;
  scope: ApiClientRequestScope;
  connectionRef: string;
  onSave: (input: KiloAccountInput) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(account?.name ?? '');
  const [secret, setSecret] = useState('');
  const [priority, setPriority] = useState(String(account?.priority ?? 0));
  const [limit, setLimit] = useState(account?.concurrency_limit?.toString() ?? '');
  const [enabled, setEnabled] = useState(account?.enabled ?? true);
  const [direct, setDirect] = useState(account?.allow_direct_when_proxy_full ?? false);
  const [error, setError] = useState('');
  const [organization, setOrganization] = useState<string>();
  const submitting = useRef(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || submitting.current) return;
    const key = secret;
    setSecret('');
    setError('');
    if (!account && !key.trim()) {
      setError('导入时请填写API Key。');
      return;
    }
    if (!/^-?\d+$/.test(priority) || !Number.isSafeInteger(Number(priority))) {
      setError('优先级应为整数。');
      return;
    }
    if (
      limit.trim() &&
      (!/^\d+$/.test(limit.trim()) || Number(limit) < 1 || Number(limit) > 2147483647)
    ) {
      setError('并发上限应为1至2147483647的整数，留空按当前并发模式。');
      return;
    }
    const input: KiloAccountInput = {
      name,
      priority: Number(priority),
      ...(!account || enabled !== account.enabled ? { enabled } : {}),
      concurrency_limit: limit.trim() ? Number(limit) : null,
      allow_direct_when_proxy_full: direct,
      ...(key.trim() ? { api_key: key } : {}),
      ...(account && organization !== undefined ? { organization_id: organization } : {}),
    };
    submitting.current = true;
    try {
      await onSave(input);
    } finally {
      submitting.current = false;
    }
  };
  const close = () => {
    setSecret('');
    onClose();
  };
  return (
    <form className={styles.form} onSubmit={submit} autoComplete="off">
      <Input
        label="账号名称"
        value={name}
        onChange={(event) => setName(event.target.value)}
        disabled={busy}
      />
      <Input
        label={account ? '替换API Key（留空保留）' : 'API Key'}
        type="password"
        autoComplete="new-password"
        value={secret}
        onChange={(event) => setSecret(event.target.value)}
        disabled={busy}
        hint="仅用于本次提交，不回显已保存的密钥；提交后输入框立即清空。"
      />
      <div className={styles.formColumns}>
        <Input
          label="优先级"
          type="number"
          step={1}
          value={priority}
          onChange={(event) => setPriority(event.target.value)}
          disabled={busy}
        />
        <Input
          label="并发上限"
          type="number"
          min={1}
          max={2147483647}
          step={1}
          value={limit}
          placeholder="留空按当前并发模式"
          onChange={(event) => setLimit(event.target.value)}
          disabled={busy}
          hint="留空按当前并发模式；降低上限不打断在途请求。"
        />
      </div>
      <ToggleSwitch label="启用账号" checked={enabled} onChange={setEnabled} disabled={busy} />
      {account && (
        <p className={styles.note}>
          普通编辑不改变人工启停；从停用切为启用会同时解除本账号自动限制。
        </p>
      )}
      <ToggleSwitch
        label="健康代理全部满载时允许直连"
        checked={direct}
        onChange={setDirect}
        disabled={busy}
      />
      <p className={styles.note}>
        无健康代理或连接故障时不会触发直连。手工更换密钥需等待在途请求结束。
      </p>
      {account && (
        <KiloOrganizationPicker
          account={account}
          scope={scope}
          connectionRef={connectionRef}
          disabled={busy}
          value={organization}
          onChange={setOrganization}
        />
      )}
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="button" variant="secondary" onClick={close}>
          取消
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? '正在提交' : account ? '保存账号' : '导入账号'}
        </Button>
      </div>
    </form>
  );
}

export function KiloAccountsPanel({
  scope,
  accountID,
  concurrency: sharedConcurrency,
  onSnapshot,
  onConfigureEgress,
}: {
  scope: ApiClientRequestScope;
  accountID?: string;
  concurrency?: CredentialConcurrencyState;
  onSnapshot?: (value: KiloAccountsSnapshot) => void;
  onConfigureEgress?: () => void;
}) {
  const onSnapshotRef = useRef(onSnapshot);
  onSnapshotRef.current = onSnapshot;
  const ownConcurrency = useCredentialConcurrencySnapshot(scope, !sharedConcurrency);
  const concurrency = sharedConcurrency ?? ownConcurrency;
  const observedReference =
    concurrency.snapshot?.mode === 'local' ? concurrency.snapshot.connection_ref : '';
  const observedReferenceRef = useRef(observedReference);
  observedReferenceRef.current = observedReference;
  const [detail, setDetail] = useState<KiloAccount | null>(null);
  const [proxySnapshot, setProxySnapshot] = useState<KiloProxyState>();
  const [proxyReadAt, setProxyReadAt] = useState<number>();
  const [proxyStale, setProxyStale] = useState(false);
  const [proxyRefresh, setProxyRefresh] = useState(0);
  const [snapshot, setSnapshot] = useState<KiloAccountsSnapshot>();
  const [editor, setEditor] = useState<Editor | null>(null);
  const [deviceOpen, setDeviceOpen] = useState(false);
  const [quotaAccount, setQuotaAccount] = useState<string | null>(null);
  const [testAccount, setTestAccount] = useState<KiloAccount | null>(null);
  const [probesAccount, setProbesAccount] = useState<KiloAccount | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [needsRefresh, setNeedsRefresh] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const alive = useRef(true);
  const activeScope = useRef(scope);
  activeScope.current = scope;
  const currentReference = useRef('');
  const sequence = useRef(0);
  const writing = useRef(false);
  const writeGeneration = useRef(0);
  const load = useCallback(async () => {
    const attempt = ++sequence.current;
    const current = () =>
      alive.current && activeScope.current === scope && attempt === sequence.current;
    if (current()) setLoading(true);
    try {
      const result = await kiloApi.list(scope);
      if (!current()) return;
      if (
        !result.connection_ref ||
        (observedReferenceRef.current && result.connection_ref !== observedReferenceRef.current)
      )
        throw new Error('连接引用未确认');
      if (currentReference.current && currentReference.current !== result.connection_ref) {
        setEditor(null);
        setDeviceOpen(false);
        setQuotaAccount(null);
        setTestAccount(null);
        setProbesAccount(null);
        setMessage('');
      }
      currentReference.current = result.connection_ref;
      setSnapshot(result);
      setProxyRefresh((version) => version + 1);

      setNeedsRefresh(false);
      setError('');
    } catch {
      if (current()) {
        setError('账号列表读取失败，请刷新；当前显示不代表最新状态。');
        setNeedsRefresh(true);
      }
      throw new Error('列表刷新未确认');
    } finally {
      if (current()) setLoading(false);
    }
  }, [scope]);
  useEffect(() => {
    alive.current = true;
    setSnapshot(undefined);
    setProxySnapshot(undefined);
    setProxyReadAt(undefined);
    setProxyStale(false);
    setDetail(null);
    writeGeneration.current += 1;
    setEditor(null);
    setDeviceOpen(false);
    setQuotaAccount(null);
    setTestAccount(null);
    setProbesAccount(null);
    setError('');
    setMessage('');
    setBusy(false);
    currentReference.current = '';
    writing.current = false;
    void load().catch(() => {});
    return () => {
      alive.current = false;
      sequence.current += 1;
    };
  }, [load]);
  useEffect(() => {
    if (
      !observedReference ||
      !currentReference.current ||
      observedReference === currentReference.current
    )
      return;
    currentReference.current = '';
    setSnapshot(undefined);
    setProxySnapshot(undefined);
    setProxyReadAt(undefined);
    setProxyStale(false);
    writeGeneration.current += 1;
    setEditor(null);
    setDetail(null);
    setDeviceOpen(false);
    setQuotaAccount(null);
    setTestAccount(null);
    setProbesAccount(null);
    writing.current = false;
    setBusy(false);
    void load().catch(() => {});
  }, [observedReference, load]);
  const write = async (
    operation: (reference: string) => Promise<KiloAccountSaved | void>,
    recoveryID?: string
  ) => {
    const reference = currentReference.current;
    if (
      writing.current ||
      deviceOpen ||
      quotaAccount ||
      testAccount ||
      probesAccount ||
      loading ||
      needsRefresh ||
      !reference
    )
      return;
    const current = () =>
      alive.current && activeScope.current === scope && currentReference.current === reference;
    const writeVersion = ++writeGeneration.current;
    writing.current = true;
    sequence.current += 1;
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const result = await operation(reference);
      if (!current()) return;
      if (result && result.connection_ref !== reference) throw { status: 409 };
      if (recoveryID) {
        if (
          !result ||
          result.id !== recoveryID ||
          !result.enabled ||
          result.auto_restriction?.reason !== 'none' ||
          !result.auto_restriction.persisted
        )
          throw { status: 409 };
        setSnapshot((previous) =>
          previous && previous.connection_ref === reference
            ? {
                ...previous,
                accounts: previous.accounts.map((account) =>
                  account.id === recoveryID ? result : account
                ),
              }
            : previous
        );
      }
      const savedNote = recoveryID
        ? '本账号已人工启用并解除自身自动限制；其它鉴权终态和模型冷却不变，不代表模型可用。'
        : '账号操作已保存。';
      const organizationNote =
        result && typeof result.organization_id === 'string'
          ? `已保存组织：${kiloOrganizationLabel(result.organization_id)}。`
          : '';
      setEditor(null);
      try {
        await load();
        if (current()) setMessage(`${savedNote}${organizationNote}`);
      } catch {
        if (current())
          setMessage(
            recoveryID
              ? `${savedNote}列表刷新待确认。${organizationNote}`
              : `账号操作已保存，列表刷新待确认。${organizationNote}`
          );
      }
    } catch (failure) {
      if (!current()) return;
      setError(kiloErrorMessage(failure));
      if (![400, 403].includes((failure as { status?: number })?.status ?? 0)) {
        setNeedsRefresh(true);
        setEditor(null);
      }
    } finally {
      if (
        alive.current &&
        activeScope.current === scope &&
        writeVersion === writeGeneration.current
      ) {
        writing.current = false;
        setBusy(false);
      }
    }
  };
  const blocked =
    busy ||
    deviceOpen ||
    quotaAccount !== null ||
    testAccount !== null ||
    probesAccount !== null ||
    loading ||
    needsRefresh ||
    !snapshot;
  const accounts = (snapshot?.accounts ?? []).filter(
    (account) => !accountID || account.id === accountID
  );
  useEffect(() => {
    let active = true;
    if (
      snapshot?.connection_ref &&
      (!observedReference || snapshot.connection_ref === observedReference)
    )
      void kiloProxiesApi
        .list(scope)
        .then((state) => {
          if (active && state.connection_ref === snapshot.connection_ref) {
            setProxySnapshot(state);
            setProxyReadAt(Date.now());
            setProxyStale(false);
          }
        })
        .catch(() => {
          if (active) setProxyStale(true);
        });
    return () => {
      active = false;
    };
  }, [scope, snapshot?.connection_ref, proxyRefresh, concurrency.snapshot, observedReference]);
  const quotaCoordinator = useMemo(
    () =>
      new KiloQuotaCoordinator(scope, snapshot?.connection_ref ?? '', async (view, signal) => {
        const current = () =>
          alive.current &&
          !signal.aborted &&
          activeScope.current === scope &&
          currentReference.current === view.connection_ref;
        const publish = (sourcePolicy: KiloAccount['source_policy']) => {
          if (!current()) return;
          setSnapshot((previous) =>
            previous?.connection_ref === view.connection_ref
              ? {
                  ...previous,
                  accounts: previous.accounts.map((account) =>
                    account.id === view.account_id &&
                    account.quota_snapshot === view &&
                    account.quota_source_summary?.binding_revision === view.binding_revision
                      ? { ...account, source_policy: sourcePolicy }
                      : account
                  ),
                }
              : previous
          );
        };
        publish(undefined);
        const policy = await kiloQuotaApi.getPolicy(view.account_id, scope, signal);
        if (!current()) return;
        if (policy.connection_ref !== view.connection_ref) throw { status: 409 };
        publish(policy);
      }),
    [scope, snapshot?.connection_ref]
  );
  useEffect(() => {
    if (snapshot) onSnapshotRef.current?.(snapshot);
  }, [snapshot]);
  useEffect(
    () =>
      quotaCoordinator.subscribe(() => {
        const views = quotaCoordinator.snapshot().views;
        setSnapshot((previous) => {
          if (!previous) return previous;
          let changed = false;
          const accounts = previous.accounts.map((account) => {
            const view = views[account.id];
            const source = account.quota_source_summary;
            if (
              !view ||
              !source ||
              view.connection_ref !== previous.connection_ref ||
              view.binding_revision !== source.binding_revision ||
              view.source.kind !== source.kind ||
              (source.kind === 'cpa_codex' && view.source.credential_id !== source.credential_id) ||
              view === account.quota_snapshot
            )
              return account;
            changed = true;
            return {
              ...account,
              quota_snapshot: view,
              quota_source_summary: { ...source, status: view.source.status },
            };
          });
          return changed ? { ...previous, accounts } : previous;
        });
      }),
    [quotaCoordinator]
  );
  useEffect(() => {
    quotaCoordinator.setAccounts(snapshot?.accounts.map((account) => account.id) ?? []);
  }, [quotaCoordinator, snapshot]);
  useEffect(() => {
    const unauthorized = () => quotaCoordinator.unauthorized();
    globalThis.window?.addEventListener('unauthorized', unauthorized);
    return () => {
      globalThis.window?.removeEventListener('unauthorized', unauthorized);
      quotaCoordinator.dispose();
    };
  }, [quotaCoordinator]);
  return (
    <section className={styles.page}>
      {!accountID && (
        <header className={styles.header}>
          <div>
            <h1>Kilo账号</h1>
            <p>管理当前连接的Kilo账号，密钥仅写入服务端。</p>
          </div>
          <div className={styles.actions}>
            <Button
              variant="secondary"
              disabled={
                busy ||
                loading ||
                deviceOpen ||
                quotaAccount !== null ||
                testAccount !== null ||
                probesAccount !== null
              }
              onClick={() => {
                void load().catch(() => {});
              }}
            >
              {loading ? '正在刷新' : '刷新列表'}
            </Button>
            <Button disabled={blocked} onClick={() => setEditor({ kind: 'create' })}>
              导入账号
            </Button>
            <Button disabled={blocked || !!editor} onClick={() => setDeviceOpen(true)}>
              设备授权
            </Button>
          </div>
        </header>
      )}
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      {!snapshot && loading && <p role="status">正在读取账号列表…</p>}
      {snapshot && (
        <p className={styles.summary}>
          共{accounts.length}个账号 · 在途
          {accounts.reduce((sum, account) => sum + account.active, 0)} · 等待
          {accounts.reduce((sum, account) => sum + account.waiting, 0)} · 默认并发20
        </p>
      )}
      {snapshot && accounts.length === 0 && (
        <div className={styles.empty}>
          <h2>尚无Kilo账号</h2>
          <p>导入API Key或完成设备授权并保存后，账号将进入当前连接的Kilo通道。</p>
        </div>
      )}
      {accounts.length > 0 && (
        <div className={styles.accountCards}>
          {accounts.map((account) => {
            const state =
              concurrency.snapshot?.mode === 'local' &&
              concurrency.snapshot.connection_ref === snapshot?.connection_ref
                ? concurrency.snapshot.credentials.filter(
                    (item) => item.auth_id === account.id && item.provider === 'kilo'
                  )
                : [];
            const credential = state.length === 1 ? state[0] : undefined;
            return (
              <article key={account.id} className={styles.accountCard}>
                <div className={styles.header}>
                  <h2>{account.name || account.id}</h2>
                  <span>{account.enabled ? '已启用' : '已停用'}</span>
                </div>
                <KiloRecoveryStatus account={account} />
                <p className={styles.note}>
                  组织：{kiloOrganizationLabel(account.organization_id)} ·{' '}
                  {account.concurrency_limit === null ? '未设置人工限制' : '自定义并发'}
                </p>
                <p className={styles.note}>
                  {credential
                    ? `当前并发 ${credential.active} / ${credential.effective_limit}${credential.pinned_waiting ? ` · 等待 ${credential.pinned_waiting}` : ''}${concurrency.error ? ' · 状态陈旧' : ''}`
                    : '当前并发未知'}
                </p>
                <KiloQuotaSummary
                  account={account}
                  sharedSource={
                    account.quota_source_summary?.kind === 'cpa_codex' &&
                    accounts.filter(
                      (item) =>
                        item.quota_source_summary?.credential_id ===
                          account.quota_source_summary?.credential_id &&
                        item.quota_source_summary?.kind === 'cpa_codex'
                    ).length > 1
                  }
                />
                <KiloEgressSummary
                  accountID={account.id}
                  identity={account.client_identity}
                  snapshot={proxySnapshot}
                  readAt={proxyReadAt}
                  stale={proxyStale}
                />
                {accountID && <KiloClientIdentity identity={account.client_identity} />}
                <div className={styles.actions}>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={blocked}
                    onClick={() => setDetail(account)}
                  >
                    查看详情
                  </Button>
                  {onConfigureEgress && (
                    <Button size="sm" variant="secondary" onClick={onConfigureEgress}>
                      配置出口
                    </Button>
                  )}
                  <DropdownMenu
                    ariaLabel={`${account.name || account.id}更多操作`}
                    triggerLabel="更多"
                    disabled={blocked}
                    items={[
                      {
                        key: 'edit',
                        label: '编辑',
                        onClick: () => setEditor({ kind: 'edit', account }),
                      },
                      {
                        key: 'quota',
                        label: '关联额度',
                        onClick: () => setQuotaAccount(account.id),
                      },
                      {
                        key: 'manual',
                        label: '手动模型测试',
                        onClick: () => setTestAccount(account),
                      },
                      {
                        key: 'probes',
                        label: '自动生成配置',
                        onClick: () => setProbesAccount(account),
                      },
                      ...(account.enabled ||
                      !account.auto_restriction ||
                      account.auto_restriction.reason === 'none'
                        ? [
                            {
                              key: 'status',
                              label: account.enabled ? '停用' : '启用',
                              onClick: () => {
                                void write((reference) =>
                                  kiloApi.update(
                                    account.id,
                                    { enabled: !account.enabled },
                                    reference,
                                    scope
                                  )
                                );
                              },
                            },
                          ]
                        : []),
                      ...(account.auto_restriction && account.auto_restriction.reason !== 'none'
                        ? [
                            {
                              key: 'recover',
                              label: account.enabled ? '解除自动限制' : '启用并解除自动限制',
                              onClick: () => {
                                void write(
                                  (reference) =>
                                    kiloApi.update(account.id, { enabled: true }, reference, scope),
                                  account.id
                                );
                              },
                            },
                          ]
                        : []),
                      {
                        key: 'delete',
                        label: '删除',
                        tone: 'danger',
                        onClick: () => setEditor({ kind: 'delete', account }),
                      },
                    ]}
                  />
                </div>
              </article>
            );
          })}
        </div>
      )}
      {detail && (
        <Modal open title={detail.name || 'Kilo账号详情'} onClose={() => setDetail(null)}>
          <div className={styles.form}>
            <p>
              人工状态：{detail.enabled ? '启用' : '停用'} · 优先级 {detail.priority}
            </p>
            <KiloRecoveryStatus account={detail} />
            <KiloQuotaSummary account={detail} />
            <KiloClientIdentity identity={detail.client_identity} />
            <p>
              组织：{kiloOrganizationLabel(detail.organization_id)} · 健康出口满载直连：
              {detail.allow_direct_when_proxy_full ? '允许' : '关闭'}
            </p>
            <div className={styles.actions}>
              <Button
                variant="secondary"
                onClick={() => {
                  setDetail(null);
                  setQuotaAccount(detail.id);
                }}
              >
                额度详情
              </Button>
              <Button
                variant="secondary"
                onClick={() => {
                  setDetail(null);
                  setProbesAccount(detail);
                }}
              >
                自动任务
              </Button>
              <Button
                variant="secondary"
                onClick={() => {
                  setDetail(null);
                  setEditor({ kind: 'edit', account: detail });
                }}
              >
                编辑
              </Button>
            </div>
            <details>
              <summary>账号标识</summary>
              <p>{detail.id}</p>
              <p>并发覆盖：{detail.concurrency_limit ?? '未设置人工限制'}</p>
            </details>
          </div>
        </Modal>
      )}
      {snapshot && !accountID && <KiloQuotaBatch coordinator={quotaCoordinator} />}
      {probesAccount && snapshot && (
        <Modal open title="账号自动生成配置" onClose={() => setProbesAccount(null)}>
          <KiloProbesPanel
            account={probesAccount}
            scope={scope}
            connectionRef={snapshot.connection_ref}
            onClose={() => setProbesAccount(null)}
          />
        </Modal>
      )}
      {testAccount && snapshot && (
        <Modal open title="单账号手动模型测试" onClose={() => setTestAccount(null)}>
          <KiloManualTest
            account={testAccount}
            scope={scope}
            connectionRef={snapshot.connection_ref}
            onClose={() => setTestAccount(null)}
          />
        </Modal>
      )}
      {deviceOpen && snapshot && (
        <Modal open title="Kilo设备授权" onClose={() => setDeviceOpen(false)}>
          <KiloDeviceAuth
            scope={scope}
            connectionRef={snapshot.connection_ref}
            onClose={() => setDeviceOpen(false)}
            onSaved={load}
          />
        </Modal>
      )}
      {quotaAccount && snapshot && (
        <Modal open title="关联Codex/ChatGPT额度" onClose={() => setQuotaAccount(null)}>
          <KiloQuotaPanel
            accountID={quotaAccount}
            scope={scope}
            connectionRef={snapshot.connection_ref}
            onClose={() => setQuotaAccount(null)}
            onChanged={load}
            coordinator={quotaCoordinator}
          />
        </Modal>
      )}
      {editor && (
        <Modal
          open
          title={
            editor.kind === 'create'
              ? '导入Kilo账号'
              : editor.kind === 'delete'
                ? '确认删除账号'
                : '编辑Kilo账号'
          }
          onClose={() => setEditor(null)}
        >
          {editor.kind === 'delete' ? (
            <div className={styles.form}>
              <p>
                删除“{editor.account.name || editor.account.id}
                ”？在途请求结束后才可删除，删除后该账号停止提供服务。
              </p>
              <div className={styles.actions}>
                <Button variant="secondary" onClick={() => setEditor(null)}>
                  取消
                </Button>
                <Button
                  variant="danger"
                  disabled={busy}
                  onClick={() => {
                    void write((reference) => kiloApi.remove(editor.account.id, reference, scope));
                  }}
                >
                  确认删除
                </Button>
              </div>
            </div>
          ) : (
            <KiloAccountForm
              key={
                editor.kind === 'edit'
                  ? `${editor.account.id}:${snapshot?.connection_ref}`
                  : 'create'
              }
              account={editor.kind === 'edit' ? editor.account : undefined}
              scope={scope}
              connectionRef={snapshot?.connection_ref ?? ''}
              busy={busy}
              onClose={() => setEditor(null)}
              onSave={(input) =>
                write((reference) =>
                  editor.kind === 'create'
                    ? kiloApi.create(input, reference, scope)
                    : kiloApi.update(editor.account.id, input, reference, scope)
                )
              }
            />
          )}
        </Modal>
      )}
    </section>
  );
}

export function KiloAccountsPage() {
  const [searchParams] = useSearchParams();
  const authenticated = useAuthStore((state) => state.isAuthenticated);
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const scope = useMemo(() => ({ apiBase, managementKey }), [apiBase, managementKey]);
  return authenticated ? (
    <KiloAccountsPanel
      key={JSON.stringify([apiBase, managementKey])}
      scope={scope}
      accountID={searchParams.get('account') || undefined}
    />
  ) : (
    <p>管理认证已失效，本页查询和手动测试已停止。</p>
  );
}
