import type { AuthFilesResponse } from '@/types';
import { authFilesApi } from './authFiles';
import { bpsSettingsApi } from './bpsSettings';
import type { ApiClientRequestScope } from './client';

// Bind the displayed credential list before and after loading, not when probing it.
export const loadBpsBoundAuthFiles = async (
  scope: ApiClientRequestScope
): Promise<AuthFilesResponse> => {
  const before = await bpsSettingsApi.get(scope);
  const result = await authFilesApi.list(scope);
  const after = await bpsSettingsApi.get(scope);
  if (!before.connection_ref || before.connection_ref !== after.connection_ref)
    throw new Error('CPA连接已变更，请刷新凭证列表');
  return {
    ...result,
    files: result.files.map((file) => ({ ...file, bps_connection_ref: before.connection_ref })),
  };
};
