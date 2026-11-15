import { useEffect, type ReactNode } from 'react';
import pageStyles from './KiloAddAccount.module.scss';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  kiloProxiesApi,
  type KiloProxyState,
  type KiloEgressInput,
} from '@/services/api/kiloProxies';
import { KiloEgressFields } from './KiloEgressFields';
import { validateKiloEgressInput } from './kiloEgressInput';
import { useState, useRef, type FormEvent } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import type { KiloDeviceInput } from '@/services/api/kiloDevice';
import styles from './KiloAccountsPage.module.scss';

export function KiloDeviceForm({
  busy,
  onStart,
  scope,
  connectionRef,
  locked = false,
  sourceFields,
  method = 'device',
  onMethodChange,
  onImport,
}: {
  busy: boolean;
  locked?: boolean;
  sourceFields?: ReactNode;
  method?: 'device' | 'import';
  onMethodChange?: (method: 'device' | 'import') => void;
  onImport?: (input: KiloDeviceInput, apiKey: string) => Promise<void>;
  scope: ApiClientRequestScope;
  connectionRef: string;
  onStart: (input: KiloDeviceInput) => Promise<void>;
}) {
  const disabled = busy || locked;
  const submitting = useRef(false);
  const [keyDraft, setKeyDraft] = useState({ scope, connectionRef, value: '' });
  const apiKey =
    keyDraft.scope === scope && keyDraft.connectionRef === connectionRef ? keyDraft.value : '';
  const setApiKey = (value: string) => setKeyDraft({ scope, connectionRef, value });
  const [name, setName] = useState('');
  const [priority, setPriority] = useState('0');
  const [limit, setLimit] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [fallback, setFallback] = useState(false);
  const [mode, setMode] = useState<'direct' | 'pool'>('pool');
  const [error, setError] = useState('');
  const [loaded, setLoaded] = useState<{
    scope: ApiClientRequestScope;
    connectionRef: string;
    snapshot: KiloProxyState;
  }>();
  const snapshot =
    loaded?.scope === scope && loaded.connectionRef === connectionRef ? loaded.snapshot : undefined;
  const [proxyID, setProxyID] = useState('');
  const [egress, setEgress] = useState<KiloEgressInput>({
    mode: 'pool',
    candidate_proxy_ids: null,
    exclusive_proxy_ids: [],
    primary_proxy_id: '',
  });
  useEffect(() => {
    setKeyDraft({ scope, connectionRef, value: '' });
    setProxyID('');
    setLoaded(undefined);
    setError('');
    const abort = new AbortController();
    void kiloProxiesApi
      .list(scope, abort.signal)
      .then((state) => {
        if (!abort.signal.aborted) {
          if (state.connection_ref !== connectionRef) throw new Error();
          setLoaded({ scope, connectionRef, snapshot: state });
          setEgress({
            capacity_mode: state.auto_assignment_enabled ? 'ip' : 'fixed',
            allocation_mode: 'auto',
            max_ips: 1,
            specified_ips: [],
            mode: state.auto_assignment_enabled ? 'auto' : 'pool',
            candidate_proxy_ids: null,
            exclusive_proxy_ids: [],
            primary_proxy_id: '',
          });
        }
      })
      .catch(() => {
        if (!abort.signal.aborted) setError('出口读取失败，请关闭窗口并刷新列表后重新打开。');
      });
    return () => abort.abort();
  }, [scope, connectionRef]);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (disabled || submitting.current) return;
    if (!name.trim() || [...name.trim()].length > 256) {
      setError('账号名称应为1至256个字符。');
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
      setError('并发上限应为1至2147483647，留空按当前并发模式。');
      return;
    }
    const validationError = validateKiloEgressInput(egress);
    if (!snapshot || validationError) {
      setError(validationError || '请等待出口读取完成。');
      return;
    }
    if (
      !snapshot ||
      egress.candidate_proxy_ids?.length === 0 ||
      (method === 'device' &&
        mode === 'pool' &&
        (!proxyID || (egress.candidate_proxy_ids && !egress.candidate_proxy_ids.includes(proxyID))))
    ) {
      setError('请确认后续出口，并选择本次授权固定使用的代理。');
      return;
    }
    setError('');
    const input: KiloDeviceInput = {
      name: name.trim(),
      priority: Number(priority),
      enabled,
      concurrency_limit: limit.trim() ? Number(limit) : null,
      allow_direct_when_proxy_full: egress.mode === 'auto' ? false : fallback,
      authorization_egress: {
        mode,
        ...(mode === 'pool' ? { proxy_id: proxyID } : {}),
        expected_revision: snapshot.revision,
      },
      inference_egress: { ...egress, expected_revision: snapshot.revision },
    };
    submitting.current = true;
    try {
      if (method === 'import') {
        if (!apiKey.trim()) {
          setError('请填写 API Key。');
          return;
        }
        const secret = apiKey;
        setApiKey('');
        await onImport?.(input, secret);
      } else await onStart(input);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : '配置待核，请检查当前字段。');
    } finally {
      submitting.current = false;
    }
  };
  return (
    <form onSubmit={submit} autoComplete="off" aria-label="添加 Kilo 凭证表单">
      <section className={pageStyles.section}>
        <h2>账号信息</h2>
        <div className={pageStyles.columns}>
          <Input
            label="账号名称"
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={disabled}
          />
        </div>
        <details className={pageStyles.advanced}>
          <summary>高级设置</summary>
          <div className={pageStyles.columns}>
            <Input
              label="优先级"
              type="number"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              disabled={disabled}
            />
            <Input
              label="并发上限"
              type="number"
              min={1}
              max={2147483647}
              value={limit}
              onChange={(e) => setLimit(e.target.value)}
              disabled={disabled}
              placeholder="留空按当前并发模式"
            />
            <ToggleSwitch
              label="保存后启用账号"
              checked={enabled}
              onChange={setEnabled}
              disabled={disabled}
            />
            <ToggleSwitch
              label="正式推理健康代理全部满载时允许直连"
              checked={fallback}
              onChange={setFallback}
              disabled={disabled || egress.mode === 'auto'}
            />
          </div>
        </details>
      </section>
      {sourceFields && (
        <section className={pageStyles.section}>
          <h2>关联 Codex 额度</h2>
          <div className={pageStyles.source}>{sourceFields}</div>
        </section>
      )}
      <section className={pageStyles.section}>
        <h2>保存后的推理出口</h2>
        {snapshot ? (
          <>
            <div className={pageStyles.columns}>
              <div>
                <label className={pageStyles.label}>分配方式</label>
                <Select
                  ariaLabel="分配方式"
                  value={
                    egress.mode === 'auto'
                      ? egress.allocation_mode === 'specified'
                        ? 'specified'
                        : 'auto'
                      : egress.mode
                  }
                  disabled={disabled}
                  options={[
                    { value: 'auto', label: '自动独占' },
                    { value: 'specified', label: '人工指定 IP' },
                    { value: 'pool', label: '共享池' },
                    { value: 'direct', label: '直连' },
                  ]}
                  onChange={(value) => {
                    setEgress({
                      mode: value === 'specified' ? 'auto' : (value as KiloEgressInput['mode']),
                      capacity_mode: value === 'auto' || value === 'specified' ? 'ip' : 'fixed',
                      allocation_mode: value === 'specified' ? 'specified' : 'auto',
                      max_ips: 1,
                      specified_ips: [],
                      candidate_proxy_ids: null,
                      exclusive_proxy_ids: [],
                      primary_proxy_id: '',
                    });
                    setProxyID('');
                  }}
                />
              </div>
              {egress.mode === 'auto' &&
              egress.allocation_mode !== 'specified' &&
              egress.capacity_mode === 'ip' ? (
                <Input
                  label="IP 数量上限"
                  type="number"
                  min={1}
                  step={1}
                  value={String(egress.max_ips ?? 1)}
                  disabled={disabled}
                  onChange={(event) =>
                    setEgress({ ...egress, max_ips: Number(event.target.value) })
                  }
                />
              ) : (
                <div>
                  <label className={pageStyles.label}>配置容量</label>
                  <div className={pageStyles.fieldValue}>
                    {egress.mode === 'auto'
                      ? `${(egress.allocation_mode === 'specified' ? (egress.specified_ips?.length ?? 0) : 1) * (snapshot.ip_concurrency_limit ?? 20)} 并发`
                      : egress.mode === 'pool'
                        ? '沿用共享池容量'
                        : '沿用原账号上限'}
                  </div>
                </div>
              )}
            </div>
            {egress.mode === 'auto' && egress.allocation_mode === 'specified' && (
              <div className={pageStyles.ipChoices} aria-label="指定固定 IP">
                {(snapshot.ip_groups ?? [])
                  .filter((group) => !group.retired)
                  .map((group) => (
                    <label key={group.ip}>
                      <input
                        type="checkbox"
                        checked={(egress.specified_ips ?? []).includes(group.ip)}
                        disabled={disabled || !!group.owner_account_id}
                        onChange={(event) => {
                          const ips = event.target.checked
                            ? [...(egress.specified_ips ?? []), group.ip]
                            : (egress.specified_ips ?? []).filter((ip) => ip !== group.ip);
                          setEgress({
                            ...egress,
                            specified_ips: ips,
                            max_ips: Math.max(1, ips.length),
                          });
                        }}
                      />
                      <span>
                        <strong>{group.ip}</strong>
                        <small>
                          {group.owner_account_id
                            ? '已归属其他账号'
                            : `空闲固定 IP · ${snapshot.ip_concurrency_limit ?? 20} 并发`}
                        </small>
                      </span>
                    </label>
                  ))}
                {!snapshot.ip_groups?.length && (
                  <p className={styles.note}>当前没有可分配的固定 IP。</p>
                )}
              </div>
            )}
            <p className={styles.note}>
              {egress.mode === 'auto'
                ? egress.allocation_mode === 'specified'
                  ? '保存时按所选集合配齐；发生争抢时保留凭证并显示等待。'
                  : `先分配 1 个 IP，满载后按需扩展，最多 ${egress.max_ips ?? 1} 个。`
                : egress.mode === 'pool'
                  ? '保存后按共享池既有策略使用代理。'
                  : '保存后推理显式直连。'}
            </p>
            <details className={pageStyles.advanced}>
              <summary>高级出口设置</summary>
              <div className={pageStyles.egress}>
                <KiloEgressFields
                  value={egress}
                  onChange={(next) => {
                    setEgress(next);
                    setProxyID('');
                  }}
                  snapshot={snapshot}
                  disabled={disabled}
                />
              </div>
            </details>
          </>
        ) : (
          <p>正在读取出口…</p>
        )}
      </section>
      <section className={pageStyles.section}>
        <h2>本次授权</h2>
        <div className={pageStyles.columns}>
          {onMethodChange && (
            <div>
              <label className={pageStyles.label}>授权方式</label>
              <Select
                ariaLabel="授权方式"
                value={method}
                options={[
                  { value: 'device', label: '设备授权' },
                  { value: 'import', label: '已有 API Key' },
                ]}
                onChange={(value) => {
                  setApiKey('');
                  onMethodChange(value as 'device' | 'import');
                }}
                disabled={disabled}
              />
            </div>
          )}
          {method === 'device' ? (
            <div>
              <label className={pageStyles.label}>本次授权固定单出口</label>
              <Select
                ariaLabel="授权出口"
                value={mode === 'direct' ? 'direct' : proxyID}
                options={[
                  { value: '', label: '选择本次授权代理' },
                  ...(snapshot?.proxies ?? [])
                    .filter(
                      (proxy) =>
                        !proxy.account_id &&
                        (!egress.candidate_proxy_ids ||
                          egress.candidate_proxy_ids.includes(proxy.id))
                    )
                    .map((proxy) => ({
                      value: proxy.id,
                      label: `${proxy.name || proxy.id} · 公网 ${proxy.observed_exit_ip || '未知'}`,
                    })),
                  { value: 'direct', label: '显式直连' },
                ]}
                onChange={(value) => {
                  setMode(value === 'direct' ? 'direct' : 'pool');
                  setProxyID(value === 'direct' ? '' : value);
                }}
                disabled={disabled || !snapshot}
              />
            </div>
          ) : (
            <Input
              label="API Key"
              type="password"
              autoComplete="new-password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              disabled={disabled}
            />
          )}
        </div>
        <p className={styles.note}>
          {method === 'device'
            ? '开始后锁定本次授权出口，授权出口与保存后的推理出口分别配置。'
            : '提交后清空密钥；出口与额度关联完成前，新凭证保持停用。'}
        </p>
      </section>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {!locked && (
        <footer className={pageStyles.footer}>
          <span>配置尚未提交</span>
          <Button type="submit" disabled={busy || !snapshot}>
            {busy ? '正在处理' : method === 'device' ? '发起设备授权' : '保存 API Key 凭证'}
          </Button>
        </footer>
      )}
    </form>
  );
}
