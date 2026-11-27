import { useEffect, useRef, useState } from 'react';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  siwcApi,
  siwcErrorKey,
  type SiwcCredential,
  type SiwcIdentity,
  type SiwcResult,
  type SiwcSession,
} from '@/services/api/siwc';

type Phase =
  | 'loading'
  | 'idle'
  | 'starting'
  | 'wait'
  | 'submitting'
  | 'cancelling'
  | 'ok'
  | 'cancelled'
  | 'error';
type View = {
  owner: string;
  phase: Phase;
  credential?: SiwcCredential;
  session?: SiwcSession;
  error?: string;
  submitted?: boolean;
};
type ActiveSession = {
  value: SiwcSession;
  scope: ApiClientRequestScope;
  controller: AbortController;
  deadline: number;
  submitted?: boolean;
  cancelling?: boolean;
  poll?: ReturnType<typeof setTimeout>;
  expiry?: ReturnType<typeof setTimeout>;
};

export function useSiwcBinding(
  target: SiwcIdentity,
  scope: ApiClientRequestScope,
  readOnly: boolean
) {
  const owner = JSON.stringify([
    scope.apiBase,
    scope.managementKey,
    target.auth_id,
    target.auth_index,
  ]);
  const currentOwner = useRef(owner);
  currentOwner.current = owner;
  const mounted = useRef(false);
  const sequence = useRef(0);
  const active = useRef<ActiveSession | undefined>(undefined);
  const reading = useRef<AbortController | undefined>(undefined);
  const [view, setView] = useState<View>({ owner, phase: 'loading' });
  const visible = view.owner === owner ? view : { owner, phase: 'loading' as const };
  const valid = () => mounted.current && currentOwner.current === owner;
  const update = (changes: Partial<View>) => {
    if (valid())
      setView((v) => ({ ...(v.owner === owner ? v : { owner, phase: 'idle' }), ...changes }));
  };
  const clear = (session: ActiveSession) => {
    clearTimeout(session.poll);
    clearTimeout(session.expiry);
    session.controller.abort();
    if (active.current === session) active.current = undefined;
  };
  const applies = (session: ActiveSession) => valid() && active.current === session;
  const accept = (session: ActiveSession, result: SiwcResult) => {
    if (!applies(session)) return;
    if (result.status === 'ok') {
      clear(session);
      update({ phase: 'ok', credential: result.credential, session: undefined, error: undefined });
    } else if (result.status === 'cancelled') {
      clear(session);
      update({ phase: 'cancelled', session: undefined, error: undefined });
    } else if (result.status === 'error') {
      clear(session);
      update({ phase: 'error', session: undefined, error: 'session_failed' });
    }
  };
  const cancelSession = async (session: ActiveSession, expired = false) => {
    if (session.cancelling) return;
    session.cancelling = true;
    clearTimeout(session.poll);
    session.controller.abort();
    if (applies(session)) update({ phase: 'cancelling' });
    try {
      const result = await siwcApi.cancel(session.value, session.value.state, session.scope);
      if (!applies(session)) return;
      if (result.status === 'wait') {
        session.controller = new AbortController();
        update({ phase: 'wait', error: expired ? 'expired_unconfirmed' : 'cancel_pending' });
        if (!expired) session.poll = setTimeout(() => void poll(session), 1500);
      } else accept(session, result);
    } catch (error) {
      if (applies(session)) {
        const key = siwcErrorKey(error);
        if (['expired', 'identity_changed', 'unsupported'].includes(key)) {
          clear(session);
          update({ phase: 'error', session: undefined, error: key });
        } else {
          session.controller = new AbortController();
          update({ phase: 'error', error: expired ? 'expired_unconfirmed' : key });
        }
      }
    } finally {
      session.cancelling = false;
    }
  };
  const poll = async (session: ActiveSession) => {
    if (!applies(session) || Date.now() >= session.deadline) return;
    const controller = session.controller;
    try {
      const result = await siwcApi.status(
        session.value,
        session.value.state,
        session.scope,
        controller.signal
      );
      if (!applies(session) || controller.signal.aborted || session.controller !== controller)
        return;
      accept(session, result);
    } catch (error) {
      if (!applies(session) || controller.signal.aborted || session.controller !== controller)
        return;
      const key = siwcErrorKey(error);
      if (['unsupported', 'identity_changed', 'expired'].includes(key)) {
        clear(session);
        update({ phase: 'error', session: undefined, error: key });
        return;
      }
      update({ error: key });
    }
    if (applies(session) && session.controller === controller && !controller.signal.aborted)
      session.poll = setTimeout(() => void poll(session), 1500);
  };
  const load = async () => {
    const attempt = ++sequence.current;
    reading.current?.abort();
    const controller = new AbortController();
    reading.current = controller;
    update({ phase: 'loading', error: undefined });
    try {
      const credential = await siwcApi.credential(target, scope, controller.signal);
      if (valid() && sequence.current === attempt) update({ phase: 'idle', credential });
    } catch (error) {
      if (valid() && sequence.current === attempt && !controller.signal.aborted)
        update({ phase: 'error', error: siwcErrorKey(error), credential: undefined });
    }
  };
  useEffect(() => {
    mounted.current = true;
    if (target.auth_id && target.auth_index) void load();
    else update({ phase: 'error', error: 'identity_missing' });
    return () => {
      mounted.current = false;
      sequence.current += 1;
      reading.current?.abort();
      const session = active.current;
      if (session) {
        clear(session);
        void siwcApi.cancel(session.value, session.value.state, session.scope).catch(() => {});
      }
    };
    // 绑定仅随连接与精确凭证身份切换，状态更新不重启会话。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [owner]);

  const start = async () => {
    const credential = visible.credential;
    if (
      !valid() ||
      readOnly ||
      active.current ||
      !credential?.siwc_bind_supported ||
      visible.phase === 'starting'
    )
      return;
    const attempt = ++sequence.current;
    const controller = new AbortController();
    reading.current = controller;
    update({ phase: 'starting', error: undefined });
    try {
      const value = await siwcApi.bind(credential, scope, controller.signal);
      if (!valid() || attempt !== sequence.current) {
        void siwcApi.cancel(value, value.state, scope).catch(() => {});
        return;
      }
      const session: ActiveSession = {
        value,
        scope,
        controller: new AbortController(),
        deadline: Date.now() + 300_000,
      };
      active.current = session;
      update({ phase: 'wait', session: value, submitted: false });
      session.expiry = setTimeout(() => void cancelSession(session, true), 300_000);
      session.poll = setTimeout(() => void poll(session), 1500);
    } catch (error) {
      if (valid() && attempt === sequence.current && !controller.signal.aborted)
        update({ phase: 'error', error: siwcErrorKey(error) });
    }
  };
  const submit = async (redirectUrl: string) => {
    const session = active.current;
    if (
      !session ||
      !applies(session) ||
      session.submitted ||
      session.cancelling ||
      !redirectUrl.trim() ||
      Date.now() >= session.deadline
    )
      return;
    session.submitted = true;
    const controller = session.controller;
    update({ phase: 'submitting', error: undefined, submitted: true });
    try {
      await siwcApi.callback(
        session.value,
        session.value.state,
        redirectUrl.trim(),
        session.scope,
        controller.signal
      );
      if (applies(session) && session.controller === controller && !controller.signal.aborted)
        update({ phase: 'wait', submitted: true });
    } catch (error) {
      if (applies(session) && session.controller === controller && !controller.signal.aborted) {
        const key = siwcErrorKey(error);
        if (['expired', 'identity_changed', 'unsupported'].includes(key)) {
          clear(session);
          update({ phase: 'error', session: undefined, error: key });
        } else {
          session.submitted = key !== 'callback_invalid';
          update({ phase: 'wait', submitted: session.submitted, error: key });
        }
      }
    }
  };
  const check = async () => {
    const session = active.current;
    if (!session || !applies(session)) return;
    try {
      accept(session, await siwcApi.status(session.value, session.value.state, session.scope));
    } catch (error) {
      if (applies(session)) {
        const key = siwcErrorKey(error);
        if (['expired', 'identity_changed', 'unsupported'].includes(key)) {
          clear(session);
          update({ phase: 'error', session: undefined, error: key });
        } else update({ error: key });
      }
    }
  };
  return {
    ...visible,
    start,
    submit,
    check,
    refresh: load,
    cancel: () => {
      const session = active.current;
      if (session) return cancelSession(session);
    },
  };
}
