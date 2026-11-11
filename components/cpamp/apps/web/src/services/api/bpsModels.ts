import { apiClient, type ApiClientRequestScope } from './client';
import { bpsRequestConfig } from './bpsAccess';

export interface BPSModelsSnapshot {
  models: string[];
  connection_ref: string;
  revision: string;
}
export const normalizeBpsModelDraft = (draft: string): string[] => [
  ...new Set(
    draft
      .split(/\r?\n/)
      .map((id) => id.trim())
      .filter(Boolean)
  ),
];
export const bpsModelsApi = {
  get: (scope: ApiClientRequestScope) =>
    apiClient.get<BPSModelsSnapshot>('/bps/models', bpsRequestConfig(scope)),
  save: (snapshot: BPSModelsSnapshot, models: string[], scope: ApiClientRequestScope) =>
    apiClient.put('/bps/models', { ...snapshot, models }, bpsRequestConfig(scope)),
};

export const describeBpsModelPermission = (
  id: string,
  allowedModels: string[] | null,
  allowed: boolean | null
): string =>
  allowedModels === null || allowed === null
    ? '权限未确认'
    : allowed && allowedModels.includes(id)
      ? '官方允许；推理未验证'
      : '权限不匹配';
