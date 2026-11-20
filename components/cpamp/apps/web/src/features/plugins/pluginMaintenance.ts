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
  if (operation.action !== 'install') {
    const action =
      operation.action === 'configure'
        ? '插件配置'
        : operation.action === 'remove'
          ? '插件移除'
          : '插件维护';
    if (operation.status === 'failed') return `${action}失败`;
    if (operation.status === 'completed' || operation.status === 'completed_disabled') {
      return `${action}完成${operation.status === 'completed_disabled' ? '，插件保持停用' : ''}${unresolvedLicense ? '；许可尚未验证' : ''}`;
    }
    if (operation.status === 'queued') return `等待${action}`;
    if (operation.status === 'installing') return `${action}进行中`;
    if (operation.status === 'awaiting_restart') return `等待重启，${action}尚未完成`;
  }
  return unresolvedLicense && ['completed', 'completed_disabled'].includes(operation.status)
    ? '安装已返回，许可尚未验证'
    : PLUGIN_UPDATE_LABELS[operation.status];
}
