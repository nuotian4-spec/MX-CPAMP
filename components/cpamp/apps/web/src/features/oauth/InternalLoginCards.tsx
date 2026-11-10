import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { publishAccountCredentialMutationRevision } from '@/stores';
import {
  loginTasksApi,
  isLoginTaskActive,
  loginTaskLabel,
  type LoginProfile,
  type LoginTask,
} from '@/services/api/loginTasks';
import type { ApiClientRequestScope } from '@/services/api/client';
import { createCodexInspectionConnectionFingerprint } from '@/features/monitoring/codexInspection';

function PasswordLogin({
  profile,
  scope,
}: {
  profile: LoginProfile;
  scope: ApiClientRequestScope;
}) {
  const [task, setTask] = useState<LoginTask>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const form = useRef<HTMLFormElement>(null);
  const liveTask = useRef<LoginTask | undefined>(undefined);
  const generation = useRef(0);
  liveTask.current = task;
  const active = isLoginTaskActive(task);
  const taskID = task?.id;
  useEffect(
    () => () => {
      generation.current += 1;
      const previous = liveTask.current;
      if (previous && isLoginTaskActive(previous) && previous.status !== 'importing') {
        void loginTasksApi.cancel(previous.id, scope).catch(() => {});
      }
    },
    [scope]
  );
  useEffect(() => {
    if (!taskID || !active) return;
    let disposed = false;
    const timer = window.setInterval(() => {
      loginTasksApi
        .get(taskID, scope)
        .then((next) => {
          if (disposed) return;
          setTask(next);
          if (next.status === 'succeeded')
            publishAccountCredentialMutationRevision({
              connectionFingerprint:
                createCodexInspectionConnectionFingerprint(scope.apiBase, scope.managementKey) ||
                '',
              provider: profile,
              kind: 'oauth',
            });
        })
        .catch(() => {
          if (!disposed) setError('任务状态暂不可读，请稍后查询；不要重复提交账号。');
        });
    }, 1000);
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, [taskID, active, scope, profile]);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setBusy(true);
    setError('');
    const data = new FormData(event.currentTarget);
    const input = {
      profile,
      email: String(data.get('email') || ''),
      password: String(data.get('password') || ''),
      totp_secret: String(data.get('totp_secret') || ''),
    };
    form.current?.reset();
    const attempt = generation.current;
    try {
      const created = await loginTasksApi.create(input, scope);
      if (attempt !== generation.current) {
        void loginTasksApi.cancel(created.id, scope).catch(() => {});
        return;
      }
      setTask(created);
    } catch {
      setError('登录任务创建失败，检查管理员权限或内部登录服务。');
    } finally {
      input.password = '';
      input.totp_secret = '';
      data.delete('password');
      data.delete('totp_secret');
      setBusy(false);
    }
  };
  const cancel = async () => {
    if (!task) return;
    try {
      setTask(await loginTasksApi.cancel(task.id, scope));
      setError('');
    } catch {
      setError('导入已开始或服务暂不可达，请查询真实结果。');
    }
  };
  return (
    <div id={`oauth-provider-${profile}-2fa`}>
      <Card title={`${profile === 'bps' ? 'BPS' : 'Codex'} 2FA`}>
        <p>
          本次仅申请 {profile === 'bps' ? 'Excel/BPS' : 'Codex'} 授权。密码与 TOTP
          种子仅当次使用，不用于无人值守重登。
        </p>
        <form ref={form} onSubmit={submit} autoComplete="off">
          <Input
            name="email"
            type="email"
            label="邮箱"
            required
            disabled={busy || active}
            autoComplete="off"
          />
          <Input
            name="password"
            type="password"
            label="密码"
            required
            disabled={busy || active}
            autoComplete="new-password"
          />
          <Input
            name="totp_secret"
            type="password"
            label="TOTP 种子"
            required
            disabled={busy || active}
            autoComplete="off"
          />
          <Button type="submit" disabled={busy || active}>
            {busy ? '正在创建' : '开始登录'}
          </Button>
          {active && (
            <Button
              type="button"
              onClick={cancel}
              disabled={task?.status === 'importing' || task?.status === 'cancelling'}
            >
              取消
            </Button>
          )}
        </form>
        {task && (
          <p role="status">
            {loginTaskLabel(task)}
            {task.auth_id ? ` · ${task.auth_id}` : ''}
            {task.error_code
              ? ` · ${task.error_code === 'password_rejected' ? '密码校验失败，请重新输入' : task.error_code === 'totp_rejected' ? 'TOTP校验失败，请检查种子' : task.error_code}`
              : ''}
          </p>
        )}
        {error && <p role="alert">{error}</p>}
      </Card>
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
    } catch {
      setStatus('回调提交失败；官方页面可能已消费授权码，不自动重试。');
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
        <p>仅申请 Excel/BPS 授权。官方回调可能先消费授权码；真实可兑换性待用户授权验收。</p>
        <Button onClick={start} disabled={busy || Boolean(session)}>
          打开授权入口
        </Button>
        {session && (
          <>
            <p>
              <a href={session.url} target="_blank" rel="noopener noreferrer">
                前往官方授权页面
              </a>
            </p>
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
      <PasswordLogin profile="codex" scope={scope} />
      <BPSOAuth scope={scope} />
      <PasswordLogin profile="bps" scope={scope} />
    </>
  );
}
