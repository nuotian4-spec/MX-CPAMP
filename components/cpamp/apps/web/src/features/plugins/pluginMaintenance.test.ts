import { describe, expect, it } from 'vitest';
import { pluginMaintenanceLabel } from './pluginMaintenance';
import type { UpdateOperation } from '@/types/pluginImport';

describe('插件维护动作', () => {
  it.each([
    [undefined, '插件维护失败'],
    ['configure', '插件配置失败'],
    ['remove', '插件移除失败'],
    ['install', '安装失败'],
  ])('%s', (action, expected) => {
    expect(pluginMaintenanceLabel({ action, status: 'failed' } as UpdateOperation, false)).toBe(
      expected
    );
  });
});

it.each([undefined, 'configure', 'remove'])('停用终态 %s 不使用安装标签', (action) => {
  const text = pluginMaintenanceLabel(
    { action, status: 'completed_disabled' } as UpdateOperation,
    true
  );
  expect(text).toContain('保持停用');
  expect(text).toContain('许可尚未验证');
  expect(text).not.toContain('安装');
});
