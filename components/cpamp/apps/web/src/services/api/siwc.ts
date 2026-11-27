import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export interface SiwcIdentity {
  auth_id: string;
  auth_index: string;
}
export interface SiwcTarget extends SiwcIdentity {
  connection_ref: string;
}
export interface SiwcCredential extends SiwcTarget {
  provider: 'codex';
  siwc_bind_supported: boolean;
  use_siwc: boolean;
  siwc_status: 'off' | 'valid' | 'invalid';
  siwc_bound: boolean;
  siwc_expires_at?: string;
  read_only_reason?: string;
}
export interface SiwcSession extends SiwcTarget {
  status: 'wait';
  flow: 'siwc_bind';
  url: string;
  state: string;
  expires_in: number;
}
export type SiwcResult =
  | { status: 'wait'; cancelled?: false }
  | { status: 'error'; code?: string; cancelled?: false }
  | { status: 'ok'; credential: SiwcCredential; cancelled?: false }
  | { status: 'cancelled'; cancelled: true };

export class SiwcProtocolError extends Error {
  constructor(public code: string) {
    super(code);
  }
}
const fail = (code = 'siwc_invalid_response'): never => {
  throw new SiwcProtocolError(code);
};
const object = (value: unknown): Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : fail();
const config = (scope: ApiClientRequestScope, signal?: AbortSignal) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
  signal,
});

export function readSiwcCredential(
  value: unknown,
  target: SiwcIdentity | SiwcTarget
): SiwcCredential {
  const data = object(value);
  if (typeof data.siwc_bind_supported !== 'boolean') fail('siwc_unsupported');
  if (
    data.auth_id !== target.auth_id ||
    data.auth_index !== target.auth_index ||
    data.provider !== 'codex' ||
    typeof data.connection_ref !== 'string' ||
    !data.connection_ref ||
    ('connection_ref' in target && data.connection_ref !== target.connection_ref)
  )
    fail('siwc_identity_changed');
  if (
    typeof data.use_siwc !== 'boolean' ||
    typeof data.siwc_bound !== 'boolean' ||
    !['off', 'valid', 'invalid'].includes(String(data.siwc_status))
  )
    fail();
  return data as unknown as SiwcCredential;
}
function readResult(value: unknown, target: SiwcTarget, cancel = false): SiwcResult {
  const data = object(value);
  if (cancel && typeof data.cancelled !== 'boolean') fail();
  if (data.status === 'cancelled' && cancel && data.cancelled === true)
    return { status: 'cancelled', cancelled: true };
  if (cancel && data.cancelled !== false) fail();
  if (data.status === 'ok')
    return {
      status: 'ok',
      credential: readSiwcCredential(data.credential, target),
      ...(cancel ? { cancelled: false as const } : {}),
    };
  if (data.status === 'wait' || data.status === 'error')
    return {
      status: data.status,
      ...(typeof data.code === 'string' ? { code: data.code } : {}),
      ...(cancel ? { cancelled: false as const } : {}),
    };
  return fail();
}
const identity = (target: SiwcTarget): SiwcTarget => ({
  auth_id: target.auth_id,
  auth_index: target.auth_index,
  connection_ref: target.connection_ref,
});
const sessionParams = (target: SiwcTarget, state: string) => ({ ...identity(target), state });

export const siwcApi = {
  async credential(target: SiwcIdentity, scope: ApiClientRequestScope, signal?: AbortSignal) {
    return readSiwcCredential(
      await apiClient.get('/siwc/credential', { ...config(scope, signal), params: target }),
      target
    );
  },
  async bind(
    target: SiwcTarget,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ): Promise<SiwcSession> {
    const data = object(
      await apiClient.post('/siwc/bind', identity(target), config(scope, signal))
    );
    if (
      data.status !== 'wait' ||
      data.flow !== 'siwc_bind' ||
      data.connection_ref !== target.connection_ref ||
      data.auth_id !== target.auth_id ||
      data.auth_index !== target.auth_index ||
      typeof data.state !== 'string' ||
      !data.state ||
      typeof data.url !== 'string' ||
      data.expires_in !== 300
    )
      fail();
    const url = data.url;
    if (typeof url !== 'string') return fail();
    try {
      if (new URL(url).protocol !== 'https:') fail();
    } catch {
      fail();
    }
    return data as unknown as SiwcSession;
  },
  async callback(
    target: SiwcTarget,
    state: string,
    redirect_url: string,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) {
    const data = object(
      await apiClient.post(
        '/siwc/callback',
        { ...sessionParams(target, state), redirect_url },
        config(scope, signal)
      )
    );
    if (data.status !== 'wait' || data.submitted !== true) fail();
    return { status: 'wait' as const, submitted: true as const };
  },
  async status(
    target: SiwcTarget,
    state: string,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) {
    return readResult(
      await apiClient.get('/siwc/status', {
        ...config(scope, signal),
        params: sessionParams(target, state),
      }),
      target
    );
  },
  async cancel(target: SiwcTarget, state: string, scope: ApiClientRequestScope) {
    return readResult(
      await apiClient.delete('/siwc/session', {
        ...config(scope),
        params: sessionParams(target, state),
      }),
      target,
      true
    );
  },
};

export function siwcErrorKey(error: unknown): string {
  const value = error as { code?: string; status?: number; data?: { code?: string } } | undefined;
  const code = value?.data?.code ?? value?.code;
  if (code === 'siwc_unsupported' || value?.status === 404 || value?.status === 501)
    return 'unsupported';
  if (code === 'siwc_identity_changed' || value?.status === 409) return 'identity_changed';
  if (value?.status === 410 || code === 'siwc_session_expired') return 'expired';
  if (['siwc_callback_invalid', 'siwc_callback_state_mismatch'].includes(String(code)) && value?.status === 400) return 'callback_invalid';
  if (code === 'siwc_invalid_response') return 'invalid_response';
  return 'request_failed';
}
