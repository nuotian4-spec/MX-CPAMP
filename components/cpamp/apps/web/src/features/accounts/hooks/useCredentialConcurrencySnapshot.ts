import { useCallback, useEffect, useRef, useState } from 'react';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  credentialConcurrencyApi,
  type ConcurrencySnapshot,
} from '@/services/api/credentialConcurrency';

export function useCredentialConcurrencySnapshot(scope: ApiClientRequestScope, enabled = true) {
  const [result, setResult] = useState<{
    scope: ApiClientRequestScope;
    snapshot: ConcurrencySnapshot;
  }>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const sequence = useRef(0);
  const activeScope = useRef(scope);
  activeScope.current = scope;
  const alive = useRef(true);
  const request = useRef<AbortController | null>(null);
  const load = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const attempt = ++sequence.current;
    const current = () =>
      alive.current && activeScope.current === scope && attempt === sequence.current;
    try {
      const snapshot = await credentialConcurrencyApi.list(scope, controller.signal);
      if (current()) {
        setResult({ scope, snapshot });
        setError('');
        return snapshot;
      }
      throw new Error('并发目标已变化，请刷新后核对');
    } catch (error) {
      if (current() && !controller.signal.aborted) setError('并发状态暂不可读，请稍后刷新');
      throw error;
    }
  }, [scope]);
  useEffect(() => {
    if (!enabled) return;
    alive.current = true;
    setError('');
    setBusy(false);
    let pending = false;
    const poll = async () => {
      if (pending || globalThis.document?.visibilityState === 'hidden') return;
      pending = true;
      try {
        await load();
      } catch {
        /* 错误由独立状态区展示。 */
      } finally {
        pending = false;
      }
    };
    void poll();
    const timer = globalThis.setInterval(() => {
      void poll();
    }, 5000);
    const visible = () => {
      if (globalThis.document?.visibilityState !== 'hidden') void poll();
    };
    globalThis.document?.addEventListener?.('visibilitychange', visible);
    return () => {
      alive.current = false;
      sequence.current += 1;
      globalThis.clearInterval(timer);
      globalThis.document?.removeEventListener?.('visibilitychange', visible);
      request.current?.abort();
    };
  }, [load, enabled]);
  const refresh = async () => {
    setBusy(true);
    try {
      await load();
    } catch {
      /* 保留最后确认的快照。 */
    } finally {
      if (alive.current && activeScope.current === scope) setBusy(false);
    }
  };
  return {
    snapshot: result?.scope === scope ? result.snapshot : undefined,
    error,
    busy,
    load,
    refresh,
  };
}

export type CredentialConcurrencyState = ReturnType<typeof useCredentialConcurrencySnapshot>;
