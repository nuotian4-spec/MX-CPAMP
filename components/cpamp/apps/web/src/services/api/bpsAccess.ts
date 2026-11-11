import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export interface BPSAccess {
  provider: 'bps';
  auth_index: string;
  auth_id: string;
  queried_at: string;
  allowed: boolean | null;
  models: string[] | null;
  upstream_status: number | null;
  error: string | null;
  quota: null;
  inference_verified: false;
  auto_recover_eligible: false;
}

export const bpsRequestConfig = (scope: ApiClientRequestScope) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
});

export const bpsAccessApi = {
  async query(
    authIndex: string,
    scope: ApiClientRequestScope,
    connectionRef: string
  ): Promise<BPSAccess> {
    if (!authIndex.trim() || !connectionRef) throw new Error('请刷新凭证列表建立连接引用');
    const result = await apiClient.get<BPSAccess>('/bps/access', {
      ...bpsRequestConfig(scope),
      params: { auth_index: authIndex, connection_ref: connectionRef },
    });
    if (
      result.provider !== 'bps' ||
      result.auth_index !== authIndex ||
      result.quota !== null ||
      result.inference_verified !== false ||
      result.auto_recover_eligible !== false ||
      !Number.isFinite(Date.parse(result.queried_at)) ||
      (result.error === null &&
        (typeof result.allowed !== 'boolean' ||
          !Array.isArray(result.models) ||
          result.upstream_status !== 200)) ||
      (result.error !== null && (result.allowed !== null || result.models !== null))
    ) {
      throw new Error('BPS权限回执无效');
    }
    return result;
  },
};

export const bpsAccessMessage = (result: BPSAccess): string => {
  if (result.error) return `BPS权限查询失败：${result.error}；额度未知；推理未验证`;
  return `${result.allowed ? 'BPS访问权限查询通过' : 'BPS访问权限受限'}；额度未知；推理未验证`;
};
