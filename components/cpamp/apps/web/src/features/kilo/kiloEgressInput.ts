import type { KiloEgressInput } from '@/services/api/kiloProxies';

export function validateKiloEgressInput(value: KiloEgressInput): string {
  if (value.candidate_proxy_ids?.length === 0) return '请选择至少一个出口。';
  if (value.capacity_mode === 'ip') {
    if (!Number.isSafeInteger(value.max_ips) || (value.max_ips ?? 0) < 1)
      return '最多分配 IP 数应为正整数。';
    if (
      value.allocation_mode === 'specified' &&
      (!value.specified_ips?.length || value.specified_ips.length > (value.max_ips ?? 1))
    )
      return '请选择至少一个固定 IP，且数量不超过配置上限。';
  }
  return '';
}
