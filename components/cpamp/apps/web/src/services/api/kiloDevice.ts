import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import type { KiloEgressInput } from './kiloProxies';
import type { KiloAccountSaved } from './kilo';
import { normalizeApiBase } from '@/utils/connection';

export interface KiloDeviceInput {
  name: string;
  priority: number;
  enabled: boolean;
  concurrency_limit: number | null;
  allow_direct_when_proxy_full: boolean;
  authorization_egress: { mode: 'direct' | 'pool'; proxy_id?: string; expected_revision?: number };
  inference_egress?: KiloEgressInput & { expected_revision: number };
}
export interface KiloDeviceStarted {
  authorization_egress?: KiloDeviceInput['authorization_egress'];
  inference_egress?: KiloDeviceInput['inference_egress'];
  id: string;
  user_code: string;
  verification_url: string;
  expires_in: number;
  interval: 3;
  inference_egress_mode: 'pool' | 'direct' | 'auto';
  connection_ref: string;
}
export interface KiloDeviceState {
  authorization_egress?: KiloDeviceInput['authorization_egress'];
  inference_egress?: KiloDeviceInput['inference_egress'];
  status: 'pending' | 'approved' | 'saved' | 'denied' | 'expired' | 'cancelled';
  email?: string;
  save_expires_in?: number;
  account_id?: string;
  connection_ref: string;
}
export type KiloDeviceSaved = KiloAccountSaved & {
  credential_saved?: boolean;
  egress_ready?: boolean;
  egress_error?: 'specified_conflict' | 'capacity_wait' | 'storage_failed' | null;
  egress_mode: 'pool' | 'direct' | 'auto';
  inference_egress?: KiloDeviceInput['inference_egress'];
  egress_status: 'available' | 'pending';
  egress_reason:
    | ''
    | 'shared_proxy_not_configured'
    | 'shared_proxy_unverified'
    | 'shared_proxy_unhealthy'
    | 'shared_proxy_capacity'
    | 'auto_egress_waiting'
    | 'auto_egress_draining'
    | 'auto_egress_unavailable'
    | 'auto_egress_storage_failed'
    | 'auto_egress_capacity'
    | 'specified_conflict'
    | 'capacity_wait'
    | 'storage_failed';
};
const config = (scope: ApiClientRequestScope, signal?: AbortSignal) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
  signal,
});
export const kiloDeviceApi = {
  start: (
    input: KiloDeviceInput,
    reference: string,
    scope: ApiClientRequestScope,
    signal?: AbortSignal
  ) =>
    apiClient.post<KiloDeviceStarted>(
      '/kilo/device-auth',
      { ...input, connection_ref: reference },
      config(scope, signal)
    ),
  poll: (id: string, reference: string, scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.post<KiloDeviceState>(
      `/kilo/device-auth/${encodeURIComponent(id)}/poll`,
      { connection_ref: reference },
      config(scope, signal)
    ),
  save: (id: string, reference: string, scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.post<KiloDeviceSaved>(
      `/kilo/device-auth/${encodeURIComponent(id)}/save`,
      { connection_ref: reference },
      config(scope, signal)
    ),
  cancel: (id: string, reference: string, scope: ApiClientRequestScope, signal?: AbortSignal) =>
    apiClient.delete<KiloDeviceState>(`/kilo/device-auth/${encodeURIComponent(id)}`, {
      ...config(scope, signal),
      data: { connection_ref: reference },
    }),
};

export function kiloDeviceError(error: unknown, action?: string): string {
  const failure = error as { status?: number; data?: { code?: string } } | undefined;
  if (action === 'start') {
    if (failure?.status === 503 && failure.data?.code === 'device_egress_unavailable')
      return '设备授权尚未启动：所选授权出口暂不可用，请检查代理健康、容量或出口配置。';
    if (!failure?.status || failure.status >= 500)
      return '设备授权启动结果未确认，请先核实管理连接与授权出口；尚未进入账号保存步骤。';
  }
  switch (failure?.status) {
    case 400:
      return '设备授权字段未被接受，请检查输入。';
    case 401:
      return '管理认证已失效，请重新连接。';
    case 403:
      return '管理请求被拒绝，请核实当前管理连接。';
    case 404:
      return '授权会话不存在或已回收，请核实账号列表。';
    case 409:
      return '连接或授权状态冲突，结果待核；请核实同一会话，连接变化时先刷新账号列表。';
    case 429:
      return '授权查询暂受限，原期限保持不变；稍后显式核实状态。';
    case 499:
      return '本地请求已中止，不代表服务端已取消授权；请核实同一会话。';
    case 503:
      return '操作暂不可用，保存结果待核；请先核实同一会话或账号列表。';
    default:
      return '授权结果未确认，请核实同一会话，勿直接重复发起或保存。';
  }
}
