import type { AuthFileItem } from '@/types';
import type { ApiClientRequestScope } from '@/services/api/client';
import { bpsResetCreditsApi, type BpsResetCredits } from '@/services/api/bpsResetCredits';
import {
  useQuotaStore,
  captureQuotaCacheGeneration,
  isQuotaCacheGenerationCurrent,
} from '@/stores/useQuotaStore';
import { getCpaBpsQuotaKey, getCpaBpsQuotaScope } from './cpaBpsQuotaState';
export interface BpsResetCreditsState {
  status: 'loading' | 'known' | 'unknown' | 'error';
  connectionScope: string;
  connectionRef: string;
  result?: BpsResetCredits;
  lastKnown?: BpsResetCredits;
  error?: string;
}
export const getBpsResetCreditsKey = getCpaBpsQuotaKey;
const pending = new Map<string, Promise<void>>();
export function refreshBpsResetCredits(
  file: AuthFileItem,
  scope: ApiClientRequestScope,
  reference: string,
  isCurrent: () => boolean = () => true
): Promise<void> {
  const generation = captureQuotaCacheGeneration();
  const key = getBpsResetCreditsKey(file),
    connectionScope = getCpaBpsQuotaScope(scope);
  const requestKey = JSON.stringify([generation, connectionScope, reference, key]);
  const existing = pending.get(requestKey);
  if (existing) return existing;
  const old = useQuotaStore.getState().bpsResetCredits[key];
  const previous =
    old?.connectionScope === connectionScope && old.connectionRef === reference ? old : undefined;
  const lastKnown = previous?.result?.status === 'known' ? previous.result : previous?.lastKnown;
  const current = () => isQuotaCacheGenerationCurrent(generation) && isCurrent();
  const commit = (state: BpsResetCreditsState) => {
    if (current())
      useQuotaStore.getState().setBpsResetCredits((states) => ({ ...states, [key]: state }));
  };
  const identity = { connectionScope, connectionRef: reference, lastKnown };
  const loading: BpsResetCreditsState = { ...identity, status: 'loading' };
  commit(loading);
  const request = (async () => {
    try {
      const result = await bpsResetCreditsApi.query(
        String(file.authIndex ?? file.auth_index ?? ''),
        String(file.id ?? ''),
        scope,
        reference
      );
      commit({
        connectionScope,
        connectionRef: reference,
        status: result.status,
        result,
        lastKnown: result.status === 'known' ? result : lastKnown,
      });
    } catch {
      commit({ ...identity, status: 'error', error: '重置次数查询未确认，请刷新后重试' });
    } finally {
      pending.delete(requestKey);
      if (!current() && isQuotaCacheGenerationCurrent(generation)) {
        useQuotaStore.getState().setBpsResetCredits((states) => {
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
