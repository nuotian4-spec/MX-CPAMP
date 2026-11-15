import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export type KiloProbeStatus =
  | 'never'
  | 'scheduled'
  | 'running'
  | 'completed'
  | 'incomplete'
  | 'failed'
  | 'canceled'
  | 'skipped';
export type KiloProbeError =
  | 'incomplete'
  | 'request_failed'
  | 'canceled'
  | 'account_disabled'
  | 'account_restricted'
  | 'account_unavailable'
  | 'account_busy';
export interface KiloProbeState {
  enabled: boolean;
  model: string;
  interval_seconds: number;
  status: KiloProbeStatus;
  next_run_at: string | null;
  last_started_at: string | null;
  last_completed_at: string | null;
  error_code: KiloProbeError | null;
  model_calls: number;
  skipped_count: number;
}
export interface KiloProbesSnapshot {
  account_id: string;
  connection_ref: string;
  revision: number;
  runtime_started_at: string;
  manual_model_calls: number;
  health: KiloProbeState;
  model_probe: KiloProbeState;
}
export interface KiloProbesConfig {
  expected_revision: number;
  health: { enabled: boolean };
  model_probe: { enabled: boolean; model: string; interval_seconds: number };
}
const config = (scope: ApiClientRequestScope, signal: AbortSignal) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
  signal,
});
export const kiloProbesApi = {
  get: (id: string, scope: ApiClientRequestScope, signal: AbortSignal) =>
    apiClient.get<KiloProbesSnapshot>(
      `/kilo/accounts/${encodeURIComponent(id)}/probes`,
      config(scope, signal)
    ),
  save: (
    id: string,
    input: KiloProbesConfig,
    connectionRef: string,
    scope: ApiClientRequestScope,
    signal: AbortSignal
  ) =>
    apiClient.put<KiloProbesSnapshot>(
      `/kilo/accounts/${encodeURIComponent(id)}/probes`,
      {
        expected_revision: input.expected_revision,
        health: { enabled: input.health.enabled },
        model_probe: {
          enabled: input.model_probe.enabled,
          model: input.model_probe.model,
          interval_seconds: input.model_probe.interval_seconds,
        },
        connection_ref: connectionRef,
      },
      config(scope, signal)
    ),
};
export function kiloProbesError(error: unknown): string {
  switch ((error as { status?: number })?.status) {
    case 400:
      return '探测配置未被接受，请检查模型和60–86400整数秒周期。';
    case 401:
      return '管理认证已失效，本页操作已停止。';
    case 403:
      return '自动生成管理请求被拒绝。';
    case 404:
      return 'Kilo账号不存在或不属于此通道，请刷新列表。';
    case 409:
      return '配置版本、账号或管理连接已变化，请重新读取；原目标状态待核。';
    case 499:
      return '本页请求已中止，服务器保存结果待核，请重新读取。';
    case 503:
      return '自动生成配置暂不可用，请重新读取确认原状态。';
    default:
      return '自动生成配置或状态回执未确认，请重新读取；未自动重试。';
  }
}
