import axios from 'axios';
import { normalizeApiBase } from '@/utils/connection';
import type { PluginImportPreview, UpdateOperation } from '@/types/pluginImport';
import type {
  CoreUpdateDiscovery,
  CoreUpdatePreview,
} from '@/features/settings/SystemUpdateSection';

function scopedUpdateRequest(apiBase: string, managementKey: string) {
  const base = normalizeApiBase(apiBase).replace(/\/v8\/management\/?$/, '');
  const authorization = `Bearer ${managementKey}`;
  return async <T>(path: string, method = 'GET', data?: unknown, zip = false): Promise<T> => {
    if (!base || !managementKey) throw new Error('请先连接目标 CPA 管理服务');
    const response = await axios.request<T>({
      url: `${base}/v8/management${path}`,
      method,
      data,
      timeout: zip ? 120_000 : 45_000,
      headers: {
        Authorization: authorization,
        'Content-Type': zip ? 'application/zip' : 'application/json',
      },
    });
    return response.data;
  };
}

export function createPluginImportApi(apiBase: string, managementKey: string) {
  const request = scopedUpdateRequest(apiBase, managementKey);
  return {
    upload: (file: File) => request<PluginImportPreview>('/plugins/imports', 'POST', file, true),
    cancel: (id: string) => request<void>(`/plugins/imports/${encodeURIComponent(id)}`, 'DELETE'),
    install: (
      id: string,
      confirmation: { confirm_unverified: boolean; confirm_license_network: boolean }
    ) =>
      request<UpdateOperation>(`/plugins/imports/${encodeURIComponent(id)}/install`, 'POST', {
        confirm: true,
        ...confirmation,
      }),
    getOperation: (id: string) =>
      request<UpdateOperation>(`/updates/operations/${encodeURIComponent(id)}`),
    getLatestOperation: () =>
      request<{ operation: UpdateOperation | null }>('/updates/operations/latest?type=plugin'),
  };
}

export function createCoreUpdateApi(apiBase: string, managementKey: string) {
  const request = scopedUpdateRequest(apiBase, managementKey);
  return {
    discover: () => request<CoreUpdateDiscovery>('/updates/core'),
    preview: (releaseId: string) =>
      request<CoreUpdatePreview>('/updates/core/previews', 'POST', { release_id: releaseId }),
    install: (candidateId: string) =>
      request<UpdateOperation>(
        `/updates/core/previews/${encodeURIComponent(candidateId)}/install`,
        'POST',
        { confirm: true }
      ),
    getOperation: (id: string) =>
      request<UpdateOperation>(`/updates/operations/${encodeURIComponent(id)}`),
  };
}
