import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';

export interface BPSSettings {
  enabled: boolean;
  images_enabled: boolean;
  image_transport: 'native' | 'relay';
  image_relay_base_url: string;
  limits: Record<string, number>;
  connection_ref: string;
}
export type BPSSettingsInput = Pick<BPSSettings, 'enabled' | 'images_enabled' | 'image_transport' | 'image_relay_base_url' | 'connection_ref'>;
const config = (scope: ApiClientRequestScope) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api`,
});
export const bpsSettingsApi = {
  get: (scope: ApiClientRequestScope) => apiClient.get<BPSSettings>('/bps/settings', config(scope)),
  save: (input: BPSSettingsInput, scope: ApiClientRequestScope) => apiClient.put<{ status: 'saved' }>('/bps/settings', input, config(scope)),
};
