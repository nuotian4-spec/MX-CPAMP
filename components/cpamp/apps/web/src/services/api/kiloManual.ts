import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export interface KiloTestModels {
  models: { id: string }[];
  connection_ref: string;
}
export interface KiloManualResult {
  account_id: string;
  connection_ref: string;
  model: string;
  status: 'completed' | 'incomplete' | 'failed' | 'canceled';
  started_at: string;
  completed_at: string;
  duration_ms: number;
  text: string;
  error_code: 'incomplete' | 'request_failed' | 'canceled' | null;
}
const config = (scope: ApiClientRequestScope, signal: AbortSignal) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
  signal,
});
export const kiloManualApi = {
  models: (scope: ApiClientRequestScope, signal: AbortSignal) =>
    apiClient.get<KiloTestModels>('/kilo/test-models', config(scope, signal)),
  test: (
    id: string,
    model: string,
    prompt: string,
    connectionRef: string,
    scope: ApiClientRequestScope,
    signal: AbortSignal
  ) =>
    apiClient.post<KiloManualResult>(
      `/kilo/accounts/${encodeURIComponent(id)}/test`,
      { model, prompt, connection_ref: connectionRef },
      { ...config(scope, signal), timeout: 0 }
    ),
};
export function kiloManualError(error: unknown): string {
  switch ((error as { status?: number })?.status) {
    case 400:
      return '模型或提示词未被接受，请检查输入。';
    case 401:
      return '管理认证已失效，本次测试已停止。';
    case 403:
      return '管理测试请求被拒绝。';
    case 404:
      return 'Kilo账号不存在或不属于此通道，请刷新列表。';
    case 409:
      return '账号身份或管理连接已变化，原测试结果待核，请刷新后再操作。';
    case 429:
      return '账号与出口准入等待预算已耗尽；未自动重试。';
    case 499:
      return '本次请求已中止，服务端结果未确认。';
    case 503:
      return '测试执行器或本地状态暂不可用。';
    default:
      return '测试回执未确认；未自动重试，请核实后再操作。';
  }
}
