import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { useAuthStore } from '@/stores';
import { useCredentialConcurrencySnapshot } from '@/features/accounts/hooks/useCredentialConcurrencySnapshot';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { KiloDeviceInput, KiloDeviceSaved } from '@/services/api/kiloDevice';
import {
  kiloQuotaApi,
  type KiloQuotaBinding,
  type KiloQuotaSources,
} from '@/services/api/kiloQuota';
import { KiloDeviceAuth } from './KiloDeviceAuth';
import { KiloDeviceForm } from './KiloDeviceForm';
import { KiloQuotaSourceFields, type KiloQuotaSourceDraft } from './KiloQuotaSourceFields';
import { KiloAddFlow, type KiloAddProgress } from './kiloAddFlow';
import styles from './KiloAddAccount.module.scss';

const emptyKiloSourceDraft = (): KiloQuotaSourceDraft => ({
  mode: 'none',
  selected: '',
  search: '',
  access: '',
  outerID: '',
});

export function KiloAddAccount({
  connectionRef,
}: {
  scope: ApiClientRequestScope;
  connectionRef: string;
  onSaved: () => Promise<void>;
}) {
  const navigate = useNavigate();
  return (
    <Button disabled={!connectionRef} onClick={() => navigate('/kilo-add')}>
      添加 Kilo 账号
    </Button>
  );
}

export function KiloAddAccountEditor({
  scope,
  connectionRef,
}: {
  scope: ApiClientRequestScope;
  connectionRef: string;
}) {
  const navigate = useNavigate();
  const [method, setMethod] = useState<'device' | 'import'>('device');
  const [draft, setDraft] = useState(emptyKiloSourceDraft);
  const [sources, setSources] = useState<KiloQuotaSources>();
  const [sourceError, setSourceError] = useState('');
  const [sourceBusy, setSourceBusy] = useState(false);
  const [working, setWorking] = useState(false);
  const [progress, setProgress] = useState<KiloAddProgress>();
  const [edition, setEdition] = useState(0);
  const flow = useRef<KiloAddFlow | undefined>(undefined);
  const live = useRef({ scope, connectionRef });
  live.current = { scope, connectionRef };
  const mounted = useRef(true);
  const sourceController = useRef<AbortController | undefined>(undefined);
  const changedConnection = !!flow.current && !flow.current.matchesConnection(scope, connectionRef);
  const loadSources = useCallback(async () => {
    if (!connectionRef) return;
    sourceController.current?.abort();
    const abort = new AbortController();
    sourceController.current = abort;
    setSourceBusy(true);
    setSourceError('');
    try {
      const next = await kiloQuotaApi.sources(scope, abort.signal);
      if (
        abort.signal.aborted ||
        live.current.scope !== scope ||
        live.current.connectionRef !== connectionRef
      )
        return;
      if (next.connection_ref !== connectionRef)
        throw new Error('来源连接已变化，请刷新当前连接。');
      setSources(next);
    } catch (error) {
      if (!abort.signal.aborted)
        setSourceError(error instanceof Error ? error.message : '本地来源读取失败。');
    } finally {
      if (!abort.signal.aborted) setSourceBusy(false);
    }
  }, [scope, connectionRef]);
  useEffect(() => {
    mounted.current = true;
    setSources(undefined);
    if (!flow.current) setDraft(emptyKiloSourceDraft());
    else setDraft((value) => ({ ...value, access: '' }));
    void loadSources();
    return () => {
      sourceController.current?.abort();
      flow.current?.clearMaterial();
    };
  }, [loadSources]);
  useEffect(
    () => () => {
      mounted.current = false;
      flow.current?.clearMaterial();
    },
    []
  );
  const source = (): KiloQuotaBinding | undefined => {
    if (draft.mode === 'none') return undefined;
    if (draft.mode === 'imported') {
      if (!draft.access.trim()) throw new Error('请填写独立额度 access token。');
      return {
        kind: 'imported',
        access_token: draft.access,
        ...(draft.outerID.trim() ? { chatgpt_account_id: draft.outerID.trim() } : {}),
      };
    }
    const option = sources?.sources.find((item) => item.credential_id === draft.selected);
    if (sources?.connection_ref !== connectionRef || !option?.chatgpt_account_id)
      throw new Error('请选择身份完整的 Codex 凭证。');
    return {
      kind: 'cpa_codex',
      credential_id: option.credential_id,
      chatgpt_account_id: option.chatgpt_account_id,
    };
  };
  const prepare = (input: KiloDeviceInput) => {
    if (!input.inference_egress) throw new Error('请确认正式出口配置。');
    let selected: KiloQuotaBinding | undefined;
    try {
      selected = source();
      setSourceError('');
    } catch (error) {
      setSourceError((error as Error).message);
      throw error;
    }
    const savedScope = scope,
      savedRef = connectionRef;
    flow.current?.clearMaterial();
    flow.current = new KiloAddFlow(
      scope,
      connectionRef,
      input.inference_egress,
      selected,
      input.enabled ?? true,
      () =>
        mounted.current &&
        live.current.scope === savedScope &&
        live.current.connectionRef === savedRef,
      (next) => {
        if (mounted.current) setProgress(next);
      }
    );
    return flow.current;
  };
  const importKey = async (input: KiloDeviceInput, apiKey: string) => {
    if (flow.current && progress) return;
    const current = prepare(input);
    setDraft((value) => ({ ...value, access: '' }));
    setWorking(true);
    setProgress({ ...current.progress });
    try {
      await current.create({
        name: input.name,
        priority: input.priority,
        concurrency_limit: input.concurrency_limit,
        allow_direct_when_proxy_full: input.allow_direct_when_proxy_full,
        api_key: apiKey,
      });
    } finally {
      if (mounted.current) {
        setDraft((value) => ({ ...value, access: '' }));
        setWorking(false);
      }
    }
  };
  const savedDevice = async (id: string, details?: KiloDeviceSaved) => {
    setWorking(true);
    try {
      await flow.current?.deviceSaved(id, details);
    } finally {
      if (mounted.current) {
        setDraft((value) => ({ ...value, access: '' }));
        setWorking(false);
      }
    }
  };
  const recover = async (read: boolean) => {
    const current = flow.current;
    if (!current || working || changedConnection) return;
    setWorking(true);
    try {
      if (read) await current.read();
      else {
        if (!current.progress.source) {
          const selected = source();
          if (selected) current.supplyMaterial(selected);
        }
        await current.resume();
      }
    } catch (error) {
      setSourceError((error as Error).message);
    } finally {
      setDraft((value) => ({ ...value, access: '' }));
      setWorking(false);
    }
  };
  return (
    <div className={styles.page}>
      <header className={styles.heading}>
        <div>
          <h1>添加 Kilo 凭证</h1>
          <p>配置账号、额度来源与出口，在本页完成授权和保存。</p>
        </div>
        <Button variant="secondary" onClick={() => navigate('/accounts?provider=kilo')}>
          返回凭证列表
        </Button>
      </header>
      {!connectionRef && <p role="status">正在确认当前连接…</p>}
      {changedConnection && <p role="alert">连接已变化，本次保存结果保留；请返回原连接核实。</p>}
      <div className={styles.sheet}>
        <KiloDeviceAuth
          key={edition}
          scope={scope}
          connectionRef={connectionRef}
          onClose={() => navigate('/accounts?provider=kilo')}
          onSaved={savedDevice}
          renderConfig={(start, busy, locked) => (
            <KiloDeviceForm
              scope={scope}
              connectionRef={connectionRef}
              busy={busy || working || !connectionRef}
              locked={locked || !!progress || changedConnection}
              method={method}
              onMethodChange={setMethod}
              onStart={async (input) => {
                prepare(input);
                setDraft((value) => ({ ...value, access: '' }));
                await start(input);
              }}
              onImport={importKey}
              sourceFields={
                <>
                  <KiloQuotaSourceFields
                    value={draft}
                    onChange={setDraft}
                    sources={sources}
                    busy={
                      sourceBusy || busy || working || locked || !!progress || changedConnection
                    }
                    loadSources={loadSources}
                    allowLater
                  />
                  {sourceError && <p role="alert">{sourceError}</p>}
                </>
              }
            />
          )}
        />
        {progress && (
          <section className={styles.result} aria-label="保存结果">
            <strong>
              {progress.id ? '凭证已保存' : progress.error ? '创建结果待核' : '正在保存凭证'}
            </strong>
            {progress.id && <p>AuthID：{progress.id}</p>}
            {progress.id && (
              <p>
                {progress.egress ? '正式出口配置已保存' : '正式出口待完成'} ·{' '}
                {progress.source
                  ? draft.mode === 'none'
                    ? '额度来源稍后关联'
                    : '额度来源已关联，待观测'
                  : '额度关联待完成'}
              </p>
            )}
            {progress.error && <p role="alert">{progress.error}</p>}
            {progress.egress && progress.egressWaiting && <p>出口配置已保存，等待可用容量配齐。</p>}
            {progress.recovered && progress.id && (
              <p>恢复仅补出口和来源，保留当前启停；需要启用时请在凭证列表操作。</p>
            )}
            {!progress.done &&
              progress.id &&
              !progress.source &&
              draft.mode === 'imported' &&
              !progress.needsRead && (
                <Input
                  label="重新填写独立额度 access token"
                  type="password"
                  value={draft.access}
                  onChange={(event) => setDraft((old) => ({ ...old, access: event.target.value }))}
                  disabled={working || changedConnection}
                />
              )}
            <div className={styles.footer}>
              {!progress.done && (
                <Button
                  disabled={working || changedConnection || !progress.id}
                  onClick={() => void recover(progress.needsRead)}
                >
                  {progress.needsRead ? '读取已保存状态' : '继续完成待办'}
                </Button>
              )}
              {progress.done && (
                <>
                  <span>
                    {method === 'device'
                      ? '本次保存已完成'
                      : progress.enabled
                        ? '保存完成 · 已启用'
                        : '保存完成 · 保持停用'}
                  </span>
                  <Button
                    variant="secondary"
                    onClick={() => {
                      flow.current?.clearMaterial();
                      flow.current = undefined;
                      setProgress(undefined);
                      setDraft(emptyKiloSourceDraft());
                      setEdition((value) => value + 1);
                    }}
                  >
                    继续添加
                  </Button>
                </>
              )}
            </div>
          </section>
        )}
      </div>
    </div>
  );
}

export function KiloAddAccountPage() {
  const authenticated = useAuthStore((state) => state.isAuthenticated);
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const scope = useMemo(() => ({ apiBase, managementKey }), [apiBase, managementKey]);
  const concurrency = useCredentialConcurrencySnapshot(scope, authenticated);
  return authenticated ? (
    <>
      <KiloAddAccountEditor
        scope={scope}
        connectionRef={
          concurrency.snapshot?.mode === 'local' ? concurrency.snapshot.connection_ref : ''
        }
      />
      {concurrency.error && <p role="alert">{concurrency.error}</p>}
    </>
  ) : (
    <p>请先连接管理服务。</p>
  );
}
