import type { AuthFileItem } from '@/types';
import type { ApiClientRequestScope } from '@/services/api/client';
import { cpaBpsQuotaApi } from '@/services/api/cpaBpsQuota';
import {
  useQuotaStore,
  captureQuotaCacheGeneration,
  isQuotaCacheGenerationCurrent,
} from '@/stores/useQuotaStore';
import { buildQuotaCredentialIdentity } from '@/utils/quota/credentialScope';
import { getCpaBpsQuotaKey, getCpaBpsQuotaScope, type CpaBpsQuotaState } from './cpaBpsQuotaState';

type CpaBpsQuotaRefreshOutcome =
  | { status: 'success' }
  | { status: 'ignored' }
  | { status: 'error'; error: string };

const pending = new Map<string, Promise<CpaBpsQuotaRefreshOutcome>>();

export function refreshCpaBpsQuota(
  file: AuthFileItem,
  scope: ApiClientRequestScope,
  isCurrent: () => boolean = () => true
): Promise<CpaBpsQuotaRefreshOutcome> {
  const generation = captureQuotaCacheGeneration();
  const key = getCpaBpsQuotaKey(file);
  const connectionScope = getCpaBpsQuotaScope(scope);
  const requestKey = `${generation}:${connectionScope}:${key}`;
  const existing = pending.get(requestKey);
  if (existing) return existing;
  const stored = useQuotaStore.getState().cpaBpsQuota[key];
  const previous = stored?.connectionScope === connectionScope ? stored : undefined;
  const current = () => isQuotaCacheGenerationCurrent(generation) && isCurrent();
  const commit = (state: CpaBpsQuotaState) => {
    if (current())
      useQuotaStore.getState().setCpaBpsQuota((states) => ({ ...states, [key]: state }));
  };
  const identity = { ...buildQuotaCredentialIdentity(file), connectionScope };
  const loading: CpaBpsQuotaState = {
    ...identity,
    status: 'loading',
    result: previous?.result,
    lastQueriedAt: previous?.lastQueriedAt,
  };
  commit(loading);
  const request = (async () => {
    try {
      const result = await cpaBpsQuotaApi.query(
        String(file.authIndex ?? file.auth_index ?? ''),
        scope,
        typeof file.id === 'string' ? file.id : undefined
      );
      if (!current()) return { status: 'ignored' as const };
      commit({ ...identity, status: 'success', result, lastQueriedAt: result.queried_at });
      return { status: 'success' as const };
    } catch (cause) {
      const error = cause instanceof Error ? cause.message : '插件额度查询失败';
      if (!current()) return { status: 'ignored' as const };
      commit({
        ...identity,
        status: 'error',
        error,
        result: previous?.result,
        lastQueriedAt: previous?.lastQueriedAt,
      });
      return { status: 'error' as const, error };
    } finally {
      pending.delete(requestKey);
      if (!current() && isQuotaCacheGenerationCurrent(generation)) {
        useQuotaStore.getState().setCpaBpsQuota((states) => {
          if (states[key] !== loading) return states;
          const next = { ...states };
          if (previous) next[key] = previous;
          else delete next[key];
          return next;
        });
      }
    }
  })();
  pending.set(requestKey, request);
  return request;
}
