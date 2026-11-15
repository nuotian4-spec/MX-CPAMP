import { apiClient, type ApiClientRequestScope } from './client';
import { bpsRequestConfig } from './bpsAccess';
export interface BpsResetCredits {
  provider: 'bps';
  auth_id: string;
  auth_index: string;
  source: 'chatgpt_account_reset_credits';
  query_mode: 'direct';
  queried_at: string;
  count: number | null;
  status: 'known' | 'unknown' | 'error';
  error: string | null;
}
export const bpsResetCreditsApi = {
  async query(
    authIndex: string,
    authId: string,
    scope: ApiClientRequestScope,
    connectionRef: string
  ): Promise<BpsResetCredits> {
    if (!authIndex.trim() || !authId.trim() || !connectionRef)
      throw new Error('请刷新凭证列表建立连接引用');
    const raw = await apiClient.get<BpsResetCredits>('/bps/reset-credits', {
      ...bpsRequestConfig(scope),
      params: { auth_index: authIndex, auth_id: authId, connection_ref: connectionRef },
    });
    if (
      raw.provider !== 'bps' ||
      raw.auth_id !== authId ||
      raw.auth_index !== authIndex ||
      raw.source !== 'chatgpt_account_reset_credits' ||
      raw.query_mode !== 'direct' ||
      !Number.isFinite(Date.parse(raw.queried_at))
    )
      throw new Error('重置次数回执与凭证不匹配');
    const count =
      typeof raw.count === 'number' && Number.isSafeInteger(raw.count) && raw.count >= 0
        ? raw.count
        : null;
    if (raw.status === 'error') return { ...raw, count: null, error: '查询失败' };
    return { ...raw, count, status: count === null ? 'unknown' : 'known', error: null };
  },
};
