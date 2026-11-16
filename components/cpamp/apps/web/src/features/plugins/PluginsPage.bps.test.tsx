import type { ReactNode } from 'react';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PluginsPage } from './PluginsPage';
import { Button } from '@/components/ui/Button';

const mocks = vi.hoisted(() => ({
  auth: {
    apiBase: 'http://first.invalid',
    managementKey: 'synthetic-first',
    connectionStatus: 'connected',
    supportsPlugin: true,
  },
  list: vi.fn(),
  getConfig: vi.fn(),
  patchConfig: vi.fn(),
  notification: vi.fn(),
  clearCache: vi.fn(),
  noop: vi.fn(),
  t: (key: string) => key,
}));
vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => {} },
  useTranslation: () => ({ t: mocks.t }),
}));
vi.mock('react-router-dom', () => ({
  useNavigate: () => mocks.noop,
  useSearchParams: () => [new URLSearchParams(), mocks.noop],
}));
vi.mock('@/stores', () => ({
  useAuthStore: (select: (state: typeof mocks.auth) => unknown) => select(mocks.auth),
  useConfigStore: (select: (state: object) => unknown) => select({ clearCache: mocks.clearCache }),
  useNotificationStore: (select: (state: object) => unknown) =>
    select({ showNotification: mocks.notification, showConfirmation: mocks.noop }),
}));
vi.mock('@/hooks/useHeaderRefresh', () => ({ useHeaderRefresh: () => {} }));
vi.mock('@/hooks/usePanelFeatureAvailability', () => ({
  usePanelFeatureAvailability: () => ({ managerServiceBase: '' }),
}));
vi.mock('@/services/api', () => ({
  pluginsApi: { list: mocks.list, getConfig: mocks.getConfig, patchConfig: mocks.patchConfig },
  pluginStoreApi: {},
}));
vi.mock('@/services/api/pluginImports', () => ({
  createPluginImportApi: () => ({ getLatestOperation: async () => ({ operation: null }) }),
}));
vi.mock('./PluginStorePage', () => ({ PluginStorePage: () => null }));
vi.mock('@/components/ui/Drawer', () => ({
  Drawer: ({
    open,
    children,
    footer,
  }: {
    open: boolean;
    children: ReactNode;
    footer: ReactNode;
  }) =>
    open ? (
      <section data-config-drawer>
        {children}
        {footer}
      </section>
    ) : null,
}));

let renderer: ReactTestRenderer | undefined;
const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((ok, fail) => {
    resolve = ok;
    reject = fail;
  });
  return { promise, resolve, reject };
};
const plugin = () => ({
  id: 'cpa-bps',
  path: 'plugins/bps.so',
  configured: true,
  registered: false,
  enabled: true,
  effectiveEnabled: false,
  supportsOAuth: false,
  logo: '',
  metadata: null,
  menus: [],
  configFields: [{ name: 'session_proxy_pool', type: 'string', enumValues: [], description: '' }],
});
const button = (label: string) =>
  renderer!.root
    .findAllByType(Button)
    .find((item) => item.props.children === label || item.props['aria-label'] === label)!;
const mount = async () => {
  await act(async () => {
    renderer = create(<PluginsPage />);
  });
};
const switchConnection = async () => {
  mocks.auth.apiBase = 'http://second.invalid';
  mocks.auth.managementKey = 'synthetic-second';
  await act(async () => {
    renderer!.update(<PluginsPage />);
  });
};
beforeEach(() => {
  vi.clearAllMocks();
  mocks.auth = {
    apiBase: 'http://first.invalid',
    managementKey: 'synthetic-first',
    connectionStatus: 'connected',
    supportsPlugin: true,
  };
  mocks.list.mockResolvedValue({
    pluginsEnabled: true,
    pluginsDir: 'plugins',
    plugins: [plugin()],
  });
  mocks.getConfig.mockResolvedValue({
    enabled: true,
    session_proxy_pool: 'http://old.invalid:80',
    retained: { nested: true },
  });
  mocks.patchConfig.mockResolvedValue({});
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', {
    location: { search: '' },
    dispatchEvent: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer!.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
});

describe('BPS 插件页配置 continuation', () => {
  it('多行 string 控件接正式表单，保存仅 touched 且固定发起连接', async () => {
    await mount();
    await act(async () => {
      await button('plugin_management.edit_config').props.onClick();
    });
    expect(mocks.getConfig).toHaveBeenCalledWith('cpa-bps', {
      apiBase: 'http://first.invalid',
      managementKey: 'synthetic-first',
    });
    expect(renderer!.root.findByType('textarea').props.rows).toBe(10);
    const value = 'http://one.invalid:80\nsocks5://two.invalid:90\n';
    await act(async () => {
      renderer!.root.findByType('textarea').props.onChange({ target: { value } });
    });
    await act(async () => {
      await button('common.save').props.onClick();
    });
    expect(mocks.patchConfig).toHaveBeenCalledWith(
      'cpa-bps',
      { session_proxy_pool: value },
      { apiBase: 'http://first.invalid', managementKey: 'synthetic-first' }
    );
    expect(mocks.notification).toHaveBeenCalledWith('plugin_management.save_success', 'success');
  });
  it('切换连接后迟到配置读取错误不通知新页面', async () => {
    const pending = deferred<unknown>();
    mocks.getConfig.mockReturnValueOnce(pending.promise);
    await mount();
    let request!: Promise<void>;
    await act(async () => {
      request = button('plugin_management.edit_config').props.onClick();
    });
    await switchConnection();
    await act(async () => {
      pending.reject(new Error('synthetic-private-old-error'));
      await request;
    });
    expect(mocks.notification).not.toHaveBeenCalled();
    expect(renderer!.root.findAllByType('textarea')).toHaveLength(0);
  });
  it('切换连接后迟到 PATCH 失败不通知新页面', async () => {
    const pending = deferred<unknown>();
    mocks.patchConfig.mockReturnValueOnce(pending.promise);
    await mount();
    await act(async () => {
      await button('plugin_management.edit_config').props.onClick();
    });
    let request!: Promise<void>;
    await act(async () => {
      request = button('common.save').props.onClick();
    });
    await switchConnection();
    await act(async () => {
      pending.reject(new Error('synthetic-private-old-error'));
      await request;
    });
    expect(mocks.notification).not.toHaveBeenCalled();
  });
  it('PATCH 完成后列表刷新跨连接迟到也不通知旧成功', async () => {
    await mount();
    await act(async () => {
      await button('plugin_management.edit_config').props.onClick();
    });
    const pending = deferred<unknown>();
    mocks.list.mockReturnValueOnce(pending.promise);
    let request!: Promise<void>;
    await act(async () => {
      request = button('common.save').props.onClick();
    });
    await switchConnection();
    await act(async () => {
      pending.resolve({ pluginsEnabled: true, pluginsDir: 'plugins', plugins: [plugin()] });
      await request;
    });
    expect(mocks.notification).not.toHaveBeenCalled();
  });
  it('同连接 BPS 保存失败不把代理认证内容放进通知', async () => {
    mocks.patchConfig.mockRejectedValueOnce(
      new Error('http://synthetic-user:synthetic-private@proxy.invalid:80')
    );
    await mount();
    await act(async () => {
      await button('plugin_management.edit_config').props.onClick();
    });
    await act(async () => {
      await button('common.save').props.onClick();
    });
    expect(mocks.notification).toHaveBeenCalledWith('plugin_management.save_failed', 'error');
    expect(JSON.stringify(mocks.notification.mock.calls)).not.toContain('synthetic-private');
  });
});
