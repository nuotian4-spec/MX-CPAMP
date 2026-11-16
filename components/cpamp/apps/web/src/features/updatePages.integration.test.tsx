import axios, { type InternalAxiosRequestConfig } from 'axios';
import type { ReactElement, ReactNode } from 'react';
import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PluginsPage } from './plugins/PluginsPage';
import { SystemPage } from './system/SystemPage';
import type { UpdateOperation } from '@/types/pluginImport';

const mocks = vi.hoisted(() => ({
  auth: {
    apiBase: 'https://cpa-one.test',
    managementKey: 'test-key-one',
    connectionStatus: 'connected',
    supportsPlugin: true,
    isAuthenticated: true,
  },
  managerBase: 'https://manager-one.test',
  demo: false,
  params: new URLSearchParams(),
  t: (key: string) => key,
  list: vi.fn(),
  clearCache: vi.fn(),
  fetchConfig: vi.fn(),
  fetchModels: vi.fn(),
  noop: vi.fn(),
  storageWrite: vi.fn(),
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mocks.t, i18n: { language: 'zh' } }),
}));
vi.mock('react-router-dom', () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useNavigate: () => mocks.noop,
  useSearchParams: () => [mocks.params, mocks.noop],
}));
vi.mock('@/stores', () => ({
  useAuthStore: (selector?: (state: typeof mocks.auth) => unknown) =>
    selector ? selector(mocks.auth) : mocks.auth,
  useConfigStore: (selector: (state: object) => unknown) =>
    selector({
      config: { apiKeys: ['model-test-key'] },
      fetchConfig: mocks.fetchConfig,
      clearCache: mocks.clearCache,
    }),
  useModelsStore: (selector: (state: object) => unknown) =>
    selector({
      models: [],
      loading: false,
      error: null,
      cache: null,
      fetchModels: mocks.fetchModels,
    }),
  useThemeStore: (selector: (state: object) => unknown) => selector({ resolvedTheme: 'light' }),
  useNotificationStore: (selector?: (state: object) => unknown) => {
    const state = { showNotification: mocks.noop, showConfirmation: mocks.noop };
    return selector ? selector(state) : state;
  },
}));
vi.mock('@/services/api', () => ({ pluginsApi: { list: mocks.list }, pluginStoreApi: {} }));
vi.mock('@/services/api/apiKeys', () => ({ apiKeysApi: { list: mocks.noop } }));
vi.mock('@/services/api/usageService', () => ({ usageServiceApi: { getStatus: mocks.noop } }));
vi.mock('@/hooks/useHeaderRefresh', () => ({ useHeaderRefresh: () => {} }));
vi.mock('@/hooks/usePanelFeatureAvailability', () => ({
  usePanelFeatureAvailability: () => ({
    checking: false,
    managerServiceBase: mocks.managerBase,
    managerServiceAvailable: false,
  }),
}));
vi.mock('@/features/demo/demoMode', () => ({ isDemoMode: () => mocks.demo }));
vi.mock('./plugins/PluginStorePage', () => ({ PluginStorePage: () => null }));
vi.mock('./system/ManagerUpdates', () => ({ useManagerUpdates: () => ({ available: false }) }));
vi.mock('./system/components/BPSSettingsCard', () => ({ BPSSettingsCard: () => null }));
vi.mock('./system/components/DatabaseStatusCard', () => ({ DatabaseStatusCard: () => null }));
vi.mock('@/components/ui/Drawer', () => ({ Drawer: () => null }));
vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ open, children, footer }: { open: boolean; children: ReactNode; footer: ReactNode }) =>
    open ? (
      <div role="dialog">
        {children}
        {footer}
      </div>
    ) : null,
}));

const pluginPreview = {
  id: 'import-fixed',
  plugin_id: 'synthetic-plugin',
  version: '2.0.0',
  current_version: '1.0.0',
  platform: 'linux-amd64',
  library_path: 'synthetic/plugin.so',
  currently_enabled: false,
  compatibility: 'verified',
  installable: true,
  license_required: false,
  license_status: 'not_required',
  blockers: [],
};
const coreDiscovery = {
  site_id: 'site-one',
  registered: true,
  current_version: '1.0.0',
  official_version: '9.0.0',
  site_version: '2.0.0',
  releases: [{ id: 'site-release', version: '2.0.0', notes: '本站发布' }],
  blockers: [],
};
const corePreview = {
  id: 'core-fixed',
  site_id: 'site-one',
  version: '2.0.0',
  current_version: '1.0.0',
  maintenance: ['维护期间备份当前核心'],
  notes: '本站固定候选',
  installable: true,
  blockers: [],
};
const operation = (status: UpdateOperation['status']): UpdateOperation => ({
  id: 'operation-one',
  type: 'plugin',
  target_version: '2.0.0',
  status,
  stage: status,
  active_version: status === 'completed' ? '2.0.0' : '1.0.0',
  registered: true,
  license_status: 'not_required',
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
const originalAdapter = axios.defaults.adapter;
let renderer: ReactTestRenderer;
let requests: InternalAxiosRequestConfig[];
let route: (request: InternalAxiosRequestConfig) => unknown | Promise<unknown>;
const defaultRoute = (request: InternalAxiosRequestConfig): unknown => {
  const path = new URL(request.url!).pathname;
  if (path.endsWith('/operations/latest')) return { operation: null };
  if (path.endsWith('/plugins/imports') && request.method === 'post') return pluginPreview;
  if (path.endsWith('/install')) return operation('queued');
  if (path.includes('/updates/operations/')) return operation('installing');
  if (path.endsWith('/updates/core')) return coreDiscovery;
  if (path.endsWith('/updates/core/previews')) return corePreview;
  if (request.method === 'delete') return undefined;
  throw new Error(`测试未登记的请求：${request.method} ${path}`);
};
const button = (label: string) =>
  renderer.root.findAllByType('button').find((node) => text(node) === label)!;
const click = async (label: string) => {
  await act(async () => {
    button(label).props.onClick();
  });
};
const mount = async (page: ReactElement) => {
  await act(async () => {
    renderer = create(page);
  });
};
const rerender = async (page: ReactElement) => {
  await act(async () => {
    renderer.update(page);
  });
};
const upload = async () => {
  await click('上传 ZIP');
  await act(async () => {
    renderer.root
      .findByProps({ 'aria-label': '选择 ZIP 文件' })
      .props.onChange({
        target: { files: [new File(['synthetic zip'], 'plugin.zip', { type: 'application/zip' })] },
      });
  });
  await click('上传并预览');
};
const operationRequests = () =>
  requests.filter((request) => request.url?.endsWith('/updates/operations/operation-one'));

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', {
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    dispatchEvent: mocks.noop,
    addEventListener: mocks.noop,
    removeEventListener: mocks.noop,
    location: { search: '' },
  });
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: mocks.storageWrite,
    removeItem: mocks.noop,
  });
  Object.assign(mocks.auth, {
    apiBase: 'https://cpa-one.test',
    managementKey: 'test-key-one',
    connectionStatus: 'connected',
    supportsPlugin: true,
    isAuthenticated: true,
  });
  mocks.managerBase = 'https://manager-one.test';
  mocks.demo = false;
  mocks.params = new URLSearchParams();
  mocks.list
    .mockReset()
    .mockResolvedValue({ plugins: [], pluginsEnabled: true, pluginsDir: 'plugins' });
  mocks.clearCache.mockClear();
  mocks.storageWrite.mockClear();
  mocks.fetchConfig.mockResolvedValue({ apiKeys: ['model-test-key'] });
  mocks.fetchModels.mockResolvedValue([]);
  requests = [];
  route = defaultRoute;
  axios.defaults.adapter = async (config) => {
    requests.push(config);
    return { config, data: await route(config), status: 200, statusText: 'OK', headers: {} };
  };
});
afterEach(async () => {
  if (renderer)
    await act(async () => {
      renderer.unmount();
    });
  axios.defaults.adapter = originalAdapter;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('原页面更新接线联测', () => {
  it('插件页实际挂载导入弹窗并走 raw ZIP 与固定候选安装', async () => {
    route = (request) =>
      request.url?.endsWith('/updates/operations/operation-one')
        ? operation('completed')
        : defaultRoute(request);
    await mount(<PluginsPage />);
    await upload();
    expect(text(renderer.root)).toContain('synthetic-plugin');
    await click('确认安装');
    const uploaded = requests.find((request) => request.url?.endsWith('/plugins/imports'))!;
    expect(uploaded.data).toBeInstanceOf(File);
    expect(uploaded.headers.get('Content-Type')).toBe('application/zip');
    const installed = requests.find((request) =>
      request.url?.endsWith('/plugins/imports/import-fixed/install')
    )!;
    expect(JSON.parse(installed.data)).toEqual({
      confirm: true,
      confirm_unverified: false,
      confirm_license_network: false,
    });
    expect(renderer.root.findByProps({ 'data-import-state': 'success' })).toBeDefined();
    expect(mocks.clearCache).toHaveBeenCalledOnce();
    expect(mocks.list).toHaveBeenLastCalledWith({
      apiBase: 'https://cpa-one.test',
      managementKey: 'test-key-one',
    });
    expect(
      requests.every((request) => request.url?.startsWith('https://cpa-one.test/v8/management/'))
    ).toBe(true);
    expect(mocks.storageWrite).not.toHaveBeenCalled();
  });

  it('插件页断线立即关闭导入会话，迟到完成不显示成功', async () => {
    const pending = deferred<UpdateOperation>();
    route = (request) =>
      request.url?.endsWith('/updates/operations/operation-one')
        ? pending.promise
        : defaultRoute(request);
    await mount(<PluginsPage />);
    await upload();
    await click('确认安装');
    mocks.auth.connectionStatus = 'disconnected';
    await rerender(<PluginsPage />);
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(0);
    await act(async () => {
      pending.resolve(operation('completed'));
    });
    expect(renderer.root.findAllByProps({ 'data-import-state': 'success' })).toHaveLength(0);
    expect(mocks.clearCache).not.toHaveBeenCalled();
    mocks.auth.connectionStatus = 'connected';
    await rerender(<PluginsPage />);
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(0);
  });

  it('插件页最新任务快照声称完成仍先查询原编号', async () => {
    const pending = deferred<UpdateOperation>();
    route = (request) =>
      request.url?.includes('/operations/latest')
        ? { operation: operation('completed') }
        : request.url?.endsWith('/updates/operations/operation-one')
          ? pending.promise
          : defaultRoute(request);
    await mount(<PluginsPage />);
    expect(operationRequests()).toHaveLength(1);
    expect(renderer.root.findByProps({ 'data-plugin-maintenance-state': 'pending' })).toBeDefined();
    await act(async () => {
      pending.resolve(operation('completed'));
    });
    expect(renderer.root.findByProps({ 'data-plugin-maintenance-state': 'success' })).toBeDefined();
    mocks.auth.connectionStatus = 'disconnected';
    await rerender(<PluginsPage />);
    expect(
      renderer.root.findAllByProps({ 'data-plugin-maintenance-state': 'success' })
    ).toHaveLength(0);
  });

  it.each(['awaiting_restart', 'recovery_required'] as const)(
    '插件页恢复 %s 任务继续查原编号',
    async (status) => {
      route = (request) =>
        request.url?.includes('/operations/latest')
          ? { operation: operation(status) }
          : request.url?.endsWith('/updates/operations/operation-one')
            ? operation(operationRequests().length === 1 ? status : 'completed')
            : defaultRoute(request);
      await mount(<PluginsPage />);
      expect(
        renderer.root.findByProps({ 'data-plugin-maintenance-state': 'attention' })
      ).toBeDefined();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2000);
      });
      expect(operationRequests()).toHaveLength(2);
      expect(
        renderer.root.findByProps({ 'data-plugin-maintenance-state': 'success' })
      ).toBeDefined();
    }
  );

  it('插件页任务查询断线保留未确认，不把旧快照当成功', async () => {
    route = (request) => {
      if (request.url?.includes('/operations/latest')) return { operation: operation('completed') };
      if (request.url?.endsWith('/updates/operations/operation-one'))
        throw new Error('任务连接中断');
      return defaultRoute(request);
    };
    await mount(<PluginsPage />);
    expect(
      renderer.root.findAllByProps({ 'data-plugin-maintenance-state': 'success' })
    ).toHaveLength(0);
    expect(text(renderer.root)).toContain('任务连接中断');
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000);
    });
    expect(operationRequests()).toHaveLength(2);
  });

  it.each(['apiBase', 'managementKey', 'managerBase'] as const)(
    '插件页 %s 变化清理旧导入会话与迟到预览',
    async (field) => {
      const pending = deferred<typeof pluginPreview>();
      route = (request) =>
        request.url?.endsWith('/plugins/imports') ? pending.promise : defaultRoute(request);
      await mount(<PluginsPage />);
      await upload();
      if (field === 'managerBase') mocks.managerBase = 'https://manager-two.test';
      else if (field === 'apiBase') mocks.auth.apiBase = 'https://cpa-two.test';
      else mocks.auth.managementKey = 'test-key-two';
      await rerender(<PluginsPage />);
      await act(async () => {
        pending.resolve(pluginPreview);
      });
      expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(0);
      expect(text(renderer.root)).not.toContain('synthetic-plugin');
      route = defaultRoute;
      await upload();
      const latestUpload = requests
        .filter((request) => request.url?.endsWith('/plugins/imports'))
        .slice(-1)[0]!;
      expect(latestUpload.url).toBe(`${mocks.auth.apiBase}/v8/management/plugins/imports`);
      expect(latestUpload.headers.get('Authorization')).toBe(`Bearer ${mocks.auth.managementKey}`);
    }
  );

  it('系统原页接入本站核心更新，即使 Manager 不可用仍只请求 CPA', async () => {
    route = (request) =>
      request.url?.endsWith('/updates/operations/operation-one')
        ? { ...operation('completed'), type: 'core' }
        : defaultRoute(request);
    await mount(<SystemPage />);
    expect(text(renderer.root)).toContain('核心更新');
    await act(async () => {
      renderer.root
        .findByProps({ 'aria-label': '本站发布版本' })
        .props.onChange({ target: { value: 'site-release' } });
    });
    await click('生成更新预览');
    await act(async () => {
      renderer.root
        .findByProps({ 'aria-label': '确认维护备份影响' })
        .props.onChange({ target: { checked: true } });
    });
    await click('确认更新此候选');
    expect(renderer.root.findByProps({ 'data-core-update-state': 'success' })).toBeDefined();
    const installed = requests.find((request) =>
      request.url?.endsWith('/updates/core/previews/core-fixed/install')
    )!;
    expect(JSON.parse(installed.data)).toEqual({ confirm: true });
    expect(
      requests.every((request) => request.url?.startsWith('https://cpa-one.test/v8/management/'))
    ).toBe(true);
    expect(mocks.storageWrite).not.toHaveBeenCalled();
  });

  it('系统原页断线禁用核心查询，重连重新发现而非沿用成功', async () => {
    mocks.auth.connectionStatus = 'disconnected';
    await mount(<SystemPage />);
    expect(requests).toHaveLength(0);
    mocks.auth.connectionStatus = 'connected';
    await rerender(<SystemPage />);
    expect(requests.filter((request) => request.url?.endsWith('/updates/core'))).toHaveLength(1);
    expect(renderer.root.findAllByProps({ 'data-core-update-state': 'success' })).toHaveLength(0);
  });
});
