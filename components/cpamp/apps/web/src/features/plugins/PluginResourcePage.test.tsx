import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PluginResourcePage } from './PluginResourcePage';

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  bridge: vi.fn(),
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

let renderer: ReactTestRenderer | undefined;
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', { addEventListener: vi.fn(), removeEventListener: vi.fn() });
  mocks.params = { pluginId: 'cpa-bps', menuIndex: '0' };
  mocks.list.mockResolvedValue({
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
    const input = { setAttribute: vi.fn() };
    const docs = { querySelectorAll: (s: string) => s.startsWith('input') ? [input] : [] };
    const frame = { contentDocument: docs, inert: true };
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
    iframe.props.onLoad();
    expect(input.setAttribute).toHaveBeenCalledWith('disabled', '');
    expect(frame.inert).toBe(false);
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
