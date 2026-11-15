import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import type { KiloQuotaView, KiloQuotaSourceStatus, KiloSourcePolicy } from './kiloQuota';
import { normalizeApiBase } from '@/utils/connection';

export type KiloClientIdentityExit =
  | { kind: 'ip'; ip: string }
  | { kind: 'direct' }
  | { kind: 'legacy' };
export interface KiloClientIdentity {
  state: 'persisted' | 'pending' | 'invalid';
  profiles: { ref: string; exits?: KiloClientIdentityExit[] }[];
}

export interface KiloAccount {
  client_identity?: KiloClientIdentity | null;
  source_policy?: KiloSourcePolicy;
  quota_source_summary?: {
    kind: 'none' | 'cpa_codex' | 'imported';
    credential_id: string;
    display_label: string;
    status: KiloQuotaSourceStatus;
    binding_revision: number;
  };
  quota_snapshot?: KiloQuotaView;
  auto_restriction?: {
    reason: 'none' | 'quota_exhausted' | 'account_restricted';
    generation: number;
    identity_revision: number;
    source_revision: number;
    recover_at: string | null;
    query_status:
      | 'never'
      | 'running'
      | 'success'
      | 'failed'
      | 'cancelled'
      | 'no_source'
      | 'no_future_reset';
    error_code: string | null;
    persisted: boolean;
  };
  id: string;
  name: string;
  organization_id?: string;
  enabled: boolean;
  priority: number;
  concurrency_limit: number | null;
  effective_limit: number;
  active: number;
  waiting: number;
  allow_direct_when_proxy_full: boolean;
}
export interface KiloAccountsSnapshot {
  accounts: KiloAccount[];
  connection_ref: string;
}
export type KiloAccountInput = Partial<
  Pick<
    KiloAccount,
    'name' | 'enabled' | 'priority' | 'concurrency_limit' | 'allow_direct_when_proxy_full'
  >
> & { api_key?: string; organization_id?: string };
export interface KiloOrganizations {
  organizations: { id: string; name: string }[];
  has_personal_account: boolean;
  connection_ref: string;
}
export type KiloAccountSaved = KiloAccount & { connection_ref: string };
const config = (scope: ApiClientRequestScope) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
});
export const kiloApi = {
  organizations: (id: string, scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.get<KiloOrganizations>(`/kilo/accounts/${encodeURIComponent(id)}/organizations`, {
      ...config(scope),
      signal,
    }),
  list: (scope: ApiClientRequestScope) =>
    apiClient.get<KiloAccountsSnapshot>('/kilo/accounts', config(scope)),
  create: (input: KiloAccountInput, connectionRef: string, scope: ApiClientRequestScope) =>
    apiClient.post<KiloAccountSaved>(
      '/kilo/accounts',
      { ...input, connection_ref: connectionRef },
      config(scope)
    ),
  update: (
    id: string,
    input: KiloAccountInput,
    connectionRef: string,
    scope: ApiClientRequestScope
  ) =>
    apiClient.patch<KiloAccountSaved>(
      `/kilo/accounts/${encodeURIComponent(id)}`,
      { ...input, connection_ref: connectionRef },
      config(scope)
    ),
  remove: (id: string, connectionRef: string, scope: ApiClientRequestScope) =>
    apiClient.delete<void>(`/kilo/accounts/${encodeURIComponent(id)}`, {
      ...config(scope),
      data: { connection_ref: connectionRef },
    }),
};

export function kiloErrorMessage(error: unknown): string {
  const status = (error as { status?: number } | undefined)?.status;
  switch (status) {
    case 400:
      return '账号字段未被接受，请检查输入。';
    case 401:
      return '管理认证已失效，请重新连接。';
    case 404:
      return '账号已不存在，请刷新列表。';
    case 403:
      return '待用Key没有所选组织或个人空间权限，账号未保存。请重新选择或核实Key。';
    case 499:
      return '本次请求已中止，保存结果待核，请刷新确认。';
    case 409:
      return '账号状态或连接已变更，操作结果待核，请刷新确认后再操作。';
    case 503:
      return '账号管理暂不可用，请刷新确认。';
    default:
      return '请求结果待核，请刷新确认，勿直接重复提交。';
  }
}

export function kiloOrganizationLabel(value: string | undefined): string {
  return value === undefined ? '组织信息未返回' : value === '' ? '个人空间' : value;
}
