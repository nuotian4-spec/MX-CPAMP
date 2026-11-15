import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  credentialConcurrencyApi,
  type CredentialConcurrency,
  type ConcurrencySnapshot,
} from '@/services/api/credentialConcurrency';

export type ConcurrencyEditorState = { dirty: boolean; saving: boolean };

export function CredentialConcurrencyEditor({
  credential,
  defaultLimit,
  connectionRef,
  scope,
  onSaved,
  onStateChange,
  resetVersion = 0,
  disabled = false,
}: {
  credential: CredentialConcurrency;
  defaultLimit: number;
  connectionRef: string;
  scope: ApiClientRequestScope;
  onSaved: () => Promise<ConcurrencySnapshot>;
  onStateChange?: (state: ConcurrencyEditorState) => void;
  resetVersion?: number;
  disabled?: boolean;
}) {
  const initial = credential.concurrency_limit?.toString() ?? '';
  const [value, setValue] = useState(initial);
  const [confirmed, setConfirmed] = useState(initial);
  const draftRef = useRef(value);
  const confirmedRef = useRef(confirmed);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const [message, setMessage] = useState('');
  const alive = useRef(true);
  const identity = JSON.stringify([
    scope.apiBase,
    scope.managementKey,
    connectionRef,
    credential.auth_id,
    credential.auth_index,
    credential.provider,
  ]);
  const activeIdentity = useRef(identity);
  activeIdentity.current = identity;
  const edit = (next: string) => {
    draftRef.current = next;
    setValue(next);
    setMessage('');
  };
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    const next = credential.concurrency_limit?.toString() ?? '';
    if (draftRef.current === confirmedRef.current) {
      draftRef.current = next;
      setValue(next);
    }
    confirmedRef.current = next;
    setConfirmed(next);
  }, [credential.concurrency_limit]);
  useEffect(() => {
    draftRef.current = confirmedRef.current;
    setValue(confirmedRef.current);
    setMessage('');
  }, [resetVersion]);
  const dirty = value !== confirmed;
  useEffect(() => {
    onStateChange?.({ dirty, saving: busy });
  }, [dirty, busy, onStateChange]);
  useEffect(
    () => () => {
      onStateChange?.({ dirty: false, saving: false });
    },
    [onStateChange]
  );
  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busyRef.current || disabled || !credential.editable) return;
    const raw = value.trim();
    if (raw && (!/^\d+$/.test(raw) || Number(raw) < 1 || Number(raw) > 2147483647)) {
      setMessage('请输入1至2147483647的整数，或留空继承默认');
      return;
    }
    const current = () => alive.current && activeIdentity.current === identity;
    busyRef.current = true;
    setBusy(true);
    setMessage('');
    try {
      await credentialConcurrencyApi.update(
        credential.auth_id,
        raw ? Number(raw) : null,
        connectionRef,
        scope
      );
      if (!current()) return;
      try {
        const snapshot = await onSaved();
        if (!current()) return;
        const matches =
          snapshot.mode === 'local' && snapshot.connection_ref === connectionRef
            ? snapshot.credentials.filter(
                (item) =>
                  item.auth_id === credential.auth_id &&
                  item.auth_index === credential.auth_index &&
                  item.provider === credential.provider
              )
            : [];
        const saved = matches.length === 1 ? matches[0] : undefined;
        const expected = raw ? Number(raw) : null;
        if (
          !saved ||
          snapshot.mode !== 'local' ||
          saved.concurrency_limit !== expected ||
          saved.limit_source !== (expected === null ? 'default' : 'override') ||
          saved.effective_limit !== (expected ?? snapshot.default_limit)
        ) {
          setMessage('并发写入已提交，但生效回读未确认，请刷新核对');
          return;
        }
        confirmedRef.current = raw;
        draftRef.current = raw;
        setConfirmed(raw);
        setValue(raw);
        setMessage('并发设置已保存并确认生效');
      } catch {
        if (current()) setMessage('并发写入已提交，状态刷新失败，请刷新核对');
      }
    } catch (error) {
      if (current())
        setMessage(error instanceof Error ? error.message : '保存回执未确认，请刷新后核对');
    } finally {
      if (current()) {
        busyRef.current = false;
        setBusy(false);
      }
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
          <div role="group" aria-label="并发上限来源">
            <Button
              type="button"
              variant="secondary"
              aria-pressed={value === ''}
              disabled={busy || disabled}
              onClick={() => edit('')}
            >
              继承统一默认（当前{defaultLimit}）
            </Button>
            <Button
              type="button"
              variant="secondary"
              aria-pressed={value !== ''}
              disabled={busy || disabled}
              onClick={() => edit(value || String(defaultLimit))}
            >
              自定义上限
            </Button>
          </div>
          <Input
            label="并发上限"
            type="number"
            min={1}
            max={2147483647}
            step={1}
            value={value}
            placeholder={`默认 ${defaultLimit}`}
            onChange={(event) => edit(event.target.value)}
            disabled={busy || disabled}
            hint="留空继承默认；降低上限不打断正在生成的请求"
          />
          {dirty && <p role="status">并发有未保存修改</p>}
          <Button type="submit" disabled={busy || disabled}>
            {busy ? '正在保存' : '保存并发'}
          </Button>
          <Button
            type="button"
            variant="secondary"
            disabled={!dirty || busy}
            onClick={() => edit(confirmedRef.current)}
          >
            重置并发
          </Button>
        </form>
      ) : (
        <p>{readOnlyReason || '该凭证仅支持查看'}</p>
      )}
      {message && <p role="status">{message}</p>}
    </Card>
  );
}
