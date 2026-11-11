import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useAuthStore } from '@/stores';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  credentialConcurrencyApi,
  type CredentialConcurrency,
  type ConcurrencySnapshot,
} from '@/services/api/credentialConcurrency';

function CredentialCard({
  credential,
  defaultLimit,
  connectionRef,
  scope,
  onSaved,
}: {
  credential: CredentialConcurrency;
  defaultLimit: number;
  connectionRef: string;
  scope: ApiClientRequestScope;
  onSaved: () => Promise<void>;
}) {
  const [value, setValue] = useState(credential.concurrency_limit?.toString() || '');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  useEffect(() => {
    setValue(credential.concurrency_limit?.toString() || '');
  }, [credential.concurrency_limit]);
  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const raw = value.trim();
    if (raw && (!/^\d+$/.test(raw) || Number(raw) < 1 || Number(raw) > 2147483647)) {
      setMessage('请输入1至2147483647的整数，或留空继承默认');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      await credentialConcurrencyApi.update(
        credential.auth_id,
        raw ? Number(raw) : null,
        connectionRef,
        scope
      );
      await onSaved();
      setMessage('设置已保存');
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '保存回执未确认，请刷新后核对');
    } finally {
      setBusy(false);
    }
  };
  const readOnlyReason = {
    runtime_source: '运行时凭证仅支持查看',
    plugin_virtual: '插件虚拟凭证仅支持查看',
    keyless_group: '无独立Key的兼容组使用默认上限',
    '': '',
  }[credential.read_only_reason];
  return (
    <Card title={credential.name || credential.auth_id}>
      <p>
        {credential.provider} ·{' '}
        {credential.source === 'file'
          ? 'OAuth / 凭证文件'
          : credential.source === 'config'
            ? '上游 API Key'
            : '运行时凭证'}
      </p>
      <p>
        正在使用 <strong>{credential.active}</strong> / {credential.effective_limit} · 定向等待{' '}
        {credential.pinned_waiting}
      </p>
      <p>
        {credential.limit_source === 'default' ? `继承统一默认 ${defaultLimit}` : '独立并发上限'} ·
        同一凭证的全部模型共用额度
      </p>
      {credential.editable ? (
        <form onSubmit={save}>
          <Input
            label="并发上限"
            type="number"
            min={1}
            max={2147483647}
            step={1}
            value={value}
            placeholder={`默认 ${defaultLimit}`}
            onChange={(event) => setValue(event.target.value)}
            disabled={busy}
            hint="留空继承默认；降低上限不打断正在生成的请求"
          />
          <Button type="submit" disabled={busy}>
            {busy ? '正在保存' : '保存设置'}
          </Button>
        </form>
      ) : (
        <p>{readOnlyReason || '该凭证仅支持查看'}</p>
      )}
      {message && <p role="status">{message}</p>}
    </Card>
  );
}
export function CredentialConcurrencyPanel({ scope }: { scope: ApiClientRequestScope }) {
  const [snapshot, setSnapshot] = useState<ConcurrencySnapshot>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const sequence = useRef(0);
  const load = useCallback(async () => {
    const attempt = ++sequence.current;
    const next = await credentialConcurrencyApi.list(scope);
    if (attempt === sequence.current) {
      setSnapshot(next);
      setError('');
    }
  }, [scope]);
  useEffect(() => {
    let disposed = false;
    let pending = false;
    setSnapshot(undefined);
    setError('');
    const poll = async () => {
      if (pending) return;
      pending = true;
      const attempt = ++sequence.current;
      try {
        const next = await credentialConcurrencyApi.list(scope);
        if (!disposed && attempt === sequence.current) {
          setSnapshot(next);
          setError('');
        }
      } catch {
        if (!disposed) setError('并发状态暂不可读，请稍后刷新');
      } finally {
        pending = false;
      }
    };
    void poll();
    const timer = window.setInterval(() => {
      void poll();
    }, 5000);
    return () => {
      disposed = true;
      sequence.current += 1;
      window.clearInterval(timer);
    };
  }, [scope]);
  const refresh = async () => {
    setBusy(true);
    try {
      await load();
    } catch {
      setError('并发状态暂不可读，请稍后刷新');
    } finally {
      setBusy(false);
    }
  };
  return (
    <div>
      <h1>凭证并发</h1>
      <p>分别限制每一份上游凭证。Codex与BPS独立计数；客户端访问本站的API Key规则保持原设置。</p>
      <Button onClick={refresh} disabled={busy}>
        {busy ? '正在刷新' : '刷新状态'}
      </Button>
      {error && <p role="alert">{error}</p>}
      {snapshot?.mode === 'home' && (
        <Card title="Home模式">
          <p>并发由Home管理，本页不提供本地计数和修改。</p>
        </Card>
      )}
      {snapshot?.mode === 'local' && (
        <>
          <p role="status">
            统一默认 {snapshot.default_limit} · 公共池等待 {snapshot.pool_waiting} ·
            满额最多等待30秒，超时返回429
          </p>
          {snapshot.credentials.length === 0 && <p>尚无上游凭证</p>}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit,minmax(min(100%,320px),1fr))',
              gap: 16,
              marginTop: 16,
            }}
          >
            {snapshot.credentials.map((credential) => (
              <CredentialCard
                key={`${snapshot.connection_ref}:${credential.auth_id}`}
                credential={credential}
                defaultLimit={snapshot.default_limit}
                connectionRef={snapshot.connection_ref}
                scope={scope}
                onSaved={load}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
export function CredentialConcurrencyPage() {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const scope = useMemo(() => ({ apiBase, managementKey }), [apiBase, managementKey]);
  return <CredentialConcurrencyPanel key={`${apiBase}:${managementKey}`} scope={scope} />;
}
