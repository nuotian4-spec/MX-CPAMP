import type { CpaBpsQuota } from '@/services/api/cpaBpsQuota';
import { formatTimestamp } from './accountsPagePresentation';

export const formatCpaBpsWindow = (raw: string): string => {
  const value = raw.trim();
  const match = /^(\d+)\s*([mhd])$/i.exec(value);
  if (!match) return value || '额度窗口';
  const amount = Number(match[1]);
  if (!Number.isFinite(amount) || amount <= 0) return value;
  const unit = match[2].toLowerCase() as 'm' | 'h' | 'd';
  const minutes = amount * { m: 1, h: 60, d: 1440 }[unit];
  if (minutes % 1440 === 0) return `${minutes / 1440} 天窗口`;
  if (minutes % 60 === 0) return `${minutes / 60} 小时窗口`;
  return `${minutes} 分钟窗口`;
};

export const formatCpaBpsGroup = (raw: string): string =>
  raw.trim().toLowerCase() === 'chatgpt account quota'
    ? 'ChatGPT 账号额度'
    : raw.trim() || '额度窗口';

export const formatCpaBpsPlan = (quota: CpaBpsQuota): string => {
  const plan = quota.subscription?.tierName.trim() || quota.subscription?.plan.trim() || '';
  return plan.toLowerCase() === 'free' ? 'Free' : plan;
};

export const formatCpaBpsTime = (raw: string | undefined): string => {
  if (!raw?.trim()) return '未知';
  const timestamp = Date.parse(raw);
  if (!Number.isFinite(timestamp)) return '未知';
  const formatted = formatTimestamp(timestamp, 'zh-CN');
  return formatted === '-' ? '未知' : formatted;
};

export const cpaBpsRemainingPercent = (fraction: number | null): number | null =>
  fraction !== null && Number.isFinite(fraction) && fraction >= 0 && fraction <= 1
    ? fraction * 100
    : null;

export const formatCpaBpsPercent = (percent: number | null): string =>
  percent === null
    ? '未知'
    : `${new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 }).format(percent)}%`;

export const presentCpaBpsMetric = (metric: CpaBpsQuota['summary'][number]) => {
  const isResetCount = metric.key === 'reset_cards_available';
  return {
    label: isResetCount ? '可用重置次数' : metric.label || metric.key || '额度信息',
    value:
      metric.value === null
        ? '未知'
        : new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 4 }).format(metric.value),
    unit: isResetCount ? '次' : metric.currency || metric.unit,
    hint: isResetCount ? '用于重置的可用次数，与推理剩余额度不同' : '',
  };
};

export const cpaBpsResetCount = (quota: CpaBpsQuota): number | null => {
  const values = quota.summary
    .filter((metric) => metric.key === 'reset_cards_available')
    .map((metric) => metric.value);
  if (
    !values.length ||
    values.some((value) => value === null || !Number.isSafeInteger(value) || value < 0) ||
    values.some((value) => value !== values[0])
  )
    return null;
  return values[0];
};
