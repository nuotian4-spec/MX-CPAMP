import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PluginResourcePage } from './PluginResourcePage';

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  bridge: vi.fn(),
  manual: vi.fn(),
  t: (key: string) => key,
  params: { pluginId: 'cpa-bps', menuIndex: '0' },
}));
vi.mock('react-router-dom', () => ({
  useParams: () => mocks.params,
  Link: ({ to, children }: { to: string; children: string }) => <a href={to}>{children}</a>,
}));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: mocks.t }) }));
vi.mock('@/hooks/useHeaderRefresh', () => ({ useHeaderRefresh: () => undefined }));
vi.mock('@/services/api', () => ({ pluginsApi: { list: mocks.list } }));
vi.mock('@/stores', () => ({
  useAuthStore: (select: (state: Record<string, unknown>) => unknown) =>
    select({
      apiBase: 'https://panel.invalid',
      managementKey: 'TEST_ONLY',
      connectionStatus: 'connected',
      supportsPlugin: true,
    }),
  useThemeStore: (select: (state: Record<string, unknown>) => unknown) =>
    select({ resolvedTheme: 'light' }),
}));
vi.mock('./pluginHostStyle', () => ({ createPluginHostStyleBridge: mocks.bridge }));
vi.mock('./bpsManualMenu', () => ({ bindBpsManualMenu: mocks.manual }));

let renderer: ReactTestRenderer | undefined;
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', { addEventListener: vi.fn(), removeEventListener: vi.fn() });
  mocks.params = { pluginId: 'cpa-bps', menuIndex: '0' };
  mocks.list.mockResolvedValue({
    pluginsEnabled: true,
    plugins: [
      {
        id: 'cpa-bps',
        effectiveEnabled: true,
        logo: '',
        metadata: null,
        menus: [
          { path: '/v0/resource/plugins/cpa-bps/index.html', menu: 'BPS 授权', description: '' },
          { path: '/v0/resource/plugins/cpa-bps/other.html', menu: '其他', description: '' },
        ],
      },
    ],
  });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer?.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
});
describe('BPS 插件资源页', () => {
  it('打开原生授权页面，并将手动 OAuth 保留为独立链接', async () => {
    const disconnect = vi.fn();
    mocks.manual.mockReturnValue({ autoReady: true, manualReady: true, disconnect });
    const frame = { inert: true };
    await act(async () => {
      renderer = create(<PluginResourcePage />, {
        createNodeMock: (element) => element.type === 'iframe' ? frame : null,
      });
    });
    const iframe = renderer!.root.findByType('iframe');
    expect(iframe.props.src).toBe('https://panel.invalid/v0/resource/plugins/cpa-bps/index.html');
    expect(iframe.props.title).toBe('BPS 授权');
    expect(iframe.props.sandbox).toBe('allow-same-origin');
    expect(iframe.props.inert).toBe(true);
    expect(iframe.props.allow).toBeUndefined();
    await act(async () => iframe.props.onLoad());
    expect(mocks.manual).toHaveBeenCalledWith(frame, iframe.props.src, {
      apiBase: 'https://panel.invalid', managementKey: 'TEST_ONLY',
    });
    expect(renderer!.root.findAllByType('span').flatMap(el => el.children)).toEqual([
      'plugin_resource.bps_auto_live', 'plugin_resource.bps_interactive',
    ]);
    expect(renderer!.root.findByType('a').props.href).toBe('/oauth?provider=cpa-bps');
    await act(async () => renderer!.unmount()); renderer = undefined;
    expect(disconnect).toHaveBeenCalledOnce();
  });
  it('手动区未识别时保留只读原生预览和面板手动入口', async () => {
    mocks.manual.mockReturnValue(null);
    await act(async () => {
      renderer = create(<PluginResourcePage />, {
        createNodeMock: (element) => element.type === 'iframe' ? {} : null,
      });
    });
    const iframe = renderer!.root.findByType('iframe');
    await act(async () => iframe.props.onLoad());
    expect(iframe.props.hidden).toBeFalsy();
    expect(iframe.props.inert).toBe(true);
    expect(renderer!.root.findAllByType('span').flatMap(el => el.children)).toEqual([
      'plugin_resource.bps_auto_unavailable', 'plugin_resource.bps_preview',
    ]);
    expect(renderer!.root.findByType('a').props.href).toBe('/oauth?provider=cpa-bps');
  });
  it('同插件其他资源继续显示iframe', async () => {
    mocks.params = { pluginId: 'cpa-bps', menuIndex: '1' };
    await act(async () => {
      renderer = create(<PluginResourcePage />);
    });
    expect(renderer!.root.findByType('iframe').props.src).toBe(
      'https://panel.invalid/v0/resource/plugins/cpa-bps/other.html'
    );
    expect(renderer!.root.findByType('iframe').props.sandbox).toBeUndefined();
    expect(renderer!.root.findAllByType('a')).toHaveLength(0);
  });
});
