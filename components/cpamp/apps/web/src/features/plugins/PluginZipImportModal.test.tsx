import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import type { ReactNode } from 'react';
import type { PluginImportPreview, UpdateOperation } from '@/types/pluginImport';
import { PluginZipImportModal, type PluginZipImportModalProps } from './PluginZipImportModal';

vi.mock('@/components/ui/Modal', () => ({
  Modal: ({
    children,
    footer,
    onClose,
    closeDisabled,
  }: {
    children: ReactNode;
    footer: ReactNode;
    onClose: () => void;
    closeDisabled: boolean;
  }) => (
    <div role="dialog">
      {children}
      {footer}
      <button aria-label="关闭弹窗" disabled={closeDisabled} onClick={onClose} />
    </div>
  ),
}));

const preview = (overrides: Partial<PluginImportPreview> = {}): PluginImportPreview => ({
  id: 'import-one',
  plugin_id: 'sample-plugin',
  version: '2.0.0',
  platform: 'linux-amd64',
  library_path: 'sample/plugin.so',
  license_required: false,
  current_version: '1.0.0',
  currently_enabled: false,
  compatibility: 'verified',
  installable: true,
  blockers: [],
  ...overrides,
});
const operation = (status: UpdateOperation['status']): UpdateOperation => ({
  id: 'operation-one',
  type: 'plugin_import',
  target_version: '2.0.0',
  status,
  stage: status,
  active_version: status === 'completed' ? '2.0.0' : '1.0.0',
  registered: status === 'completed',
  license_status: 'verified',
  message: '',
  started_at: '2026-11-16T00:00:00Z',
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const text = (node: ReactTestInstance | string): string =>
  typeof node === 'string' ? node : node.children.map(text).join('');

let renderer: ReactTestRenderer;
let props: PluginZipImportModalProps;
type ImportApi = PluginZipImportModalProps['api'];
let api: { [Method in keyof ImportApi]: Mock<ImportApi[Method]> };
const button = (label: string) =>
  renderer.root.findAllByType('button').find((node) => text(node) === label)!;
const click = async (label: string) => {
  await act(async () => {
    button(label).props.onClick();
  });
};
const check = async (label: string, checked: boolean) => {
  await act(async () => {
    renderer.root.findByProps({ 'aria-label': label }).props.onChange({ target: { checked } });
  });
};
const unverifiedConfirmation = '我确认本固定候选兼容性未验证';
const licenseConfirmation = '我确认启动或重新加载可能联网验证许可';
const mount = async () => {
  await act(async () => {
    renderer = create(<PluginZipImportModal {...props} />);
  });
};
const update = async (next: Partial<PluginZipImportModalProps>) => {
  props = { ...props, ...next };
  await act(async () => {
    renderer.update(<PluginZipImportModal {...props} />);
  });
};
const upload = async () => {
  await act(async () => {
    renderer.root.findByProps({ 'aria-label': '选择 ZIP 文件' }).props.onChange({
      target: { files: [new File(['synthetic'], 'sample.zip', { type: 'application/zip' })] },
    });
  });
  await click('上传并预览');
};

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  api = {
    upload: vi.fn().mockResolvedValue(preview()),
    cancel: vi.fn().mockResolvedValue(undefined),
    install: vi.fn().mockResolvedValue(operation('queued')),
    getOperation: vi.fn().mockResolvedValue(operation('installing')),
  };
  props = {
    isOpen: true,
    connectionScope: 'connection-a',
    onClose: vi.fn(),
    onInstalled: vi.fn(),
    api,
  };
});
afterEach(async () => {
  if (renderer)
    await act(async () => {
      renderer.unmount();
    });
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('插件 ZIP 导入边界', () => {
  it('预览显示原停用及许可影响，取消调用预览取消而非安装', async () => {
    api.upload.mockResolvedValue(preview({ license_required: true }));
    await mount();
    await upload();
    expect(text(renderer.root)).toContain('安装不会自动启用');
    expect(text(renderer.root)).toContain('可能连接插件许可服务');
    await click('取消导入');
    expect(api.cancel).toHaveBeenCalledExactlyOnceWith('import-one');
    expect(api.install).not.toHaveBeenCalled();
    expect(props.onClose).toHaveBeenCalledOnce();
  });

  it.each([
    { compatibility: 'incompatible' as const, installable: true },
    { compatibility: 'verified' as const, installable: false },
    { compatibility: 'unverified' as const, installable: false },
  ])('不兼容或不可安装的包仍硬阻断：%o', async (state) => {
    api.upload.mockResolvedValue(preview({ ...state, blockers: ['平台待核验'] }));
    await mount();
    await upload();
    expect(button('确认安装').props.disabled).toBe(true);
    await click('确认安装');
    expect(api.install).not.toHaveBeenCalled();
    expect(text(renderer.root)).toContain('平台待核验');
  });

  it('未验证候选须独立勾选确认，安装后仍标未验证', async () => {
    api.upload.mockResolvedValue(preview({ compatibility: 'unverified' }));
    api.getOperation.mockResolvedValue(operation('completed'));
    await mount();
    await upload();
    expect(button('确认安装').props.disabled).toBe(true);
    expect(renderer.root.findByProps({ 'aria-label': unverifiedConfirmation }).props.checked).toBe(
      false
    );
    await click('确认安装');
    expect(api.install).not.toHaveBeenCalled();
    await check(unverifiedConfirmation, true);
    expect(button('确认安装').props.disabled).toBe(false);
    await click('确认安装');
    expect(api.install).toHaveBeenCalledExactlyOnceWith('import-one', {
      confirm_unverified: true,
      confirm_license_network: false,
    });
    expect(text(renderer.root)).toContain('尚未验证');
    expect(props.onInstalled).toHaveBeenCalledOnce();
  });

  it('许可联网必须单独显式确认后提交', async () => {
    api.upload.mockResolvedValue(preview({ license_required: true }));
    await mount();
    await upload();
    expect(text(renderer.root)).toContain('启动或重新加载可能连接插件许可服务');
    expect(renderer.root.findAllByProps({ 'aria-label': unverifiedConfirmation })).toHaveLength(0);
    expect(renderer.root.findByProps({ 'aria-label': licenseConfirmation }).props.checked).toBe(
      false
    );
    expect(button('确认安装').props.disabled).toBe(true);
    await click('确认安装');
    expect(api.install).not.toHaveBeenCalled();
    await check(licenseConfirmation, true);
    expect(button('确认安装').props.disabled).toBe(false);
    await click('确认安装');
    expect(api.install).toHaveBeenCalledExactlyOnceWith('import-one', {
      confirm_unverified: false,
      confirm_license_network: true,
    });
  });

  it('未验证与许可联网确认相互独立，齐备后一次提交', async () => {
    api.upload.mockResolvedValue(preview({ compatibility: 'unverified', license_required: true }));
    await mount();
    await upload();
    expect(button('确认安装').props.disabled).toBe(true);
    await check(unverifiedConfirmation, true);
    expect(button('确认安装').props.disabled).toBe(true);
    await click('确认安装');
    expect(api.install).not.toHaveBeenCalled();
    await check(unverifiedConfirmation, false);
    await check(licenseConfirmation, true);
    expect(button('确认安装').props.disabled).toBe(true);
    await click('确认安装');
    expect(api.install).not.toHaveBeenCalled();
    await check(unverifiedConfirmation, true);
    expect(button('确认安装').props.disabled).toBe(false);
    expect(
      renderer.root.findAllByType('button').filter((node) => text(node) === '确认安装')
    ).toHaveLength(1);
    await click('确认安装');
    expect(api.install).toHaveBeenCalledExactlyOnceWith('import-one', {
      confirm_unverified: true,
      confirm_license_network: true,
    });
  });

  it('许可需求未知也须显式联网确认，安装结果保留许可未验证', async () => {
    api.upload.mockResolvedValue(preview({ license_required: false, license_status: 'unknown' }));
    api.getOperation.mockResolvedValue({
      ...operation('license_unverified'),
      license_status: 'unknown',
    });
    await mount();
    await upload();
    expect(text(renderer.root)).toContain('许可需求尚未知');
    expect(text(renderer.root)).not.toContain('导入预览未声明必需的许可验证');
    expect(button('确认安装').props.disabled).toBe(true);
    await click('确认安装');
    expect(api.install).not.toHaveBeenCalled();
    await check(licenseConfirmation, true);
    await click('确认安装');
    expect(api.install).toHaveBeenCalledExactlyOnceWith('import-one', {
      confirm_unverified: false,
      confirm_license_network: true,
    });
    expect(renderer.root.findByProps({ 'data-import-state': 'attention' })).toBeDefined();
    expect(text(renderer.root)).toContain('许可尚未验证');
    expect(props.onInstalled).not.toHaveBeenCalled();
  });

  it.each(['unknown', 'unverified', 'not_required'])(
    '许可需求未知时不将 completed/%s 展示为绿色完成',
    async (licenseStatus) => {
      api.upload.mockResolvedValue(preview({ license_required: false, license_status: 'unknown' }));
      api.getOperation.mockResolvedValue({
        ...operation('completed'),
        license_status: licenseStatus,
      });
      await mount();
      await upload();
      await check(licenseConfirmation, true);
      await click('确认安装');
      expect(renderer.root.findAllByProps({ 'data-import-state': 'success' })).toHaveLength(0);
      expect(text(renderer.root)).toContain('许可尚未验证');
      expect(text(renderer.root)).not.toContain('许可状态：not_required');
      expect(props.onInstalled).not.toHaveBeenCalled();
    }
  );

  it('切换连接后同候选也重新确认，不沿用旧勾选', async () => {
    api.upload.mockResolvedValue(preview({ compatibility: 'unverified', license_required: true }));
    await mount();
    await upload();
    await check(unverifiedConfirmation, true);
    await check(licenseConfirmation, true);
    expect(button('确认安装').props.disabled).toBe(false);
    await update({ connectionScope: 'connection-b' });
    await upload();
    expect(renderer.root.findByProps({ 'aria-label': unverifiedConfirmation }).props.checked).toBe(
      false
    );
    expect(renderer.root.findByProps({ 'aria-label': licenseConfirmation }).props.checked).toBe(
      false
    );
    expect(button('确认安装').props.disabled).toBe(true);
    await click('确认安装');
    expect(api.install).not.toHaveBeenCalled();
  });

  it('接受安装不算成功，同任务查询到真实终态才通知完成', async () => {
    api.getOperation
      .mockResolvedValueOnce(operation('installing'))
      .mockResolvedValueOnce(operation('completed'));
    await mount();
    await upload();
    await click('确认安装');
    expect(props.onInstalled).not.toHaveBeenCalled();
    expect(renderer.root.findByProps({ 'data-import-state': 'pending' })).toBeDefined();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(api.getOperation.mock.calls).toEqual([['operation-one'], ['operation-one']]);
    expect(props.onInstalled).toHaveBeenCalledOnce();
    expect(renderer.root.findByProps({ 'data-import-state': 'success' })).toBeDefined();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });
    expect(api.getOperation).toHaveBeenCalledTimes(2);
  });

  it('即使安装响应声称完成也等查询确认，连续确认只提交一次', async () => {
    const accepted = deferred<UpdateOperation>();
    const queried = deferred<UpdateOperation>();
    api.install.mockReturnValue(accepted.promise);
    api.getOperation.mockReturnValue(queried.promise);
    await mount();
    await upload();
    const confirm = button('确认安装');
    await act(async () => {
      confirm.props.onClick();
      confirm.props.onClick();
    });
    expect(api.install).toHaveBeenCalledExactlyOnceWith('import-one', {
      confirm_unverified: false,
      confirm_license_network: false,
    });
    await act(async () => {
      accepted.resolve(operation('completed'));
    });
    expect(props.onInstalled).not.toHaveBeenCalled();
    expect(text(renderer.root)).toContain('正在核验安装结果');
    await act(async () => {
      queried.resolve(operation('completed'));
    });
    expect(props.onInstalled).toHaveBeenCalledOnce();
  });

  it('切换连接后丢弃旧上传结果', async () => {
    const uploaded = deferred<PluginImportPreview>();
    api.upload.mockReturnValue(uploaded.promise);
    await mount();
    await upload();
    await update({ connectionScope: 'connection-b' });
    await act(async () => {
      uploaded.resolve(preview());
    });
    expect(text(renderer.root)).not.toContain('sample-plugin');
    expect(api.install).not.toHaveBeenCalled();
    expect(props.onInstalled).not.toHaveBeenCalled();
  });

  it('切换连接后丢弃旧任务终态，不刷新新连接', async () => {
    const queried = deferred<UpdateOperation>();
    api.getOperation.mockReturnValue(queried.promise);
    await mount();
    await upload();
    await click('确认安装');
    await update({ connectionScope: 'connection-b' });
    await act(async () => {
      queried.resolve(operation('completed'));
    });
    expect(props.onInstalled).not.toHaveBeenCalled();
    expect(text(renderer.root)).not.toContain('operation-one');
    expect(api.cancel).not.toHaveBeenCalled();
  });

  it('维护开始后关窗不取消后台任务，也不接收迟到结果', async () => {
    const queried = deferred<UpdateOperation>();
    api.getOperation.mockReturnValue(queried.promise);
    await mount();
    await upload();
    await click('确认安装');
    await click('关闭');
    expect(props.onClose).toHaveBeenCalledOnce();
    expect(api.cancel).not.toHaveBeenCalled();
    await update({ isOpen: false });
    await act(async () => {
      queried.resolve(operation('completed'));
    });
    expect(props.onInstalled).not.toHaveBeenCalled();
    expect(renderer.toJSON()).toBeNull();
  });

  it.each(['awaiting_restart', 'recovery_required'] as const)(
    '%s 保持未完成，不显示绿色结果',
    async (status) => {
      api.getOperation.mockResolvedValue(operation(status));
      await mount();
      await upload();
      await click('确认安装');
      expect(renderer.root.findByProps({ 'data-import-state': 'attention' })).toBeDefined();
      expect(props.onInstalled).not.toHaveBeenCalled();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1500);
      });
      expect(api.getOperation).toHaveBeenCalledTimes(2);
    }
  );

  it.each(['failed', 'license_unverified', 'license_error'] as const)(
    '%s 收口但不报告成功',
    async (status) => {
      api.getOperation.mockResolvedValue(operation(status));
      await mount();
      await upload();
      await click('确认安装');
      expect(props.onInstalled).not.toHaveBeenCalled();
      expect(renderer.root.findAllByProps({ 'data-import-state': 'success' })).toHaveLength(0);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(6000);
      });
      expect(api.getOperation).toHaveBeenCalledOnce();
    }
  );

  it('取消失败保留预览并显示错误', async () => {
    api.cancel.mockRejectedValue(new Error('取消服务暂不可用'));
    await mount();
    await upload();
    await click('取消导入');
    expect(props.onClose).not.toHaveBeenCalled();
    expect(text(renderer.root.findByProps({ role: 'alert' }))).toContain('取消服务暂不可用');
    expect(button('确认安装').props.disabled).toBe(false);
  });

  it('状态查询失败只重试相同任务，不重复安装', async () => {
    api.getOperation
      .mockRejectedValueOnce(new Error('查询暂不可用'))
      .mockResolvedValueOnce(operation('completed_disabled'));
    await mount();
    await upload();
    await click('确认安装');
    await click('重试查询');
    expect(api.install).toHaveBeenCalledOnce();
    expect(api.getOperation.mock.calls).toEqual([['operation-one'], ['operation-one']]);
    expect(props.onInstalled).toHaveBeenCalledOnce();
    expect(text(renderer.root)).toContain('插件保持停用');
  });
});
