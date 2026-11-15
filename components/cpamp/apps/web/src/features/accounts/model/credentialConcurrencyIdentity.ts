import type { CredentialConcurrency } from '@/services/api/credentialConcurrency';
import { normalizeAccountProvider, type AccountRow } from './accountRows';

export function matchAccountConcurrency(row: AccountRow, credentials: CredentialConcurrency[]) {
  const id = typeof row.raw.id === 'string' ? row.raw.id : '';
  if (!id) return { status: 'identity-missing' as const };
  const matches = credentials.filter(
    (credential) =>
      credential.auth_id === id &&
      normalizeAccountProvider({ name: '', provider: credential.provider }) ===
        normalizeAccountProvider({ name: '', provider: row.provider }) &&
      (!row.authIndex || credential.auth_index === row.authIndex)
  );
  if (matches.length !== 1)
    return { status: matches.length ? ('ambiguous' as const) : ('missing' as const) };
  return { status: 'matched' as const, credential: matches[0] };
}
