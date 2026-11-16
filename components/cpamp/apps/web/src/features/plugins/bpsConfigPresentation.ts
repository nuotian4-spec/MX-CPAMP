import type { PluginConfigField } from '@/types';

const labels: Record<string, string> = {
  session_proxy_pool_enabled: '启用会话代理池',
  session_proxy_pool: '会话代理列表',
  session_proxy_idle_minutes: '会话空闲释放时间（分钟）',
};

export const isSupportedConfigType = (type: string) =>
  ['string', 'boolean', 'number', 'integer', 'enum', 'array', 'object'].includes(
    type.trim().toLowerCase()
  );

export const bpsConfigPresentation = (pluginId: string, field: PluginConfigField) => ({
  label: pluginId === 'cpa-bps' ? labels[field.name] || field.name : field.name,
  multiline:
    pluginId === 'cpa-bps' &&
    field.name === 'session_proxy_pool' &&
    field.type.trim().toLowerCase() === 'string',
});
