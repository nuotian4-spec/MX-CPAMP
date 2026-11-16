import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';

export interface BpsLicenseStatus {
  active: boolean;
  required: boolean;
  code: string;
  keyHint: string;
  boundIp: string;
  licenseExpiresAt: string;
  expiresAt: string;
  hasLastError: boolean;
}

export type BpsLicenseFailure =
  | 'missing'
  | 'unauthorized'
  | 'network'
  | 'timeout'
  | 'invalid'
  | 'server';

export class BpsLicenseError extends Error {
  readonly reason: BpsLicenseFailure;
  constructor(reason: BpsLicenseFailure) {
    super(reason);
    this.reason = reason;
  }
}

const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const dateText = (value: unknown): string =>
  typeof value === 'string' && /^\d{4}-\d{2}-\d{2}[\dTtZz :+.\-]*$/.test(value) ? value : '';

export function normalizeBpsLicense(value: unknown): BpsLicenseStatus {
  if (
    !record(value) ||
    typeof value.active !== 'boolean' ||
    typeof value.required !== 'boolean' ||
    typeof value.code !== 'string' ||
    !value.code.trim()
  ) {
    throw new BpsLicenseError('invalid');
  }
  // 只接纳脱敏提示；完整授权码和供应商原始错误从不进入展示状态。
  const hint = typeof value.key_hint === 'string' ? value.key_hint : '';
  const keyHint = /^[A-Za-z0-9*•…._\- ]{1,80}$/.test(hint) && /[*•…]/.test(hint) ? hint : '';
  const ip = typeof value.bound_ip === 'string' ? value.bound_ip : '';
  return {
    active: value.active,
    required: value.required,
    code: [
      'active',
      'ok',
      'license_required',
      'license_expired',
      'license_invalid',
      'not_activated',
    ].includes(value.code)
      ? value.code
      : 'unknown',
    keyHint,
    boundIp: /^[\da-fA-F:.]{2,45}$/.test(ip) ? ip : '',
    licenseExpiresAt: dateText(value.license_expires_at),
    expiresAt: dateText(value.expires_at),
    hasLastError: Boolean(value.last_error),
  };
}

export function classifyBpsLicenseError(error: unknown): BpsLicenseFailure {
  if (error instanceof BpsLicenseError) return error.reason;
  if (!record(error)) return 'network';
  if (error.status === 404) return 'missing';
  if (error.status === 401 || error.status === 403) return 'unauthorized';
  if (error.code === 'ECONNABORTED' || error.code === 'ETIMEDOUT') return 'timeout';
  return typeof error.status === 'number' ? 'server' : 'network';
}

export const createBpsLicenseApi = (scope: ApiClientRequestScope) => {
  const captured = { apiBase: scope.apiBase, managementKey: scope.managementKey };
  const request = () => createScopedApiRequestConfig(captured);
  return {
    async get(): Promise<BpsLicenseStatus> {
      return normalizeBpsLicense(await apiClient.get('/cpa-bps/license', request()));
    },
    async activate(licenseKey: string): Promise<void> {
      await apiClient.post('/cpa-bps/license/activate', { license_key: licenseKey }, request());
    },
    async refresh(): Promise<void> {
      await apiClient.post('/cpa-bps/license/refresh', {}, request());
    },
  };
};
