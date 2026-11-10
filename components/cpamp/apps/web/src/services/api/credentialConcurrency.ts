import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';
export interface CredentialConcurrency {
  auth_id: string;
  auth_index: string;
  name: string;
  provider: string;
  source: 'file' | 'config' | 'runtime';
  editable: boolean;
  read_only_reason: '' | 'runtime_source' | 'plugin_virtual' | 'keyless_group';
  concurrency_limit: number | null;
  effective_limit: number;
  limit_source: 'default' | 'override';
  active: number;
  pinned_waiting: number;
}
export type ConcurrencySnapshot =
  | { mode: 'home'; credentials: [] }
  | {
      mode: 'local';
      connection_ref: string;
      default_limit: number;
      pool_waiting: number;
      credentials: CredentialConcurrency[];
    };
const config = (scope: ApiClientRequestScope) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
});
export const credentialConcurrencyApi = {
  list: (scope: ApiClientRequestScope) =>
    apiClient.get<ConcurrencySnapshot>('/credential-concurrency', config(scope)),
  update: (
    auth_id: string,
    concurrency_limit: number | null,
    connection_ref: string,
    scope: ApiClientRequestScope
  ) =>
    apiClient.patch<{ status: 'saved' }>(
      '/credential-concurrency',
      { auth_id, concurrency_limit, connection_ref },
      config(scope)
    ),
};
