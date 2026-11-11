import type { AuthFileItem, CodexQuotaState } from '@/types';
import { resolveCredentialIdentity } from '@/utils/authFileCredentialIdentity';
import { buildQuotaCredentialIdentity } from './credentialScope';

export const bpsCredentialUpdatedAtMs = (file: AuthFileItem): number => {
  const values = [
    file.updatedAtMs,
    file.updated_at_ms,
    file.updatedAt,
    file.updated_at,
    file.modified,
    file.modtime,
    file.lastRefresh,
    file.last_refresh,
  ];
  return Math.max(
    0,
    ...values.map((value) => {
      if (value === null || value === undefined || value === '') return 0;
      const numeric = Number(value);
      const parsed = Number.isFinite(numeric)
        ? numeric < 1e12
          ? numeric * 1000
          : numeric
        : Date.parse(String(value));
      return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
    })
  );
};

export const getBpsQuotaStoreKey = (file: AuthFileItem): string => {
  const identity = resolveCredentialIdentity(file);
  return `bps::${JSON.stringify([identity.physicalName, identity.provider, identity.authIndex, identity.accountId, identity.accountSnapshot, bpsCredentialUpdatedAtMs(file)])}`;
};

export const buildBpsQuotaCredentialIdentity = (file?: AuthFileItem) =>
  file
    ? {
        ...buildQuotaCredentialIdentity(file),
        authFileKey: getBpsQuotaStoreKey(file),
      }
    : {};

export const scopeBpsQuotaState = (
  file: AuthFileItem,
  state: CodexQuotaState | undefined
): CodexQuotaState | undefined =>
  state?.authFileKey === getBpsQuotaStoreKey(file) ? state : undefined;

export const getBpsQuotaState = (states: Record<string, CodexQuotaState>, file: AuthFileItem) =>
  scopeBpsQuotaState(file, states[getBpsQuotaStoreKey(file)]);

export const matchesBpsInspectionIdentity = (
  file: AuthFileItem,
  saved:
    | { provider: string; authIndex: string | null; accountId: string; accountSnapshot: string }
    | undefined,
  atMs: number
): boolean => {
  const current = resolveCredentialIdentity(file);
  return Boolean(
    saved &&
    saved.provider === 'bps' &&
    current.provider === 'bps' &&
    saved.authIndex === current.authIndex &&
    (saved.accountId || saved.accountSnapshot) &&
    saved.accountId === current.accountId &&
    saved.accountSnapshot === current.accountSnapshot &&
    atMs > bpsCredentialUpdatedAtMs(file)
  );
};
