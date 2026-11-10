import { useEffect, useRef, useState } from 'react';
import { BatchPasswordLogin } from './BatchPasswordLogin';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { publishAccountCredentialMutationRevision } from '@/stores';
import { loginTasksApi } from '@/services/api/loginTasks';
import type { ApiClientRequestScope } from '@/services/api/client';
import { createCodexInspectionConnectionFingerprint } from '@/features/monitoring/codexInspection';

function InputURL({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div>
      <label>
        授权链接
        <input
          readOnly
          value={value}
          aria-label="BPS 授权链接"
          onFocus={(event) => event.target.select()}
        />
      </label>
      <Button
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
          } catch {
            setCopied(false);
          }
        }}
      >
        {copied ? '已复制' : '复制授权链接'}
      </Button>
    </div>
  );
}

function BPSOAuth({ scope }: { scope: ApiClientRequestScope }) {
  const [session, setSession] = useState<{ url: string; state: string }>();
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);
  const callback = useRef<HTMLInputElement>(null);
  const liveSession = useRef(session);
  liveSession.current = session;
  const generation = useRef(0);
  useEffect(
    () => () => {
      generation.current += 1;
      const previous = liveSession.current;
      if (previous) void loginTasksApi.cancelOAuth(previous.state, scope).catch(() => {});
    },
    [scope]
  );
  useEffect(() => {
    if (!session) return;
    let disposed = false;
    const timer = window.setInterval(() => {
      loginTasksApi
        .status(session.state, scope)
        .then((result) => {
          if (disposed) return;
          if (result.status === 'ok') {
            setStatus('账号已保存并注册');
            setSession(undefined);
            publishAccountCredentialMutationRevision({
              connectionFingerprint:
                createCodexInspectionConnectionFingerprint(scope.apiBase, scope.managementKey) ||
                '',
              provider: 'bps',
              kind: 'oauth',
            });
          } else if (result.status === 'error') {
            setStatus('授权失败，请检查官方页面验证');
            setSession(undefined);
          }
        })
        .catch(() => {
          if (!disposed) setStatus('状态查询失败，请稍后查询，不要重复提交回调。');
        });
    }, 1500);
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, [session, scope]);
  const start = async () => {
    setBusy(true);
    const attempt = generation.current;
    try {
      const created = await loginTasksApi.startBPS(scope);
      if (attempt !== generation.current) {
        void loginTasksApi.cancelOAuth(created.state, scope).catch(() => {});
        return;
      }
      setSession(created);
      setStatus('等待官方授权');
    } catch {
      setStatus('创建授权失败，请检查管理员权限或 CPA。');
    } finally {
      setBusy(false);
    }
  };
  const submit = async () => {
    if (!session || !callback.current) return;
    let value = callback.current.value;
    callback.current.value = '';
    setBusy(true);
    try {
      await loginTasksApi.callback(session.state, value, scope);
      setStatus('回调已接收，等待 CPA 保存注册结果');
    } catch (error) {
      const status = (error as { status?: number }).status;
      setStatus(
        status === 400
          ? '回调地址未通过校验，请核对完整地址和本次授权会话。'
          : '回调结果未确认，请查询状态；不会自动重复兑换授权码。'
      );
    } finally {
      value = '';
      setBusy(false);
    }
  };
  const cancel = async () => {
    if (!session) return;
    try {
      await loginTasksApi.cancelOAuth(session.state, scope);
      setSession(undefined);
      setStatus('授权会话已取消');
    } catch {
      setStatus('取消结果待确认，请查询 CPA 状态。');
    }
  };
  return (
    <div id="oauth-provider-bps">
      <Card title="BPS OAuth">
        <p>生成链接后前往官方页面授权，再提交完整回调地址。仅申请 BPS 授权。</p>
        <Button onClick={start} disabled={busy || Boolean(session)}>
          生成授权链接
        </Button>
        {session && (
          <>
            <p>
              <a href={session.url} target="_blank" rel="noopener noreferrer">
                打开 BPS 授权链接
              </a>
            </p>
            <InputURL value={session.url} />
            <label>
              完整回调 URL
              <input ref={callback} type="password" autoComplete="off" />
            </label>
            <Button onClick={submit} disabled={busy}>
              提交回调
            </Button>
            <Button onClick={cancel} disabled={busy}>
              取消
            </Button>
          </>
        )}
        <p role="status">{status}</p>
      </Card>
    </div>
  );
}
export function InternalLoginCards({ scope }: { scope: ApiClientRequestScope }) {
  return (
    <>
      <BatchPasswordLogin profile="codex" scope={scope} />
      <BPSOAuth scope={scope} />
      <BatchPasswordLogin profile="bps" scope={scope} />
    </>
  );
}
