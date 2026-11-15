import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export type KiloAdmissionReason =
  | ''
  | 'unverified'
  | 'probe_failed'
  | 'ip_unknown'
  | 'ip_changed'
  | 'not_public_ip'
  | 'duplicate_ip';
export interface KiloProxyAdmission {
  admission_status?: 'legacy' | 'pending' | 'admitted';
  admission_reason?: KiloAdmissionReason;
  admitted_ip?: string;
  admitted_at?: string | null;
}
export interface KiloProxyPreview {
  status: 'ready' | 'pending' | 'duplicate';
  reason: KiloAdmissionReason;
  observed_exit_ip: string | null;
  observed_at: string | null;
  registered_proxy_id?: string;
}
export const kiloAdmissionReason: Record<KiloAdmissionReason, string> = {
  '': '',
  unverified: '尚未核验',
  probe_failed: '观测失败',
  ip_unknown: '公网IP未知',
  ip_changed: '公网IP已变化',
  not_public_ip: '未观测到公网IP',
  duplicate_ip: '公网IP已登记',
};
export function kiloProxyDuplicate(error: unknown): { ip?: string; proxy_id?: string } | null {
  const failure = error as {
    status?: number;
    data?: { error?: unknown; ip?: unknown; proxy_id?: unknown };
  };
  if (failure?.status !== 409 || failure.data?.error !== 'proxy_ip_already_registered') return null;
  return {
    ip:
      typeof failure.data.ip === 'string' && /^[0-9a-fA-F:.]+$/.test(failure.data.ip)
        ? failure.data.ip
        : undefined,
    proxy_id:
      typeof failure.data.proxy_id === 'string' &&
      /^[A-Za-z0-9_-]{1,128}$/.test(failure.data.proxy_id)
        ? failure.data.proxy_id
        : undefined,
  };
}
export interface KiloProxy extends KiloProxyAdmission {
  inference_active?: number;
  management_active?: number;
  effective_limit?: number;
  fixed_exit_ip?: string;
  observed_exit_ip?: string | null;
  observed_at?: string | null;
  observation_status?: 'unknown' | 'pending' | 'ok' | 'failed' | 'stale';
  owner_kind?: 'none' | 'account' | 'device';
  id: string;
  name: string;
  order: number;
  enabled: boolean;
  account_id: string;
  scheme: string;
  host: string;
  port: string;
  has_auth: boolean;
  healthy: boolean;
  active: number;
  owner: string;
}
export interface KiloRecycledProxy extends KiloProxyAdmission {
  fixed_exit_ip?: string;
  retirement_reason?: KiloRetirementReason;
  id: string;
  name: string;
  order: number;
  enabled: boolean;
  original_account_id: string;
  scheme: string;
  host: string;
  port: string;
  has_auth: boolean;
  archived_at: string;
}
export interface KiloEgress {
  capacity_mode?: 'fixed' | 'ip';
  allocation_mode?: 'auto' | 'specified';
  max_ips?: number;
  specified_ips?: string[];
  assigned_ips?: string[];
  configured_capacity?: number;
  current_capacity?: number;
  inference_active?: number;
  management_active?: number;
  manual_limit?: number | null;
  effective_limit?: number;
  entry_limit?: number | null;
  egress_ready?: boolean;

  assigned_ip?: string;
  allocation_state?: 'manual' | 'waiting' | 'assigned' | 'draining' | 'egress_unavailable' | '';
  allocation_reason?:
    | KiloRetirementReason
    | 'storage_failed'
    | 'specified_conflict'
    | 'capacity_wait'
    | 'partial_egress_unavailable';
  candidate_proxy_ids?: string[] | null;
  account_id: string;
  mode: 'pool' | 'direct' | 'auto';
  primary_proxy_id: string;
}
export interface KiloProxyState {
  ip_concurrency_limit?: number;
  ip_entry_limit?: number | null;
  auto_assignment_enabled?: boolean;
  max_unique_accounts?: number;
  auto_error?: 'storage_failed' | null;
  ip_groups?: KiloIPGroup[];
  revision: number;
  runtime_revision: number;
  managed: boolean;
  proxy_limit: number;
  proxies: KiloProxy[];
  recycle: KiloRecycledProxy[];
  accounts: KiloEgress[];
  connection_ref: string;
}
export type KiloRetirementReason =
  | ''
  | 'quota_return'
  | 'account_removed'
  | 'unique_account_limit'
  | 'max_ips_reduced'
  | 'fixed_capacity'
  | 'egress_unavailable';
export const kiloRetirementReason: Record<KiloRetirementReason, string> = {
  '': '无',
  max_ips_reduced: 'IP 上限下调，等待多余绑定排空',
  fixed_capacity: '切换固定并发，等待多余绑定排空',
  egress_unavailable: '出口故障，等待在途排空',
  quota_return: '额度暂停归还',
  account_removed: '账号移除归还',
  unique_account_limit: '累计账号数达到上限',
};
export interface KiloIPGroup {
  active?: number;
  inference_active?: number;
  management_active?: number;
  capacity?: number;
  entry_limit?: number | null;
  ip: string;
  owner_account_id: string;
  account_ids: string[];
  draining: boolean;
  retired: boolean;
  reason: KiloRetirementReason;
  unique_accounts: number;
  max_unique_accounts: number;
  state: 'available' | 'assigned' | 'retiring' | 'draining' | 'retired' | 'unverified' | 'at_limit';
  proxy_ids: string[];
}
export interface KiloAutoAssignmentInput {
  ip_concurrency_limit?: number;
  ip_entry_limit?: number | null;
  auto_assignment_enabled: boolean;
  max_unique_accounts?: number;
}
export type KiloProxyInput = Partial<
  Pick<KiloProxy, 'name' | 'order' | 'enabled' | 'account_id'>
> & { url?: string; fixed_exit_ip?: string };
export interface KiloEgressInput {
  capacity_mode?: 'fixed' | 'ip';
  allocation_mode?: 'auto' | 'specified';
  max_ips?: number;
  specified_ips?: string[];

  candidate_proxy_ids?: string[] | null;
  mode: 'pool' | 'direct' | 'auto';
  exclusive_proxy_ids: string[];
  primary_proxy_id: string;
}
const config = (scope: ApiClientRequestScope, signal?: AbortSignal) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
  signal,
});
const version = (snapshot: KiloProxyState) => ({
  expected_revision: snapshot.revision,
  connection_ref: snapshot.connection_ref,
});
export const kiloProxiesApi = {
  preview: (
    url: string,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.post<KiloProxyPreview>(
      '/kilo/proxies/preview',
      { url, connection_ref: snapshot.connection_ref },
      config(scope, signal)
    ),
  autoAssignment: (
    input: KiloAutoAssignmentInput,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.patch<KiloProxyState>(
      '/kilo/proxy-pool',
      { ...input, ...version(snapshot) },
      config(scope, signal)
    ),
  list: (scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.get<KiloProxyState>('/kilo/proxies', config(scope, signal)),
  recycle: (scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.get<KiloProxyState>('/kilo/proxy-recycle', config(scope, signal)),
  archive: (
    id: string,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.delete<KiloProxyState>(`/kilo/proxies/${encodeURIComponent(id)}`, {
      ...config(scope, signal),
      data: version(snapshot),
    }),
  restore: (
    id: string,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.post<KiloProxyState>(
      `/kilo/proxy-recycle/${encodeURIComponent(id)}/restore`,
      version(snapshot),
      config(scope, signal)
    ),
  clear: (snapshot: KiloProxyState, scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.delete<KiloProxyState>('/kilo/proxy-recycle', {
      ...config(scope, signal),
      data: { ...version(snapshot), confirm: true },
    }),
  recheck: (
    id: string,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.post<KiloProxyState>(
      `/kilo/proxies/${encodeURIComponent(id)}/recheck`,
      version(snapshot),
      config(scope, signal)
    ),
  create: (
    input: KiloProxyInput,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.post<KiloProxyState>(
      '/kilo/proxies',
      { ...input, ...version(snapshot) },
      config(scope, signal)
    ),
  update: (
    id: string,
    input: KiloProxyInput,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.patch<KiloProxyState>(
      `/kilo/proxies/${encodeURIComponent(id)}`,
      { ...input, ...version(snapshot) },
      config(scope, signal)
    ),
  capacity: (
    limit: number,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.patch<KiloProxyState>(
      '/kilo/proxy-pool',
      { proxy_limit: limit, ...version(snapshot) },
      config(scope, signal)
    ),
  egress: (
    id: string,
    input: KiloEgressInput,
    snapshot: KiloProxyState,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.put<KiloProxyState>(
      `/kilo/accounts/${encodeURIComponent(id)}/egress`,
      { ...input, ...version(snapshot) },
      config(scope, signal)
    ),
};
export function kiloProxyError(error: unknown): string {
  if (kiloProxyDuplicate(error)) return '公网IP已登记，重复条目未保存；请查看原记录。';
  switch ((error as { status?: number })?.status) {
    case 400:
      return '代理或出口字段未被接受，请检查输入。';
    case 401:
      return '管理认证已失效，请重新连接。';
    case 404:
      return '代理或账号已不存在，请刷新核对。';
    case 409:
      return '版本、连接已变化或资源有在途请求，结果待核；请刷新核对后重新打开表单并显式提交。';
    case 499:
      return '请求已取消，未确认完成；请刷新核对后显式操作。';
    case 502:
      return '健康检测或操作结果未确认；恢复失败会保留回收站，请刷新核对。';
    case 503:
      return '代理管理暂不可用，结果请刷新确认，勿直接重复提交。';
    default:
      return '代理管理结果待核，请刷新确认，勿直接重复提交。';
  }
}
