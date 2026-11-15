import { useEffect, useMemo, useRef, useState } from 'react';
import { BatchPasswordLogin, type PasswordLoginStatus } from './BatchPasswordLogin';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { publishAccountCredentialMutationRevision } from '@/stores';
import {
  loginTasksApi,
  bpsOutcome,
  bpsOutcomeLabel,
  type BPSLoginMode,
  type BPSOutcome,
  type BPSOAuthStatus,
} from '@/services/api/loginTasks';
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

type BPSPhase =
  | 'authorizing'
  | 'submitting'
  | 'observing'
  | 'cancelling'
  | 'confirmed'
  | 'failed'
  | 'unknown'
  | 'cancelled';
type BPSAttempt = {
  url: string;
  state: string;
  generation: number;
  scope: ApiClientRequestScope;
  phase: BPSPhase;
  submitted: boolean;
  queryVersion: number;
  querying: boolean;
  notified: boolean;
};
function BPSOAuth({ scope }: { scope: ApiClientRequestScope }) {
  const [session, setSession] = useState<BPSAttempt>();
  const [phase, setPhase] = useState<BPSPhase>();
  const [status, setStatus] = useState('');
  const [lastOutcome, setLastOutcome] = useState<BPSOutcome>();
  const [creating, setCreating] = useState(false);
  const creatingRef = useRef(false);
  const [querying, setQuerying] = useState(false);
  const callback = useRef<HTMLInputElement>(null);
  const liveSession = useRef<BPSAttempt | undefined>(undefined);
  const generation = useRef(0);
  const mounted = useRef(true);
  const currentScope = useRef(scope);
  currentScope.current = scope;
  const isCurrent = (attempt: BPSAttempt) =>
    mounted.current &&
    liveSession.current === attempt &&
    generation.current === attempt.generation &&
    currentScope.current.apiBase === attempt.scope.apiBase &&
    currentScope.current.managementKey === attempt.scope.managementKey;
  const transition = (attempt: BPSAttempt, next: BPSPhase) => {
    if (!isCurrent(attempt)) return;
    attempt.phase = next;
    setPhase(next);
  };
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      generation.current += 1;
      const previous = liveSession.current;
      if (previous && !previous.submitted && previous.phase === 'authorizing') {
        void loginTasksApi.cancelOAuth(previous.state, previous.scope).catch(() => {});
      }
    };
  }, [scope]);
  const recordUnknown = (attempt: BPSAttempt, raw: BPSOAuthStatus) => {
    transition(attempt, 'unknown');
    setLastOutcome(bpsOutcome(raw, attempt.submitted));
    setStatus(
      attempt.submitted
        ? '授权结果尚未确认，可只读查询；不会重新提交回调。'
        : '尚未提交回调，可查询状态或重新开始授权。'
    );
  };
  const consumeResult = (attempt: BPSAttempt, result: BPSOAuthStatus, manual: boolean) => {
    if (!isCurrent(attempt) || attempt.phase === 'confirmed') return;
    if (result.status === 'ok' && result.saved === true && result.adopted === true) {
      attempt.queryVersion += 1;
      transition(attempt, 'confirmed');
      setStatus('账号已保存并注册');
      setLastOutcome({
        provider: 'bps',
        stage: 'cpa_import',
        error_code: 'unknown',
        reason: 'unknown',
        http_status: null,
        save_outcome: 'confirmed',
      });
      if (!attempt.notified) {
        attempt.notified = true;
        publishAccountCredentialMutationRevision({
          connectionFingerprint:
            createCodexInspectionConnectionFingerprint(
              attempt.scope.apiBase,
              attempt.scope.managementKey
            ) || '',
          provider: 'bps',
          kind: 'oauth',
        });
      }
    } else if (result.status === 'wait') {
      if (manual && !attempt.submitted) {
        transition(attempt, 'authorizing');
        setLastOutcome(undefined);
        setStatus('授权会话已恢复，可继续提交完整回调地址。');
      } else if (manual) setStatus('CPA 尚未返回完成结果，可稍后再次查询。');
    } else {
      const outcome = bpsOutcome(result, attempt.submitted);
      if (result.status === 'error' && outcome.save_outcome === 'not_saved') {
        attempt.queryVersion += 1;
        transition(attempt, 'failed');
        setLastOutcome(outcome);
        setStatus('授权未完成');
      } else {
        recordUnknown(attempt, result);
      }
    }
  };
  const queryStatus = async (attempt: BPSAttempt, manual: boolean) => {
    if (
      !isCurrent(attempt) ||
      attempt.querying ||
      !['authorizing', 'observing', 'unknown'].includes(attempt.phase)
    )
      return;
    const version = attempt.queryVersion;
    attempt.querying = true;
    setQuerying(true);
    try {
      const result = await loginTasksApi.status(attempt.state, attempt.scope);
      if (isCurrent(attempt) && attempt.queryVersion === version)
        consumeResult(attempt, result, manual);
    } catch (error) {
      if (!isCurrent(attempt) || attempt.queryVersion !== version) return;
      const detail = (error as { data?: BPSOAuthStatus }).data;
      consumeResult(
        attempt,
        {
          ...detail,
          status: 'error',
          error_code:
            detail?.error_code ||
            ((error as { status?: number }).status === 404
              ? 'session_missing_or_expired'
              : 'oauth_status_unavailable'),
        },
        manual
      );
    } finally {
      attempt.querying = false;
      if (isCurrent(attempt)) setQuerying(false);
    }
  };
  const queryRef = useRef(queryStatus);
  queryRef.current = queryStatus;
  useEffect(() => {
    if (!session || (phase !== 'authorizing' && phase !== 'observing')) return;
    let disposed = false;
    let timer: number;
    const tick = async () => {
      await queryRef.current(session, false);
      if (!disposed) timer = window.setTimeout(tick, 1500);
    };
    timer = window.setTimeout(tick, 1500);
    return () => {
      disposed = true;
      window.clearTimeout(timer);
    };
  }, [session, phase]);
  const start = async (restart = false) => {
    const previous = liveSession.current;
    const restartingUnsubmitted = restart && previous?.phase === 'unknown' && !previous.submitted;
    if (
      creatingRef.current ||
      (previous &&
        !['confirmed', 'failed', 'cancelled'].includes(previous.phase) &&
        !restartingUnsubmitted)
    )
      return;
    creatingRef.current = true;
    setCreating(true);
    setLastOutcome(undefined);
    setStatus('');
    const attemptGeneration = ++generation.current;
    if (restartingUnsubmitted) {
      previous.queryVersion += 1;
      liveSession.current = undefined;
      setSession(undefined);
      setPhase(undefined);
      setQuerying(false);
    }
    const sameCreation = () =>
      mounted.current &&
      attemptGeneration === generation.current &&
      currentScope.current.apiBase === scope.apiBase &&
      currentScope.current.managementKey === scope.managementKey;
    try {
      const created = await loginTasksApi.startBPS(scope);
      if (!sameCreation()) {
        void loginTasksApi.cancelOAuth(created.state, scope).catch(() => {});
        return;
      }
      const attempt: BPSAttempt = {
        ...created,
        scope,
        generation: attemptGeneration,
        phase: 'authorizing',
        submitted: false,
        queryVersion: 0,
        querying: false,
        notified: false,
      };
      liveSession.current = attempt;
      setSession(attempt);
      setPhase('authorizing');
      setQuerying(false);
      setStatus('等待官方授权');
    } catch (error) {
      if (!sameCreation()) return;
      setLastOutcome(
        bpsOutcome(
          {
            ...(error as { data?: BPSOAuthStatus }).data,
            status: 'error',
            error_code: 'oauth_start_failed',
          },
          false
        )
      );
      setStatus('创建授权失败，请检查管理员权限或 CPA。');
    } finally {
      if (sameCreation()) {
        creatingRef.current = false;
        setCreating(false);
      }
    }
  };
  const submit = async () => {
    const attempt = liveSession.current;
    if (
      !attempt ||
      !isCurrent(attempt) ||
      !callback.current ||
      attempt.submitted ||
      attempt.phase !== 'authorizing'
    )
      return;
    let value = callback.current.value;
    callback.current.value = '';
    attempt.submitted = true;
    attempt.queryVersion += 1;
    transition(attempt, 'submitting');
    setLastOutcome(undefined);
    try {
      const result = await loginTasksApi.callback(attempt.state, value, attempt.scope);
      if (!isCurrent(attempt) || liveSession.current?.phase !== 'submitting') return;
      if (result?.status === 'error') consumeResult(attempt, result, false);
      else {
        transition(attempt, 'observing');
        setStatus('回调已接收，等待 CPA 保存注册结果');
      }
    } catch (error) {
      if (!isCurrent(attempt) || liveSession.current?.phase !== 'submitting') return;
      const httpStatus = (error as { status?: number }).status;
      const detail = (error as { data?: BPSOAuthStatus }).data;
      if (httpStatus === 400) {
        attempt.submitted = false;
        transition(attempt, 'authorizing');
        setLastOutcome(
          bpsOutcome(
            {
              ...detail,
              status: 'error',
              error_code: detail?.error_code || 'invalid_callback_input',
            },
            false
          )
        );
        setStatus('回调地址未通过校验，请核对完整地址和本次授权会话。');
      } else {
        consumeResult(
          attempt,
          {
            ...detail,
            status: 'error',
            error_code: detail?.error_code || 'callback_result_unknown',
          },
          false
        );
      }
    } finally {
      value = '';
    }
  };
  const cancel = async () => {
    const attempt = liveSession.current;
    if (!attempt || !isCurrent(attempt) || attempt.submitted || attempt.phase !== 'authorizing')
      return;
    attempt.queryVersion += 1;
    transition(attempt, 'cancelling');
    try {
      const result = await loginTasksApi.cancelOAuth(attempt.state, attempt.scope);
      if (!isCurrent(attempt)) return;
      if (result.cancelled === true) {
        transition(attempt, 'cancelled');
        setLastOutcome(undefined);
        setStatus('授权会话已取消');
      } else
        recordUnknown(attempt, {
          ...result,
          status: 'error',
          error_code: result.error_code || 'oauth_status_unavailable',
        });
    } catch (error) {
      if (!isCurrent(attempt)) return;
      const detail = (error as { data?: BPSOAuthStatus }).data;
      recordUnknown(attempt, {
        ...detail,
        status: 'error',
        error_code: detail?.error_code || 'oauth_status_unavailable',
      });
    }
  };
  const stopObserving = () => {
    const attempt = liveSession.current;
    if (!attempt || !isCurrent(attempt) || attempt.phase !== 'observing') return;
    attempt.queryVersion += 1;
    recordUnknown(attempt, { status: 'error', error_code: 'callback_result_unknown' });
    setStatus('已停止自动查询，可继续只读查询结果。');
  };
  const busy = creating || phase === 'submitting' || phase === 'cancelling';
  const active =
    session && !['confirmed', 'failed', 'unknown', 'cancelled'].includes(session.phase);
  return (
    <div id="oauth-provider-bps">
      <Card title="BPS OAuth">
        <p>生成链接后前往官方页面授权，再提交完整回调地址。仅申请 BPS 授权。</p>
        <Button
          onClick={() => start()}
          disabled={
            creating ||
            Boolean(session && !['confirmed', 'failed', 'cancelled'].includes(session.phase))
          }
        >
          生成授权链接
        </Button>
        {active && (
          <>
            <p>
              <a href={session.url} target="_blank" rel="noopener noreferrer">
                打开 BPS 授权链接
              </a>
            </p>
            <InputURL value={session.url} />
            <p>
              完整回调地址来自官方登录结束后的 bps.openai.com/.../auth/callback 页面， 不是上方
              auth.openai.com 授权链接。请复制地址栏的完整地址。
            </p>
            <label>
              完整回调 URL
              <input ref={callback} type="password" autoComplete="off" />
            </label>
            <Button onClick={submit} disabled={busy || session.submitted}>
              提交回调
            </Button>
            {!session.submitted && (
              <Button onClick={cancel} disabled={busy}>
                取消
              </Button>
            )}
            {phase === 'observing' && <Button onClick={stopObserving}>停止观察</Button>}
          </>
        )}
        {session && phase === 'unknown' && (
          <>
            <Button onClick={() => queryStatus(session, true)} disabled={querying}>
              查询结果
            </Button>
            {!session.submitted && (
              <Button onClick={() => start(true)} disabled={creating}>
                重新开始授权
              </Button>
            )}
          </>
        )}
        <p role="status">{status}</p>
        {lastOutcome && (
          <p role="status" data-testid="bps-last-outcome">
            {bpsOutcomeLabel(lastOutcome)}
          </p>
        )}
      </Card>
    </div>
  );
}
export function InternalLoginCards({ scope }: { scope: ApiClientRequestScope }) {
  const capabilityScope = useMemo(
    () => ({ apiBase: scope.apiBase, managementKey: scope.managementKey }),
    [scope.apiBase, scope.managementKey]
  );
  const [capabilities, setCapabilities] = useState<{
    scope: ApiClientRequestScope;
    status: PasswordLoginStatus;
    bpsMode?: BPSLoginMode;
  }>();
  useEffect(() => {
    let disposed = false;
    loginTasksApi.capabilities(capabilityScope).then(
      (result) => {
        if (!disposed)
          setCapabilities({
            scope: capabilityScope,
            bpsMode:
              result?.bps_login_mode === 'builtin' || result?.bps_login_mode === 'plugin'
                ? result.bps_login_mode
                : 'unavailable',
            status:
              result?.password_login_configured === true
                ? 'configured'
                : result?.password_login_configured === false
                  ? 'unconfigured'
                  : 'error',
          });
      },
      () => {
        if (!disposed) setCapabilities({ scope: capabilityScope, status: 'error' });
      }
    );
    return () => {
      disposed = true;
    };
  }, [capabilityScope]);
  const passwordLoginStatus =
    capabilities?.scope === capabilityScope ? capabilities.status : 'loading';
  const bpsMode = capabilities?.scope === capabilityScope ? capabilities.bpsMode : undefined;
  return (
    <>
      <BatchPasswordLogin profile="codex" scope={scope} passwordLoginStatus={passwordLoginStatus} />
      {bpsMode === 'builtin' ? (
        <>
          <BPSOAuth scope={scope} />
          <BatchPasswordLogin
            profile="bps"
            scope={scope}
            passwordLoginStatus={passwordLoginStatus}
          />
        </>
      ) : (
        <Card title="BPS 登录">
          <p role="status">
            {bpsMode === 'plugin'
              ? '已启用 cpa-bps 插件，请使用插件 OAuth 卡片。BPS 2FA 暂未接入；插件未加载时不会回退旧登录。'
              : 'BPS 登录模式未确认，旧 BPS OAuth 和 2FA 入口已停用。请检查 CPA 插件配置或连接。'}
          </p>
        </Card>
      )}
    </>
  );
}
