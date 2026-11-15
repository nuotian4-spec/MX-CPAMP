import { sha256Hex } from '@/utils/apiKeyHash';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { AuthFileItem, CredentialScopedQuotaState } from '@/types';
import type { CpaBpsQuotaResult } from '@/services/api/cpaBpsQuota';
import { getQuotaCredentialStoreKey } from '@/utils/quota/credentialScope';

export interface CpaBpsQuotaState extends CredentialScopedQuotaState {
  connectionScope: string;
  status: 'loading' | 'success' | 'error';
  result?: CpaBpsQuotaResult;
  lastQueriedAt?: string;
  error?: string;
}

export const getCpaBpsQuotaScope = (scope: ApiClientRequestScope): string =>
  sha256Hex(`${scope.apiBase}\u0000${scope.managementKey}`);

export const getCpaBpsQuotaKey = (file: AuthFileItem): string =>
  JSON.stringify([getQuotaCredentialStoreKey(file), file.id ?? '']);

export const getCpaBpsQuotaState = (
  states: Record<string, CpaBpsQuotaState> | undefined,
  file: AuthFileItem
): CpaBpsQuotaState | undefined => states?.[getCpaBpsQuotaKey(file)];
