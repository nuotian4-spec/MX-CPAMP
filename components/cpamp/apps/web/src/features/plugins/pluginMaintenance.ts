import type { UpdateOperation } from '@/types/pluginImport';

export const PLUGIN_UPDATE_LABELS: Record<UpdateOperation['status'], string> = {
  queued: '等待安装',
  installing: '正在安装',
  reloading: '正在重新加载插件',
  completed: '安装完成',
  completed_disabled: '安装完成，插件保持停用',
  failed: '安装失败',
  license_unverified: '许可尚未验证',
  license_error: '许可验证失败',
  awaiting_restart: '等待重启，安装尚未完成',
  recovery_required: '任务中断，需核实恢复',
};

export function pluginMaintenanceLabel(
  operation: UpdateOperation,
  unresolvedLicense: boolean
): string {
  return unresolvedLicense && ['completed', 'completed_disabled'].includes(operation.status)
    ? '安装已返回，许可尚未验证'
    : PLUGIN_UPDATE_LABELS[operation.status];
}
