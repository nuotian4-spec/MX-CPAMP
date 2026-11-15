import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export type KiloQuotaSourceStatus =
  | 'ready'
  | 'refresh_required'
  | 'needs_sign_in'
  | 'identity_unknown'
  | 'no_outer_material'
  | 'unavailable'
  | 'none'
  | 'source_missing'
  | 'source_changed'
  | 'needs_reimport';
export interface KiloQuotaSourceOption {
  credential_id: string;
  label: string;
  chatgpt_account_id: string | null;
  status: KiloQuotaSourceStatus;
  has_query_material: boolean;
}
export interface KiloQuotaSources {
  sources: KiloQuotaSourceOption[];
  connection_ref: string;
}
export type KiloQuotaBinding =
  | { kind: 'cpa_codex'; credential_id: string; chatgpt_account_id: string }
  | { kind: 'imported'; access_token: string; chatgpt_account_id?: string };
export interface KiloSourcePolicy {
  enabled: boolean;
  interval_seconds: number;
  revision: number;
  paused: boolean;
  pause_generation: number;
  scope: '' | '18000+604800' | '604800' | '2592000';
  status:
    | 'disabled'
    | 'pending'
    | 'ready'
    | 'paused'
    | 'source_pending'
    | 'query_failed'
    | 'waiting_query_egress'
    | 'storage_failed';
  observed_at: string | null;
  next_query_at: string | null;
  error_code: string | null;
  persisted: boolean;
}
export type KiloSourcePolicySnapshot = KiloSourcePolicy & { connection_ref: string };
export type KiloSourcePolicyInput = Pick<KiloSourcePolicy, 'enabled' | 'interval_seconds'> & {
  expected_policy_revision: number;
  confirm_scope?: boolean;
};
export interface KiloQuotaView {
  account_id: string;
  binding_revision: number;
  connection_ref: string;
  source: {
    kind: 'none' | 'cpa_codex' | 'imported';
    credential_id?: string;
    status: KiloQuotaSourceStatus;
  };
  query: {
    status: 'never' | 'running' | 'success' | 'failed' | 'cancelled';
    attempted_at: string | null;
    observed_at: string | null;
    error_code: string | null;
  };
  plan: string | null;
  windows: {
    group: string;
    slot: 'primary_window' | 'secondary_window';
    duration_seconds: number | null;
    remaining_percent: number | null;
    used_percent?: number | null;
    reset_at: string | null;
    source: 'api_query';
  }[];
  credits: { remaining: number | null; source_field: string | null; unit: string | null };
  stale: boolean;
}
const config = (scope: ApiClientRequestScope, signal?: AbortSignal) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
  signal,
});
const path = (id: string) => `/kilo/accounts/${encodeURIComponent(id)}/quota`;
export const kiloQuotaApi = {
  getPolicy: (id: string, scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.get<KiloSourcePolicySnapshot>(
      `/kilo/accounts/${encodeURIComponent(id)}/source-policy`,
      config(scope, signal)
    ),
  putPolicy: (
    id: string,
    input: KiloSourcePolicyInput,
    reference: string,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.put<KiloSourcePolicySnapshot>(
      `/kilo/accounts/${encodeURIComponent(id)}/source-policy`,
      { ...input, connection_ref: reference },
      config(scope, signal)
    ),
  sources: (scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.get<KiloQuotaSources>('/kilo/quota-sources', config(scope, signal)),
  get: (id: string, scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.get<KiloQuotaView>(path(id), config(scope, signal)),
  put: (
    id: string,
    source: KiloQuotaBinding,
    revision: number,
    reference: string,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.put<KiloQuotaView>(
      `${path(id)}-source`,
      { source, expected_binding_revision: revision, connection_ref: reference },
      config(scope, signal)
    ),
  remove: (
    id: string,
    revision: number,
    reference: string,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.delete<KiloQuotaView>(`${path(id)}-source`, {
      ...config(scope, signal),
      data: { expected_binding_revision: revision, connection_ref: reference },
    }),
  query: async (
    id: string,
    revision: number,
    reference: string,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ): Promise<{ view: KiloQuotaView; cancelled: boolean }> => {
    try {
      return {
        view: await apiClient.post<KiloQuotaView>(
          `${path(id)}/query`,
          { expected_binding_revision: revision, connection_ref: reference },
          config(scope, signal)
        ),
        cancelled: false,
      };
    } catch (error) {
      const failure = error as { status?: number; data?: KiloQuotaView };
      const view = failure.data;
      if (
        failure.status === 499 &&
        view?.query?.status === 'cancelled' &&
        typeof view.account_id === 'string' &&
        Number.isSafeInteger(view.binding_revision) &&
        typeof view.connection_ref === 'string' &&
        Array.isArray(view.windows) &&
        view.source &&
        view.credits
      ) {
        return { view, cancelled: true };
      }
      throw error;
    }
  },
};
export const kiloQuotaSourceLabel: Record<KiloQuotaSourceStatus, string> = {
  ready: '可查询',
  refresh_required: '登录凭据待续期',
  needs_sign_in: '需重新登录',
  identity_unknown: '来源身份待确认',
  no_outer_material: '缺少额度查询材料',
  unavailable: '来源暂不可用',
  none: '未绑定',
  source_missing: '引用来源已不存在',
  source_changed: '引用来源身份已变化',
  needs_reimport: '独立材料需重新导入',
};
export function kiloQuotaFailure(code: string | null): string {
  const messages: Record<string, string> = {
    source_missing: '引用来源已不存在',
    source_changed: '引用来源身份已变化',
    identity_unknown: '来源身份待确认',
    no_outer_material: '缺少额度查询材料',
    source_unavailable: '来源暂不可用',
    refresh_failed: '原生命周期续期失败',
    refresh_proxy_invalid: '原续期代理无效',
    needs_reimport: '独立材料需重新导入',
    usage_unauthorized: '额度上游未授权，不影响管理登录',
    usage_forbidden: '额度上游拒绝访问',
    usage_rate_limited: '额度上游限流',
    usage_http_error: '额度上游请求失败',
    usage_invalid_response: '额度上游回执无效',
    usage_network_error: '额度网络请求失败',
    usage_cancelled: '本次额度查询已取消',
    egress_unavailable: 'Kilo正式出口暂不可用',
    query_egress_unavailable: '额度查询出口暂不可用，等待独立查询出口',
  };
  return code ? (messages[code] ?? '额度查询失败，原因未知') : '额度查询失败，原因未知';
}
export function kiloQuotaError(error: unknown): string {
  switch ((error as { status?: number })?.status) {
    case 400:
      return '额度字段未被接受，请检查输入。';
    case 401:
      return '管理认证已失效，请重新连接。';
    case 403:
      return '管理请求被拒绝。';
    case 404:
      return 'Kilo账号已不存在，请刷新列表。';
    case 409:
      return '绑定版本、来源身份或连接已变化，请重新读取当前观测。';
    case 499:
      return '本地请求已中止，服务端结果待核，请重新读取当前观测。';
    default:
      return '额度操作结果待核，请重新读取当前观测，勿直接重复提交。';
  }
}
