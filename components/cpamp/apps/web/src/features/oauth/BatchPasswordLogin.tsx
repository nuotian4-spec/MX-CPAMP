import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { publishAccountCredentialMutationRevision } from '@/stores';
import { createCodexInspectionConnectionFingerprint } from '@/features/monitoring/codexInspection';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  loginTaskLabel,
  loginDiagnosticLabel,
  type LoginProfile,
  type LoginTask,
} from '@/services/api/loginTasks';
import {
  loginBatchesApi,
  parseLoginBatch,
  clearBatchInput,
  isLoginBatchActive,
  batchStatusLabel,
  type LoginBatch,
  type BatchPreviewRow,
} from '@/services/api/loginBatches';

export function BatchPasswordLogin({
  profile,
  scope,
}: {
  profile: LoginProfile;
  scope: ApiClientRequestScope;
}) {
  const [batch, setBatch] = useState<LoginBatch>();
  const [otherActive, setOtherActive] = useState(false);
  const [preview, setPreview] = useState<BatchPreviewRow[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const input = useRef<HTMLTextAreaElement>(null);
  const current = useRef<LoginBatch | undefined>(undefined);
  const generation = useRef(0);
  const published = useRef(new Set<string>());
  current.current = batch;
  const active = isLoginBatchActive(batch);
  useEffect(() => {
    let disposed = false;
    let polling = false;
    generation.current += 1;
    current.current = undefined;
    setBatch(undefined);
    setPreview([]);
    setOtherActive(false);
    setError('');
    if (input.current) input.current.value = '';
    const apply = (next: LoginBatch) => {
      current.current = next;
      setBatch(next);
      for (const row of next.items) {
        if (
          ((row.status === 'succeeded' && row.auth_index) ||
            row.error_code === 'credential_mapping_unconfirmed') &&
          !published.current.has(row.id)
        ) {
          published.current.add(row.id);
          publishAccountCredentialMutationRevision({
            connectionFingerprint:
              createCodexInspectionConnectionFingerprint(scope.apiBase, scope.managementKey) || '',
            provider: profile,
            kind: 'oauth',
          });
        }
      }
    };
    const poll = async () => {
      if (polling) return;
      polling = true;
      try {
        const known = current.current;
        if (known && isLoginBatchActive(known)) {
          const next = await loginBatchesApi.get(known.id, scope);
          if (!disposed) {
            apply(next);
            setError('');
          }
        } else {
          const next = await loginBatchesApi.active(scope);
          if (!disposed) {
            setOtherActive(!!next && next.profile !== profile);
            if (next?.profile === profile) apply(next);
          }
        }
      } catch (error) {
        if (!disposed && (error as { status?: number }).status === 404 && current.current)
          setBatch({ ...current.current, status: 'interrupted', error_code: 'batch_not_found' });
        if (!disposed && current.current && isLoginBatchActive(current.current))
          setError('批次状态暂不可读或已随服务重启清除；不会自动重交账号。');
      } finally {
        polling = false;
      }
    };
    void (async () => {
      try {
        const latest = await loginBatchesApi.latest(profile, scope);
        if (!disposed && !current.current && latest) apply(latest);
      } catch {
        if (!disposed && !current.current) setError('最近批次摘要暂不可读，请稍后刷新。');
      }
    })();
    void poll();
    const timer = window.setInterval(() => {
      void poll();
    }, 1500);
    return () => {
      disposed = true;
      generation.current += 1;
      window.clearInterval(timer);
    };
  }, [scope, profile]);
  const previewInput = () => {
    const parsed = parseLoginBatch(input.current?.value || '');
    setPreview(parsed.preview);
    setError(parsed.error);
    clearBatchInput(parsed.items);
  };
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const parsed = parseLoginBatch(input.current?.value || '');
    setPreview(parsed.preview);
    if (parsed.error || !parsed.items.length) {
      setError(parsed.error || '没有可提交的有效账号');
      clearBatchInput(parsed.items);
      return;
    }
    const known: LoginBatch = { id: crypto.randomUUID(), profile, status: 'running', items: [] };
    current.current = known;
    setBatch(known);
    setBusy(true);
    setError('');
    if (input.current) input.current.value = '';
    const attempt = generation.current;
    try {
      const next = await loginBatchesApi.create(
        { batch_id: known.id, profile, items: parsed.items },
        scope
      );
      if (attempt === generation.current) setBatch(next);
    } catch (error) {
      if (
        attempt === generation.current &&
        [400, 401, 403, 409, 413, 503].includes((error as { status?: number }).status || 0)
      ) {
        setBatch({ ...known, status: 'interrupted', error_code: 'batch_not_accepted' });
        setError('批次未被接受，请检查输入或等待当前批次完成后重新提供材料。');
        return;
      }
      if (attempt === generation.current)
        setError('创建回执未确认，按批次编号查询；请勿重复提交。');
    } finally {
      clearBatchInput(parsed.items);
      if (attempt === generation.current) setBusy(false);
    }
  };
  const cancel = async () => {
    if (!batch) return;
    try {
      setBatch(await loginBatchesApi.cancel(batch.id, scope));
      setError('');
    } catch {
      setError('取消回执未确认；正在保存的账号会等待真实结果，请继续查询。');
    }
  };
  return (
    <div id={`oauth-provider-${profile}-2fa`}>
      <Card title={`${profile === 'bps' ? 'BPS' : 'Codex'} 2FA 批量导入`}>
        <p>
          每批最多100行，逐条执行。仅申请{profile === 'bps' ? 'BPS' : 'Codex'}
          授权；密码与TOTP仅本次使用，未开始材料最多等待30分钟。关闭页面后继续执行，服务重启后不续跑。
        </p>
        <form onSubmit={submit} autoComplete="off">
          <label>
            账号列表
            <textarea
              ref={input}
              name="accounts"
              rows={6}
              autoComplete="off"
              spellCheck={false}
              placeholder="邮箱----密码----TOTP种子"
              disabled={busy || active || otherActive}
              style={{ width: '100%', fontFamily: 'monospace' }}
            />
          </label>
          <Button type="button" onClick={previewInput} disabled={busy || active || otherActive}>
            检查列表
          </Button>
          <Button type="submit" disabled={busy || active || otherActive}>
            {busy ? '正在创建批次' : '开始批量登录'}
          </Button>
          {active && (
            <Button type="button" onClick={cancel} disabled={batch?.status === 'cancelling'}>
              取消剩余条目
            </Button>
          )}
        </form>
        {otherActive && <p role="status">另一通道的批次正在执行，请等待完成。</p>}
        {preview.length > 0 && (
          <details>
            <summary>
              输入检查：{preview.filter((row) => !row.error).length}条可用，
              {preview.filter((row) => row.error).length}条跳过
            </summary>
            <ul>
              {preview.map((row) => (
                <li key={row.line_no}>
                  第{row.line_no}行 · {row.email_hint} · {row.error || '可提交'}
                </li>
              ))}
            </ul>
          </details>
        )}
        {batch && (
          <>
            <p role="status">
              {batchStatusLabel(batch)} · 已结束{' '}
              {
                batch.items.filter(
                  (row) =>
                    !['pending', 'created', 'running', 'importing', 'cancelling'].includes(
                      row.status
                    )
                ).length
              }
              /{batch.items.length}
              {batch.error_code ? ` · ${batch.error_code}` : ''}
            </p>
            <ol>
              {batch.items.map((row) => (
                <li key={row.line_no}>
                  第{row.line_no}行 · {row.email_hint} ·{' '}
                  {row.error_code === 'credential_mapping_unconfirmed'
                    ? '已保存，凭证索引待核实'
                    : row.status === 'pending'
                      ? '等待执行'
                      : row.status === 'skipped'
                        ? '已跳过'
                        : loginTaskLabel(row as LoginTask)}
                  {loginDiagnosticLabel(row) ? ` · ${loginDiagnosticLabel(row)}` : ''}
                  {row.error_code && row.error_code !== 'credential_mapping_unconfirmed'
                    ? ` · ${row.error_code}`
                    : ''}
                  {row.auth_index ? ` · 凭证索引 ${row.auth_index}` : ''}
                </li>
              ))}
            </ol>
          </>
        )}
        {error && <p role="alert">{error}</p>}
      </Card>
    </div>
  );
}
